package dbustest

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

type echo struct{}

func (echo) Echo(s string) (string, *dbus.Error) { return s, nil }

// The name is long on purpose: t.TempDir() embeds it, and a socket
// under that path would overflow sun_path.
func TestAPrivateBusStartsEvenWhenTheTestNameIsFarLongerThanAUnixSocketPathMayBeWithoutBeingTruncated(t *testing.T) {
	bus := Start(t)
	if bus.Address == "" {
		t.Fatal("no address")
	}
	_ = bus.Conn(t)
}

func TestPrivateBusCarriesCallsBetweenPeers(t *testing.T) {
	bus := Start(t)
	server := bus.Conn(t)
	if err := server.Export(echo{}, "/test", "com.example.Echo"); err != nil {
		t.Fatal(err)
	}
	if reply, err := server.RequestName("com.example.Echo", dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName = %v, %v", reply, err)
	}
	client := bus.Conn(t)
	var got string
	if err := client.Object("com.example.Echo", "/test").Call("com.example.Echo.Echo", 0, "hi").Store(&got); err != nil {
		t.Fatal(err)
	}
	if got != "hi" {
		t.Errorf("echo = %q", got)
	}
	// An unowned name is an error, not a hang.
	if err := client.Object("com.example.Missing", "/test").Call("com.example.Echo.Echo", 0, "hi").Err; err == nil {
		t.Error("call to an unowned name: want an error")
	}
}

func TestVarIsOrderedAcrossGoroutines(t *testing.T) {
	var v Var[int]
	if v.Load() != 0 {
		t.Fatal("the zero Var holds the zero value")
	}
	done := make(chan struct{})
	for range 8 {
		go func() {
			v.Update(func(n int) int { return n + 1 })
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
	if got := v.Load(); got != 8 {
		t.Fatalf("after 8 updates = %d, want 8 (an update was lost)", got)
	}
	v.Store(3)
	if got := v.Load(); got != 3 {
		t.Fatalf("after Store(3) = %d", got)
	}
}
