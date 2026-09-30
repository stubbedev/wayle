package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePAM is a scripted libpam: authenticate runs the conversation
// over the batches and passes when the secret matches; everything the
// backend calls is recorded.
type fakePAM struct {
	batches  [][]pamMessage
	password string
	acctRC   int32
	startErr error

	service, user string
	replies       [][]pamReply
	codes         []int32
	calls         []string
	endStatus     int32
}

func (f *fakePAM) start(service, user string, converse func([]pamMessage) ([]pamReply, int32)) (pamTxn, error) {
	f.service, f.user = service, user
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &fakeTxn{f: f, converse: converse}, nil
}

type fakeTxn struct {
	f        *fakePAM
	converse func([]pamMessage) ([]pamReply, int32)
}

const (
	fakeAuthErr = 7
	fakeAcctErr = 9
)

func (t *fakeTxn) authenticate() int32 {
	t.f.calls = append(t.f.calls, "authenticate")
	ok := false
	for _, batch := range t.f.batches {
		replies, code := t.converse(batch)
		t.f.replies = append(t.f.replies, replies)
		t.f.codes = append(t.f.codes, code)
		if code != pamSuccess {
			return pamConvErr
		}
		for i, m := range batch {
			if m.style == pamPromptEchoOff && replies[i].text == t.f.password {
				ok = true
			}
		}
	}
	if !ok {
		return fakeAuthErr
	}
	return pamSuccess
}

func (t *fakeTxn) acctMgmt() int32 {
	t.f.calls = append(t.f.calls, "acct_mgmt")
	return t.f.acctRC
}

func (t *fakeTxn) setcred(flags int32) int32 {
	t.f.calls = append(t.f.calls, "setcred")
	if flags != pamDeleteCred {
		t.f.calls = append(t.f.calls, "setcred-wrong-flags")
	}
	return 0
}

func (t *fakeTxn) end(status int32) {
	t.f.calls = append(t.f.calls, "end")
	t.f.endStatus = status
}

func (t *fakeTxn) strerror(code int32) string {
	switch code {
	case fakeAuthErr:
		return "Authentication failure"
	case fakeAcctErr:
		return "User account has expired"
	case pamConvErr:
		return "Conversation error"
	}
	return "error"
}

// answering is an Ask that answers every input prompt with answer and
// records every prompt.
func answering(answer string, seen *[]Prompt) Ask {
	return func(p Prompt) (string, bool) {
		*seen = append(*seen, p)
		if p.WantsInput() {
			return answer, true
		}
		return "", false
	}
}

func newTestPAM(f *fakePAM, unlocked *[]string) *PAM {
	return &PAM{Service: "wayle", lib: f, unlockKeyring: func(pw string) { *unlocked = append(*unlocked, pw) }}
}

// TestPAMConversationHandling pins the conversation: an echo-off
// prompt reaches the UI as a secret, text-info and error messages are
// shown without failing the batch (answered NULL), the verified
// password unlocks the keyring, and the transaction always ends with
// setcred(DELETE_CRED) then pam_end.
func TestPAMConversationHandling(t *testing.T) {
	f := &fakePAM{
		password: "hunter2",
		batches: [][]pamMessage{
			{{style: pamTextInfo, text: "Welcome"}},
			{{style: pamErrorMsg, text: "Password expires in 3 days"}, {style: pamPromptEchoOff, text: "Password: "}},
		},
	}
	var unlocked []string
	var seen []Prompt
	if err := newTestPAM(f, &unlocked).Run("alice", answering("hunter2", &seen)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []Prompt{
		{Kind: PromptInfo, Text: "Welcome"},
		{Kind: PromptError, Text: "Password expires in 3 days"},
		{Kind: PromptSecret, Text: "Password: "},
	}
	if len(seen) != len(want) {
		t.Fatalf("prompts = %+v, want %+v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("prompt %d = %+v, want %+v", i, seen[i], want[i])
		}
	}
	if f.replies[1][0].set || !f.replies[1][1].set || f.replies[1][1].text != "hunter2" {
		t.Errorf("replies = %+v: an error message answers NULL, the prompt the password", f.replies[1])
	}
	if f.user != "alice" || f.service != "wayle" {
		t.Errorf("transaction opened for %q on %q", f.user, f.service)
	}
	if got := strings.Join(f.calls, ","); got != "authenticate,acct_mgmt,setcred,end" {
		t.Errorf("calls = %s", got)
	}
	if len(unlocked) != 1 || unlocked[0] != "hunter2" {
		t.Errorf("keyring unlocks = %q, want the verified password once", unlocked)
	}
}

func TestPAMWrongPasswordFailsWithoutKeyring(t *testing.T) {
	f := &fakePAM{password: "right", batches: [][]pamMessage{{{style: pamPromptEchoOff, text: "Password: "}}}}
	var unlocked []string
	var seen []Prompt
	err := newTestPAM(f, &unlocked).Run("alice", answering("wrong", &seen))
	if err == nil || !strings.Contains(err.Error(), "Authentication failure") {
		t.Fatalf("Run = %v, want the authentication failure", err)
	}
	if len(unlocked) != 0 {
		t.Error("a failed authentication unlocked the keyring")
	}
	if got := strings.Join(f.calls, ","); got != "authenticate,setcred,end" {
		t.Errorf("calls = %s: a failure must still drop credentials and end", got)
	}
}

func TestPAMAccountCheckFailure(t *testing.T) {
	f := &fakePAM{password: "pw", acctRC: fakeAcctErr, batches: [][]pamMessage{{{style: pamPromptEchoOff, text: "Password: "}}}}
	var unlocked []string
	var seen []Prompt
	err := newTestPAM(f, &unlocked).Run("alice", answering("pw", &seen))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Run = %v, want the account failure", err)
	}
	if len(unlocked) != 0 {
		t.Error("an expired account unlocked the keyring")
	}
}

// TestPAMCancelAbortsBatch: a cancelled prompt aborts the batch with
// PAM_CONV_ERR and no replies, and the run fails.
func TestPAMCancelAbortsBatch(t *testing.T) {
	f := &fakePAM{password: "pw", batches: [][]pamMessage{{{style: pamPromptEchoOff, text: "Password: "}}}}
	var unlocked []string
	err := newTestPAM(f, &unlocked).Run("alice", func(Prompt) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("a cancelled conversation authenticated")
	}
	if f.codes[0] != pamConvErr || f.replies[0] != nil {
		t.Errorf("batch code %d replies %+v, want PAM_CONV_ERR and none", f.codes[0], f.replies[0])
	}
}

func TestPAMUnsupportedStyleAborts(t *testing.T) {
	conv := &pamConversation{ask: func(Prompt) (string, bool) { return "x", true }}
	replies, code := conv.handle([]pamMessage{{style: 7, text: "binary"}})
	if code != pamConvErr || replies != nil || !errors.Is(conv.err, errUnsupportedStyle) {
		t.Errorf("binary prompt: code %d replies %+v err %v", code, replies, conv.err)
	}
}

func TestPAMEchoOnPromptIsVisible(t *testing.T) {
	var seen []Prompt
	conv := &pamConversation{ask: answering("123456", &seen)}
	replies, code := conv.handle([]pamMessage{{style: pamPromptEchoOn, text: "OTP: "}})
	if code != pamSuccess || !replies[0].set || replies[0].text != "123456" {
		t.Fatalf("echo-on reply: code %d %+v", code, replies)
	}
	if seen[0].Kind != PromptVisible || conv.secret != "" {
		t.Errorf("echo-on prompt kind %v, captured secret %q; a visible answer is not the password", seen[0].Kind, conv.secret)
	}
}

func TestPAMStartFailure(t *testing.T) {
	f := &fakePAM{startErr: errors.New("load libpam: not found")}
	var unlocked []string
	err := newTestPAM(f, &unlocked).Run("", func(Prompt) (string, bool) { return "", false })
	if err == nil || !strings.Contains(err.Error(), "could not start PAM transaction") {
		t.Fatalf("Run = %v", err)
	}
}

func TestPAMEmptyUserIsSessionUser(t *testing.T) {
	t.Setenv("USER", "carol")
	f := &fakePAM{password: "pw", batches: [][]pamMessage{{{style: pamPromptEchoOff}}}}
	var unlocked []string
	var seen []Prompt
	_ = newTestPAM(f, &unlocked).Run("", answering("pw", &seen))
	if f.user != "carol" {
		t.Errorf("transaction user = %q, want the session user", f.user)
	}
}

// --- the real libpam, through purego ---

// realPAM opens the system libpam against a private service directory
// (pam_start_confdir), so the tests run real PAM modules without
// touching /etc/pam.d. Skips where libpam or confdir support is
// missing.
func realPAM(t *testing.T, service, rules string) *PAM {
	t.Helper()
	l, err := loadLibpam()
	if err != nil {
		t.Skipf("no libpam: %v", err)
	}
	if l.startConfdir == nil {
		t.Skip("libpam lacks pam_start_confdir")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, service), []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	return &PAM{Service: service, lib: systemPAM{confdir: dir}}
}

// TestRealPAMPermitAndDeny drives real modules through the purego
// callback: pam_permit authenticates without a prompt, pam_deny fails.
func TestRealPAMPermitAndDeny(t *testing.T) {
	permit := realPAM(t, "wayle-test", "auth required pam_permit.so\naccount required pam_permit.so\n")
	var seen []Prompt
	if err := permit.Run("nobody", answering("", &seen)); err != nil {
		t.Fatalf("pam_permit: %v", err)
	}
	if len(seen) != 0 {
		t.Errorf("pam_permit prompted: %+v", seen)
	}
	deny := realPAM(t, "wayle-test", "auth required pam_deny.so\naccount required pam_permit.so\n")
	if err := deny.Run("nobody", answering("", &seen)); err == nil {
		t.Fatal("pam_deny authenticated")
	}
}

// TestRealPAMConversationCallback proves the C callback path end to
// end: pam_echo's text reaches the UI as info, and pam_unix's password
// prompt reaches it as a secret whose answer PAM then judges.
func TestRealPAMConversationCallback(t *testing.T) {
	echo := realPAM(t, "wayle-test", "auth optional pam_echo.so wayle-conversation-probe\nauth required pam_permit.so\naccount required pam_permit.so\n")
	var seen []Prompt
	if err := echo.Run("nobody", answering("", &seen)); err != nil {
		t.Fatalf("pam_echo + pam_permit: %v", err)
	}
	if len(seen) != 1 || seen[0].Kind != PromptInfo || seen[0].Text != "wayle-conversation-probe" {
		t.Fatalf("prompts = %+v, want the pam_echo text as info", seen)
	}

	unix := realPAM(t, "wayle-test", "auth required pam_unix.so nodelay\naccount required pam_permit.so\n")
	seen = nil
	err := unix.Run("wayle-no-such-user", answering("not-the-password", &seen))
	if err == nil {
		t.Fatal("pam_unix accepted a nonexistent user")
	}
	if len(seen) != 1 || seen[0].Kind != PromptSecret || !strings.Contains(seen[0].Text, "assword") {
		t.Fatalf("prompts = %+v, want pam_unix's password prompt as a secret", seen)
	}

	seen = nil
	cancelled := unix.Run("wayle-no-such-user", func(p Prompt) (string, bool) {
		seen = append(seen, p)
		return "", false
	})
	if cancelled == nil || len(seen) != 1 {
		t.Fatalf("cancelled prompt: err %v prompts %+v", cancelled, seen)
	}
}
