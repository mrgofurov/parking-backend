// Package agentproto is the wire protocol between the backend and the
// device-agent: JSON text messages over one WebSocket that the agent opens.
//
//	agent  -> backend  {"type":"hello","version":"…","hostname":"…"}
//	backend -> agent   {"type":"command","id":"…","op":"open","spec":{…}}
//	agent  -> backend  {"type":"result","id":"…","ok":true,"sent":true}
//
// Liveness uses WebSocket ping/pong control frames sent by the backend.
package agentproto

import "github.com/smart-parking/parking-backend/internal/pkg/barrierctl"

// Path is the backend endpoint the agent connects to.
const Path = "/api/v1/agent/ws"

const (
	TypeHello   = "hello"
	TypeCommand = "command"
	TypeResult  = "result"

	OpOpen  = "open"
	OpClose = "close"
	OpPing  = "ping"
)

type Message struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	// command
	Op   string           `json:"op,omitempty"`
	Spec *barrierctl.Spec `json:"spec,omitempty"`

	// result
	OK    bool                   `json:"ok,omitempty"`
	Sent  bool                   `json:"sent,omitempty"` // close: a physical close command was sent
	Error string                 `json:"error,omitempty"`
	Ping  *barrierctl.PingResult `json:"ping,omitempty"`

	// hello
	Version  string `json:"version,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}
