package configdocs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/wayle/config"
)

// Generate writes every page under dir: one per section (bar modules
// in modules/), types.md, and index.md (generate_all).
func Generate(dir string) error {
	sections := config.DocSections()
	known := knownTypes(sections)
	for _, sec := range sections {
		if err := writeSection(dir, sec, known); err != nil {
			return err
		}
	}
	if err := writePage(filepath.Join(dir, "types.md"), typesPage(collectTypeDefs(sections))); err != nil {
		return err
	}
	return writePage(filepath.Join(dir, "index.md"), indexPage(sections))
}

// GenerateOne writes the named section's page only
// (generate_module_by_name).
func GenerateOne(dir, name string) error {
	sections := config.DocSections()
	for _, sec := range sections {
		if sec.Name == name {
			return writeSection(dir, sec, knownTypes(sections))
		}
	}
	return fmt.Errorf("module `%s` not registered", name)
}

func knownTypes(sections []config.DocSection) map[string]bool {
	known := map[string]bool{}
	for n := range collectTypeDefs(sections) {
		known[n] = true
	}
	return known
}

func writeSection(dir string, sec config.DocSection, known map[string]bool) error {
	target := dir
	if sec.LayoutID != "" {
		target = filepath.Join(dir, "modules")
	}
	return writePage(filepath.Join(target, sec.Name+".md"), modulePage(sec, known))
}

func writePage(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // the docs tree
		return fmt.Errorf("cannot write `%s`: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // the docs tree
		return fmt.Errorf("cannot write `%s`: %w", path, err)
	}
	return nil
}

// indexPage is render_config_index.
func indexPage(sections []config.DocSection) string {
	var b strings.Builder
	b.WriteString("---\ntitle: Config reference\noutline: [2]\n---\n\n<div v-pre>\n\n# Config reference\n\n")
	b.WriteString("Every config file lives at `~/.config/wayle/config.toml`. Each page below covers one section. Every field has a default; start with an empty file and add only what you want to change.\n\n")
	b.WriteString("::: tip\nEditor intellisense via JSON Schema. Install [Tombi](https://marketplace.visualstudio.com/items?itemName=tombi-toml.tombi) for VSCode or the `tombi` LSP for Neovim, Helix, or Zed. The schema is written to `~/.config/wayle/schema.json` on startup.\n:::\n\n")
	b.WriteString("## Top-level sections\n\n| Section | What it controls |\n|---|---|\n")
	for _, sec := range sections {
		if sec.LayoutID == "" {
			fmt.Fprintf(&b, "| [`%s`](/config/%s) | %s |\n", sec.Name, sec.Name, hook(sec))
		}
	}
	b.WriteString("\n## Bar modules\n\nModules appear inside `[[bar.layout]]` arrays. Each row links to the full reference.\n\n")
	b.WriteString("| Module | Purpose |\n|---|---|\n")
	for _, sec := range sections {
		if sec.LayoutID != "" {
			fmt.Fprintf(&b, "| [`%s`](/config/modules/%s) | %s |\n", sec.Name, sec.Name, hook(sec))
		}
	}
	b.WriteString("\n## Shared types\n\nEvery named type referenced across the config (`Color`, `ClickAction`, `Size`, and others) is documented on the [types page](/config/types).\n")
	b.WriteString("\n</div>\n")
	return b.String()
}

// hook is module_hook: the description's first non-blank line.
func hook(sec config.DocSection) string {
	desc, _ := sec.Schema["description"].(string)
	for _, l := range lines(desc) {
		if strings.TrimSpace(l) != "" {
			return strings.TrimSpace(l)
		}
	}
	return ""
}
