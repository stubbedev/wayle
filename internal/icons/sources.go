package icons

import "strings"

// CustomPrefix marks user-imported icons (sources/mod.rs CUSTOM_PREFIX).
const CustomPrefix = "cm"

// Source is one icon CDN (sources/mod.rs's IconSource).
type Source struct {
	DisplayName string
	CLIName     string
	Prefix      string
	Description string
	Website     string
	urlPattern  string // with %s for the slug
}

// CDNURL is the icon's download URL.
func (s Source) CDNURL(slug string) string { return strings.Replace(s.urlPattern, "%s", slug, 1) }

// IconName is "<prefix>-<slug>".
func (s Source) IconName(slug string) string { return s.Prefix + "-" + slug }

// InstalledName is the name an installed icon goes by (and its file
// stem): "<prefix>-<slug>-symbolic".
func (s Source) InstalledName(slug string) string { return s.IconName(slug) + "-symbolic" }

// The sources, one per CDN icon set.
var (
	Tabler = Source{
		DisplayName: "Tabler Icons (Outline)", CLIName: "tabler", Prefix: "tb",
		Description: "UI icons with outline/stroke style (home, settings, bell)",
		Website:     "https://tabler.io/icons",
		urlPattern:  "https://unpkg.com/@tabler/icons@latest/icons/outline/%s.svg",
	}
	TablerFilled = Source{
		DisplayName: "Tabler Icons (Filled)", CLIName: "tabler-filled", Prefix: "tbf",
		Description: "UI icons with solid/filled style (home, settings, bell)",
		Website:     "https://tabler.io/icons",
		urlPattern:  "https://unpkg.com/@tabler/icons@latest/icons/filled/%s.svg",
	}
	SimpleIcons = Source{
		DisplayName: "Simple Icons", CLIName: "simple-icons", Prefix: "si",
		Description: "Brand and app logos (firefox, spotify, github)",
		Website:     "https://simpleicons.org",
		urlPattern:  "https://unpkg.com/simple-icons@latest/icons/%s.svg",
	}
	Material = Source{
		DisplayName: "Material Design", CLIName: "material", Prefix: "md",
		Description: "Google Material Design icons",
		Website:     "https://fonts.google.com/icons",
		urlPattern:  "https://cdn.jsdelivr.net/npm/@material-symbols/svg-400/outlined/%s.svg",
	}
	Lucide = Source{
		DisplayName: "Lucide Icons", CLIName: "lucide", Prefix: "ld",
		Description: "Alternative UI icons, Feather fork (home, settings, bell)",
		Website:     "https://lucide.dev/icons",
		urlPattern:  "https://unpkg.com/lucide-static@latest/icons/%s.svg",
	}
)

// Sources are all sources in sources::all order.
var Sources = []Source{Tabler, TablerFilled, SimpleIcons, Material, Lucide}

// SourceByCLIName is sources::from_cli_name.
func SourceByCLIName(name string) (Source, error) {
	for _, s := range Sources {
		if s.CLIName == name {
			return s, nil
		}
	}
	names := make([]string, len(Sources))
	for i, s := range Sources {
		names[i] = s.CLIName
	}
	return Source{}, &InvalidSourceError{Name: name, Available: strings.Join(names, ", ")}
}

// AllPrefixes are the source prefixes plus the custom one.
func AllPrefixes() []string {
	out := make([]string, 0, len(Sources)+1)
	for _, s := range Sources {
		out = append(out, s.Prefix)
	}
	return append(out, CustomPrefix)
}
