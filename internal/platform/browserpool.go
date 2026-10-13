package platform

import (
	"context"
	"sync"
	"time"
)

type Browser interface {
	Eval(context.Context, string, any) error
	Close() error
}
type Launcher func(context.Context, string, bool) (Browser, error)
type browserEntry struct {
	browser Browser
	busy    bool
	headed  bool
	last    time.Time
}
type BrowserPool struct {
	mu      sync.Mutex
	entries map[string]*browserEntry
	limit   int
	idle    time.Duration
	launch  Launcher
	closed  bool
	changed chan struct{}
}
type Lease struct {
	Browser Browser
	pool    *BrowserPool
	id      string
	once    sync.Once
}

func NewBrowserPool(limit int, idle time.Duration, launch Launcher) *BrowserPool {
	return &BrowserPool{entries: map[string]*browserEntry{}, limit: limit, idle: idle, launch: launch, changed: make(chan struct{})}
}
func (p *BrowserPool) notify() { close(p.changed); p.changed = make(chan struct{}) }
func (p *BrowserPool) Acquire(ctx context.Context, id string, headed bool) (*Lease, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, fail("BROWSER_POOL_CLOSED")
		}
		// Closing a login window must not leave a reusable "headed" lease behind.
		for cachedID, e := range p.entries {
			if !e.busy {
				if live, ok := e.browser.(interface{ Alive() bool }); ok && !live.Alive() {
					_ = e.browser.Close()
					delete(p.entries, cachedID)
					p.notify()
				}
			}
		}
		if e := p.entries[id]; e != nil {
			if !e.busy && e.headed == headed {
				e.busy = true
				p.mu.Unlock()
				return &Lease{Browser: e.browser, pool: p, id: id}, nil
			}
			if !e.busy && e.headed != headed {
				if e.headed {
					p.mu.Unlock()
					return nil, fail("LOGIN_WINDOW_OPEN")
				}
				_ = e.browser.Close()
				delete(p.entries, id)
				p.notify()
			}
		}
		if p.entries[id] == nil {
			if len(p.entries) >= p.limit {
				var victim string
				var oldest time.Time
				for k, e := range p.entries {
					if !e.busy && !e.headed && (victim == "" || e.last.Before(oldest)) {
						victim = k
						oldest = e.last
					}
				}
				if victim != "" {
					_ = p.entries[victim].browser.Close()
					delete(p.entries, victim)
					p.notify()
				}
			}
			if len(p.entries) < p.limit {
				p.entries[id] = &browserEntry{busy: true, headed: headed, last: time.Now()}
				p.mu.Unlock()
				b, err := p.launch(ctx, id, headed)
				p.mu.Lock()
				if err != nil {
					delete(p.entries, id)
					p.notify()
					p.mu.Unlock()
					return nil, err
				}
				if p.closed {
					_ = b.Close()
					delete(p.entries, id)
					p.notify()
					p.mu.Unlock()
					return nil, fail("BROWSER_POOL_CLOSED")
				}
				p.entries[id].browser = b
				p.mu.Unlock()
				return &Lease{Browser: b, pool: p, id: id}, nil
			}
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

// Discard a failed transport before making the slot available; the profile is retained.
func (l *Lease) Discard() {
	l.once.Do(func() {
		p := l.pool
		p.mu.Lock()
		defer p.mu.Unlock()
		if e := p.entries[l.id]; e != nil {
			_ = e.browser.Close()
			delete(p.entries, l.id)
			p.notify()
		}
	})
}
func (l *Lease) Release() {
	l.once.Do(func() {
		p := l.pool
		p.mu.Lock()
		defer p.mu.Unlock()
		if e := p.entries[l.id]; e != nil {
			e.busy = false
			e.last = time.Now()
			p.notify()
		}
	})
}
func (p *BrowserPool) Stop(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.entries[id]
	if e == nil {
		return nil
	}
	if e.busy {
		return fail("BROWSER_BUSY")
	}
	err := e.browser.Close()
	delete(p.entries, id)
	p.notify()
	return err
}
func (p *BrowserPool) Sweep() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, e := range p.entries {
		ttl := p.idle
		if e.headed {
			ttl = 15 * time.Minute
		}
		dead := false
		if live, ok := e.browser.(interface{ Alive() bool }); ok {
			dead = !live.Alive()
		}
		if !e.busy && (dead || time.Since(e.last) > ttl) {
			_ = e.browser.Close()
			delete(p.entries, id)
			p.notify()
		}
	}
}
func (p *BrowserPool) Stats() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	rows := []map[string]any{}
	for id, e := range p.entries {
		rows = append(rows, map[string]any{"account_id": id, "busy": e.busy, "login_window": e.headed})
	}
	return map[string]any{"limit": p.limit, "active": len(p.entries), "instances": rows}
}
func (p *BrowserPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for id, e := range p.entries {
		if e.browser != nil {
			_ = e.browser.Close()
		}
		delete(p.entries, id)
	}
	p.notify()
}
