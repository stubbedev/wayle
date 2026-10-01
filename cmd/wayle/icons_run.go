package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/icons"
	"github.com/stubbedev/wayle/resources"
)

// newIconManager is the handlers' IconManager::new (a variable so tests
// can point it at a fake CDN).
var newIconManager = icons.NewManager

// cliError is an error whose text is printed verbatim after "Error: ".
type cliError string

func (e cliError) Error() string { return string(e) }

// spawnError renders a failed spawn like Rust's io::Error: a missing
// binary is ENOENT.
func spawnError(err error) string {
	if errors.Is(err, exec.ErrNotFound) {
		return "No such file or directory (os error 2)"
	}
	return icons.IOErrorString(err)
}

// iconsSetup is icons/setup.rs: copy the bundled icons into the icons
// directory. The icons are embedded in the binary, where the Rust CLI
// reads them from its build-time source path.
func iconsSetup(m *cli.Matches) error {
	registry, err := icons.NewRegistry()
	if err != nil {
		return err
	}
	dest := registry.IconsDir()
	if err := os.MkdirAll(dest, 0o777); err != nil { //nolint:gosec // create_dir_all's mode, trimmed by the umask
		return cliError("Failed to create icons directory: " + icons.IOErrorString(err))
	}
	entries, err := fs.ReadDir(resources.Icons, resources.IconsDir)
	if err != nil {
		return cliError("Failed to read resources directory: " + err.Error())
	}
	count := 0
	for _, e := range entries {
		if path.Ext(e.Name()) != ".svg" {
			continue
		}
		src := path.Join(resources.IconsDir, e.Name())
		data, err := resources.Icons.ReadFile(src)
		if err == nil {
			err = os.WriteFile(filepath.Join(dest, e.Name()), data, 0o666) //nolint:gosec // fs::copy's mode, trimmed by the umask
		}
		if err != nil {
			return cliError(fmt.Sprintf("Failed to copy resources/%s: %s", src, icons.IOErrorString(err)))
		}
		fmt.Fprintf(m.Stdout(), "Installed: %s\n", strings.TrimSuffix(e.Name(), ".svg"))
		count++
	}
	fmt.Fprintf(m.Stdout(), "\n%d icons installed to %s\n", count, dest)
	return nil
}

// iconsInstall is icons/install.rs.
func iconsInstall(m *cli.Matches) error {
	name, _ := cli.Value[string](m, "source")
	source, err := icons.SourceByCLIName(name)
	if err != nil {
		return err
	}
	manager, err := newIconManager()
	if err != nil {
		return err
	}
	result, err := manager.Install(context.Background(), source, cli.Values[string](m, "slugs"))
	if err != nil {
		return err
	}
	for _, n := range result.Installed {
		fmt.Fprintf(m.Stdout(), "Installed: %s\n", n)
	}
	for _, f := range result.Failed {
		fmt.Fprintf(m.Stderr(), "Failed: %s - %s\n", f.Slug, f.Error)
	}
	if len(result.Failed) > 0 {
		fmt.Fprintf(m.Stderr(), "\n%d installed, %d failed\n", len(result.Installed), len(result.Failed))
	}
	return nil
}

// iconsImport is icons/import.rs.
func iconsImport(m *cli.Matches) error {
	src, _ := cli.Value[string](m, "path")
	manager, err := newIconManager()
	if err != nil {
		return err
	}
	if st, err := os.Stat(src); err == nil && st.IsDir() {
		result, err := manager.ImportDir(src)
		if err != nil {
			return err
		}
		if len(result.Installed) == 0 && len(result.Failed) == 0 {
			fmt.Fprintf(m.Stdout(), "No SVG files found in %s\n", src)
			return nil
		}
		for _, n := range result.Installed {
			fmt.Fprintf(m.Stdout(), "Imported: %s\n", n)
		}
		for _, f := range result.Failed {
			fmt.Fprintf(m.Stderr(), "Failed %s: %s\n", f.Slug, f.Error)
		}
		fmt.Fprintf(m.Stdout(), "\nImported %d icons (%d failed)\n", len(result.Installed), len(result.Failed))
		if len(result.Installed) == 0 {
			return cliError("All imports failed")
		}
		return nil
	}
	name, ok := cli.Value[string](m, "name")
	if !ok {
		return cliError("Name required when importing a single file")
	}
	iconName, err := manager.ImportLocal(src, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Imported: %s\n", iconName)
	return nil
}

// iconsRemove is icons/remove.rs: stop at the first failure.
func iconsRemove(m *cli.Matches) error {
	manager, err := newIconManager()
	if err != nil {
		return err
	}
	for _, name := range cli.Values[string](m, "names") {
		if err := manager.Remove(name); err != nil {
			return err
		}
		fmt.Fprintf(m.Stdout(), "Removed: %s\n", name)
	}
	return nil
}

// iconsSources is icons/sources.rs.
func iconsSources(m *cli.Matches) error {
	out := m.Stdout()
	fmt.Fprint(out, "\nAvailable icon sources:\n\n")
	for _, s := range icons.Sources {
		fmt.Fprintf(out, "  %-16s %-6s %s\n", s.CLIName, s.Prefix+"-", s.Description)
		fmt.Fprintf(out, "  %-16s        %s\n\n", "", s.Website)
	}
	return nil
}

// iconsList is icons/list.rs.
func iconsList(m *cli.Matches) error {
	manager, err := newIconManager()
	if err != nil {
		return err
	}
	all := manager.List()
	prefix, filtered := cli.Value[string](m, "source")
	var names []string
	for _, n := range all {
		if !filtered || strings.HasPrefix(n, prefix+"-") {
			names = append(names, n)
		}
	}
	out := m.Stdout()
	if len(names) == 0 {
		if filtered {
			fmt.Fprintf(out, "No icons installed with prefix '%s-'\n", prefix)
		} else {
			fmt.Fprintln(out, "No icons installed")
		}
		return nil
	}
	if m.Flag("interactive") {
		return runFzf(out, m.Stderr(), names)
	}
	fmt.Fprintf(out, "\nInstalled icons (%d):\n\n", len(names))
	for _, n := range names {
		fmt.Fprintf(out, "  %s\n", n)
	}
	fmt.Fprintln(out)
	return nil
}

// runFzf is list.rs's run_fzf: pick one name, copy it with wl-copy.
func runFzf(out, errOut io.Writer, names []string) error {
	cmd := exec.Command("fzf", "--prompt", "Search icons: ")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return cliError(err.Error())
	}
	if err := cmd.Start(); err != nil {
		return cliError("fzf not found. Install fzf or use without -i flag.")
	}
	for _, n := range names {
		_, _ = fmt.Fprintln(stdin, n)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return nil
		}
		return cliError(icons.IOErrorString(err))
	}
	selected := strings.TrimSpace(stdout.String())
	if selected == "" {
		return nil
	}
	if err := copyToClipboard(selected); err != nil {
		fmt.Fprintf(errOut, "Failed to copy to clipboard: %s\n", err)
		return nil
	}
	fmt.Fprintf(out, "Copied to clipboard: %s\n", selected)
	return nil
}

func copyToClipboard(text string) error {
	cmd := exec.Command("wl-copy")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return cliError(err.Error())
	}
	if err := cmd.Start(); err != nil {
		return cliError("wl-copy not found. Install wl-clipboard.")
	}
	if _, err := io.WriteString(stdin, text); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return cliError("Failed to write to wl-copy: " + icons.IOErrorString(err))
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return cliError("wl-copy failed: " + stderr.String())
		}
		return cliError(icons.IOErrorString(err))
	}
	return nil
}

// iconsOpen is icons/open.rs: set the directory up, hand it to xdg-open
// without waiting.
func iconsOpen(m *cli.Matches) error {
	registry, err := icons.NewRegistry()
	if err != nil {
		return err
	}
	if err := registry.EnsureSetup(); err != nil {
		return err
	}
	base := registry.BasePath()
	cmd := exec.Command("xdg-open", base) //nolint:gosec // the icons directory
	if err := cmd.Start(); err != nil {
		return cliError("Failed to run xdg-open: " + spawnError(err))
	}
	_ = cmd.Process.Release()
	fmt.Fprintf(m.Stdout(), "Opened: %s\n", base)
	return nil
}

// iconsExport is icons/export.rs. Like Rust, it lists system icons too
// but copies from the user directory, so those report a failed copy.
func iconsExport(m *cli.Matches) error {
	dest, _ := cli.Value[string](m, "destination")
	manager, err := newIconManager()
	if err != nil {
		return err
	}
	names := manager.List()
	if len(names) == 0 {
		fmt.Fprintln(m.Stdout(), "No icons installed")
		return nil
	}
	if err := os.MkdirAll(dest, 0o777); err != nil { //nolint:gosec // create_dir_all's mode, trimmed by the umask
		return cliError(fmt.Sprintf("cannot create %s: %s", dest, icons.IOErrorString(err)))
	}
	src := manager.Registry().IconsDir()
	copied := 0
	for _, n := range names {
		if err := copyFile(filepath.Join(src, n+".svg"), filepath.Join(dest, n+".svg")); err != nil {
			fmt.Fprintf(m.Stderr(), "Failed to copy %s: %s\n", n, icons.IOErrorString(err))
			continue
		}
		copied++
	}
	fmt.Fprintf(m.Stdout(), "Exported %d icons to %s\n", copied, dest)
	return nil
}

// copyFile is fs::copy: contents and permission bits.
func copyFile(from, to string) error {
	in, err := os.Open(from) //nolint:gosec // inside the icons directory
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, st.Mode().Perm()) //nolint:gosec // the user's export destination
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(st.Mode().Perm()); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// iconsSync is icons/sync.rs.
func iconsSync(m *cli.Matches) error {
	referenced, err := referencedIcons()
	if err != nil {
		return err
	}
	manager, err := newIconManager()
	if err != nil {
		return err
	}
	installed := map[string]bool{}
	for _, n := range manager.List() {
		installed[n] = true
	}
	missing := icons.FindMissing(referenced, installed)
	out := m.Stdout()
	if len(missing) == 0 {
		fmt.Fprintf(out, "All %d referenced icons already installed.\n", len(referenced))
		return nil
	}
	if m.Flag("dry_run") {
		fmt.Fprintf(out, "Would install %d icons:\n", len(missing))
		for _, icon := range missing {
			if icon.Source == "" {
				fmt.Fprintf(out, "  %s (no auto-install: import manually)\n", icon.Name)
			} else {
				fmt.Fprintf(out, "  %s (from %s)\n", icon.Name, icon.Source)
			}
		}
		return nil
	}
	summary := icons.InstallMissing(context.Background(), missing, manager)
	for _, n := range summary.Installed {
		fmt.Fprintf(out, "Installed: %s\n", n)
	}
	for _, s := range summary.Skipped {
		fmt.Fprintf(out, "Skipped (manual import required): %s\n", s)
	}
	for _, f := range summary.Failed {
		fmt.Fprintf(m.Stderr(), "Failed: %s - %s\n", f.Name, f.Error)
	}
	fmt.Fprintf(out, "\n%d installed, %d skipped, %d failed\n", len(summary.Installed), len(summary.Skipped), len(summary.Failed))
	return nil
}

// referencedIcons is the icon names the effective config mentions (sync.rs:
// every string leaf of the serialized config).
func referencedIcons() (map[string]bool, error) {
	svc, err := loadUserConfig()
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	icons.ScanValue(svc.Value(), referenced)
	return referenced, nil
}
