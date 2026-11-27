package application

import "sync"

// modelPolicy is the admin's AI switch and default monthly token budget as model
// calls read them; the admin routes change it at runtime.
type modelPolicy struct {
	mu            sync.RWMutex
	enabled       bool
	monthlyTokens int64
}

func newAIPolicy(enabled bool, monthlyTokens int64) *modelPolicy {
	return &modelPolicy{enabled: enabled, monthlyTokens: monthlyTokens}
}

func (p *modelPolicy) set(enabled bool, monthlyTokens int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled, p.monthlyTokens = enabled, monthlyTokens
}

func (p *modelPolicy) get() (bool, int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.enabled, p.monthlyTokens
}
