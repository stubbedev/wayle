package dbustest

import "sync"

// Var is state a bus handler writes and the test reads. A method call
// and its reply cross a socket, which the race detector cannot see as
// ordering, so a plain captured variable reads as a race even when the
// round trip orders it. Var's lock makes the ordering visible.
type Var[T any] struct {
	mu sync.Mutex
	v  T
}

// Load returns the current value.
func (v *Var[T]) Load() T {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.v
}

// Store replaces the value.
func (v *Var[T]) Store(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.v = value
}

// Update changes the value in place and returns the result.
func (v *Var[T]) Update(fn func(T) T) T {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.v = fn(v.v)
	return v.v
}
