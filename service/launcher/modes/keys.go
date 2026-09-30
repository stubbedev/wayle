package modes

import (
	"context"

	"github.com/stubbedev/wayle/service/launcher"
)

// Keys lists the launcher's own keybindings (keys.rs). Read-only: the
// rows are informational.
type Keys struct {
	bindings []launcher.Binding
}

// NewKeys lists the session's effective bindings.
func NewKeys(bindings []launcher.Binding) *Keys { return &Keys{bindings: bindings} }

// Name is "keys".
func (*Keys) Name() string { return "keys" }

// Load lists "kb-<action>: <keys>" rows, none selectable.
func (k *Keys) Load(context.Context) launcher.ModeState {
	items := make([]launcher.Item, len(k.bindings))
	for i, b := range k.bindings {
		items[i] = launcher.NewItem("kb-" + b.Action + ": " + b.Keys)
		items[i].Flags |= launcher.FlagNonselectable
	}
	return launcher.ModeState{Items: items, Prompt: "keys", NoCustom: true}
}

// Activate does nothing.
func (*Keys) Activate(context.Context, launcher.Target, launcher.ActivateKind, string) launcher.Action {
	return launcher.ActionNothing{}
}

// AllowsCustom is false: typed text is not a binding.
func (*Keys) AllowsCustom() bool { return false }
