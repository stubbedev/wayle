package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// Service is the live configuration: the defaults, the config file
// layer, and the runtime override layer (runtime.toml, written by
// `wayle config set` and the settings GUI), resolved into immutable
// *Config snapshots that subscribers receive on every effective change
// (crates/wayle-config/src/infrastructure/{service,persistence}.rs).
// Snapshots are never mutated; a reload publishes a new one.
type Service struct {
	dir  string
	sink DiagnosticSink

	mu      sync.Mutex
	cfg     *Config
	tree    any    // the merged config document last loaded, nil for none
	staged  *table // the runtime leaves in effect, canonical paths
	subs    map[int]func(old, new *Config)
	nextSub int

	secretsMu   sync.Mutex
	secretsSubs map[int]func()
	themeSubs   map[int]func()
	nextSignal  int

	watcher *watcher
}

// Open loads the user's configuration from the platform directory and
// starts hot reload: config.toml (or .yaml) with its imports on the
// config layer, runtime.toml on the runtime layer. A file-level failure
// logs and keeps the defaults for that layer, as the Rust service does;
// bad values are reported as diagnostics on stderr.
func Open() (*Service, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	s := Load(dir, StderrDiagnostics)
	if err := s.startWatcher(); err != nil {
		return s, err
	}
	return s, nil
}

// Load builds a service over dir without watching it (the CLI's
// one-shot ConfigService::load).
func Load(dir string, sink DiagnosticSink) *Service {
	s := &Service{
		dir:         dir,
		sink:        sink,
		subs:        map[int]func(old, new *Config){},
		secretsSubs: map[int]func(){},
		themeSubs:   map[int]func(){},
		staged:      newTable(),
	}
	LoadEnvFiles(dir)
	tree, err := loadTree(s.MainPath())
	if err != nil {
		log.Printf("config: using defaults, config.toml failed:\n%v", err)
		tree = nil
	}
	s.tree = tree
	runtimeTree, err := readRuntime(s.RuntimePath())
	if err != nil {
		log.Printf("config: runtime.toml failed:\n%v", err)
	}
	cfg, staged, rerr := build(tree, runtimeTree, nil, sink)
	if rerr != nil {
		log.Printf("config: invalid runtime.toml value:\n%v", rerr)
	}
	s.cfg, s.staged = cfg, staged
	return s
}

// MainPath is the discovered main config file.
func (s *Service) MainPath() string { return DiscoverMain(s.dir) }

// RuntimePath is runtime.toml in the config directory.
func (s *Service) RuntimePath() string { return filepath.Join(s.dir, "runtime.toml") }

// Dir is the config directory the service reads.
func (s *Service) Dir() string { return s.dir }

// Config returns the current snapshot. Callers must not mutate it.
func (s *Service) Config() *Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Subscribe registers fn for every change of the effective config; fn
// runs on the reloading goroutine with the previous and new snapshots
// (UI callers hop to their loop). The returned func unsubscribes.
func (s *Service) Subscribe(fn func(old, new *Config)) (cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subs, id)
	}
}

// Watch calls fn with the selected value whenever it changes, the Go
// form of ConfigProperty::watch for one field or section.
func Watch[T any](s *Service, selector func(*Config) T, fn func(T)) (cancel func()) {
	return s.Subscribe(func(old, new *Config) {
		next := selector(new)
		if reflect.DeepEqual(selector(old), next) {
			return
		}
		fn(next)
	})
}

// publish swaps in a new snapshot and notifies the subscribers when
// the effective config changed.
func (s *Service) publish(cfg *Config, staged *table, tree any) {
	s.mu.Lock()
	old := s.cfg
	s.cfg, s.staged, s.tree = cfg, staged, tree
	var subs []func(old, new *Config)
	if !reflect.DeepEqual(old, cfg) {
		for _, fn := range s.subs {
			subs = append(subs, fn)
		}
	}
	s.mu.Unlock()
	for _, fn := range subs {
		fn(old, cfg)
	}
}

// build applies the config tree then the runtime tree onto the
// defaults. warn, when set, holds the runtime overrides active before
// a reload: config leaves they shadow get the "change ignored"
// warning.
func build(configTree, runtimeTree any, warn *table, sink DiagnosticSink) (*Config, *table, error) {
	cfg := Defaults()
	ca := &layerApply{kind: configLayer, sink: sink}
	if warn != nil {
		ca.overridden = func(path string) bool { return stagedAt(warn, path) }
	}
	if configTree != nil {
		_ = ca.applyContainer(valueOf(cfg), configTree, "")
	}
	ra := &layerApply{kind: runtimeLayer, sink: sink, staged: newTable()}
	var err error
	if runtimeTree != nil {
		err = ra.applyContainer(valueOf(cfg), runtimeTree, "")
	}
	return cfg, ra.staged, err
}

// readRuntime parses runtime.toml (always TOML).
func readRuntime(path string) (any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // runtime.toml in the config dir
	if err != nil {
		return nil, &LoadError{msg: "cannot read file", err: err}
	}
	tree, err := parseTOML(data)
	if err != nil {
		return nil, &LoadError{msg: err.Error() + "\n  --> " + path, err: err}
	}
	return tree, nil
}

// Value is the effective config as toml::Value::try_from serializes it,
// in the parsed shape: map[string]any tables (canonical keys), []any
// arrays, string, bool, int64 and float64 leaves.
func (s *Service) Value() any {
	return toPlain(encode(s.Config()))
}

// GetByPath returns the effective value at a dot path, as the config
// serializes (canonical keys; f32 values widened to f64 the way
// toml::Value::try_from widens them).
func (s *Service) GetByPath(path string) (any, error) {
	v := widenFloats(encode(s.Config()))
	for seg := range strings.SplitSeq(path, ".") {
		next, ok := lookup(v, seg)
		if !ok {
			return nil, &InvalidFieldError{Field: seg, Component: path, Reason: FieldNotFound}
		}
		v = next
	}
	return v, nil
}

// SetByPath sets a runtime override at a dot path. A value the field
// rejects is a *RuntimeValueError; an unknown path applies nothing.
func (s *Service) SetByPath(path string, value any) error {
	root := map[string]any{}
	if err := insertPath(root, path, value); err != nil {
		return err
	}
	s.mu.Lock()
	tree, staged := s.tree, cloneTable(s.staged)
	s.mu.Unlock()
	cfg, _, _ := build(tree, toPlain(staged), nil, DiscardDiagnostics)
	ra := &layerApply{kind: runtimeLayer, sink: s.sink, staged: staged}
	if err := ra.applyContainer(valueOf(cfg), root, ""); err != nil {
		return err
	}
	s.publish(cfg, staged, tree)
	return nil
}

// ResetByPath drops the runtime override at a dot path; cleared reports
// whether one existed. The path must name one leaf.
func (s *Service) ResetByPath(path string) (cleared bool, err error) {
	canonical, err := clearRuntimePath(typeOf[Config](), path)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	tree, staged := s.tree, cloneTable(s.staged)
	s.mu.Unlock()
	cleared = stagedAt(staged, canonical)
	deleteStaged(staged, strings.Split(canonical, "."))
	cfg, staged, _ := build(tree, toPlain(staged), nil, DiscardDiagnostics)
	s.publish(cfg, staged, tree)
	return cleared, nil
}

// ResetAllRuntime drops every runtime override and removes
// runtime.toml; a missing file is not an error.
func (s *Service) ResetAllRuntime() error {
	s.mu.Lock()
	tree := s.tree
	s.mu.Unlock()
	cfg, staged, _ := build(tree, nil, nil, DiscardDiagnostics)
	s.publish(cfg, staged, tree)
	err := os.Remove(s.RuntimePath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// RuntimeTOML renders the runtime overrides as runtime.toml content.
func (s *Service) RuntimeTOML() string {
	s.mu.Lock()
	staged := s.staged
	s.mu.Unlock()
	out, _ := tomlPretty(widenFloats(staged))
	return out
}

// Save persists the runtime overrides to runtime.toml atomically:
// write runtime.tmp, fsync, rename over, fsync the directory.
func (s *Service) Save() error {
	content := s.RuntimeTOML()
	path := s.RuntimePath()
	tmp := strings.TrimSuffix(path, filepath.Ext(path)) + ".tmp"
	persistErr := func(p string, err error) error {
		return &LoadError{msg: fmt.Sprintf("cannot persist config to '%s'", p), err: err}
	}
	f, err := os.Create(tmp) //nolint:gosec // runtime.tmp in the config dir
	if err != nil {
		return persistErr(tmp, err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return persistErr(tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return persistErr(tmp, err)
	}
	if err := f.Close(); err != nil {
		return persistErr(tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return persistErr(path, err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return persistErr(filepath.Dir(path), err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return persistErr(filepath.Dir(path), err)
	}
	return nil
}

// reloadMain re-reads the main config and runtime.toml after a file
// change (watcher.rs reload_main_config). A broken main file leaves
// everything as it was; an unreadable runtime.toml drops the runtime
// layer.
func (s *Service) reloadMain() error {
	tree, err := loadTree(s.MainPath())
	if err != nil {
		return err
	}
	s.mu.Lock()
	previous := s.staged
	s.mu.Unlock()
	runtimeTree, _ := readRuntime(s.RuntimePath())
	cfg, staged, _ := build(tree, runtimeTree, previous, s.sink)
	s.publish(cfg, staged, tree)
	return nil
}

// reloadRuntime re-reads only runtime.toml; an unreadable file keeps
// the current overrides (reload_runtime_only).
func (s *Service) reloadRuntime() {
	runtimeTree, err := readRuntime(s.RuntimePath())
	if err != nil {
		return
	}
	s.mu.Lock()
	tree := s.tree
	s.mu.Unlock()
	cfg, staged, _ := build(tree, runtimeTree, nil, s.sink)
	s.publish(cfg, staged, tree)
}

// SubscribeSecrets registers fn for every reload of the .env files.
func (s *Service) SubscribeSecrets(fn func()) (cancel func()) {
	return s.subscribeSignal(s.secretsSubs, fn)
}

// SubscribeThemes registers fn for every change under themes/.
func (s *Service) SubscribeThemes(fn func()) (cancel func()) {
	return s.subscribeSignal(s.themeSubs, fn)
}

func (s *Service) subscribeSignal(subs map[int]func(), fn func()) func() {
	s.secretsMu.Lock()
	defer s.secretsMu.Unlock()
	id := s.nextSignal
	s.nextSignal++
	subs[id] = fn
	return func() {
		s.secretsMu.Lock()
		defer s.secretsMu.Unlock()
		delete(subs, id)
	}
}

func (s *Service) signal(subs map[int]func()) {
	s.secretsMu.Lock()
	fns := make([]func(), 0, len(subs))
	for _, fn := range subs {
		fns = append(fns, fn)
	}
	s.secretsMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Close stops hot reload.
func (s *Service) Close() {
	if s.watcher != nil {
		s.watcher.close()
	}
}
