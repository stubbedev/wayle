package settings

import (
	"slices"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// actionChoice is ActionChoice: a labeled preset command.
type actionChoice struct{ label, command string }

// Action editor texts (editors/action: English in Rust too).
const (
	actionNoneLabel   = "None"
	actionCustomLabel = "Custom command…"
	actionPlaceholder = "Shell command"
)

// actionEditor is ActionControl over a click action's command string
// (ClickAction and WorkspaceClickAction both encode as one): the
// presets, None, and a custom command whose entry slides in and writes
// as it is typed.
type actionEditor struct {
	*widget.Box
	k       *kit
	slot    slot
	choices []actionChoice
	drop    *widget.Dropdown
	entry   *widget.Entry
	reveal  *widget.Revealer
	custom  bool
	syncing bool
}

func newActionEditor(k *kit, s slot, choices []actionChoice) *actionEditor {
	c := &actionEditor{Box: widget.NewBox(widget.Column, 4, 0), k: k, slot: s, choices: choices}
	labels := make([]string, 0, len(choices)+2)
	for _, ch := range choices {
		labels = append(labels, ch.label)
	}
	labels = append(labels, actionNoneLabel, actionCustomLabel)
	c.drop = widget.NewDropdown(k.face, 14, labels, 0)
	c.drop.OnSelect = c.selected
	c.entry = k.entry()
	c.entry.SetPlaceholder(actionPlaceholder)
	c.entry.OnChanged = func(text string) {
		if c.custom && !c.syncing {
			_ = c.slot.set(text)
		}
	}
	c.reveal = widget.NewRevealer(c.entry)
	c.reveal.SetTransition(widget.RevealSlideDown)
	c.Append(c.drop, false)
	c.Append(c.reveal, false)
	command := c.command()
	c.custom = command != "" && c.choiceIndex(command) < 0
	c.refresh()
	return c
}

func (c *actionEditor) command() string {
	s, _ := c.slot.get().(string)
	return s
}

func (c *actionEditor) noneIndex() int   { return len(c.choices) }
func (c *actionEditor) customIndex() int { return len(c.choices) + 1 }

func (c *actionEditor) choiceIndex(command string) int {
	return slices.IndexFunc(c.choices, func(ch actionChoice) bool { return ch.command == command })
}

// selected is ActionMsg::Selected: a preset or None writes it and hides
// the entry; Custom shows the entry and writes what it holds.
func (c *actionEditor) selected(i int) {
	if c.syncing {
		return
	}
	switch {
	case i < c.noneIndex():
		c.custom = false
		c.reveal.SetRevealed(false)
		_ = c.slot.set(c.choices[i].command)
	case i == c.noneIndex():
		c.custom = false
		c.reveal.SetRevealed(false)
		_ = c.slot.set("")
	default:
		c.custom = true
		c.reveal.SetRevealed(true)
		if text := c.entry.Text(); text != "" {
			_ = c.slot.set(text)
		}
	}
}

// refresh is ActionMsg::Refresh: a preset or empty command leaves
// custom mode (unless custom was just chosen with nothing typed yet).
func (c *actionEditor) refresh() {
	command := c.command()
	if (c.choiceIndex(command) >= 0 || command == "") && (!c.custom || command != "") {
		c.custom = false
	}
	i := c.choiceIndex(command)
	switch {
	case i >= 0:
	case command == "" && !c.custom:
		i = c.noneIndex()
	default:
		i = c.customIndex()
	}
	c.syncing = true
	c.drop.SetSelected(i)
	reveal := i == c.customIndex()
	c.reveal.SetRevealed(reveal)
	if reveal && c.entry.Text() != command {
		c.entry.SetText(command)
	}
	c.syncing = false
}

// actions is action::action for a row.
func actions(choices []actionChoice) rowOpt {
	return withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newActionEditor(k, s, choices)
	})
}

// actionChoices is choices_for: each module's preset click actions.
func actionChoices(module string) []actionChoice {
	shell := func(label, command string) actionChoice { return actionChoice{label, command} }
	dropdown := func(label, id string) actionChoice { return actionChoice{label, "dropdown:" + id} }
	switch module {
	case "recorder":
		return []actionChoice{
			shell("Toggle recording", "wayle recorder toggle"), shell("Start recording", "wayle recorder start"),
			shell("Stop recording", "wayle recorder stop"), shell("Pause recording", "wayle recorder pause"),
			shell("Resume recording", "wayle recorder resume"), dropdown("Open recorder panel", "recorder"),
		}
	case "screenshot":
		return []actionChoice{
			shell("Capture region", "wayle screenshot region"), shell("Capture output", "wayle screenshot output"),
			shell("Capture window", "wayle screenshot window"),
		}
	case "battery":
		return []actionChoice{dropdown("Open battery panel", "battery")}
	case "bluetooth":
		return []actionChoice{dropdown("Open bluetooth panel", "bluetooth")}
	case "brightness":
		return []actionChoice{
			{"Brightness up (+5%)", "brightness:5"},
			{"Brightness down (-5%)", "brightness:-5"},
			{"Toggle blackout (0% ⇄ last)", "brightness:toggle"},
			dropdown("Open brightness panel", "brightness"),
		}
	case "network":
		return []actionChoice{dropdown("Open network panel", "network")}
	case "media":
		return []actionChoice{
			shell("Play / pause", "wayle media play-pause"), shell("Next track", "wayle media next"),
			shell("Previous track", "wayle media previous"), dropdown("Open media panel", "media"),
		}
	case "notifications", "notification":
		return []actionChoice{shell("Toggle do not disturb", "wayle notify dnd"), dropdown("Open notifications panel", "notification")}
	case "mail":
		return []actionChoice{dropdown("Open mail panel", "mail")}
	case "weather":
		return []actionChoice{dropdown("Open weather panel", "weather")}
	case "treeman":
		return []actionChoice{dropdown("Open treeman panel", "treeman")}
	case "dashboard":
		return []actionChoice{dropdown("Open dashboard", "dashboard")}
	case "clock":
		return []actionChoice{dropdown("Open calendar", "calendar"), dropdown("Open weather panel", "weather")}
	case "world-clock":
		return []actionChoice{dropdown("Open calendar", "calendar")}
	case "hyprsunset":
		return []actionChoice{shell("Toggle night light", ":toggle")}
	case "power-profiles":
		return []actionChoice{shell("Cycle profile", ":cycle")}
	case "power":
		return []actionChoice{shell("Open power menu", ":menu")}
	case "idle-inhibit":
		return []actionChoice{
			shell("Toggle idle inhibit", "wayle idle toggle"),
			shell("Toggle idle inhibit (indefinite)", "wayle idle toggle --indefinite"),
		}
	case "volume":
		return []actionChoice{
			shell("Toggle output mute", "wayle audio output-mute"), shell("Volume up (+5%)", "wayle audio output-volume +5"),
			shell("Volume down (-5%)", "wayle audio output-volume -5"), dropdown("Open audio panel", "audio"),
		}
	case "microphone":
		return []actionChoice{
			shell("Toggle input mute", "wayle audio input-mute"), shell("Mic volume up (+5%)", "wayle audio input-volume +5"),
			shell("Mic volume down (-5%)", "wayle audio input-volume -5"), dropdown("Open audio panel", "audio"),
		}
	case "cava":
		return []actionChoice{dropdown("Open audio panel", "audio")}
	}
	return nil
}

// The bar-button sections every module page shares (sections/bar_button),
// over the module's path (modules.battery).

func barDisplaySection(module string) sectionSpec {
	return sectionSpec{title: "settings-section-bar-display", rows: fields(
		module+".icon-show", module+".label-show", module+".label-max-length", module+".border-show")}
}

func colorsSection(module string) sectionSpec {
	return sectionSpec{title: "settings-section-colors", rows: fields(
		module+".icon-color", module+".icon-bg-color", module+".label-color", module+".button-bg-color", module+".border-color")}
}

func actionsSection(module string, choices []actionChoice) sectionSpec {
	rows := make([]rowSpec, 0, 5)
	for _, key := range []string{"left-click", "right-click", "middle-click", "scroll-up", "scroll-down"} {
		rows = append(rows, field(module+"."+key, actions(choices)))
	}
	return sectionSpec{title: "settings-section-actions", rows: rows}
}
