package main

import (
	"regexp"
	"strings"
	"sync"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

var logPrefixPattern = regexp.MustCompile(`^(?:\[[^\]]*\] ?)+: `)

var legacyPrefixPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \[[A-Z]+\] `)

var (
	uuidPattern = regexp.MustCompile(`^UUID of player (.{1,48}?) is ([0-9a-fA-F-]{32,36})$`)

	loginPattern = regexp.MustCompile(`^(.{1,48}?) ?\[/[^\]]+\] logged in with entity id \d+`)

	disconnectPattern = regexp.MustCompile(`^(.{1,48}?) lost connection: `)

	chatPattern        = regexp.MustCompile(`^(?:\[Not Secure\] )?<([^>]{1,48})> (.*)$`)
	advancementPattern = regexp.MustCompile(`^(.{1,48}?) has (?:made the advancement|reached the goal|completed the challenge) \[(.+)\]$`)
)

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

type consoleEvents struct {
	sup *supervisor

	mu     sync.Mutex
	online map[string]bool
	uuids  map[string]string
}

func newConsoleEvents(sup *supervisor) *consoleEvents {
	return &consoleEvents{
		sup:    sup,
		online: make(map[string]bool),
		uuids:  make(map[string]string),
	}
}

func stripLogPrefix(line string) (string, bool) {
	if prefix := logPrefixPattern.FindString(line); prefix != "" {
		return line[len(prefix):], true
	}
	if prefix := legacyPrefixPattern.FindString(line); prefix != "" {
		return line[len(prefix):], true
	}
	return "", false
}

func (e *consoleEvents) handleLine(raw string) {
	line := strings.TrimRight(raw, "\r")
	if !e.sup.isReady() && readyPattern.MatchString(line) {
		e.sup.markReady(time.Since(e.sup.startedAt).Seconds())
	}

	msg, ok := stripLogPrefix(line)
	if !ok {
		return
	}

	if m := uuidPattern.FindStringSubmatch(msg); m != nil {
		e.setUUID(m[1], m[2])
		return
	}
	if m := loginPattern.FindStringSubmatch(msg); m != nil {
		e.playerChange(m[1], true)
		return
	}
	if m := disconnectPattern.FindStringSubmatch(msg); m != nil {
		e.playerChange(m[1], false)
		return
	}
	if m := chatPattern.FindStringSubmatch(msg); m != nil {
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
	if player, found := e.matchDeath(msg); found {
		e.emit(agentv1.PlayerEventType_PLAYER_EVENT_TYPE_DEATH, player, msg, -1)
	}
}

func (e *consoleEvents) matchDeath(msg string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for name := range e.online {
		if len(msg) <= len(name)+1 || !strings.HasPrefix(msg, name) || msg[len(name)] != ' ' {
			continue
		}
		rest := msg[len(name)+1:]
		for _, phrase := range deathPhrases {
			if strings.HasPrefix(rest, phrase) {
				return name, true
			}
		}
	}
	return "", false
}

func (e *consoleEvents) playerChange(player string, joined bool) {
	e.mu.Lock()
	if e.online[player] == joined {
		e.mu.Unlock()
		return
	}
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

func (e *consoleEvents) setUUID(player, uuid string) {
	if player == "" || uuid == "" {
		return
	}
	e.mu.Lock()
	e.uuids[player] = uuid
	e.mu.Unlock()
}

func (e *consoleEvents) syncRoster(players []mgmtPlayer) {
	current := make(map[string]bool, len(players))
	for _, p := range players {
		if p.Name == "" {
			continue
		}
		current[p.Name] = true
		e.setUUID(p.Name, p.ID)
	}

	var joins, leaves []string
	e.mu.Lock()
	for name := range current {
		if !e.online[name] {
			joins = append(joins, name)
		}
	}
	for name := range e.online {
		if !current[name] {
			leaves = append(leaves, name)
		}
	}
	e.mu.Unlock()

	for _, name := range joins {
		e.playerChange(name, true)
	}
	for _, name := range leaves {
		e.playerChange(name, false)
	}
}

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
