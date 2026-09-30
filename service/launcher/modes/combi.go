package modes

import (
	"context"
	"strings"

	"github.com/stubbedev/wayle/service/launcher"
)

// Combi merges several modes into one list, !bang filterable
// (combi.rs).
type Combi struct {
	children      []launcher.Mode
	displayFormat string
	// owners and locals map a merged row to its child and child row.
	owners []int
	locals []uint32
}

// NewCombi builds over child modes (rofi -combi-modes) with a row
// template over {mode} and {text}.
func NewCombi(children []launcher.Mode, displayFormat string) *Combi {
	return &Combi{children: children, displayFormat: displayFormat}
}

// Name is "combi".
func (*Combi) Name() string { return "combi" }

func (c *Combi) merge(states []launcher.ModeState) launcher.ModeState {
	var items []launcher.Item
	c.owners, c.locals = nil, nil
	for ci, state := range states {
		mode := c.children[ci].Name()
		for li, it := range state.Items {
			if c.displayFormat != "{text}" {
				text := it.Display
				it.Display = launcher.Render(c.displayFormat, launcher.Values(map[string]string{"mode": mode, "text": text}))
			}
			items = append(items, it)
			c.owners = append(c.owners, ci)
			c.locals = append(c.locals, uint32(li))
		}
	}
	return launcher.ModeState{Items: items, Prompt: "combi"}
}

func (c *Combi) locate(index uint32) (int, uint32, bool) {
	if int(index) >= len(c.owners) {
		return 0, 0, false
	}
	return c.owners[index], c.locals[index], true
}

// reloadAll re-merges every child after one changed.
func (c *Combi) reloadAll(ctx context.Context) launcher.ModeState {
	states := make([]launcher.ModeState, len(c.children))
	for i, child := range c.children {
		states[i] = child.Load(ctx)
	}
	return c.merge(states)
}

// Load loads and merges every child.
func (c *Combi) Load(ctx context.Context) launcher.ModeState { return c.reloadAll(ctx) }

// Activate routes a row to its owner; custom input goes to the first
// child that accepts it.
func (c *Combi) Activate(ctx context.Context, target launcher.Target, kind launcher.ActivateKind, input string) launcher.Action {
	if row, ok := target.Row(); ok {
		if child, local, ok := c.locate(row); ok {
			return c.forward(ctx, c.children[child].Activate(ctx, launcher.RowTarget(local), kind, input))
		}
	}
	for _, child := range c.children {
		if launcher.AllowsCustom(child) {
			return c.forward(ctx, child.Activate(ctx, launcher.NoRow, kind, input))
		}
	}
	return launcher.ActionNothing{}
}

// Delete routes shift-delete to the row's owner.
func (c *Combi) Delete(ctx context.Context, index uint32) launcher.Action {
	child, local, ok := c.locate(index)
	if !ok {
		return launcher.ActionNothing{}
	}
	return c.forward(ctx, launcher.Delete(ctx, c.children[child], local))
}

// Subset masks the rows of children whose name starts with the bang.
func (c *Combi) Subset(bang string) ([]bool, bool) {
	if bang == "" {
		return nil, false
	}
	allowed := make([]bool, len(c.children))
	any := false
	for i, child := range c.children {
		allowed[i] = strings.HasPrefix(child.Name(), bang)
		any = any || allowed[i]
	}
	if !any {
		return nil, false
	}
	mask := make([]bool, len(c.owners))
	for i, owner := range c.owners {
		mask[i] = allowed[owner]
	}
	return mask, true
}

// forward turns a child's reload into a re-merge of the whole list.
func (c *Combi) forward(ctx context.Context, a launcher.Action) launcher.Action {
	if _, reload := a.(launcher.ActionReload); reload {
		return launcher.ActionReload{State: c.reloadAll(ctx)}
	}
	return a
}
