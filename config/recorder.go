package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// Recorder output formats.
const (
	RecorderMkv = "mkv"
	RecorderMp4 = "mp4"
)

// Schema icon defaults (RecorderConfig's state trio).
const (
	defaultRecIdleIcon      = "ld-video-symbolic"
	defaultRecRecordingIcon = "ld-circle-dot-symbolic"
	defaultRecPausedIcon    = "ld-circle-pause-symbolic"
)

// RecorderConfig is the recorder module configuration.
type RecorderConfig struct {
	Click            ClickConfig
	Format           string
	LabelShow        bool
	Framerate        int
	Microphone       bool
	MicrophoneDevice string
	SystemAudio      bool
	ShowCursor       bool
	StartDelayMS     int
	OutputDirectory  string
	OutputFormat     string
	Icon             IconConfig
	Icons            map[string]IconConfig
	Colors           map[string]ColorValue
}

// Icon keys.
const (
	RecorderIdle      = "idle"
	RecorderRecording = "recording"
	RecorderPaused    = "paused"
)

// DefaultsRecorder returns the schema defaults.
func DefaultsRecorder() RecorderConfig {
	return RecorderConfig{
		Format:           "{{ elapsed }}",
		LabelShow:        true,
		Framerate:        60,
		Microphone:       false,
		MicrophoneDevice: "",
		SystemAudio:      true,
		ShowCursor:       true,
		StartDelayMS:     1400,
		OutputDirectory:  "",
		OutputFormat:     RecorderMkv,
		Icon:             DefaultsIcon(true, defaultRecIdleIcon),
		Icons: map[string]IconConfig{
			RecorderIdle:      DefaultsIcon(true, defaultRecIdleIcon),
			RecorderRecording: DefaultsIcon(true, defaultRecRecordingIcon),
			RecorderPaused:    DefaultsIcon(true, defaultRecPausedIcon),
		},
		Colors: map[string]ColorValue{},
		Click:  DefaultsClick(map[string]string{"left-click": "wayle recorder toggle", "right-click": "dropdown:recorder"}),
	}
}

// applyRecorder overlays [modules.recorder].
func applyRecorder(md toml.MetaData, prim toml.Primitive) (RecorderConfig, error) {
	cfg := DefaultsRecorder()
	var doc struct {
		Format           *string     `toml:"format"`
		LabelShow        *bool       `toml:"label-show"`
		Framerate        *int        `toml:"framerate"`
		Microphone       *bool       `toml:"microphone"`
		MicrophoneDevice *string     `toml:"microphone-device"`
		SystemAudio      *bool       `toml:"system-audio"`
		ShowCursor       *bool       `toml:"show-cursor"`
		StartDelayMS     *int        `toml:"start-delay-ms"`
		OutputDirectory  *string     `toml:"output-directory"`
		OutputFormat     *string     `toml:"output-format"`
		IconShow         *bool       `toml:"icon-show"`
		IconIdle         *string     `toml:"icon-idle"`
		IconRecording    *string     `toml:"icon-recording"`
		IconPaused       *string     `toml:"icon-paused"`
		IconColor        *ColorValue `toml:"icon-color"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Format != nil {
		cfg.Format = *doc.Format
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if doc.Framerate != nil {
		cfg.Framerate = *doc.Framerate
	}
	if doc.Microphone != nil {
		cfg.Microphone = *doc.Microphone
	}
	if doc.MicrophoneDevice != nil {
		cfg.MicrophoneDevice = *doc.MicrophoneDevice
	}
	if doc.SystemAudio != nil {
		cfg.SystemAudio = *doc.SystemAudio
	}
	if doc.ShowCursor != nil {
		cfg.ShowCursor = *doc.ShowCursor
	}
	if doc.StartDelayMS != nil {
		cfg.StartDelayMS = *doc.StartDelayMS
	}
	if doc.OutputDirectory != nil {
		cfg.OutputDirectory = *doc.OutputDirectory
	}
	if doc.OutputFormat != nil {
		if *doc.OutputFormat != RecorderMkv && *doc.OutputFormat != RecorderMp4 {
			return cfg, errors.New("recorder: output-format must be mkv or mp4")
		}
		cfg.OutputFormat = *doc.OutputFormat
	}
	if doc.IconShow != nil {
		for name, icon := range cfg.Icons {
			icon.Show = *doc.IconShow
			cfg.Icons[name] = icon
		}
		cfg.Icon.Show = *doc.IconShow
	}
	for name, key := range map[string]*string{
		RecorderIdle:      doc.IconIdle,
		RecorderRecording: doc.IconRecording,
		RecorderPaused:    doc.IconPaused,
	} {
		if key == nil {
			continue
		}
		icon := cfg.Icons[name]
		icon.Name = *key
		cfg.Icons[name] = icon
	}
	if doc.IconColor != nil {
		cfg.Icon.Color = *doc.IconColor
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}
