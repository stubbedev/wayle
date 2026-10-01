package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// File loading with imports, ported from
// crates/wayle-config/src/infrastructure/loading: a config file may
// list `imports = [...]` (paths relative to the importing file, a bare
// name trying .yaml, .yml, then .toml); imports merge in order and the
// importing file wins; import cycles are an error naming the chain.

// LoadError is a whole-file failure: the file (or an import) could not
// be read or parsed. Its message matches the Rust Error display; the
// cause is available through errors.Unwrap.
type LoadError struct {
	msg string
	err error
}

func (e *LoadError) Error() string { return e.msg }
func (e *LoadError) Unwrap() error { return e.err }

// loadTree reads path with its imports resolved into one merged tree
// (Config::load_toml_with_imports). A missing main file is created
// with the Rust stub contents first, so fresh installs have a file to
// edit and to watch.
func loadTree(path string) (any, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := createDefaultConfigFile(path); err != nil {
			return nil, err
		}
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err == nil {
		canonical, err = filepath.Abs(canonical)
	}
	if err != nil {
		return nil, &LoadError{msg: "cannot resolve path", err: err}
	}
	var chain []string
	return loadMerged(canonical, path, &chain)
}

func createDefaultConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // the config dir is user-owned and world-readable, like the Rust create_dir_all
		return &LoadError{msg: "cannot create directory", err: err}
	}
	if err := os.WriteFile(path, []byte("# Wayle configuration file\n"), 0o644); err != nil { //nolint:gosec // config files are not secret; the Rust fs::write default mode
		return &LoadError{msg: "cannot write file", err: err}
	}
	return nil
}

// loadMerged loads the main file; importBase is the path imports
// resolve against (the path as given, not its symlink target).
func loadMerged(path, importBase string, chain *[]string) (any, error) {
	if err := detectCycle(*chain, path); err != nil {
		return nil, err
	}
	*chain = append(*chain, path)
	data, err := os.ReadFile(path) //nolint:gosec // a config path resolved by discovery or an import
	if err != nil {
		return nil, &LoadError{msg: "cannot read file", err: err}
	}
	merged, err := mergeWithImports(data, path, importBase, chain)
	if err != nil {
		return nil, err
	}
	*chain = (*chain)[:len(*chain)-1]
	return merged, nil
}

// mergeWithImports parses one file, loads its imports, then merges the
// file itself on top. The Rust loader reads the import list with a
// first parse, so a syntax error is reported by that parse, without
// the path (TomlParseInline); one parse here yields the same message.
func mergeWithImports(data []byte, path, importBase string, chain *[]string) (any, error) {
	main, err := parseDocument(data, path)
	if err != nil {
		return nil, &LoadError{msg: err.Error(), err: err}
	}
	importPaths := importList(main)
	imported := make([]any, 0, len(importPaths))
	for _, imp := range importPaths {
		resolved := resolveImport(importBase, imp)
		canonical, err := filepath.EvalSymlinks(resolved)
		if err == nil {
			canonical, err = filepath.Abs(canonical)
		}
		if err != nil {
			return nil, &LoadError{
				msg: fmt.Sprintf("cannot import '%s'", resolved),
				err: &LoadError{msg: "cannot resolve path", err: err},
			}
		}
		tree, err := loadImport(canonical, chain)
		if err != nil {
			return nil, err
		}
		imported = append(imported, tree)
	}
	return mergeAll(imported, main), nil
}

func loadImport(path string, chain *[]string) (any, error) {
	if err := detectCycle(*chain, path); err != nil {
		return nil, err
	}
	*chain = append(*chain, path)
	defer func() { *chain = (*chain)[:len(*chain)-1] }()
	data, err := os.ReadFile(path) //nolint:gosec // an import path from the user's own config
	if err != nil {
		return nil, &LoadError{
			msg: fmt.Sprintf("cannot import '%s'", path),
			err: &LoadError{msg: "cannot read file", err: err},
		}
	}
	return mergeWithImports(data, path, path, chain)
}

// detectCycle errors when path is already on the import chain, naming
// the chain by file names: "a.toml -> b.toml -> a.toml".
func detectCycle(chain []string, path string) error {
	for _, p := range chain {
		if p != path {
			continue
		}
		names := make([]string, 0, len(chain)+1)
		for _, c := range chain {
			names = append(names, filepath.Base(c))
		}
		names = append(names, filepath.Base(path))
		return &LoadError{msg: "circular import detected: " + strings.Join(names, " -> ")}
	}
	return nil
}

// importList reads the string entries of a top-level `imports` array;
// non-strings are skipped and a non-array is no imports.
func importList(tree any) []string {
	raw, _ := lookup(tree, "imports")
	list, _ := raw.([]any)
	var out []string
	for _, entry := range list {
		if s, ok := entry.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// resolveImport resolves an import against the importing file's
// directory (an absolute import stands alone, as with Path::join).
// With no extension an existing .yaml, .yml, or .toml sibling is
// taken, in that order, falling back to .toml.
func resolveImport(base, imp string) string {
	dir := filepath.Dir(base)
	join := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	if filepath.Ext(imp) != "" {
		return join(imp)
	}
	for _, ext := range []string{".yaml", ".yml", ".toml"} {
		candidate := join(imp + ext)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return join(imp + ".toml")
}
