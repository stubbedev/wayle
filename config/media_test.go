package config

import (
	"slices"
	"testing"
)

func TestMediaPlayerSelectionKeys(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[modules.media]
players-ignored = ["chromium", "discord"]
player-priority = ["*spotify*", "*firefox*"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Media.PlayersIgnored, []string{"chromium", "discord"}) {
		t.Errorf("players-ignored = %v", cfg.Media.PlayersIgnored)
	}
	if !slices.Equal(cfg.Media.PlayerPriority, []string{"*spotify*", "*firefox*"}) {
		t.Errorf("player-priority = %v", cfg.Media.PlayerPriority)
	}

	// The defaults are empty lists, and a non-list value is a load error.
	if d := DefaultsMedia(); len(d.PlayersIgnored) != 0 || len(d.PlayerPriority) != 0 {
		t.Errorf("defaults = %+v", d)
	}
	if _, err := LoadFile(writeConfig(t, "[modules.media]\nplayer-priority = \"*spotify*\"\n")); err == nil {
		t.Error("a string player-priority loaded")
	}
}
