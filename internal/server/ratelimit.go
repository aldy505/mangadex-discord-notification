package server

import (
	"sync"
	"time"
)

// loginLimiter throttles failed password attempts per client and account.
type loginLimiter struct {
	mu          sync.Mutex
	maxFailures int
	window      time.Duration
	entries     map[string]*attemptRecord
}

type attemptRecord struct {
	failures     int
	firstFailure time.Time
	blockedUntil time.Time
}

func newLoginLimiter(maxFailures int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		maxFailures: maxFailures,
		window:      window,
		entries:     make(map[string]*attemptRecord),
	}
}

// blocked reports whether the key is currently locked out.
func (l *loginLimiter) blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	record, ok := l.entries[key]
	if !ok {
		return false, 0
	}

	now := time.Now()
	if record.blockedUntil.After(now) {
		return true, record.blockedUntil.Sub(now)
	}
	if now.Sub(record.firstFailure) > l.window {
		delete(l.entries, key)
	}

	return false, 0
}

func (l *loginLimiter) failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	record, ok := l.entries[key]
	if !ok {
		record = &attemptRecord{firstFailure: now}
		l.entries[key] = record
	}
	if now.Sub(record.firstFailure) > l.window {
		record.failures = 0
		record.firstFailure = now
	}

	record.failures++
	if record.failures >= l.maxFailures {
		record.blockedUntil = now.Add(l.window)
	}

	l.pruneLocked(now)
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.entries, key)
}

func (l *loginLimiter) pruneLocked(now time.Time) {
	if len(l.entries) < 1024 {
		return
	}

	for key, record := range l.entries {
		if now.Sub(record.firstFailure) > 2*l.window && record.blockedUntil.Before(now) {
			delete(l.entries, key)
		}
	}
}
