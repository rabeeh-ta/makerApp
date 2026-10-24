// Package limit is a small in-memory rate limiter: login, signup and pairing
// attempts on the website, and AI tool calls. It resets when the process
// restarts, which is fine for slowing down guessing and runaway loops.
package limit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func New() *Limiter { return &Limiter{attempts: map[string][]time.Time{}} }

// Allow records an attempt for key and reports whether it's within max per period.
func (l *Limiter) Allow(key string, max int, per time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-per)
	recent := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cut) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= max {
		l.attempts[key] = recent
		return false
	}
	l.attempts[key] = append(recent, time.Now())
	return true
}
