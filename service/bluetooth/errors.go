package bluetooth

import "fmt"

// NoPrimaryAdapterError is a service-level operation with no adapter to
// run on (Error::NoPrimaryAdapter).
type NoPrimaryAdapterError struct{ Operation string }

func (e *NoPrimaryAdapterError) Error() string {
	return "bluetooth: cannot " + e.Operation + ": no primary adapter available"
}

// OperationError is a failed BlueZ call (Error::AdapterOperation, and
// the device controls' D-Bus errors).
type OperationError struct {
	// Operation names the call, e.g. "start discovery".
	Operation string
	Err       error
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("bluetooth: cannot %s: %v", e.Operation, e.Err)
}

func (e *OperationError) Unwrap() error { return e.Err }

// NoPendingRequestError is an answer to a pairing request that is not
// the pending one (Error::NoPendingRequest): a PIN while BlueZ asks for
// a confirmation, or any answer with nothing pending.
type NoPendingRequestError struct{ RequestType string }

func (e *NoPendingRequestError) Error() string {
	return fmt.Sprintf("bluetooth: cannot provide %s: no %s request is pending", e.RequestType, e.RequestType)
}
