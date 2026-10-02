package dbus

import (
	"os"
	"reflect"
)

// fileType is *os.File, which (wayle patch) marshals as 'h': the
// descriptor goes out like a UnixFD. A method that replies one hands
// it over — handleCall closes it once the reply is sent, or not sent —
// since nothing else could close it after the method returns without
// racing the send (a clipboard transfer pipe, a PipeWire remote).
var fileType = reflect.TypeOf((*os.File)(nil))

// closeReplyFiles closes the *os.File values a method replied.
func closeReplyFiles(ret []any) {
	for _, v := range ret {
		if f, ok := v.(*os.File); ok && f != nil {
			_ = f.Close()
		}
	}
}
