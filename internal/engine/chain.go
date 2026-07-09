package engine

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Chain is the AUTO fallback router. It tries engines in configured order and
// fails over to the next one when the current engine is known-down or fails
// before producing any token. Once tokens have streamed, a mid-stream error is
// surfaced rather than restarting on another engine (which would garble output);
// the common "provider not running" case fails at connect time, pre-token, and
// switches cleanly.
type Chain struct {
	engines []Engine
	ttl     time.Duration

	mu    sync.Mutex
	avail map[string]probeCache
}

type probeCache struct {
	ok bool
	at time.Time
}

// NewChain builds a chain from engines in priority order. ttl is how long a
// health verdict is cached (default 5s).
func NewChain(engines []Engine, ttl time.Duration) *Chain {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &Chain{engines: engines, ttl: ttl, avail: map[string]probeCache{}}
}

// Name reports the chain as the "auto" engine.
func (c *Chain) Name() string { return "auto" }

// Supports reports true if any member supports the mode.
func (c *Chain) Supports(m Mode) bool {
	for _, e := range c.engines {
		if e.Supports(m) {
			return true
		}
	}
	return false
}

// recentlyDown reports whether we have a fresh "down" verdict for e, so we can
// skip it without paying a probe on the hot path. Engines are otherwise tried
// optimistically (no pre-probe latency on the happy path).
func (c *Chain) recentlyDown(e Engine) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	pc, ok := c.avail[e.Name()]
	return ok && !pc.ok && time.Since(pc.at) < c.ttl
}

func (c *Chain) mark(e Engine, ok bool) {
	c.mu.Lock()
	c.avail[e.Name()] = probeCache{ok: ok, at: time.Now()}
	c.mu.Unlock()
}

func (c *Chain) candidates(m Mode) []Engine {
	var out []Engine
	for _, e := range c.engines {
		if e.Supports(m) {
			out = append(out, e)
		}
	}
	return out
}

// Translate proxies the first working engine's stream. Failover happens only
// before the first token.
func (c *Chain) Translate(ctx context.Context, req Request) (<-chan Chunk, error) {
	cands := c.candidates(req.Mode)
	if len(cands) == 0 {
		return nil, ErrNoEngineForMode
	}
	out := make(chan Chunk, 32)
	go func() {
		defer close(out)
		var lastErr error
		for _, e := range cands {
			if ctx.Err() != nil { // request abandoned (e.g. user typed more)
				out <- Chunk{Kind: ChunkError, Err: ctx.Err()}
				return
			}
			if c.recentlyDown(e) {
				lastErr = fmt.Errorf("%s: recently unavailable", e.Name())
				continue
			}
			sub, err := e.Translate(ctx, req)
			if err != nil {
				lastErr = err
				c.mark(e, false)
				continue
			}

			streamed, failed := false, false
			for ch := range sub {
				switch ch.Kind {
				case ChunkToken:
					streamed = true
					out <- ch
				case ChunkDone:
					c.mark(e, true)
					out <- ch
					return
				case ChunkError:
					lastErr = ch.Err
					failed = true
					c.mark(e, false)
				}
			}

			if failed {
				if ctx.Err() != nil { // cancelled: don't waste other engines
					out <- Chunk{Kind: ChunkError, Err: ctx.Err()}
					return
				}
				if streamed { // already showed partial output: surface, don't restart
					out <- Chunk{Kind: ChunkError, Err: lastErr}
					return
				}
				continue // clean pre-token failover
			}
		}
		if lastErr == nil {
			lastErr = ErrAllEnginesFailed
		}
		out <- Chunk{Kind: ChunkError, Err: fmt.Errorf("%w: %v", ErrAllEnginesFailed, lastErr)}
	}()
	return out, nil
}

// Detect returns the first available engine's detection, else "".
func (c *Chain) Detect(ctx context.Context, text string) (string, error) {
	for _, e := range c.engines {
		if c.recentlyDown(e) {
			continue
		}
		if code, err := e.Detect(ctx, text); err == nil && code != "" {
			return code, nil
		}
	}
	return "", nil
}

// Available reports whether any member is currently reachable.
func (c *Chain) Available(ctx context.Context) bool {
	for _, e := range c.engines {
		if e.Available(ctx) {
			c.mark(e, true)
			return true
		}
		c.mark(e, false)
	}
	return false
}
