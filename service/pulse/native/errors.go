package native

import "strconv"

// Error is a PA_ERR_* code the server returned for a request. The
// codes are comparable, so errors.Is(err, native.ErrNoEntity) works on
// any wrapped request error.
type Error uint32

// Server error codes (pulse/def.h's pa_error_code).
const (
	ErrAccess               Error = 1
	ErrCommand              Error = 2
	ErrInvalid              Error = 3
	ErrExist                Error = 4
	ErrNoEntity             Error = 5
	ErrConnectionRefused    Error = 6
	ErrProtocolCode         Error = 7
	ErrTimeout              Error = 8
	ErrAuthKey              Error = 9
	ErrInternal             Error = 10
	ErrConnectionTerminated Error = 11
	ErrEntityKilled         Error = 12
	ErrInvalidServer        Error = 13
	ErrModInitFailed        Error = 14
	ErrBadState             Error = 15
	ErrNoData               Error = 16
	ErrVersion              Error = 17
	ErrTooLarge             Error = 18
	ErrNotSupported         Error = 19
	ErrUnknown              Error = 20
	ErrNoExtension          Error = 21
	ErrObsolete             Error = 22
	ErrNotImplemented       Error = 23
	ErrForked               Error = 24
	ErrIO                   Error = 25
	ErrBusy                 Error = 26
)

// errorText is pa_strerror's table.
var errorText = map[Error]string{
	ErrAccess:               "Access denied",
	ErrCommand:              "Unknown command",
	ErrInvalid:              "Invalid argument",
	ErrExist:                "Entity exists",
	ErrNoEntity:             "No such entity",
	ErrConnectionRefused:    "Connection refused",
	ErrProtocolCode:         "Protocol error",
	ErrTimeout:              "Timeout",
	ErrAuthKey:              "No authentication key",
	ErrInternal:             "Internal error",
	ErrConnectionTerminated: "Connection terminated",
	ErrEntityKilled:         "Entity killed",
	ErrInvalidServer:        "Invalid server",
	ErrModInitFailed:        "Module initialization failed",
	ErrBadState:             "Bad state",
	ErrNoData:               "No data",
	ErrVersion:              "Incompatible protocol version",
	ErrTooLarge:             "Too large",
	ErrNotSupported:         "Not supported",
	ErrUnknown:              "Unknown error code",
	ErrNoExtension:          "No such extension",
	ErrObsolete:             "Obsolete functionality",
	ErrNotImplemented:       "Missing implementation",
	ErrForked:               "Client forked",
	ErrIO:                   "Input/Output error",
	ErrBusy:                 "Device or resource busy",
}

func (e Error) Error() string {
	if text, ok := errorText[e]; ok {
		return "pulse: " + text
	}
	return "pulse: error code " + strconv.FormatUint(uint64(e), 10)
}
