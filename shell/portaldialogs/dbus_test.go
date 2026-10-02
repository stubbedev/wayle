package portaldialogs

import (
	"context"
	"slices"
	"testing"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeHost answers every dialog from fixed replies and records what it
// was asked.
type fakeHost struct {
	yes    bool
	choice string
	asked  dbustest.Var[[]any]
}

func (f *fakeHost) record(args ...any) { f.asked.Store(args) }

func (f *fakeHost) Access(r AccessRequest) bool { f.record(r); return f.yes }
func (f *fakeHost) Account(reason string) bool  { f.record(reason); return f.yes }
func (f *fakeHost) ConfirmWallpaper(uri string) bool {
	f.record(uri)
	return f.yes
}

func (f *fakeHost) ChooseApplication(choices []string, contentType, uri string) string {
	f.record(choices, contentType, uri)
	return f.choice
}

func (f *fakeHost) ConfirmInstall(name, iconName string) bool {
	f.record(name, iconName)
	return f.yes
}

func TestDialogsRoundTrip(t *testing.T) {
	bus := dbustest.Start(t)
	for _, yes := range []bool{true, false} {
		host := &fakeHost{yes: yes, choice: map[bool]string{true: "org.app.desktop"}[yes]}
		release, err := NewDaemon(host).Export(bus.Conn(t))
		if err != nil {
			t.Fatal(err)
		}
		c, ctx := NewClient(bus.Conn(t)), context.Background()
		req := AccessRequest{"T", "S", "B", "Allow", "Deny", "camera"}
		if got, err := c.Access(ctx, req); err != nil || got != yes || host.asked.Load()[0] != req {
			t.Errorf("Access = %v, %v (asked %v)", got, err, host.asked.Load())
		}
		if got, err := c.Account(ctx, "why"); err != nil || got != yes || host.asked.Load()[0] != "why" {
			t.Errorf("Account = %v, %v", got, err)
		}
		if got, err := c.ConfirmWallpaper(ctx, "file:///a.png"); err != nil || got != yes {
			t.Errorf("ConfirmWallpaper = %v, %v", got, err)
		}
		if got, err := c.ConfirmInstall(ctx, "App", "app-icon"); err != nil || got != yes || host.asked.Load()[1] != "app-icon" {
			t.Errorf("ConfirmInstall = %v, %v", got, err)
		}
		got, err := c.ChooseApplication(ctx, nil, "text/plain", "file:///x")
		if asked := host.asked.Load(); err != nil || got != host.choice || len(asked[0].([]string)) != 0 || asked[1] != "text/plain" {
			t.Errorf("ChooseApplication = %q, %v (asked %v)", got, err, asked)
		}
		if _, _ = c.ChooseApplication(ctx, []string{"a", "b"}, "", ""); !slices.Equal(host.asked.Load()[0].([]string), []string{"a", "b"}) {
			t.Errorf("choices = %v", host.asked.Load()[0])
		}
		release()
	}
	// Without a host every call is an error, never a silent answer.
	if _, err := NewClient(bus.Conn(t)).Access(context.Background(), AccessRequest{}); err == nil {
		t.Error("Access answered without a host")
	}
}
