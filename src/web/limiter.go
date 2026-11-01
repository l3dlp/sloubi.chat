package web

import (
	"sync"
	"time"
)

// Limiter : un seau à jetons par clé (IP), plus un ban temporaire pour qui
// insiste après avoir été refusé trop souvent (bots qui énumèrent les noms).
type Limiter struct {
	rate    float64 // jetons par seconde
	burst   float64
	strikes int // refus avant ban (0 = jamais de ban)
	banFor  time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens   float64
	last     time.Time
	refused  int
	banUntil time.Time
}

func NewLimiter(perSecond float64, burst int, strikes int, banFor time.Duration) *Limiter {
	l := &Limiter{rate: perSecond, burst: float64(burst), strikes: strikes, banFor: banFor, buckets: map[string]*bucket{}}
	go l.gc()
	return l
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: t}
		l.buckets[key] = b
	}
	if t.Before(b.banUntil) {
		return false
	}
	b.tokens += t.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = t
	if b.tokens >= 1 {
		b.tokens--
		b.refused = 0
		return true
	}
	b.refused++
	if l.strikes > 0 && b.refused >= l.strikes {
		b.banUntil = t.Add(l.banFor)
		b.refused = 0
	}
	return false
}

// gc oublie les seaux pleins et non bannis.
func (l *Limiter) gc() {
	for range time.Tick(time.Minute) {
		l.mu.Lock()
		t := time.Now()
		for k, b := range l.buckets {
			full := b.tokens+t.Sub(b.last).Seconds()*l.rate >= l.burst
			if full && t.After(b.banUntil) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
