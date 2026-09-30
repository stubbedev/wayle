package bluetooth

import (
	"fmt"
	"log"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"
)

// agentPath is where the pairing agent lives on the bus (service.rs).
const agentPath = dbus.ObjectPath("/com/wayle/BluetoothAgent")

// agentCapability is the IO capability wayle registers: it can show a
// code and ask yes/no, and the dropdown also takes PIN/passkey entry.
const agentCapability = CapabilityDisplayYesNo

// The errors BlueZ's agent API defines for a refused request.
const (
	errRejected = "org.bluez.Error.Rejected"
	errCanceled = "org.bluez.Error.Canceled"
)

// PairingRequest is one agent prompt awaiting the user (PairingRequest
// in types/agent.rs). The concrete types are the seven variants; the
// interface is sealed.
type PairingRequest interface {
	// DevicePath is the device the request is about.
	DevicePath() dbus.ObjectPath
	pairingRequest()
}

// RequestPinCode asks for a legacy PIN (answer with ProvidePin).
type RequestPinCode struct{ Device dbus.ObjectPath }

// DisplayPinCode shows a PIN to type on the remote device; nothing to
// answer.
type DisplayPinCode struct {
	Device  dbus.ObjectPath
	PinCode string
}

// RequestPasskey asks for the numeric passkey shown on the remote
// device (answer with ProvidePasskey).
type RequestPasskey struct{ Device dbus.ObjectPath }

// DisplayPasskey shows a passkey to type on the remote device, with the
// count of digits typed so far; nothing to answer.
type DisplayPasskey struct {
	Device  dbus.ObjectPath
	Passkey uint32
	Entered uint16
}

// RequestConfirmation asks whether Passkey matches the remote device's
// (answer with ProvideConfirmation).
type RequestConfirmation struct {
	Device  dbus.ObjectPath
	Passkey uint32
}

// RequestAuthorization asks to allow a just-works pairing (answer with
// ProvideAuthorization).
type RequestAuthorization struct{ Device dbus.ObjectPath }

// RequestServiceAuthorization asks to allow a profile connection
// (answer with ProvideServiceAuthorization).
type RequestServiceAuthorization struct {
	Device dbus.ObjectPath
	UUID   string
}

// DevicePath implements PairingRequest.
func (r RequestPinCode) DevicePath() dbus.ObjectPath { return r.Device }

// DevicePath implements PairingRequest.
func (r DisplayPinCode) DevicePath() dbus.ObjectPath { return r.Device }

// DevicePath implements PairingRequest.
func (r RequestPasskey) DevicePath() dbus.ObjectPath { return r.Device }

// DevicePath implements PairingRequest.
func (r DisplayPasskey) DevicePath() dbus.ObjectPath { return r.Device }

// DevicePath implements PairingRequest.
func (r RequestConfirmation) DevicePath() dbus.ObjectPath { return r.Device }

// DevicePath implements PairingRequest.
func (r RequestAuthorization) DevicePath() dbus.ObjectPath { return r.Device }

// DevicePath implements PairingRequest.
func (r RequestServiceAuthorization) DevicePath() dbus.ObjectPath { return r.Device }

func (RequestPinCode) pairingRequest()              {}
func (DisplayPinCode) pairingRequest()              {}
func (RequestPasskey) pairingRequest()              {}
func (DisplayPasskey) pairingRequest()              {}
func (RequestConfirmation) pairingRequest()         {}
func (RequestAuthorization) pairingRequest()        {}
func (RequestServiceAuthorization) pairingRequest() {}

// agentAnswer resolves one blocked agent call: the value BlueZ gets, or
// err when the user refused or the request went away.
type agentAnswer struct {
	pin     string
	passkey uint32
	err     *dbus.Error
}

func rejected() agentAnswer {
	return agentAnswer{err: dbus.NewError(errRejected, []any{"User rejected"})}
}

func canceled() agentAnswer {
	return agentAnswer{err: dbus.NewError(errCanceled, []any{"User cancelled"})}
}

// pending is the request on screen. answer is nil for the display
// requests, which BlueZ does not wait on; otherwise the blocked agent
// call receives exactly one agentAnswer through it (buffered).
type pending struct {
	request PairingRequest
	answer  chan agentAnswer
}

// asksYesNo reports whether the request's refusal is a rejection rather
// than a cancellation (cancel_pending_request sends false to the bool
// responders and drops the value ones).
func asksYesNo(r PairingRequest) bool {
	switch r.(type) {
	case RequestConfirmation, RequestAuthorization, RequestServiceAuthorization:
		return true
	}
	return false
}

// agent is the exported org.bluez.Agent1 (agent/mod.rs). Each method
// runs on its own goroutine (godbus dispatches calls concurrently), so
// the prompting ones block until the UI answers.
type agent struct{ s *Service }

// Release is BlueZ unregistering the agent; nothing to clean up.
func (a *agent) Release() *dbus.Error { return nil }

// RequestPinCode waits for the user's legacy PIN.
func (a *agent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	ans := a.s.ask(RequestPinCode{Device: device})
	return ans.pin, ans.err
}

// DisplayPinCode shows the PIN and returns at once.
func (a *agent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	a.s.show(DisplayPinCode{Device: device, PinCode: pincode})
	return nil
}

// RequestPasskey waits for the user's passkey.
func (a *agent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	ans := a.s.ask(RequestPasskey{Device: device})
	return ans.passkey, ans.err
}

// DisplayPasskey shows the passkey (and the typing progress) and
// returns at once.
func (a *agent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	a.s.show(DisplayPasskey{Device: device, Passkey: passkey, Entered: entered})
	return nil
}

// RequestConfirmation waits for the user to confirm the passkey.
func (a *agent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	return a.s.ask(RequestConfirmation{Device: device, Passkey: passkey}).err
}

// RequestAuthorization waits for the user to allow the pairing.
func (a *agent) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	return a.s.ask(RequestAuthorization{Device: device}).err
}

// AuthorizeService waits for the user to allow the profile.
func (a *agent) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	return a.s.ask(RequestServiceAuthorization{Device: device, UUID: uuid}).err
}

// Cancel is BlueZ withdrawing the request (timeout, remote abort): the
// prompt goes away and a blocked call returns Canceled.
func (a *agent) Cancel() *dbus.Error {
	log.Printf("bluetooth: pairing cancelled")
	a.s.withdraw(func(pending) agentAnswer { return canceled() })
	return nil
}

// registerAgent exports the agent and registers it with BlueZ as the
// default agent (service.rs: RegisterAgent with DisplayYesNo). The
// default-agent request is best effort: pairing started from wayle
// still reaches the agent without it.
func (s *Service) registerAgent() error {
	if err := s.conn.Export(&agent{s: s}, agentPath, agentIface); err != nil {
		return fmt.Errorf("bluetooth: cannot register bluetooth agent: export: %w", err)
	}
	manager := s.conn.Object(bluezName, bluezRoot)
	if err := manager.Call(agentManager+".RegisterAgent", 0, agentPath, agentCapability.String()).Err; err != nil {
		return fmt.Errorf("bluetooth: cannot register bluetooth agent: %w", err)
	}
	if err := manager.Call(agentManager+".RequestDefaultAgent", 0, agentPath).Err; err != nil {
		log.Printf("bluetooth: default agent: %v", err)
	}
	return nil
}

// show puts a display request on screen; one still waiting on the user
// is cancelled, since BlueZ runs one agent request at a time.
func (s *Service) show(req PairingRequest) {
	s.setPending(&pending{request: req})
}

// ask puts a prompting request on screen and blocks until the user
// answers, cancels, or BlueZ withdraws it.
func (s *Service) ask(req PairingRequest) agentAnswer {
	p := &pending{request: req, answer: make(chan agentAnswer, 1)}
	s.setPending(p)
	return <-p.answer
}

// setPending replaces the on-screen request, cancelling the one it
// displaces.
func (s *Service) setPending(p *pending) {
	s.mu.Lock()
	old := s.pairing
	s.pairing = p
	s.mu.Unlock()
	if old != nil && old.answer != nil {
		old.answer <- canceled()
	}
	s.changed()
}

// withdraw clears the on-screen request, resolving a blocked call with
// refuse's answer (event_processor.rs's Cancelled arm).
func (s *Service) withdraw(refuse func(pending) agentAnswer) {
	s.mu.Lock()
	p := s.pairing
	s.pairing = nil
	s.mu.Unlock()
	if p == nil {
		return
	}
	if p.answer != nil {
		p.answer <- refuse(*p)
	}
	s.changed()
}

// resolve answers the pending request when match accepts it
// (providers.rs); anything else is NoPendingRequestError and leaves the
// request in place.
func (s *Service) resolve(requestType string, match func(PairingRequest) bool, ans agentAnswer) error {
	s.mu.Lock()
	p := s.pairing
	if p == nil || p.answer == nil || !match(p.request) {
		s.mu.Unlock()
		return &NoPendingRequestError{RequestType: requestType}
	}
	s.pairing = nil
	s.mu.Unlock()
	p.answer <- ans
	s.changed()
	return nil
}

// ProvidePin answers RequestPinCode. BlueZ takes 1-16 characters; a
// PIN outside that is an error and the request stays pending.
func (s *Service) ProvidePin(pin string) error {
	if n := utf8.RuneCountInString(pin); n < 1 || n > 16 {
		return fmt.Errorf("bluetooth: cannot provide pin: %d characters, want 1-16", n)
	}
	return s.resolve("pin", func(r PairingRequest) bool {
		_, ok := r.(RequestPinCode)
		return ok
	}, agentAnswer{pin: pin})
}

// ProvidePasskey answers RequestPasskey; a passkey above 999999 is an
// error and the request stays pending.
func (s *Service) ProvidePasskey(passkey uint32) error {
	if passkey > 999999 {
		return fmt.Errorf("bluetooth: cannot provide passkey: %d is not 0-999999", passkey)
	}
	return s.resolve("passkey", func(r PairingRequest) bool {
		_, ok := r.(RequestPasskey)
		return ok
	}, agentAnswer{passkey: passkey})
}

// ProvideConfirmation answers RequestConfirmation: false rejects.
func (s *Service) ProvideConfirmation(confirmed bool) error {
	return s.resolve("confirmation", func(r PairingRequest) bool {
		_, ok := r.(RequestConfirmation)
		return ok
	}, verdict(confirmed))
}

// ProvideAuthorization answers RequestAuthorization: false rejects.
func (s *Service) ProvideAuthorization(allowed bool) error {
	return s.resolve("authorization", func(r PairingRequest) bool {
		_, ok := r.(RequestAuthorization)
		return ok
	}, verdict(allowed))
}

// ProvideServiceAuthorization answers RequestServiceAuthorization:
// false rejects.
func (s *Service) ProvideServiceAuthorization(allowed bool) error {
	return s.resolve("service authorization", func(r PairingRequest) bool {
		_, ok := r.(RequestServiceAuthorization)
		return ok
	}, verdict(allowed))
}

// CancelPendingRequest dismisses whatever is on screen: the yes/no
// requests are rejected, the PIN/passkey entries cancelled, a display
// request just cleared (cancel_pending_request).
func (s *Service) CancelPendingRequest() {
	s.withdraw(func(p pending) agentAnswer {
		if asksYesNo(p.request) {
			return rejected()
		}
		return canceled()
	})
}

func verdict(accepted bool) agentAnswer {
	if accepted {
		return agentAnswer{}
	}
	return rejected()
}
