package main

import (
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/service/idleinhibit"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/service/recorder/recordertest"
)

type step struct {
	args []string
	want string
}

// runSteps runs each command and checks stdout (code 0) or, for a
// want starting with "Error: ", stderr (code 1).
func runSteps(t *testing.T, steps []step) {
	t.Helper()
	for _, s := range steps {
		stdout, stderr, code := runCaptured(t, false, s.args...)
		got, wantCode := stdout, 0
		if strings.HasPrefix(s.want, "Error: ") {
			got, wantCode = stderr, 1
		}
		if code != wantCode || got != s.want {
			t.Errorf("%v: code %d stdout %q stderr %q\nwant %q", s.args, code, stdout, stderr, s.want)
		}
	}
}

func TestIdleCommands(t *testing.T) {
	dbustest.Session(t)
	runSteps(t, []step{{[]string{"idle", "status"}, "Error: IdleInhibit service not running. Start wayle shell first.\n"}})
	state := idleinhibit.NewState(30)
	release, err := idleinhibit.NewDaemon(state).Export(dbustest.SessionConn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	runSteps(t, []step{
		{[]string{"idle", "status"}, "Inactive (duration: 30 min)\n"},
		{[]string{"idle", "remaining", "+5"}, "Error: Failed to adjust remaining: idle inhibit is not active\n"},
		{[]string{"idle", "on", "45"}, "Enabled for 45 minutes\n"},
		{[]string{"idle", "remaining", "10"}, "Set remaining to 10 minutes\n"},
		{[]string{"idle", "status"}, "Active (10:00 remaining, 45 min duration)\n"},
		{[]string{"idle", "remaining", "-4"}, "Subtracted 4 minutes from remaining\n"},
		{[]string{"idle", "duration", "+15"}, "Added 15 minutes to duration\n"},
		{[]string{"idle", "duration", "x"}, "Error: Invalid minutes: x\n"},
		{[]string{"idle", "duration", "+x"}, "Error: Invalid delta: +x\n"},
		{[]string{"idle", "toggle"}, "Disabled\n"},
		{[]string{"idle", "toggle", "-i"}, "Enabled (indefinite)\n"},
		// "Indefinite" is a zero duration (state.rs's indefinite()): with
		// a duration stored, an --indefinite run still takes timer edits,
		// as in Rust; with none, they are refused.
		{[]string{"idle", "remaining", "5"}, "Set remaining to 5 minutes\n"},
		{[]string{"idle", "off"}, "Disabled\n"},
		{[]string{"idle", "duration", "0"}, "Set duration to 0 minutes\n"},
		{[]string{"idle", "status"}, "Inactive (duration: indefinite)\n"},
		{[]string{"idle", "on"}, "Enabled (indefinite)\n"},
		{[]string{"idle", "status"}, "Active (indefinite)\n"},
		{[]string{"idle", "remaining", "5"}, "Error: Failed to set remaining: cannot set timer in indefinite mode\n"},
		{[]string{"idle", "remaining", "+5"}, "Error: Failed to adjust remaining: cannot adjust timer in indefinite mode\n"},
	})
}

func TestNotifyCommands(t *testing.T) {
	dbustest.Session(t)
	runSteps(t, []step{{[]string{"notify", "list"}, "Error: Notification service not running. Start wayle shell first.\n"}})
	svc := notifications.NewService()
	server, err := notifications.Serve(dbustest.SessionConn(t), svc)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Release() }()
	runSteps(t, []step{{[]string{"notify", "list"}, "No notifications\n"}})
	id := svc.Notify("mail", 0, "", "New message", "Hello there", nil, -1)
	svc.Notify("chat", 0, "", "Ping", "", nil, -1)
	runSteps(t, []step{
		{[]string{"notify", "list"}, "Notifications:\n  [2] chat: Ping\n  [1] mail: New message\n      Hello there\n"},
		{[]string{"notify", "status"}, "Notifications: 2\nActive popups: 2\nDo Not Disturb: disabled\nPopup duration: 5000ms\n"},
		{[]string{"notify", "dnd"}, "Do Not Disturb: enabled\n"},
		{[]string{"notify", "dismiss", "1"}, "Dismissed notification 1\n"},
		{[]string{"notify", "dismiss-all"}, "Dismissed all notifications\n"},
		{[]string{"notify", "status"}, "Notifications: 0\nActive popups: 0\nDo Not Disturb: enabled\nPopup duration: 5000ms\n"},
	})
	if id != 1 {
		t.Errorf("first id = %d", id)
	}
}

func TestRecorderCommands(t *testing.T) {
	dbustest.Session(t)
	runSteps(t, []step{{[]string{"recorder", "status"}, "Error: Recorder service not running. Start wayle shell first.\n"}})
	cfg := config.Defaults().Recorder
	cfg.OutputDirectory, cfg.StartDelayMs = t.TempDir(), 0
	state := recorder.NewState(&recordertest.Engine{}, func() config.RecorderConfig { return cfg }, recorder.Hooks{})
	release, err := recorder.NewDaemon(state).Export(dbustest.SessionConn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	runSteps(t, []step{
		{[]string{"recorder", "status"}, "Idle\n"},
		{[]string{"recorder", "start"}, "Recording started\n"},
	})
	waitFor(t, func() bool { return state.Snapshot().Active })
	stdout, _, _ := runCaptured(t, false, "recorder", "status")
	if !strings.HasPrefix(stdout, "Recording (0:0") || !strings.Contains(stdout, " -> ") {
		t.Errorf("status while recording: %q", stdout)
	}
	runSteps(t, []step{{[]string{"recorder", "pause"}, "Paused\n"}})
	if stdout, _, _ := runCaptured(t, false, "recorder", "status"); !strings.HasPrefix(stdout, "Paused (") {
		t.Errorf("status while paused: %q", stdout)
	}
	runSteps(t, []step{
		{[]string{"recorder", "resume"}, "Resumed\n"},
		{[]string{"recorder", "toggle"}, "Recording stopped\n"},
	})
	waitFor(t, func() bool { return !state.Snapshot().Active })
	runSteps(t, []step{{[]string{"recorder", "stop"}, "Recording stopped\n"}})
}
