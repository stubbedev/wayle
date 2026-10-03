module github.com/stubbedev/wayle

go 1.27.1

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/ebitengine/purego v0.11.1
	github.com/godbus/dbus/v5 v5.2.2
	github.com/neurlang/wayland v0.4.4
	github.com/rivo/uniseg v0.4.7
	github.com/stubbedev/gelm v0.0.0-20261003061157-df3d8a454619
	github.com/unxed/xkb-go v0.1.8
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.42.0
	gonum.org/v1/gonum v0.17.0
	modernc.org/sqlite v1.59.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/yalue/native_endian v1.0.2 // indirect
	golang.org/x/net v0.58.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// The godbus fd-array decode fix (third_party/godbus-dbus/WAYLE-PATCHES.md).
replace github.com/godbus/dbus/v5 => ./third_party/godbus-dbus
