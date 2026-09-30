package auth

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"os"
)

// GreetdSockEnv names the IPC socket greetd hands its greeter.
const GreetdSockEnv = "GREETD_SOCK"

// Greetd is the pre-login backend (greetd_backend.rs): an unprivileged
// greeter does not talk to PAM, greetd does, over its IPC socket — a
// native-endian u32 length prefix before each JSON body. One Run is the
// whole login: create the session, answer every auth message, and on
// success start the session; greetd then replaces the greeter.
type Greetd struct {
	conn io.ReadWriter
	// cmd resolves the session argv, read only at start_session so the
	// session picker can change mid-login without racing the backend.
	cmd func() []string
	// env holds extra KEY=value entries for the session.
	env []string
}

// NewGreetdFromEnv connects to the socket in $GREETD_SOCK. cmd is
// called once, at start_session.
func NewGreetdFromEnv(cmd func() []string, env []string) (*Greetd, error) {
	path := os.Getenv(GreetdSockEnv)
	if path == "" {
		return nil, fmt.Errorf("%s is not set; not running under greetd", GreetdSockEnv)
	}
	conn, err := net.Dial("unix", path) //nolint:gosec // greetd names its own socket for the greeter it spawned
	if err != nil {
		return nil, err
	}
	return NewGreetd(conn, cmd, env), nil
}

// NewGreetd runs the protocol over an established connection.
func NewGreetd(conn io.ReadWriter, cmd func() []string, env []string) *Greetd {
	return &Greetd{conn: conn, cmd: cmd, env: env}
}

// Close closes the connection when it is closable.
func (g *Greetd) Close() error {
	if c, ok := g.conn.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// The requests, shaped like serde's internally tagged enum: the
// "type" tag first, then exactly the variant's fields.
type (
	createSessionRequest struct {
		Type     string `json:"type"`
		Username string `json:"username"`
	}
	// postResponseRequest's Response is null for info and error
	// messages.
	postResponseRequest struct {
		Type     string  `json:"type"`
		Response *string `json:"response"`
	}
	startSessionRequest struct {
		Type string   `json:"type"`
		Cmd  []string `json:"cmd"`
		Env  []string `json:"env"`
	}
	cancelSessionRequest struct {
		Type string `json:"type"`
	}
)

func createSession(username string) createSessionRequest {
	return createSessionRequest{Type: "create_session", Username: username}
}

func postResponse(response *string) postResponseRequest {
	return postResponseRequest{Type: "post_auth_message_response", Response: response}
}

// startSession never sends null arrays: serde's Vec is always a list.
func startSession(cmd, env []string) startSessionRequest {
	if cmd == nil {
		cmd = []string{}
	}
	if env == nil {
		env = []string{}
	}
	return startSessionRequest{Type: "start_session", Cmd: cmd, Env: env}
}

var cancelSession = cancelSessionRequest{Type: "cancel_session"}

// greetdResponse is one decoded response.
type greetdResponse struct {
	Type            string `json:"type"`
	ErrorType       string `json:"error_type"`
	Description     string `json:"description"`
	AuthMessageType string `json:"auth_message_type"`
	AuthMessage     string `json:"auth_message"`
}

// validate rejects what serde's enums would: unknown variants and
// unknown error or message kinds.
func (r greetdResponse) validate() error {
	switch r.Type {
	case "success":
		return nil
	case "error":
		if r.ErrorType != "auth_error" && r.ErrorType != "error" {
			return fmt.Errorf("greetd: unknown error_type %q", r.ErrorType)
		}
		return nil
	case "auth_message":
		if _, err := greetdPrompt(r.AuthMessageType, r.AuthMessage); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("greetd: unknown response type %q", r.Type)
}

// greetdPrompt maps an auth message to the UI prompt.
func greetdPrompt(kind, text string) (Prompt, error) {
	switch kind {
	case "visible":
		return Prompt{Kind: PromptVisible, Text: text}, nil
	case "secret":
		return Prompt{Kind: PromptSecret, Text: text}, nil
	case "info":
		return Prompt{Kind: PromptInfo, Text: text}, nil
	case "error":
		return Prompt{Kind: PromptError, Text: text}, nil
	}
	return Prompt{}, fmt.Errorf("greetd: unknown auth_message_type %q", kind)
}

// send writes one length-prefixed request.
func (g *Greetd) send(req any) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if len(payload) > math.MaxUint32 {
		return errors.New("request too large")
	}
	frame := binary.NativeEndian.AppendUint32(make([]byte, 0, 4+len(payload)), uint32(len(payload)))
	frame = append(frame, payload...)
	_, err = g.conn.Write(frame)
	return err
}

// recv reads one length-prefixed response.
func (g *Greetd) recv() (greetdResponse, error) {
	var head [4]byte
	if _, err := io.ReadFull(g.conn, head[:]); err != nil {
		return greetdResponse{}, err
	}
	buf := make([]byte, binary.NativeEndian.Uint32(head[:]))
	if _, err := io.ReadFull(g.conn, buf); err != nil {
		return greetdResponse{}, err
	}
	var resp greetdResponse
	if err := json.Unmarshal(buf, &resp); err != nil {
		return greetdResponse{}, err
	}
	return resp, resp.validate()
}

// Run implements Conversation: create_session, one
// post_auth_message_response per auth message, then start_session.
// A greetd error or a cancelled input prompt cancels the session.
func (g *Greetd) Run(username string, ask Ask) error {
	// greetd needs the username up front; ask for it when not given.
	if username == "" {
		u, ok := ask(Prompt{Kind: PromptVisible, Text: "Username"})
		if !ok {
			return ErrCancelled
		}
		username = u
	}
	if err := g.send(createSession(username)); err != nil {
		return err
	}
	for {
		resp, err := g.recv()
		if err != nil {
			return err
		}
		if resp.Type == "success" {
			break
		}
		if resp.Type == "error" {
			_ = g.send(cancelSession)
			return errors.New(resp.Description)
		}
		prompt, _ := greetdPrompt(resp.AuthMessageType, resp.AuthMessage)
		answer, ok := ask(prompt)
		// A cancelled input prompt aborts the whole session.
		if prompt.WantsInput() && !ok {
			_ = g.send(cancelSession)
			return ErrCancelled
		}
		var response *string
		if ok {
			response = &answer
		}
		if err := g.send(postResponse(response)); err != nil {
			return err
		}
	}

	// Authenticated: hand off to the session; greetd replaces us.
	if err := g.send(startSession(g.cmd(), g.env)); err != nil {
		return err
	}
	resp, err := g.recv()
	if err != nil {
		return err
	}
	switch resp.Type {
	case "success":
		return nil
	case "error":
		return errors.New(resp.Description)
	}
	log.Printf("greetd: unexpected response to start_session: %+v", resp)
	return errors.New("unexpected response to start_session")
}
