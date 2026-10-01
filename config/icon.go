package config

// IconConfig is the view of a module's icon the bar's icon helpers
// take: whether it shows, which theme icon, and its color. Module
// configs build it from their own icon-* keys.
type IconConfig struct {
	Show  bool
	Name  string
	Color ColorValue
}
