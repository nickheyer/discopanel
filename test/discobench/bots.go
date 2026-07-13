package main

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tnze/go-mc/bot"
	"github.com/Tnze/go-mc/bot/basic"
	"github.com/Tnze/go-mc/chat"
	"github.com/Tnze/go-mc/data/packetid"
	pk "github.com/Tnze/go-mc/net/packet"
)

// Wraps server list ping and returns latency
func slpPing(addr string, timeout time.Duration) ([]byte, time.Duration, error) {
	return bot.PingAndListTimeout(addr, timeout)
}

// One world age observation from observer bot
type tpsSample struct {
	At  time.Time
	Age int64
}

// Drives offline bots and records world age progression
type swarm struct {
	cfg  *Config
	addr string

	mu      sync.Mutex
	samples []tpsSample

	joined     atomic.Int64
	reconnects atomic.Int64
	failures   atomic.Int64
}

// Holds what a load phase produced
type swarmResult struct {
	TPSSamples  []tpsSample
	PeakJoined  int64
	Reconnects  int64
	JoinFailures int64
}

// Joins bots staggered, holds load, returns observations
func runSwarm(ctx context.Context, cfg *Config, addr string) swarmResult {
	s := &swarm{cfg: cfg, addr: addr}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i := range cfg.Bots {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.runBot(ctx, idx)
		}(i)
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}

	select {
	case <-ctx.Done():
	case <-time.After(cfg.LoadDuration):
	}
	cancel()
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	return swarmResult{
		TPSSamples:   s.samples,
		PeakJoined:   s.joined.Load(),
		Reconnects:   s.reconnects.Load(),
		JoinFailures: s.failures.Load(),
	}
}

// Keeps one bot connected, rejoining after disconnects
func (s *swarm) runBot(ctx context.Context, idx int) {
	first := true
	for ctx.Err() == nil {
		if !first {
			s.reconnects.Add(1)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
		first = false
		if err := s.session(ctx, idx); err != nil && ctx.Err() == nil {
			s.failures.Add(1)
		}
	}
}

// Runs one connect play disconnect cycle
func (s *swarm) session(ctx context.Context, idx int) error {
	client := bot.NewClient()
	client.Auth.Name = fmt.Sprintf("bench%03d", idx)

	b := &benchBot{
		swarm:  s,
		client: client,
		// Unique heading spreads bots over distinct chunk paths
		heading:  2 * math.Pi * float64(idx) / float64(max(s.cfg.Bots, 1)),
		observer: idx == 0,
	}
	b.player = basic.NewPlayer(client, basic.DefaultSettings, basic.EventsListener{
		GameStart:  b.onGameStart,
		Teleported: b.onTeleported,
		Death:      b.onDeath,
		Disconnect: func(reason chat.Message) error { return fmt.Errorf("kicked: %s", reason.ClearString()) },
	})
	if b.observer {
		client.Events.AddListener(bot.PacketHandler{
			ID: packetid.ClientboundSetTime,
			F:  b.onSetTime,
		})
	}
	// Movement rides game loop goroutine so writes never race
	client.Events.AddGeneric(bot.PacketHandler{F: b.maybeMove})

	if err := client.JoinServer(s.addr); err != nil {
		return err
	}
	defer client.Close()

	// Close unblocks HandleGame when the phase ends
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
		case <-done:
		}
	}()

	return client.HandleGame()
}

// State for one connected bot
type benchBot struct {
	swarm    *swarm
	client   *bot.Client
	player   *basic.Player
	observer bool
	heading  float64

	started  bool
	x, y, z  float64
	outbound bool
	lastMove time.Time
	counted  bool
}

func (b *benchBot) onGameStart() error {
	if !b.counted {
		b.counted = true
		b.swarm.joined.Add(1)
	}
	b.outbound = true
	return nil
}

// Accepts server position sync and adopts the corrected coordinates
func (b *benchBot) onTeleported(x, y, z float64, yaw, pitch float32, flags byte, teleportID int32) error {
	if flags&0x01 != 0 {
		b.x += x
	} else {
		b.x = x
	}
	if flags&0x02 != 0 {
		b.y += y
	} else {
		b.y = y
	}
	if flags&0x04 != 0 {
		b.z += z
	} else {
		b.z = z
	}
	b.started = true
	return b.player.AcceptTeleportation(pk.VarInt(teleportID))
}

func (b *benchBot) onDeath() error {
	return b.player.Respawn()
}

// Records world age against wall clock for external TPS
func (b *benchBot) onSetTime(p pk.Packet) error {
	var age, timeOfDay pk.Long
	if err := p.Scan(&age, &timeOfDay); err != nil {
		return nil
	}
	b.swarm.mu.Lock()
	b.swarm.samples = append(b.swarm.samples, tpsSample{At: time.Now(), Age: int64(age)})
	b.swarm.mu.Unlock()
	return nil
}

// Walks bot radially out and back loading fresh chunks
func (b *benchBot) maybeMove(pk.Packet) error {
	if !b.started {
		return nil
	}
	now := time.Now()
	if now.Sub(b.lastMove) < 100*time.Millisecond {
		return nil
	}
	elapsed := now.Sub(b.lastMove).Seconds()
	if b.lastMove.IsZero() || elapsed > 1 {
		elapsed = 0.1
	}
	b.lastMove = now

	step := b.swarm.cfg.WalkSpeed * elapsed
	dir := 1.0
	if !b.outbound {
		dir = -1
	}
	b.x += math.Cos(b.heading) * step * dir
	b.z += math.Sin(b.heading) * step * dir

	dist := math.Hypot(b.x, b.z)
	if b.outbound && dist >= b.swarm.cfg.WalkRadius {
		b.outbound = false
	} else if !b.outbound && dist < 8 {
		b.outbound = true
	}

	return b.client.Conn.WritePacket(pk.Marshal(
		packetid.ServerboundMovePlayerPos,
		pk.Double(b.x), pk.Double(b.y), pk.Double(b.z), pk.Boolean(false),
	))
}
