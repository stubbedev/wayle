// Package extract runs the external color extractor that feeds
// theming: wallust, matugen, or pywal over the theming monitor's
// wallpaper (crates/wayle-wallpaper/src/types/color_extractor).
//
// wayle does not compute palettes itself. It invokes the configured
// tool with exactly the arguments (and, for wallust, exactly the
// generated config and template) the Rust shell does, so the tool
// writes the same palette to the same cache file:
//
//	matugen -> MatugenColorsPath (the tool's --json stdout, saved by wayle)
//	wallust -> WallustColorsPath (written by wallust through the template)
//	pywal   -> PywalColorsPath   (pywal's own ~/.cache/wal/colors.json)
//
// Theme providers read those files (crates/wayle-styling
// palette_provider) and re-read them whenever the wallpaper service
// reports a finished extraction (wallpaper.Service.Extracted and the
// ColorsExtracted D-Bus signal).
package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/config"
)

// matugenMaxSourceColor clamps --source-color-index (MATUGEN_MAX_SOURCE_COLOR).
const matugenMaxSourceColor = 3

// Config is the tool and its parameters (ColorExtractorConfig). The
// ranged fields hold values the config loader already clamped to the
// schema ranges; MatugenSourceColor is clamped to 0-3 here, as the
// Rust extract does.
type Config struct {
	Tool Tool

	MatugenScheme      MatugenScheme
	MatugenContrast    float64 // -1.0 to 1.0
	MatugenSourceColor uint8
	MatugenLight       bool

	WallustPalette       WallustPalette
	WallustSaturation    uint8 // 0-100, 0 disables the boost
	WallustCheckContrast bool
	WallustBackend       WallustBackend
	WallustColorspace    WallustColorspace
	WallustApplyGlobally bool

	PywalSaturation    float64 // 0.0-1.0
	PywalContrast      float64 // 1.0-21.0
	PywalLight         bool
	PywalApplyGlobally bool
}

// DefaultConfig is ColorExtractorConfig::default().
func DefaultConfig() Config {
	return Config{
		Tool:                 Wallust,
		MatugenScheme:        config.MatugenSchemeTonalSpot,
		WallustPalette:       config.WallustPaletteDark16,
		WallustCheckContrast: true,
		WallustBackend:       config.WallustBackendFastresize,
		WallustColorspace:    config.WallustColorspaceLabmixed,
		WallustApplyGlobally: true,
		PywalSaturation:      0.05,
		PywalContrast:        3.0,
		PywalApplyGlobally:   true,
	}
}

// CommandError is a tool that could not be started (not installed, not
// executable): ColorExtractionCommandFailed.
type CommandError struct {
	Tool Tool
	Err  error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("cannot execute color extractor `%s`", e.Tool)
}

func (e *CommandError) Unwrap() error { return e.Err }

// FailedError is a tool that exited unsuccessfully; Stderr is its
// trimmed standard error (ColorExtractionFailed).
type FailedError struct {
	Tool   Tool
	Stderr string
}

func (e *FailedError) Error() string {
	return fmt.Sprintf("color extractor `%s` failed: %s", e.Tool, e.Stderr)
}

// PathError is a wayle data/cache path that could not be resolved or
// written (ConfigPathError).
type PathError struct {
	Context string
	Err     error
}

func (e *PathError) Error() string { return "cannot access " + e.Context }

func (e *PathError) Unwrap() error { return e.Err }

// Extract runs the configured tool over image and leaves the palette
// in the tool's cache file. None is a no-op.
func Extract(ctx context.Context, cfg Config, image string) error {
	switch cfg.Tool {
	case Wallust:
		return extractWallust(ctx, cfg, image)
	case Matugen:
		return extractMatugen(ctx, cfg, image)
	case Pywal:
		return run(ctx, Pywal, "wal", pywalArgs(cfg, image))
	case None:
		return nil
	}
	return fmt.Errorf("extract: unknown tool %v", cfg.Tool)
}

// formatFloat renders like Rust's f64 Display: shortest round-trip
// digits, never an exponent ("3", "0.05", "-0.25").
func formatFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// run executes one tool and returns its stdout; failures map to the
// Rust error variants.
func run(ctx context.Context, tool Tool, program string, args []string) error {
	_, err := output(ctx, tool, program, args)
	return err
}

func output(ctx context.Context, tool Tool, program string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...) //nolint:gosec // program is one of the three fixed tool names
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.Bytes(), nil
	case errors.As(err, &exitErr):
		return nil, &FailedError{Tool: tool, Stderr: strings.TrimSpace(stderr.String())}
	default:
		return nil, &CommandError{Tool: tool, Err: err}
	}
}

// pywalArgs is pywal::extract's argument list.
func pywalArgs(cfg Config, image string) []string {
	args := []string{
		"-i", image,
		"-n",
		"--saturate", formatFloat(cfg.PywalSaturation),
		"--contrast", formatFloat(cfg.PywalContrast),
	}
	if cfg.PywalLight {
		args = append(args, "-l")
	}
	if !cfg.PywalApplyGlobally {
		args = append(args, "-s", "-t", "-e")
	}
	return args
}

// matugenArgs is matugen::extract's argument list.
func matugenArgs(cfg Config, image string) []string {
	mode := "dark"
	if cfg.MatugenLight {
		mode = "light"
	}
	return []string{
		"image", image,
		"--json", "hex",
		"--source-color-index", strconv.Itoa(int(min(cfg.MatugenSourceColor, matugenMaxSourceColor))),
		"--type", cfg.MatugenScheme.CLIValue(),
		"--contrast", formatFloat(cfg.MatugenContrast),
		"-m", mode,
	}
}

// extractMatugen runs matugen and saves its JSON for the matugen theme
// provider. A cache that cannot be written is logged, not returned:
// the extraction itself succeeded (matugen::save_output).
func extractMatugen(ctx context.Context, cfg Config, image string) error {
	stdout, err := output(ctx, Matugen, "matugen", matugenArgs(cfg, image))
	if err != nil {
		return err
	}
	path, err := MatugenColorsPath()
	if err != nil {
		log.Printf("wallpaper: cannot get matugen cache path: %v", err)
		return nil
	}
	if err := os.WriteFile(path, stdout, 0o644); err != nil { //nolint:gosec // a palette cache other tools read
		log.Printf("wallpaper: cannot save matugen colors to %s: %v", path, err)
	}
	return nil
}

// wallustTemplate is COLORS_TEMPLATE: the colors.json template wallust
// renders into WallustColorsPath.
const wallustTemplate = `{
    "wallpaper": "{{wallpaper}}",
    "background": "{{background}}",
    "foreground": "{{foreground}}",
    "cursor": "{{cursor}}",
    "color0": "{{color0}}",
    "color1": "{{color1}}",
    "color2": "{{color2}}",
    "color3": "{{color3}}",
    "color4": "{{color4}}",
    "color5": "{{color5}}",
    "color6": "{{color6}}",
    "color7": "{{color7}}",
    "color8": "{{color8}}",
    "color9": "{{color9}}",
    "color10": "{{color10}}",
    "color11": "{{color11}}",
    "color12": "{{color12}}",
    "color13": "{{color13}}",
    "color14": "{{color14}}",
    "color15": "{{color15}}"
}
`

// wallustConfig renders wayle's wallust.toml, byte for byte the Rust
// write_config format.
func wallustConfig(cfg Config, colorsOutput string) string {
	saturation := ""
	if cfg.WallustSaturation > 0 {
		saturation = fmt.Sprintf("saturation = %d\n", cfg.WallustSaturation)
	}
	return fmt.Sprintf(`palette = "%s"
backend = "%s"
color_space = "%s"
check_contrast = %t
dynamic_threshold = true
%s
[templates]
colors = { template = "colors.json", target = "%s" }
`, cfg.WallustPalette, cfg.WallustBackend, cfg.WallustColorspace, cfg.WallustCheckContrast, saturation, colorsOutput)
}

// extractWallust writes the generated config and template under the
// wayle data dir, then runs a single wallust extraction against them.
func extractWallust(ctx context.Context, cfg Config, image string) error {
	dataDir, err := dataDir()
	if err != nil {
		return &PathError{Context: "wayle data directory", Err: err}
	}
	wallustDir := filepath.Join(dataDir, "wallust")
	templatesDir := filepath.Join(wallustDir, "templates")
	configPath := filepath.Join(wallustDir, "wallust.toml")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil { //nolint:gosec // matches create_dir_all's default mode
		return &PathError{Context: "creating wallust templates directory", Err: err}
	}
	colorsOutput, err := WallustColorsPath()
	if err != nil {
		return &PathError{Context: "wallust colors output path", Err: err}
	}
	if err := os.WriteFile(configPath, []byte(wallustConfig(cfg, colorsOutput)), 0o644); err != nil { //nolint:gosec // a config wallust reads
		return &PathError{Context: "writing wayle wallust config", Err: err}
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "colors.json"), []byte(wallustTemplate), 0o644); err != nil { //nolint:gosec // a template wallust reads
		return &PathError{Context: "writing colors.json template", Err: err}
	}
	args := []string{"run", image, "-C", configPath, "--templates-dir", templatesDir}
	if !cfg.WallustApplyGlobally {
		args = append(args, "-s")
	}
	return run(ctx, Wallust, "wallust", args)
}
