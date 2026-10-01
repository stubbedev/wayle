package config

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Hot reload over inotify (crates/wayle-config/src/infrastructure/
// watcher.rs): the config directory is watched recursively, plus the
// real parent of a symlinked main config (never the /nix/store root).
// Events debounce for 100ms, capped at 2s after the first pending one
// so a continuous stream still flushes; a flush reloads what changed:
// .env files, themes/, runtime.toml alone, or everything.

const (
	debounceDelay    = 100 * time.Millisecond
	maxDebounceDelay = 2 * time.Second
	watchMask        = unix.IN_CREATE | unix.IN_MODIFY | unix.IN_DELETE | unix.IN_MOVED_FROM |
		unix.IN_MOVED_TO | unix.IN_ATTRIB | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF
)

type watcher struct {
	svc  *Service
	file *os.File

	mu    sync.Mutex
	dirs  map[int]string
	paths chan string
	done  chan struct{}
	wg    sync.WaitGroup
}

func (s *Service) startWatcher() error {
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return &LoadError{msg: "cannot initialize file watcher", err: err}
	}
	w := &watcher{
		svc:   s,
		file:  os.NewFile(uintptr(fd), "inotify"),
		dirs:  map[int]string{},
		paths: make(chan string, 64),
		done:  make(chan struct{}),
	}
	if err := w.addRecursive(s.dir); err != nil {
		_ = w.file.Close()
		return &LoadError{msg: "cannot watch '" + s.dir + "'", err: err}
	}
	if canonical, err := filepath.EvalSymlinks(s.MainPath()); err == nil {
		if parent := filepath.Dir(canonical); parent != s.dir && !isImmutableStore(parent) {
			if err := w.add(parent); err != nil {
				log.Printf("config: failed to watch canonical config folder %s: %v", parent, err)
			}
		}
	}
	s.watcher = w
	w.wg.Add(2)
	go w.read()
	go w.debounce()
	return nil
}

// isImmutableStore reports whether dir is the Nix store root: a Home
// Manager symlink resolves there, and watching it floods the watcher
// with every build on the machine for nothing.
func isImmutableStore(dir string) bool {
	store := os.Getenv("NIX_STORE_DIR")
	if store == "" {
		store = "/nix/store"
	}
	return dir == store
}

func (w *watcher) add(dir string) error {
	wd, err := unix.InotifyAddWatch(int(w.file.Fd()), dir, watchMask)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.dirs[wd] = dir
	w.mu.Unlock()
	return nil
}

func (w *watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if d.IsDir() {
			return w.add(path)
		}
		return nil
	})
}

// read decodes inotify events into changed paths.
func (w *watcher) read() {
	defer w.wg.Done()
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	for {
		n, err := w.file.Read(buf)
		if err != nil {
			if !errors.Is(err, os.ErrClosed) {
				log.Printf("config: watcher read: %v", err)
			}
			close(w.paths)
			return
		}
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off])) //nolint:gosec // the kernel's event layout
			nameLen := int(ev.Len)
			name := ""
			if nameLen > 0 {
				raw := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+nameLen]
				name = strings.TrimRight(string(raw), "\x00")
			}
			off += unix.SizeofInotifyEvent + nameLen
			w.mu.Lock()
			dir := w.dirs[int(ev.Wd)]
			w.mu.Unlock()
			if dir == "" {
				continue
			}
			path := dir
			if name != "" {
				path = filepath.Join(dir, name)
			}
			if ev.Mask&unix.IN_ISDIR != 0 && ev.Mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 {
				_ = w.addRecursive(path)
			}
			select {
			case w.paths <- path:
			case <-w.done:
				return
			}
		}
	}
}

// debounce batches paths and flushes them after the quiet period.
func (w *watcher) debounce() {
	defer w.wg.Done()
	pending := map[string]bool{}
	var timer *time.Timer
	var fire <-chan time.Time
	var ceiling time.Time
	for {
		select {
		case path, ok := <-w.paths:
			if !ok {
				return
			}
			pending[path] = true
			now := time.Now()
			if ceiling.IsZero() {
				ceiling = now.Add(maxDebounceDelay)
			}
			deadline := nextDeadline(now, ceiling)
			if timer == nil {
				timer = time.NewTimer(time.Until(deadline))
			} else {
				timer.Reset(time.Until(deadline))
			}
			fire = timer.C
		case <-fire:
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			pending = map[string]bool{}
			ceiling = time.Time{}
			fire = nil
			if err := w.svc.reloadPaths(paths); err != nil {
				log.Printf("config: config reload failed:\n%v", err)
			}
		case <-w.done:
			return
		}
	}
}

// nextDeadline is the debounce deadline for an event at now, never
// past the batch ceiling.
func nextDeadline(now, ceiling time.Time) time.Time {
	deadline := now.Add(debounceDelay)
	if deadline.After(ceiling) {
		return ceiling
	}
	return deadline
}

func (w *watcher) close() {
	close(w.done)
	_ = w.file.Close()
	w.wg.Wait()
}

// reloadPaths applies one flushed batch (reload_and_sync).
func (s *Service) reloadPaths(paths []string) error {
	themes := filepath.Join(s.dir, "themes")
	runtime := s.RuntimePath()
	runtimeTmp := strings.TrimSuffix(runtime, ".toml") + ".tmp"
	var env, theme, runtimeOnly, main bool
	for _, p := range paths {
		switch {
		case isEnvFile(p):
			env = true
		case p == themes || strings.HasPrefix(p, themes+string(filepath.Separator)):
			theme = true
		case p == runtime || p == runtimeTmp:
			runtimeOnly = true
		default:
			main = true
		}
	}
	if env {
		ReloadEnvFiles(s.dir)
		s.signal(s.secretsSubs)
	}
	if theme {
		s.signal(s.themeSubs)
	}
	switch {
	case main:
		return s.reloadMain()
	case runtimeOnly:
		s.reloadRuntime()
	}
	return nil
}
