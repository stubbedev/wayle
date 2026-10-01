package icons

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/stubbedev/wayle/internal/icons/usvg"
)

// This file ports manager.rs: install from a CDN, remove, list, and
// import local SVGs.

// dirPerm and filePerm are what Rust's create_dir_all and fs::write
// request; the umask trims them the same way.
const (
	dirPerm  os.FileMode = 0o777
	filePerm os.FileMode = 0o666
)

// InstallFailure is one icon that did not install.
type InstallFailure struct {
	Slug  string
	Error string
}

// InstallResult is a batch outcome.
type InstallResult struct {
	Installed []string
	Failed    []InstallFailure
}

// Manager installs and removes icons under a registry.
type Manager struct {
	registry *Registry
	client   *http.Client
}

// NewManager uses the default registry and HTTP client.
func NewManager() (*Manager, error) {
	r, err := NewRegistry()
	if err != nil {
		return nil, err
	}
	return &Manager{registry: r, client: http.DefaultClient}, nil
}

// NewManagerWith uses the given registry and client.
func NewManagerWith(r *Registry, client *http.Client) *Manager {
	return &Manager{registry: r, client: client}
}

// Registry is the manager's registry.
func (m *Manager) Registry() *Registry { return m.registry }

// Install fetches slugs from source concurrently; per-icon failures are
// collected, and only an uncreatable icons directory is an error.
func (m *Manager) Install(ctx context.Context, source Source, slugs []string) (InstallResult, error) {
	dir := m.registry.IconsDir()
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return InstallResult{}, &DirectoryError{Path: dir, Err: err}
	}
	type outcome struct {
		slug, name string
		err        error
	}
	outcomes := make([]outcome, len(slugs))
	var wg sync.WaitGroup
	for i, slug := range slugs {
		wg.Go(func() {
			name, err := m.fetchAndSave(ctx, source, slug, dir)
			outcomes[i] = outcome{slug, name, err}
		})
	}
	wg.Wait()
	var result InstallResult
	for _, o := range outcomes {
		if o.err != nil {
			result.Failed = append(result.Failed, InstallFailure{Slug: o.slug, Error: o.err.Error()})
			continue
		}
		result.Installed = append(result.Installed, o.name)
	}
	return result, nil
}

func (m *Manager) fetchAndSave(ctx context.Context, source Source, slug, dir string) (string, error) {
	url := source.CDNURL(slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", &HTTPError{Detail: "builder error"}
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", &HTTPError{Detail: "error sending request for url (" + url + ")"}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", &FetchError{Slug: slug, Source: "cdn", Status: resp.StatusCode}
	}
	if err := validateIconName(source.IconName(slug)); err != nil {
		return "", err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &HTTPError{Detail: "error decoding response body"}
	}
	content := strings.ToValidUTF8(string(body), "�")
	if _, err := usvg.Parse(content); err != nil {
		return "", &InvalidSVGError{Slug: slug, Reason: err.Error()}
	}
	transformed, ok := ToSymbolic(content)
	if !ok {
		return "", &InvalidSVGError{Slug: slug, Reason: reasonNoPaths}
	}
	name := source.InstalledName(slug)
	path := filepath.Join(dir, name+".svg")
	if err := os.WriteFile(path, []byte(transformed), filePerm); err != nil {
		return "", &WriteError{Path: path, Err: err}
	}
	return name, nil
}

// Remove deletes <name>.svg from the icons directory.
func (m *Manager) Remove(name string) error {
	path := filepath.Join(m.registry.IconsDir(), name+".svg")
	if !exists(path) {
		return &NotFoundError{Name: name}
	}
	if err := os.Remove(path); err != nil {
		return &DeleteError{Name: name, Err: err}
	}
	return nil
}

// List is every installed icon name, user and system, sorted.
func (m *Manager) List() []string {
	dirs := []string{m.registry.IconsDir()}
	for _, sys := range SystemIconPaths() {
		actions := filepath.Join(sys, "hicolor", "scalable", "actions")
		if exists(actions) {
			dirs = append(dirs, actions)
		}
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if stem, ok := svgStem(e.Name()); ok {
				seen[stem] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// svgStem is Path::file_stem for a file whose extension is exactly
// "svg" (a leading-dot name like ".svg" has no extension).
func svgStem(name string) (string, bool) {
	ext := filepath.Ext(name)
	if ext != ".svg" || name == ext {
		return "", false
	}
	return strings.TrimSuffix(name, ext), true
}

// IsInstalled reports <name>.svg in the icons directory.
func (m *Manager) IsInstalled(name string) bool {
	return exists(filepath.Join(m.registry.IconsDir(), name+".svg"))
}

// readUTF8 is fs::read_to_string: invalid UTF-8 is an error.
func readUTF8(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the user names the file to import
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errInvalidUTF8
	}
	return string(data), nil
}

type invalidDataError struct{}

func (invalidDataError) Error() string { return "stream did not contain valid UTF-8" }

var errInvalidUTF8 error = invalidDataError{}

// ImportLocal imports one SVG as cm-<name>-symbolic.
func (m *Manager) ImportLocal(path, name string) (string, error) {
	if err := validateIconName(name); err != nil {
		return "", err
	}
	if !exists(path) {
		return "", &NotFoundError{Name: path}
	}
	content, err := readUTF8(path)
	if err != nil {
		return "", &ReadError{Path: path, Err: err}
	}
	if _, err := usvg.Parse(content); err != nil {
		return "", &InvalidSVGError{Slug: name, Reason: err.Error()}
	}
	dir := m.registry.IconsDir()
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", &DirectoryError{Path: dir, Err: err}
	}
	transformed, ok := ToSymbolic(content)
	if !ok {
		return "", &InvalidSVGError{Slug: name, Reason: reasonNoPaths}
	}
	iconName := CustomPrefix + "-" + name + "-symbolic"
	dest := filepath.Join(dir, iconName+".svg")
	if err := os.WriteFile(dest, []byte(transformed), filePerm); err != nil {
		return "", &WriteError{Path: dest, Err: err}
	}
	return iconName, nil
}

// ImportDir imports every *.svg in dir, keeping names that carry a
// known prefix and adding cm- to the rest.
func (m *Manager) ImportDir(dir string) (InstallResult, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return InstallResult{}, &NotFoundError{Name: dir}
	}
	icons := m.registry.IconsDir()
	if err := os.MkdirAll(icons, dirPerm); err != nil {
		return InstallResult{}, &DirectoryError{Path: icons, Err: err}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return InstallResult{}, &ReadError{Path: dir, Err: err}
	}
	prefixes := AllPrefixes()
	var result InstallResult
	for _, e := range entries {
		if _, ok := svgStem(e.Name()); !ok {
			continue
		}
		path := filepath.Join(dir, e.Name())
		name, err := importSingleFile(path, icons, prefixes)
		if err != nil {
			result.Failed = append(result.Failed, InstallFailure{Slug: e.Name(), Error: err.Error()})
			continue
		}
		result.Installed = append(result.Installed, name)
	}
	return result, nil
}

func importSingleFile(path, icons string, prefixes []string) (string, error) {
	stem, ok := svgStem(filepath.Base(path))
	if !ok {
		return "", &NotFoundError{Name: path}
	}
	base := strings.TrimSuffix(stem, "-symbolic")
	hasPrefix := slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(base, p+"-") })
	if err := validateIconName(base); err != nil {
		return "", err
	}
	iconName := CustomPrefix + "-" + base + "-symbolic"
	if hasPrefix {
		iconName = base + "-symbolic"
	}
	content, err := readUTF8(path)
	if err != nil {
		return "", &ReadError{Path: path, Err: err}
	}
	if _, err := usvg.Parse(content); err != nil {
		return "", &InvalidSVGError{Slug: base, Reason: err.Error()}
	}
	transformed, ok := ToSymbolic(content)
	if !ok {
		return "", &InvalidSVGError{Slug: base, Reason: reasonNoPaths}
	}
	dest := filepath.Join(icons, iconName+".svg")
	if err := os.WriteFile(dest, []byte(transformed), filePerm); err != nil {
		return "", &WriteError{Path: dest, Err: err}
	}
	return iconName, nil
}

func validateIconName(name string) error {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return &InvalidIconNameError{Name: name}
	}
	return nil
}
