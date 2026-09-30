package auth

import (
	"errors"
	"fmt"
	"log"
	"runtime"
)

// PAM return codes and flags the backend uses (security/_pam_types.h).
const (
	pamSuccess    = 0
	pamBufErr     = 5
	pamConvErr    = 19
	pamDeleteCred = 0x0004
)

// PAM message styles (security/_pam_types.h).
const (
	pamPromptEchoOff = 1
	pamPromptEchoOn  = 2
	pamErrorMsg      = 3
	pamTextInfo      = 4
)

// pamMessage is one conversation message, decoded from C.
type pamMessage struct {
	style int32
	text  string
}

// pamReply is one conversation answer. set false leaves the C
// response slot NULL (info and error messages).
type pamReply struct {
	text string
	set  bool
}

// pamLib is the libpam surface the backend drives, narrowed so tests
// can substitute a scripted PAM. The production implementation
// (pam_purego.go) binds libpam with purego — no cgo.
type pamLib interface {
	// start opens a transaction for service and user whose
	// conversation calls converse for every batch of messages.
	start(service, user string, converse func([]pamMessage) ([]pamReply, int32)) (pamTxn, error)
}

// pamTxn is one open PAM transaction.
type pamTxn interface {
	authenticate() int32
	acctMgmt() int32
	setcred(flags int32) int32
	end(status int32)
	strerror(code int32) string
}

// PAM authenticates against a PAM service (e.g. system-auth): the
// in-session backend of the lock screen. The conversation runs on the
// worker goroutine Spawn creates, locked to its OS thread for the
// transaction's lifetime, so the UI loop never blocks on PAM. The
// password is never logged.
type PAM struct {
	// Service is the PAM service name (/etc/pam.d/<service>).
	Service string
	lib     pamLib
	// unlockKeyring hands the verified password to gnome-keyring;
	// a field so tests observe it without a daemon.
	unlockKeyring func(password string)
}

// NewPAM returns the backend for service over the system libpam.
func NewPAM(service string) *PAM {
	return &PAM{Service: service, lib: systemPAM{}, unlockKeyring: UnlockLoginKeyring}
}

// Run implements Conversation. The transaction opens for username (the
// session user when empty), authenticates, then checks the account
// (pam_authenticate + pam_acct_mgmt, the pam crate's authenticate),
// and always ends with pam_setcred(DELETE_CRED) and pam_end. On
// success the last secret answered unlocks the gnome-keyring login
// collection, best effort.
func (p *PAM) Run(username string, ask Ask) error {
	if username == "" {
		username = CurrentUsername()
	}
	// PAM modules keep per-thread state and the conversation callback
	// fires on the calling thread: pin the goroutine for the whole
	// transaction.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	conv := &pamConversation{ask: ask}
	txn, err := p.lib.start(p.Service, username, conv.handle)
	if err != nil {
		log.Printf("auth: could not start PAM transaction for %s: %v", p.Service, err)
		return fmt.Errorf("could not start PAM transaction: %w", err)
	}
	// The pam crate's teardown: drop the credentials, then end the
	// transaction with that call's status.
	defer func() { txn.end(txn.setcred(pamDeleteCred)) }()

	if status := txn.authenticate(); status != pamSuccess {
		log.Printf("auth: PAM authentication failed for %s: %s", p.Service, txn.strerror(status))
		return fmt.Errorf("authentication failed: %s", txn.strerror(status))
	}
	if status := txn.acctMgmt(); status != pamSuccess {
		log.Printf("auth: PAM account check failed for %s: %s", p.Service, txn.strerror(status))
		return fmt.Errorf("authentication failed: %s", txn.strerror(status))
	}
	if conv.secret != "" && p.unlockKeyring != nil {
		p.unlockKeyring(conv.secret)
	}
	return nil
}

// pamConversation bridges PAM's conversation callback to an Ask.
type pamConversation struct {
	ask Ask
	// secret is the last echo-off answer handed to PAM, kept for the
	// post-auth keyring unlock.
	secret string
	// err records why the conversation aborted, for tests.
	err error
}

// errUnsupportedStyle aborts a conversation on a message style the
// backend cannot answer (Linux-PAM's binary prompts).
var errUnsupportedStyle = errors.New("unsupported PAM message style")

// handle answers one batch of messages, the pure half of the C
// callback: echo-off and echo-on prompts ask the UI (a cancelled
// prompt aborts with PAM_CONV_ERR), text-info and error messages are
// shown and answered with NULL. Any failure discards the whole batch,
// as the callback contract requires.
func (c *pamConversation) handle(msgs []pamMessage) ([]pamReply, int32) {
	replies := make([]pamReply, len(msgs))
	for i, m := range msgs {
		switch m.style {
		case pamPromptEchoOff, pamPromptEchoOn:
			kind := PromptSecret
			if m.style == pamPromptEchoOn {
				kind = PromptVisible
			}
			answer, ok := c.ask(Prompt{Kind: kind, Text: m.text})
			if !ok {
				c.err = ErrCancelled
				return nil, pamConvErr
			}
			if kind == PromptSecret {
				c.secret = answer
			}
			replies[i] = pamReply{text: answer, set: true}
		case pamTextInfo:
			c.ask(Prompt{Kind: PromptInfo, Text: m.text})
		case pamErrorMsg:
			c.ask(Prompt{Kind: PromptError, Text: m.text})
		default:
			c.err = fmt.Errorf("%w %d", errUnsupportedStyle, m.style)
			return nil, pamConvErr
		}
	}
	return replies, pamSuccess
}
