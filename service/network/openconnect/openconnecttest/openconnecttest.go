// Package openconnecttest holds the fakes the NM secret agent's tests
// need from the openconnect sign-in (the Rust's
// openconnect::testing::{silent_gateway, reached}).
package openconnecttest

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// SilentGateway is a gateway that takes connections and never says a
// word — the far end of a sign-in waiting on a push nobody approves.
type SilentGateway struct {
	// Addr is the host:port to put in a profile's gateway.
	Addr string

	accepted atomic.Int64
}

// NewSilentGateway listens on a loopback port until the test ends,
// holding every connection open without answering.
func NewSilentGateway(t testing.TB) *SilentGateway {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("a loopback port for the silent gateway: %v", err)
	}
	g := &SilentGateway{Addr: listener.Addr().String()}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			g.accepted.Add(1)
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range held {
			_ = conn.Close()
		}
	})
	return g
}

// Accepted is how many connections the gateway has taken.
func (g *SilentGateway) Accepted() int { return int(g.accepted.Load()) }

// Reached waits up to five seconds for the gateway to be reached at
// least once.
func (g *SilentGateway) Reached() bool {
	for range 100 {
		if g.Accepted() > 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
