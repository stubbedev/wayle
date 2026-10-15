package osd

import (
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// iconPx is the icons' size before the stylesheet's -gtk-icon-size.
const iconPx = 24

// labelPx is the labels' size before the stylesheet's font-size.
const labelPx = 14

// build is the OSD's view! tree:
//
//	window.background.osd-host
//	└ revealer
//	  └ box.osd (vertical; muted, toggle-off, bordered, a toast's class)
//	    ├ box.osd-header        (sliders)  image.osd-icon, label.osd-label, label.osd-value
//	    ├ box.osd-header        (toggles)  image.osd-icon, label.osd-label
//	    └ progressbar.osd-bar   (sliders)
func (o *Osd) build() *face {
	f := &face{}
	f.sliderIcon = o.icon()
	f.sliderLabel = o.label("osd-label")
	f.sliderLabel.SetHExpand(true)
	f.sliderLabel.SetHAlign(widget.AlignStart)
	f.sliderLabel.SetEllipsize(widget.EllipsizeEnd)
	f.value = o.label("osd-value")
	f.sliderHeader = header(f.sliderIcon, f.sliderLabel, f.value)

	f.toggleIcon = o.icon()
	f.toggleLabel = o.label("osd-label")
	f.toggleHeader = header(f.toggleIcon, f.toggleLabel)

	f.bar = widget.NewProgressBar(0)
	f.bar.AddClass("osd-bar")

	f.container = widget.NewBox(widget.Column, 0, 0)
	f.container.Append(f.sliderHeader, false)
	f.container.Append(f.toggleHeader, false)
	f.container.Append(f.bar, false)
	f.rev = widget.NewRevealer(f.container)
	return f
}

// root wraps the revealer in the window node the stylesheet's
// window.background.osd-host rule clears.
func (f *face) root(sheet *widget.Stylesheet) widget.Widget {
	root := widget.NewBox(widget.Column, 0, 0)
	root.SetElement("window")
	root.AddClass("background", "osd-host")
	if sheet != nil {
		root.AttachStylesheet(sheet)
	}
	root.Append(f.rev, false)
	return root
}

func (o *Osd) icon() *widget.Icon {
	icon := widget.NewThemeIcon("", iconPx)
	icon.AddClass("osd-icon")
	icon.SetVAlign(widget.AlignCenter)
	return icon
}

func (o *Osd) label(class string) *widget.Label {
	l := widget.NewLabel(o.d.Font, labelPx, "", 0)
	l.AddClass(class)
	l.SetVAlign(widget.AlignCenter)
	return l
}

func header(children ...widget.Widget) *widget.Box {
	b := widget.NewBox(widget.Row, 0, 0)
	b.AddClass("osd-header")
	for _, c := range children {
		b.Append(c, false)
	}
	return b
}

// apply is the view's #[watch] setters for ev: a level shows the
// slider header and the bar, everything else the toggle header placed
// by osd.text-align.
func (f *face) apply(ev Event, cfg config.OsdConfig) {
	f.container.SetClasses(classes(ev, cfg.Border)...)
	f.sliderHeader.SetVisible(ev.bar)
	f.bar.SetVisible(ev.bar)
	f.toggleHeader.SetVisible(!ev.bar)
	f.toggleHeader.SetHAlign(textAlign(cfg.TextAlign))
	for _, icon := range []*widget.Icon{f.sliderIcon, f.toggleIcon} {
		icon.SetThemeName(ev.icon)
		icon.SetVisible(ev.icon != "")
	}
	if ev.bar {
		f.sliderLabel.SetText(ev.label)
		f.value.SetText(valueText(ev))
		f.bar.SetValue(fraction(ev))
		return
	}
	f.toggleLabel.SetText(headerLabel(ev))
}

// textAlign is toast_align.
func textAlign(a config.OsdTextAlign) widget.Align {
	switch a {
	case config.OsdTextAlignStart:
		return widget.AlignStart
	case config.OsdTextAlignEnd:
		return widget.AlignEnd
	}
	return widget.AlignCenter
}
