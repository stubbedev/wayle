package bluetooth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// agentCall is one in-flight agent method driven by the fake.
type agentCall struct {
	done chan string // the reply's error name, "" on success
}

// callAsync drives the agent method from the fake on its own goroutine,
// as bluetoothd does while the user thinks.
func callAsync(f *fakeBlueZ, method string, out any, args ...any) agentCall {
	c := agentCall{done: make(chan string, 1)}
	go func() { c.done <- f.callAgent(method, out, args...) }()
	return c
}

func (c agentCall) wait(t *testing.T) string {
	t.Helper()
	select {
	case name := <-c.done:
		return name
	case <-time.After(2 * time.Second):
		t.Fatal("the agent call never returned")
		return ""
	}
}

func (c agentCall) stillBlocked(t *testing.T) {
	t.Helper()
	select {
	case name := <-c.done:
		t.Fatalf("the agent call returned %q, want it still waiting", name)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitPairing(t *testing.T, s *Service) PairingRequest {
	t.Helper()
	waitFor(t, "a pairing request", func() bool { return s.State().Pairing != nil })
	return s.State().Pairing
}

func TestAgentRequestPinCode(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()

	var pin string
	call := callAsync(f, "RequestPinCode", &pin, dev1)
	req, ok := waitPairing(t, s).(RequestPinCode)
	if !ok || req.Device != dev1 {
		t.Fatalf("pairing = %#v, want RequestPinCode for dev1", s.State().Pairing)
	}
	// The wrong answer kind is refused and leaves the request pending.
	var noPending *NoPendingRequestError
	if err := s.ProvidePasskey(1234); !errors.As(err, &noPending) || noPending.RequestType != "passkey" {
		t.Errorf("ProvidePasskey on a PIN request = %v", err)
	}
	// So is a PIN BlueZ cannot take.
	if err := s.ProvidePin(""); err == nil {
		t.Error("empty PIN: want an error")
	}
	if err := s.ProvidePin("12345678901234567"); err == nil {
		t.Error("17-character PIN: want an error")
	}
	call.stillBlocked(t)
	if err := s.ProvidePin("0000"); err != nil {
		t.Fatalf("ProvidePin: %v", err)
	}
	if name := call.wait(t); name != "" || pin != "0000" {
		t.Errorf("reply = %q %q, want the PIN", name, pin)
	}
	if s.State().Pairing != nil {
		t.Error("an answered request stays on screen")
	}
	// Answering twice finds nothing pending.
	if err := s.ProvidePin("0000"); !errors.As(err, &noPending) {
		t.Errorf("second ProvidePin = %v, want NoPendingRequestError", err)
	}
}

func TestAgentRequestPinCodeCancelledByTheUser(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	call := callAsync(f, "RequestPinCode", new(string), dev1)
	waitPairing(t, s)
	s.CancelPendingRequest()
	// A value request is cancelled, not rejected.
	if name := call.wait(t); name != errCanceled {
		t.Errorf("reply = %q, want %s", name, errCanceled)
	}
	if s.State().Pairing != nil {
		t.Error("a cancelled request stays on screen")
	}
}

func TestAgentRequestPasskey(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	var passkey uint32
	call := callAsync(f, "RequestPasskey", &passkey, dev1)
	if _, ok := waitPairing(t, s).(RequestPasskey); !ok {
		t.Fatalf("pairing = %#v", s.State().Pairing)
	}
	if err := s.ProvidePin("0000"); err == nil {
		t.Error("a PIN answered a passkey request")
	}
	if err := s.ProvidePasskey(1000000); err == nil {
		t.Error("a 7-digit passkey was accepted")
	}
	call.stillBlocked(t)
	if err := s.ProvidePasskey(4821); err != nil {
		t.Fatalf("ProvidePasskey: %v", err)
	}
	if name := call.wait(t); name != "" || passkey != 4821 {
		t.Errorf("reply = %q %d, want 4821", name, passkey)
	}
}

func TestAgentRequestPasskeyCancelled(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	call := callAsync(f, "RequestPasskey", new(uint32), dev1)
	waitPairing(t, s)
	s.CancelPendingRequest()
	if name := call.wait(t); name != errCanceled {
		t.Errorf("reply = %q, want %s", name, errCanceled)
	}
}

// yesNo covers the three yes/no requests with the same shape.
var yesNo = []struct {
	method  string
	args    []any
	want    func(PairingRequest) bool
	provide func(*Service, bool) error
}{
	{
		"RequestConfirmation",
		[]any{dev1, uint32(123456)},
		func(r PairingRequest) bool {
			c, ok := r.(RequestConfirmation)
			return ok && c.Passkey == 123456 && c.Device == dev1
		},
		(*Service).ProvideConfirmation,
	},
	{
		"RequestAuthorization",
		[]any{dev1},
		func(r PairingRequest) bool { _, ok := r.(RequestAuthorization); return ok },
		(*Service).ProvideAuthorization,
	},
	{
		"AuthorizeService",
		[]any{dev1, "0000110b-0000-1000-8000-00805f9b34fb"},
		func(r PairingRequest) bool {
			a, ok := r.(RequestServiceAuthorization)
			return ok && a.UUID == "0000110b-0000-1000-8000-00805f9b34fb"
		},
		(*Service).ProvideServiceAuthorization,
	},
}

func TestAgentYesNoAccept(t *testing.T) {
	for _, c := range yesNo {
		t.Run(c.method, func(t *testing.T) {
			f := startFakeBlueZ(t)
			s := f.service()
			call := callAsync(f, c.method, nil, c.args...)
			if req := waitPairing(t, s); !c.want(req) {
				t.Fatalf("pairing = %#v", req)
			}
			if err := c.provide(s, true); err != nil {
				t.Fatalf("provide: %v", err)
			}
			if name := call.wait(t); name != "" {
				t.Errorf("accepted reply = %q, want success", name)
			}
		})
	}
}

func TestAgentYesNoReject(t *testing.T) {
	for _, c := range yesNo {
		t.Run(c.method, func(t *testing.T) {
			f := startFakeBlueZ(t)
			s := f.service()
			call := callAsync(f, c.method, nil, c.args...)
			waitPairing(t, s)
			if err := c.provide(s, false); err != nil {
				t.Fatalf("provide: %v", err)
			}
			if name := call.wait(t); name != errRejected {
				t.Errorf("rejected reply = %q, want %s", name, errRejected)
			}
		})
	}
}

func TestAgentYesNoCancelledByTheUserIsARejection(t *testing.T) {
	for _, c := range yesNo {
		t.Run(c.method, func(t *testing.T) {
			f := startFakeBlueZ(t)
			s := f.service()
			call := callAsync(f, c.method, nil, c.args...)
			waitPairing(t, s)
			s.CancelPendingRequest()
			if name := call.wait(t); name != errRejected {
				t.Errorf("reply = %q, want %s", name, errRejected)
			}
		})
	}
}

func TestAgentYesNoRefusesOtherAnswers(t *testing.T) {
	// Each yes/no provider is refused on the others' requests.
	for i, c := range yesNo {
		t.Run(c.method, func(t *testing.T) {
			f := startFakeBlueZ(t)
			s := f.service()
			call := callAsync(f, c.method, nil, c.args...)
			waitPairing(t, s)
			for j, other := range yesNo {
				if i == j {
					continue
				}
				var noPending *NoPendingRequestError
				if err := other.provide(s, true); !errors.As(err, &noPending) {
					t.Errorf("%s answered %s: %v", other.method, c.method, err)
				}
			}
			call.stillBlocked(t)
			s.CancelPendingRequest()
			call.wait(t)
		})
	}
}

func TestAgentCancelFromBlueZ(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	call := callAsync(f, "RequestConfirmation", nil, dev1, uint32(1))
	waitPairing(t, s)
	if name := f.callAgent("Cancel", nil); name != "" {
		t.Fatalf("Cancel = %q", name)
	}
	if name := call.wait(t); name != errCanceled {
		t.Errorf("withdrawn reply = %q, want %s", name, errCanceled)
	}
	if s.State().Pairing != nil {
		t.Error("a withdrawn request stays on screen")
	}
	// Late answers find nothing to answer.
	if err := s.ProvideConfirmation(true); err == nil {
		t.Error("answer after Cancel: want NoPendingRequestError")
	}
	// Cancel with nothing pending is harmless.
	if name := f.callAgent("Cancel", nil); name != "" {
		t.Errorf("idle Cancel = %q", name)
	}
}

func TestAgentDisplayRequests(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	// Display calls return at once: BlueZ does not wait on them.
	if name := f.callAgent("DisplayPinCode", nil, dev1, "482913"); name != "" {
		t.Fatalf("DisplayPinCode = %q", name)
	}
	pin, ok := s.State().Pairing.(DisplayPinCode)
	if !ok || pin.PinCode != "482913" {
		t.Fatalf("pairing = %#v", s.State().Pairing)
	}
	// Nothing to answer on a display request.
	if err := s.ProvidePin("482913"); err == nil {
		t.Error("a display request took a PIN")
	}
	if name := f.callAgent("DisplayPasskey", nil, dev1, uint32(4321), uint16(3)); name != "" {
		t.Fatalf("DisplayPasskey = %q", name)
	}
	key, ok := s.State().Pairing.(DisplayPasskey)
	if !ok || key.Passkey != 4321 || key.Entered != 3 {
		t.Fatalf("pairing = %#v", s.State().Pairing)
	}
	s.CancelPendingRequest()
	if s.State().Pairing != nil {
		t.Error("dismissed display request stays")
	}
}

func TestAgentNewRequestCancelsTheOld(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	first := callAsync(f, "RequestPasskey", new(uint32), dev1)
	waitPairing(t, s)
	second := callAsync(f, "RequestConfirmation", nil, dev2, uint32(7))
	if name := first.wait(t); name != errCanceled {
		t.Errorf("displaced reply = %q, want %s", name, errCanceled)
	}
	waitFor(t, "the second request", func() bool {
		_, ok := s.State().Pairing.(RequestConfirmation)
		return ok
	})
	if err := s.ProvideConfirmation(true); err != nil {
		t.Fatal(err)
	}
	if name := second.wait(t); name != "" {
		t.Errorf("second reply = %q", name)
	}
}

func TestAgentReleaseKeepsServing(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	if name := f.callAgent("Release", nil); name != "" {
		t.Fatalf("Release = %q", name)
	}
	if s.State().Pairing != nil {
		t.Error("Release put something on screen")
	}
}

func TestCloseCancelsAPendingRequest(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	ans := make(chan agentAnswer, 1)
	go func() { ans <- s.ask(RequestPasskey{Device: dev1}) }()
	waitPairing(t, s)
	_ = s.Close()
	select {
	case a := <-ans:
		if a.err == nil || a.err.Name != errCanceled {
			t.Errorf("answer on close = %+v, want Canceled", a)
		}
	case <-time.After(time.Second):
		t.Fatal("Close left the agent call blocked")
	}
}

func TestPairingFlowConfirmed(t *testing.T) {
	f := populated(t)
	s := f.service()
	f.setPairMode(dev2, "confirm")
	paired := make(chan error, 1)
	go func() { paired <- s.Device(dev2).Pair(context.Background()) }()
	req, ok := waitPairing(t, s).(RequestConfirmation)
	if !ok || req.Device != dev2 || req.Passkey != 123456 {
		t.Fatalf("pairing = %#v", s.State().Pairing)
	}
	if err := s.ProvideConfirmation(true); err != nil {
		t.Fatal(err)
	}
	if err := <-paired; err != nil {
		t.Fatalf("Pair: %v", err)
	}
	waitFor(t, "paired", func() bool { d, _ := s.State().Device(dev2); return d.Paired && d.Bonded })
}

func TestPairingFlowRejected(t *testing.T) {
	f := populated(t)
	s := f.service()
	f.setPairMode(dev2, "confirm")
	paired := make(chan error, 1)
	go func() { paired <- s.Device(dev2).Pair(context.Background()) }()
	waitPairing(t, s)
	if err := s.ProvideConfirmation(false); err != nil {
		t.Fatal(err)
	}
	err := <-paired
	if errorName(err) != "org.bluez.Error.AuthenticationRejected" {
		t.Fatalf("Pair = %v, want AuthenticationRejected", err)
	}
	if d, _ := s.State().Device(dev2); d.Paired {
		t.Error("a rejected pairing reads as paired")
	}
	if got := f.replies(); len(got) != 1 || got[0] != errRejected {
		t.Errorf("agent replies = %v", got)
	}
}

func TestPairingFlowPinAndPasskey(t *testing.T) {
	f := populated(t)
	s := f.service()
	ctx := context.Background()

	f.setPairMode(dev2, "pin")
	done := make(chan error, 1)
	go func() { done <- s.Device(dev2).Pair(ctx) }()
	waitPairing(t, s)
	if err := s.ProvidePin("1234"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("PIN pairing: %v", err)
	}

	f.setPairMode(dev1, "passkey")
	go func() { done <- s.Device(dev1).Pair(ctx) }()
	waitPairing(t, s)
	s.CancelPendingRequest()
	if err := <-done; errorName(err) != "org.bluez.Error.AuthenticationCanceled" {
		t.Fatalf("cancelled passkey pairing = %v, want AuthenticationCanceled", err)
	}
	got := f.replies()
	if len(got) != 2 || *got[0].(*string) != "1234" || got[1] != errCanceled {
		t.Errorf("agent replies = %v", got)
	}
}

func TestPairingFlowDisplayPasskeyWithdrawn(t *testing.T) {
	f := populated(t)
	s := f.service()
	f.setPairMode(dev2, "display-passkey")
	if err := s.Device(dev2).Pair(context.Background()); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	// bluetoothd's Cancel after the remote typed the key clears it.
	waitFor(t, "the display cleared", func() bool { return s.State().Pairing == nil })
	waitFor(t, "paired", func() bool { d, _ := s.State().Device(dev2); return d.Paired })
}

func TestPairingRequestDevicePath(t *testing.T) {
	reqs := []PairingRequest{
		RequestPinCode{dev1},
		DisplayPinCode{dev1, "1"},
		RequestPasskey{dev1},
		DisplayPasskey{dev1, 1, 0},
		RequestConfirmation{dev1, 1},
		RequestAuthorization{dev1},
		RequestServiceAuthorization{dev1, "u"},
	}
	for _, r := range reqs {
		if r.DevicePath() != dev1 {
			t.Errorf("%T.DevicePath() = %q", r, r.DevicePath())
		}
	}
	if !asksYesNo(RequestConfirmation{}) || asksYesNo(RequestPasskey{}) || asksYesNo(DisplayPinCode{}) {
		t.Error("asksYesNo misclassifies")
	}
}
