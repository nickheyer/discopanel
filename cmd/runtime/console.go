package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// Console lines below are printed by vanilla server code, so every loader
// and fork emits them unchanged on every version. Parsing them here gives
// player events with no game-side dependency at all.

// logPrefixPattern strips the log frame, e.g. `[12:34:56] [Server thread/INFO]: `
var logPrefixPattern = regexp.MustCompile(`^(?:\[[^\]]*\] ?)+: `)

var (
	uuidPattern        = regexp.MustCompile(`^UUID of player (\w{1,16}) is ([0-9a-fA-F-]{32,36})$`)
	joinPattern        = regexp.MustCompile(`^(\w{1,16})(?: \(formerly known as [^)]+\))? joined the game$`)
	leavePattern       = regexp.MustCompile(`^(\w{1,16}) left the game$`)
	chatPattern        = regexp.MustCompile(`^(?:\[Not Secure\] )?<(\w{1,16})> (.*)$`)
	advancementPattern = regexp.MustCompile(`^(\w{1,16}) has (?:made the advancement|reached the goal|completed the challenge) \[(.+)\]$`)

	// Modern lag lines quantify the debt, e.g. "Running 2354ms or 47 ticks behind"
	lagMsPattern = regexp.MustCompile(` Running (\d+)ms`)
)

// lagLinePrefix opens the overload confession every version prints at
// 2000ms of tick debt, from alpha through today.
const lagLinePrefix = "Can't keep up!"

// defaultLagDebtMs is the vanilla debt threshold behind each lag line.
const defaultLagDebtMs = 2000

// lagDebtMaxAge is how long a lag line stays usable for tick math.
const lagDebtMaxAge = 45 * time.Second

// deathPhrases are prefixes of every vanilla death message after the victim
// name (short stems subsume their longer "while fighting X" variants).
var deathPhrases = []string{
	"was slain by", "was shot by", "was fireballed by", "was pummeled by",
	"was pricked to death", "was stung to death", "was squashed by",
	"was skewered by", "was impaled", "was blown up by", "was killed",
	"was struck by lightning", "was frozen to death by", "was smashed by",
	"was poked to death by", "was squished too much", "was burned to a crisp",
	"was doomed to fall", "was obliterated by",
	"walked into a cactus", "walked into fire", "walked into the danger zone",
	"drowned", "died", "blew up", "burned to death", "starved to death",
	"froze to death", "suffocated in a wall", "withered away",
	"experienced kinetic energy", "hit the ground too hard",
	"fell from a high place", "fell off", "fell while climbing",
	"fell out of the world", "went up in flames", "went off with a bang",
	"tried to swim in lava", "discovered the floor was lava",
	"left the confines of this world",
	"didn't want to live in the same world as",
}

// consoleEvents tracks the roster and emits player events from console lines.
type consoleEvents struct {
	sup *supervisor

	mu        sync.Mutex
	online    map[string]bool
	uuids     map[string]string
	lagPrevAt time.Time
	lagLastAt time.Time
	lagLastMs float64
}

func newConsoleEvents(sup *supervisor) *consoleEvents {
	return &consoleEvents{
		sup:    sup,
		online: make(map[string]bool),
		uuids:  make(map[string]string),
	}
}

// handleLine inspects one console line for readiness and player events.
func (e *consoleEvents) handleLine(raw string) {
	line := strings.TrimRight(raw, "\r")
	if !e.sup.isReady() && readyPattern.MatchString(line) {
		e.sup.markReady(time.Since(e.sup.startedAt).Seconds())
	}

	prefix := logPrefixPattern.FindString(line)
	if prefix == "" {
		return
	}
	msg := line[len(prefix):]

	if m := uuidPattern.FindStringSubmatch(msg); m != nil {
		e.mu.Lock()
		e.uuids[m[1]] = m[2]
		e.mu.Unlock()
		return
	}
	if strings.HasPrefix(msg, lagLinePrefix) {
		debtMs := float64(defaultLagDebtMs)
		if m := lagMsPattern.FindStringSubmatch(msg); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
				debtMs = v
			}
		}
		e.mu.Lock()
		e.lagPrevAt = e.lagLastAt
		e.lagLastAt = time.Now()
		e.lagLastMs = debtMs
		e.mu.Unlock()
		return
	}
	if m := joinPattern.FindStringSubmatch(msg); m != nil {
		e.playerChange(m[1], true)
		return
	}
	if m := leavePattern.FindStringSubmatch(msg); m != nil {
		e.playerChange(m[1], false)
		return
	}
	if m := chatPattern.FindStringSubmatch(msg); m != nil {
		// Roster gate keeps tellraw and plugin noise out
		if e.isOnline(m[1]) {
			e.emit(agentv1.PlayerEventType_PLAYER_EVENT_TYPE_CHAT, m[1], m[2], -1)
		}
		return
	}
	if m := advancementPattern.FindStringSubmatch(msg); m != nil {
		if e.isOnline(m[1]) {
			e.emit(agentv1.PlayerEventType_PLAYER_EVENT_TYPE_ADVANCEMENT, m[1], m[2], -1)
		}
		return
	}
	if player, detail := matchDeath(msg); player != "" && e.isOnline(player) {
		e.emit(agentv1.PlayerEventType_PLAYER_EVENT_TYPE_DEATH, player, detail, -1)
	}
}

// matchDeath returns the victim when the line is a vanilla death message.
func matchDeath(msg string) (player, detail string) {
	name, rest, found := strings.Cut(msg, " ")
	if !found || len(name) > 16 {
		return "", ""
	}
	for _, phrase := range deathPhrases {
		if strings.HasPrefix(rest, phrase) {
			return name, msg
		}
	}
	return "", ""
}

func (e *consoleEvents) playerChange(player string, joined bool) {
	e.mu.Lock()
	if joined {
		e.online[player] = true
	} else {
		delete(e.online, player)
	}
	count := len(e.online)
	e.mu.Unlock()

	eventType := agentv1.PlayerEventType_PLAYER_EVENT_TYPE_LEAVE
	if joined {
		eventType = agentv1.PlayerEventType_PLAYER_EVENT_TYPE_JOIN
	}
	e.emit(eventType, player, "", count)
	e.sup.sendRoster()
}

func (e *consoleEvents) isOnline(player string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.online[player]
}

// lagDebt returns the latest tick debt and the interval it accrued over.
func (e *consoleEvents) lagDebt() (debtMs, intervalSec float64, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lagPrevAt.IsZero() || time.Since(e.lagLastAt) > lagDebtMaxAge {
		return 0, 0, false
	}
	interval := e.lagLastAt.Sub(e.lagPrevAt).Seconds()
	if interval < 1 {
		interval = 1
	}
	return e.lagLastMs, interval, true
}

// roster returns the current online player names.
func (e *consoleEvents) roster() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	players := make([]string, 0, len(e.online))
	for name := range e.online {
		players = append(players, name)
	}
	return players
}

func (e *consoleEvents) emit(t agentv1.PlayerEventType, player, detail string, playersOnline int) {
	e.mu.Lock()
	uuid := e.uuids[player]
	e.mu.Unlock()
	e.sup.send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_PlayerEvent{
		PlayerEvent: &agentv1.PlayerEvent{
			Type:          t,
			Player:        player,
			Uuid:          uuid,
			Detail:        detail,
			PlayersOnline: int32(playersOnline),
		},
	}})
}
