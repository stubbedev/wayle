package config

import (
	"errors"
	"fmt"
)

// BarItem is one entry in a bar layout section: a bare module name, a
// module with a per-instance CSS class, or a named group of modules.
type BarItem struct {
	// Module is the module name ("clock", "battery", "custom-submap").
	Module string
	// Class is an extra styling class for this instance; empty for
	// bare entries and groups.
	Class string
	// Group is non-nil when the item is a group container.
	Group *BarGroup
}

// IsGroup reports whether the item is a group container.
func (b BarItem) IsGroup() bool { return b.Group != nil }

// BarGroup is a set of modules sharing a visual container.
type BarGroup struct {
	Name    string
	Modules []BarItem
}

// UnmarshalTOML decodes the string | {module, class} | {name, modules}
// union. BurntSushi passes tables as map[string]any and bare values as
// themselves.
func (b *BarItem) UnmarshalTOML(value any) error {
	switch v := value.(type) {
	case string:
		b.Module = v
		return nil
	case map[string]any:
		if name, ok := v["name"]; ok {
			group, err := decodeGroup(name, v["modules"])
			if err != nil {
				return err
			}
			b.Group = group
			return nil
		}
		module, err := requiredString(v, "module")
		if err != nil {
			return err
		}
		class, err := optionalString(v, "class")
		if err != nil {
			return err
		}
		*b = BarItem{Module: module, Class: class}
		return nil
	default:
		return fmt.Errorf("config: bar layout item: expected a module name or table, got %T", value)
	}
}

func decodeGroup(name any, modules any) (*BarGroup, error) {
	group := &BarGroup{}
	switch n := name.(type) {
	case string:
		if n == "" {
			return nil, errors.New("config: bar layout group: empty name")
		}
		group.Name = n
	default:
		return nil, fmt.Errorf("config: bar layout group: name must be a string, got %T", name)
	}
	list, ok := modules.([]any)
	if !ok {
		return nil, fmt.Errorf("config: bar layout group %q: modules must be a list", group.Name)
	}
	for _, entry := range list {
		var item BarItem
		if err := item.UnmarshalTOML(entry); err != nil {
			return nil, err
		}
		group.Modules = append(group.Modules, item)
	}
	return group, nil
}

func requiredString(table map[string]any, key string) (string, error) {
	v, ok := table[key]
	if !ok {
		return "", fmt.Errorf("config: bar layout item: missing %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("config: bar layout item: %q must be a string, got %T", key, v)
	}
	return s, nil
}

func optionalString(table map[string]any, key string) (string, error) {
	v, ok := table[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("config: bar layout item: %q must be a string, got %T", key, v)
	}
	return s, nil
}
