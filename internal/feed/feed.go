// Package feed is the in-process change fan-out the services share:
// every subscriber gets its own buffered channel, publishing never
// blocks on a slow or departed reader (a full buffer drops the value;
// tick feeds coalesce), and unsubscribing closes the channel so the
// reader's range loop ends.
package feed

import (
	"context"
	"sync"
)

// Feed fans values out to its subscribers. The zero value is a
// coalescing feed (one-slot buffers), ready to use.
type Feed[T any] struct {
	mu     sync.Mutex
	buffer int
	subs   map[chan T]struct{}
	closed bool
}

// New is a feed whose subscriber channels buffer n values; n = 1 with
// struct{} values is a coalescing tick feed.
func New[T any](n int) *Feed[T] {
	return &Feed[T]{buffer: n, subs: map[chan T]struct{}{}}
}

// Subscribe registers a reader. stop unregisters and closes the
// channel; it is safe to call more than once. On a closed feed the
// channel comes back closed.
func (f *Feed[T]) Subscribe() (<-chan T, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan T, max(f.buffer, 1))
	if f.closed {
		close(ch)
		return ch, func() {}
	}
	if f.subs == nil {
		f.subs = map[chan T]struct{}{}
	}
	f.subs[ch] = struct{}{}
	return ch, func() { f.drop(ch) }
}

// SubscribeContext is Subscribe until ctx ends, when the channel
// closes.
func (f *Feed[T]) SubscribeContext(ctx context.Context) <-chan T {
	ch, stop := f.Subscribe()
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ch
}

func (f *Feed[T]) drop(ch chan T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.subs[ch]; ok {
		delete(f.subs, ch)
		close(ch)
	}
}

// Publish offers v to every subscriber without blocking.
func (f *Feed[T]) Publish(v T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs {
		select {
		case ch <- v:
		default:
		}
	}
}

// Close closes every subscriber's channel; later subscriptions come
// back closed.
func (f *Feed[T]) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for ch := range f.subs {
		delete(f.subs, ch)
		close(ch)
	}
}

// Len is the subscriber count.
func (f *Feed[T]) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

// Tick is a coalescing change feed: a pending tick absorbs the next.
type Tick = Feed[struct{}]

// NewTick is a coalescing tick feed.
func NewTick() *Tick { return New[struct{}](1) }

// Notify ticks every subscriber of a tick feed.
func Notify(t *Tick) { t.Publish(struct{}{}) }
