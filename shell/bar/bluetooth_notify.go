package bar

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/desktopnotify"
	"github.com/stubbedev/wayle/service/bluetooth"
)

// The pairing card lives inside the dropdown, so a request that arrives
// while the dropdown is closed would go unseen: the agent waits, BlueZ
// times out, and the connection silently fails. The notifier pushes a
// desktop notification for it instead (notify_closed_popover_request).

// btAgentTimeout is how long BlueZ waits on the agent: buttons past it
// are dead.
const btAgentTimeout = 60 * time.Second

// btOpen counts the open bluetooth dropdowns per source
// (popover_visible).
var btOpen = struct {
	sync.Mutex
	n map[bluetooth.Source]int
}{n: map[bluetooth.Source]int{}}

func btDropdownOpened(src bluetooth.Source) {
	btOpen.Lock()
	btOpen.n[src]++
	btOpen.Unlock()
}

func btDropdownClosed(src bluetooth.Source) {
	btOpen.Lock()
	if btOpen.n[src] > 0 {
		btOpen.n[src]--
	}
	btOpen.Unlock()
}

func btDropdownVisible(src bluetooth.Source) bool {
	btOpen.Lock()
	defer btOpen.Unlock()
	return btOpen.n[src] > 0
}

// btNotifySender is the desktop-notification seam (desktopnotify on the
// session bus in the shell, a fake in tests).
type btNotifySender interface {
	Send(ctx context.Context, appName, summary, body, appIcon string) (uint32, error)
	Ask(ctx context.Context, appName, summary, body, appIcon string, actions []desktopnotify.Action, timeout time.Duration) (string, bool, error)
}

// btPairingNotifier watches one source's pairing requests.
type btPairingNotifier struct {
	src    bluetooth.Source
	sender btNotifySender
	// spawn runs a notification off the watcher (go; inline in tests).
	spawn func(func())
	// last is the request last seen, so each change notifies once.
	last bluetooth.PairingRequest
}

// btNotifiers keeps one notifier per source across the bar's outputs.
var btNotifiers sync.Map

// startBtPairingNotifier starts the source's notifier once; the bar's
// bluetooth modules call it.
func startBtPairingNotifier(src bluetooth.Source) {
	if _, loaded := btNotifiers.LoadOrStore(src, struct{}{}); loaded {
		return
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		log.Printf("bluetooth: pairing notifications off: %v", err)
		return
	}
	n := &btPairingNotifier{src: src, sender: desktopnotify.NewSender(conn), spawn: func(fn func()) { go fn() }}
	ticks, _ := src.Subscribe()
	go func() {
		for range ticks {
			n.check()
		}
	}()
}

// check notifies for a new request while no dropdown shows it.
func (n *btPairingNotifier) check() {
	st := n.src.State()
	req := st.Pairing
	if req == n.last {
		return
	}
	n.last = req
	if req == nil || btDropdownVisible(n.src) {
		return
	}
	display := unknownDisplay
	if dev, ok := st.Device(req.DevicePath()); ok {
		display = resolveDeviceDisplay(dev)
	}
	n.spawn(func() { n.notify(req, display) })
}

// btNotifyBody is the notification text for a request.
func btNotifyBody(req bluetooth.PairingRequest, display deviceDisplay) string {
	device := display.name
	if device == "" || device == "-" {
		device = btText("dropdown-bluetooth-new-device")
	}
	if c, ok := req.(bluetooth.RequestConfirmation); ok {
		return btText("dropdown-bluetooth-notify-passkey", "device", device, "passkey", formatPasskey(c.Passkey))
	}
	return btText("dropdown-bluetooth-notify-body", "device", device)
}

// btNotifyResponder answers a yes/no request from a notification
// button; nil for the PIN/passkey prompts, which need the dropdown's
// inputs.
func btNotifyResponder(src bluetooth.Source, req bluetooth.PairingRequest) func(bool) error {
	switch req.(type) {
	case bluetooth.RequestConfirmation:
		return src.ProvideConfirmation
	case bluetooth.RequestAuthorization:
		return src.ProvideAuthorization
	case bluetooth.RequestServiceAuthorization:
		return src.ProvideServiceAuthorization
	}
	return nil
}

// notify posts the notification; yes/no prompts get Deny/Allow buttons
// that answer the agent directly, and an allow is trusted.
func (n *btPairingNotifier) notify(req bluetooth.PairingRequest, display deviceDisplay) {
	ctx := context.Background()
	title := btText("dropdown-bluetooth-notify-title")
	body := btNotifyBody(req, display)
	respond := btNotifyResponder(n.src, req)
	if respond == nil {
		if _, err := n.sender.Send(ctx, "Wayle", title, body, "ld-bluetooth-symbolic"); err != nil {
			log.Printf("bluetooth: pairing notification: %v", err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(ctx, btAgentTimeout+5*time.Second)
	defer cancel()
	key, ok, err := n.sender.Ask(ctx, "Wayle", title, body, "ld-bluetooth-symbolic", []desktopnotify.Action{
		{Key: "deny", Label: btText("dropdown-bluetooth-deny")},
		{Key: "allow", Label: btText("dropdown-bluetooth-allow")},
	}, btAgentTimeout)
	if err != nil {
		log.Printf("bluetooth: pairing notification: %v", err)
		return
	}
	if !ok {
		return
	}
	accepted := key == "allow"
	if err := respond(accepted); err != nil {
		log.Printf("pairing response failed: %v", err)
		return
	}
	if accepted {
		trustPaired(ctx, n.src, req.DevicePath())
	}
}
