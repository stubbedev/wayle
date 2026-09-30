package lock

import (
	"context"
	"errors"
	"image"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/shellipc"
	"github.com/stubbedev/wayle/service/auth"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/styling"
)

// appLocker adapts gelm's session lock to the screen.
type appLocker struct{ a *app.Application }

func (l appLocker) Available() bool { return l.a.SessionLockAvailable() }
func (l appLocker) Active() bool    { return l.a.SessionLock() != nil }

func (l appLocker) Lock(cfg app.SessionLockConfig) error {
	_, err := l.a.LockSession(cfg)
	return err
}

func (l appLocker) Release() {
	sl := l.a.SessionLock()
	if sl == nil {
		return
	}
	if err := sl.Unlock(); errors.Is(err, app.ErrNotLocked) {
		// Still pending: the compositor never confirmed it, withdraw.
		_ = sl.Cancel()
	}
}

func (l appLocker) Focus(w widget.Widget) {
	if sl := l.a.SessionLock(); sl != nil {
		sl.SetFocus(w)
	}
}

// Fonts resolves the prompt's faces from the configured family: the
// regular chain, and the bold clock (.lock-clock font-weight 700).
func Fonts(family string, text render.Font) credential.Fonts {
	fonts := credential.Fonts{Text: text, Clock: text}
	if bold, err := app.FontWeighted(family, clockFontPx, 700, false); err == nil {
		fonts.Clock = app.FontFallback(bold)
	}
	return fonts
}

const clockFontPx = 64

// Start wires the lock screen into the running shell: the
// com.wayle.Shell1 Lock method on the session bus, the logind
// triggers, lock-on-start, and the LockedHint restart recovery. The
// returned stop releases the bus name and the logind listener.
func Start(a *app.Application, cfg *config.Config, fonts credential.Fonts, pal *styling.Palette) (*Screen, func()) {
	sessionBus, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("lock: session bus unavailable; `wayle lock` disabled: %v", err)
		sessionBus = nil
	}
	ld := systemLogind()
	deps := Deps{
		Locker: appLocker{a: a},
		Loop:   a,
		Now:    time.Now,
		After: func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, func() { a.Invoke(fn) })
			return func() { t.Stop() }
		},
		Auth: func(service string, onEvent func(auth.Event)) authHandle {
			return auth.Spawn(auth.NewPAM(service), auth.CurrentUsername(), onEvent)
		},
		LoadImage: func(path string, blur uint32) (image.Image, error) { return credential.LoadImage(path, blur) },
	}
	if ld != nil {
		deps.SetLockedHint = func(locked bool) {
			go func() {
				if err := ld.SetLockedHint(locked); err != nil {
					log.Printf("lock: SetLockedHint failed (non-fatal): %v", err)
				}
			}()
		}
	}
	s := New(cfg, fonts, pal, deps)

	var stops []func()
	if sessionBus != nil {
		release, err := shellipc.Serve(sessionBus, shellipc.Handlers{Lock: func() bool {
			a.Invoke(s.Lock)
			return true
		}})
		if err != nil {
			log.Printf("lock: shell IPC: %v", err)
		} else {
			stops = append(stops, release)
		}
		stops = append(stops, func() { _ = sessionBus.Close() })
	}
	if ld != nil {
		ctx, cancel := context.WithCancel(context.Background())
		stops = append(stops, cancel)
		go func() {
			err := ld.Listen(ctx, func() { a.Invoke(s.Lock) }, func() { a.Invoke(s.ForceUnlock) })
			if err != nil {
				log.Printf("lock: logind listener unavailable; triggers disabled: %v", err)
			}
		}()
		// Restart recovery: ext-session-lock keeps the session locked
		// when its client dies, so a shell restarted while locked must
		// re-acquire the lock its predecessor held. logind's LockedHint
		// survives us and says whether it did.
		go func() {
			if shouldRelock(ld.LockedHint()) {
				log.Printf("lock: session was locked at startup (logind LockedHint); re-acquiring")
				a.Invoke(s.Lock)
			}
		}()
	}
	// The autologin gate: lock once when the login session's shell
	// first starts - never on a restart within the session.
	if cfg.Lock.LockOnStart && claimLockOnStart(os.Getenv("XDG_RUNTIME_DIR")) {
		a.Invoke(s.Lock)
	}
	return s, func() {
		for _, stop := range stops {
			stop()
		}
	}
}

// claimLockOnStart claims the once-per-login-session lock-on-start
// gate: true the first time in a session, false on every restart.
// The marker lives under XDG_RUNTIME_DIR, which logind clears when the
// login session ends. Without a runtime dir there is no deduplication,
// so it locks: failing secure for an access gate.
func claimLockOnStart(runtimeDir string) bool {
	if runtimeDir == "" {
		return true
	}
	marker := filepath.Join(runtimeDir, "wayle", "lock-on-start.done")
	_ = os.MkdirAll(filepath.Dir(marker), 0o700) //nolint:gosec // our own runtime dir
	// O_EXCL is the atomic first-writer-wins test: exactly one shell
	// per session sees success.
	f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // our own runtime marker
	if err == nil {
		_ = f.Close()
		return true
	}
	// An existing marker is a restart; any other failure locks.
	return !errors.Is(err, os.ErrExist)
}
