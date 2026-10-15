package widgets

import (
	"fmt"

	"github.com/stubbedev/gelm/widget"
)

// aligner is a widget with GTK's halign and valign; every gelm widget,
// and every type built on widget.Base, has them.
type aligner interface {
	SetHAlign(widget.Align)
	SetVAlign(widget.Align)
}

// Center keeps w at its natural size in the middle of the space its
// parent gives it (halign and valign center) and returns it.
func Center(w widget.Widget) widget.Widget {
	a, ok := w.(aligner)
	if !ok {
		panic(fmt.Sprintf("widgets.Center: %T has no layout properties", w))
	}
	a.SetHAlign(widget.AlignCenter)
	a.SetVAlign(widget.AlignCenter)
	return w
}
