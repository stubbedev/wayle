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
