package indexers

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

// Request pacing profile for one indexer
type rateSpec struct {
	perSec rate.Limit
	burst  int
}

// Modrinth documents 300 per minute, curseforge publishes nothing
var indexerRates = map[string]rateSpec{
	"fuego":    {perSec: 4, burst: 8},
	"modrinth": {perSec: 5, burst: 10},
}

var defaultRate = rateSpec{perSec: 5, burst: 5}

// Retry tuning, vars so tests can shrink them
var (
	maxAttempts      = 4
	baseBackoff      = 500 * time.Millisecond
	maxBackoff       = 15 * time.Second
	minRateCooldown  = 2 * time.Second
	maxRetryAfter    = 2 * time.Minute
	maxResponseBytes = int64(64 << 20)
	maxCachedBody    = 2 << 20
	maxEtagEntries   = 256
)

// Pacing, dedupe, and validator state for one indexer credential
type sharedState struct {
	limiter *rate.Limiter
	flights singleflight.Group

	mu            sync.Mutex
	cooldownUntil time.Time

	etagMu sync.Mutex
	etags  map[string]etagEntry
}

type etagEntry struct {
	etag string
	body []byte
}

var (
	statesMu sync.Mutex
	states   = map[string]*sharedState{}
)

// Returns process wide shared state for an indexer credential
func stateFor(indexer, credential string) *sharedState {
	statesMu.Lock()
	defer statesMu.Unlock()
	key := indexer + "\x00" + credential
	if s, ok := states[key]; ok {
		return s
	}
	spec, ok := indexerRates[indexer]
	if !ok {
		spec = defaultRate
	}
	s := &sharedState{
		limiter: rate.NewLimiter(spec.perSec, spec.burst),
		etags:   map[string]etagEntry{},
	}
	states[key] = s
	return s
}

// Blocks until any active rate limit cooldown passes
func (s *sharedState) waitCooldown(ctx context.Context) error {
	for {
		s.mu.Lock()
		wait := time.Until(s.cooldownUntil)
		s.mu.Unlock()
		if wait <= 0 {
			return nil
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Pauses every request on this credential after a 429
func (s *sharedState) startCooldown(d time.Duration) {
	if d <= 0 {
		return
	}
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	until := time.Now().Add(d)
	s.mu.Lock()
	if until.After(s.cooldownUntil) {
		s.cooldownUntil = until
	}
	s.mu.Unlock()
}

// Returns remembered validator and body for url
func (s *sharedState) cachedETag(url string) (string, []byte, bool) {
	s.etagMu.Lock()
	defer s.etagMu.Unlock()
	e, ok := s.etags[url]
	return e.etag, e.body, ok
}

// Remembers validator and body for url, bounded
func (s *sharedState) storeETag(url, etag string, body []byte) {
	if etag == "" || len(body) > maxCachedBody {
		return
	}
	s.etagMu.Lock()
	defer s.etagMu.Unlock()
	if _, exists := s.etags[url]; !exists && len(s.etags) >= maxEtagEntries {
		for k := range s.etags {
			delete(s.etags, k)
			break
		}
	}
	s.etags[url] = etagEntry{etag: etag, body: body}
}

// Parses Retry-After as seconds or an http date
func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

// Jittered exponential delay for the given attempt
func backoffDelay(attempt int) time.Duration {
	d := baseBackoff << attempt
	if d > maxBackoff {
		d = maxBackoff
	}
	return d + rand.N(d/2+1)
}

// Sleeps for d unless ctx ends first
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
