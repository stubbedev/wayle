package portal

import (
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// RequestIface is the object the frontend passes the handle of into
// every interactive method, so the app can abort it.
const RequestIface = "org.freedesktop.impl.portal.Request"

// request is an org.freedesktop.impl.portal.Request exported at a
// method's handle for the length of the call (request.rs RequestGuard).
// Close() from the app closes Cancelled.
type request struct {
	once      sync.Once
	cancelled chan struct{}
	unexport  func()
}

// mountRequest exports a Request at handle; end removes it again.
func mountRequest(conn *dbus.Conn, handle dbus.ObjectPath) (*request, error) {
	r := &request{cancelled: make(chan struct{})}
	unexport, err := dbusx.Export(conn, handle, dbusx.Interface{Name: RequestIface, Methods: requestObject{r}})
	if err != nil {
		return nil, err
	}
	r.unexport = unexport
	return r, nil
}

// Cancelled is closed once the app calls Close().
func (r *request) Cancelled() <-chan struct{} { return r.cancelled }

func (r *request) cancel() { r.once.Do(func() { close(r.cancelled) }) }

// end unexports the Request: it never outlives its method call.
func (r *request) end() { r.unexport() }

// requestObject carries the Request's D-Bus methods.
type requestObject struct{ r *request }

// Close aborts the interaction the request belongs to.
func (o requestObject) Close() *dbus.Error {
	o.r.cancel()
	return nil
}
