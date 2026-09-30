package auth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The libpam binding: dlopen through purego, so the binary stays
// CGO_ENABLED=0. PAM calls the conversation back through a C function
// pointer; purego's NewCallback provides one (a process-wide, never
// freed slot, so the callback is created once and routes to the live
// transaction by the appdata key).

// libpamCandidates is where libpam is looked up, in order: the soname
// through the loader's own search path (LD_LIBRARY_PATH, ld.so.cache),
// then the NixOS system profile — whose loader has no FHS default
// path — and the common distro locations.
var libpamCandidates = []string{
	"libpam.so.0",
	"/run/current-system/sw/lib/libpam.so.0",
	"/usr/lib/libpam.so.0",
	"/usr/lib64/libpam.so.0",
	"/lib/x86_64-linux-gnu/libpam.so.0",
	"/usr/lib/x86_64-linux-gnu/libpam.so.0",
	"/lib/aarch64-linux-gnu/libpam.so.0",
	"/usr/lib/aarch64-linux-gnu/libpam.so.0",
}

// libcCandidates holds the C heap allocator PAM frees our responses
// with: responses must be malloc'd memory.
var libcCandidates = []string{"libc.so.6"}

// cPamMessage and cPamResponse mirror struct pam_message and struct
// pam_response on LP64 Linux.
type cPamMessage struct {
	style int32
	msg   *byte
}

type cPamResponse struct {
	resp    unsafe.Pointer
	retcode int32
}

// cPamConv mirrors struct pam_conv.
type cPamConv struct {
	conv    uintptr
	appdata uintptr
}

// libpam holds the bound entry points.
type libpam struct {
	start        func(service, user string, conv unsafe.Pointer, pamh *uintptr) int32
	startConfdir func(service, user string, conv unsafe.Pointer, confdir string, pamh *uintptr) int32
	authenticate func(pamh uintptr, flags int32) int32
	acctMgmt     func(pamh uintptr, flags int32) int32
	setcred      func(pamh uintptr, flags int32) int32
	end          func(pamh uintptr, status int32) int32
	strerror     func(pamh uintptr, code int32) string
	calloc       func(n, size uintptr) unsafe.Pointer
	free         func(p unsafe.Pointer)
	strdup       func(s string) unsafe.Pointer
	callback     uintptr
}

var (
	libOnce sync.Once
	lib     *libpam
	errLib  error
)

// loadLibpam binds libpam and libc once per process.
func loadLibpam() (*libpam, error) {
	libOnce.Do(func() { lib, errLib = bindLibpam() })
	return lib, errLib
}

func dlopenFirst(candidates []string) (uintptr, error) {
	var errs []string
	for _, name := range candidates {
		h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		errs = append(errs, err.Error())
	}
	return 0, errors.New(strings.Join(errs, "; "))
}

func bindLibpam() (*libpam, error) {
	pam, err := dlopenFirst(libpamCandidates)
	if err != nil {
		return nil, fmt.Errorf("load libpam: %w", err)
	}
	libc, err := dlopenFirst(libcCandidates)
	if err != nil {
		return nil, fmt.Errorf("load libc: %w", err)
	}
	l := &libpam{}
	purego.RegisterLibFunc(&l.start, pam, "pam_start")
	purego.RegisterLibFunc(&l.authenticate, pam, "pam_authenticate")
	purego.RegisterLibFunc(&l.acctMgmt, pam, "pam_acct_mgmt")
	purego.RegisterLibFunc(&l.setcred, pam, "pam_setcred")
	purego.RegisterLibFunc(&l.end, pam, "pam_end")
	purego.RegisterLibFunc(&l.strerror, pam, "pam_strerror")
	// pam_start_confdir (Linux-PAM 1.4+) is optional: only the tests
	// point a transaction at a private service directory.
	if _, err := purego.Dlsym(pam, "pam_start_confdir"); err == nil {
		purego.RegisterLibFunc(&l.startConfdir, pam, "pam_start_confdir")
	}
	purego.RegisterLibFunc(&l.calloc, libc, "calloc")
	purego.RegisterLibFunc(&l.free, libc, "free")
	purego.RegisterLibFunc(&l.strdup, libc, "strdup")
	l.callback = purego.NewCallback(pamConverseTrampoline)
	return l, nil
}

// conversations routes the C callback to the transaction that owns
// it, by the appdata key.
var conversations = struct {
	sync.Mutex
	next uintptr
	m    map[uintptr]func([]pamMessage) ([]pamReply, int32)
}{m: make(map[uintptr]func([]pamMessage) ([]pamReply, int32))}

// pamConverseTrampoline is the C conversation function:
// int conv(int num_msg, const struct pam_message **msg,
// struct pam_response **resp, void *appdata_ptr).
func pamConverseTrampoline(num int32, msg, resp unsafe.Pointer, appdata uintptr) int32 {
	conversations.Lock()
	converse := conversations.m[appdata]
	conversations.Unlock()
	if converse == nil || num <= 0 || msg == nil || resp == nil || lib == nil {
		return pamConvErr
	}
	ptrs := unsafe.Slice((**cPamMessage)(msg), num) //nolint:gosec // audited: PAM passes num_msg message pointers
	msgs := make([]pamMessage, num)
	for i, p := range ptrs {
		msgs[i] = pamMessage{style: p.style, text: goString(p.msg)}
	}
	replies, code := converse(msgs)
	if code != pamSuccess {
		return code
	}
	for _, r := range replies {
		// A NUL cannot cross into C: it would silently truncate the
		// answer (the pam crate's CString::new refuses it the same way).
		if r.set && strings.IndexByte(r.text, 0) >= 0 {
			return pamConvErr
		}
	}
	arr := lib.calloc(uintptr(num), unsafe.Sizeof(cPamResponse{}))
	if arr == nil {
		return pamBufErr
	}
	out := unsafe.Slice((*cPamResponse)(arr), num) //nolint:gosec // audited: arr was calloc(num, sizeof response)
	for i, r := range replies {
		if !r.set {
			continue
		}
		out[i].resp = lib.strdup(r.text)
		if out[i].resp == nil {
			for _, o := range out[:i] {
				lib.free(o.resp)
			}
			lib.free(arr)
			return pamBufErr
		}
	}
	*(*unsafe.Pointer)(resp) = arr
	return pamSuccess
}

// goString copies a NUL-terminated C string.
func goString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 { //nolint:gosec // audited: scans a NUL-terminated C string
		n++
	}
	return string(unsafe.Slice(p, n)) //nolint:gosec // audited: n bytes precede the NUL
}

// systemPAM is the production pamLib: libpam through purego. confdir,
// when set, opens transactions with pam_start_confdir against that
// service directory instead of /etc/pam.d (tests).
type systemPAM struct {
	confdir string
}

func (s systemPAM) start(service, user string, converse func([]pamMessage) ([]pamReply, int32)) (pamTxn, error) {
	l, err := loadLibpam()
	if err != nil {
		return nil, err
	}
	if s.confdir != "" && l.startConfdir == nil {
		return nil, errors.New("libpam has no pam_start_confdir")
	}
	conversations.Lock()
	conversations.next++
	key := conversations.next
	conversations.m[key] = converse
	conversations.Unlock()

	// struct pam_conv lives on the C heap: PAM keeps the pointer for
	// the whole transaction, which Go memory must not be handed for.
	conv := l.calloc(1, unsafe.Sizeof(cPamConv{}))
	if conv == nil {
		dropConversation(key)
		return nil, errors.New("out of memory")
	}
	*(*cPamConv)(conv) = cPamConv{conv: l.callback, appdata: key}

	var handle uintptr
	var rc int32
	if s.confdir != "" {
		rc = l.startConfdir(service, user, conv, s.confdir, &handle)
	} else {
		rc = l.start(service, user, conv, &handle)
	}
	if rc != pamSuccess {
		dropConversation(key)
		l.free(conv)
		return nil, fmt.Errorf("pam_start: %s", l.strerror(0, rc))
	}
	return &systemTxn{l: l, handle: handle, key: key, conv: conv}, nil
}

func dropConversation(key uintptr) {
	conversations.Lock()
	delete(conversations.m, key)
	conversations.Unlock()
}

// systemTxn is one libpam transaction.
type systemTxn struct {
	l      *libpam
	handle uintptr
	key    uintptr
	conv   unsafe.Pointer
}

func (t *systemTxn) authenticate() int32   { return t.l.authenticate(t.handle, 0) }
func (t *systemTxn) acctMgmt() int32       { return t.l.acctMgmt(t.handle, 0) }
func (t *systemTxn) setcred(f int32) int32 { return t.l.setcred(t.handle, f) }

func (t *systemTxn) strerror(code int32) string { return t.l.strerror(t.handle, code) }

// end closes the transaction and releases the conversation.
func (t *systemTxn) end(status int32) {
	t.l.end(t.handle, status)
	dropConversation(t.key)
	t.l.free(t.conv)
}
