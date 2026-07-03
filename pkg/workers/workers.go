// Package workers provides a bounded goroutine pool for parallel work.
package workers

import "sync"

// Pool is a fixed-size worker pool.
type Pool struct {
	work   chan func()
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// New returns a Pool with n workers. If n <= 0, uses 1.
func New(n int) *Pool {
	if n < 1 {
		n = 1
	}
	p := &Pool{work: make(chan func(), n*2)}
	for i := 0; i < n; i++ {
		go p.worker()
	}
	return p
}

func (p *Pool) worker() {
	for fn := range p.work {
		fn()
		p.wg.Done()
	}
}

// Submit queues a function for execution.
func (p *Pool) Submit(fn func()) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.wg.Add(1)
	p.mu.Unlock()
	p.work <- fn
}

// Wait blocks until all submitted work is done, then closes the pool.
func (p *Pool) Wait() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	p.wg.Wait()
	close(p.work)
}
