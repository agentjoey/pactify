// Package relaysock is a minimal Socket.IO v4 / Engine.IO v4 client — just the
// subset pactify serve needs to connect to the relay as a machine, keep the
// connection alive (ping/pong), authenticate, receive `rpc` events, and emit
// events back. It is deliberately hand-rolled over gorilla/websocket rather than
// pulling a heavy socket.io Go ecosystem: the wire subset is small and the codec
// here is pure and unit-tested. Not a general socket.io client (no polling
// transport, no binary packets, no multiplexed namespaces — default `/` only).
package relaysock

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// FrameKind classifies a decoded text frame from the server.
type FrameKind int

const (
	FrameOther        FrameKind = iota
	FrameOpen                   // engine.io OPEN: "0{sid,pingInterval,pingTimeout,...}"
	FramePing                   // engine.io PING: "2"
	FrameConnect                // socket.io CONNECT (ack of ours): "40{sid}"
	FrameConnectError           // socket.io CONNECT_ERROR: "44{message}"
	FrameEvent                  // socket.io EVENT: "42[\"name\",arg,...]"
)

// Frame is a decoded server frame. Event/Args are set only for FrameEvent; Data
// holds the raw JSON payload for FrameOpen/FrameConnect/FrameConnectError.
type Frame struct {
	Kind  FrameKind
	Event string            // event name (FrameEvent)
	Args  []json.RawMessage // args after the event name (FrameEvent)
	Data  json.RawMessage   // payload (FrameOpen / FrameConnect / FrameConnectError)
}

// Pong is the engine.io PONG reply to a server PING.
const Pong = "3"

// engine.io + socket.io packet-type digits used here.
const (
	engineOpen    = '0'
	enginePing    = '2'
	engineMessage = '4'

	sioConnect      = '0'
	sioEvent        = '2'
	sioConnectError = '4'
)

// DecodeFrame parses one text frame. Frames it doesn't model return FrameOther
// with no error (harmless to ignore). A malformed EVENT payload is an error.
func DecodeFrame(s string) (Frame, error) {
	if s == "" {
		return Frame{}, errors.New("empty frame")
	}
	switch s[0] {
	case engineOpen:
		return Frame{Kind: FrameOpen, Data: json.RawMessage(s[1:])}, nil
	case enginePing:
		if s == string(enginePing) {
			return Frame{Kind: FramePing}, nil
		}
		return Frame{Kind: FrameOther}, nil
	case engineMessage:
		return decodeMessage(s[1:])
	default:
		return Frame{Kind: FrameOther}, nil
	}
}

// decodeMessage parses the socket.io packet carried in an engine.io MESSAGE.
func decodeMessage(s string) (Frame, error) {
	if s == "" {
		return Frame{Kind: FrameOther}, nil
	}
	switch s[0] {
	case sioConnect:
		return Frame{Kind: FrameConnect, Data: rawOrNil(s[1:])}, nil
	case sioConnectError:
		return Frame{Kind: FrameConnectError, Data: rawOrNil(s[1:])}, nil
	case sioEvent:
		// After the '2' may come an optional namespace (default '/' omitted) and
		// an optional numeric ack id before the JSON array. Locate the array.
		body := s[1:]
		i := strings.IndexByte(body, '[')
		if i < 0 {
			return Frame{}, fmt.Errorf("event frame has no payload array: %q", s)
		}
		var arr []json.RawMessage
		if err := json.Unmarshal([]byte(body[i:]), &arr); err != nil {
			return Frame{}, fmt.Errorf("decode event payload: %w", err)
		}
		if len(arr) == 0 {
			return Frame{}, errors.New("event payload array is empty")
		}
		var name string
		if err := json.Unmarshal(arr[0], &name); err != nil {
			return Frame{}, fmt.Errorf("event name not a string: %w", err)
		}
		return Frame{Kind: FrameEvent, Event: name, Args: arr[1:]}, nil
	default:
		return Frame{Kind: FrameOther}, nil
	}
}

func rawOrNil(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

// EncodeConnect builds the socket.io CONNECT frame for the default namespace,
// carrying the auth object (token/role/machineId) the relay reads from the
// handshake — "40" + JSON(auth).
func EncodeConnect(auth any) (string, error) {
	b, err := json.Marshal(auth)
	if err != nil {
		return "", fmt.Errorf("marshal auth: %w", err)
	}
	return fmt.Sprintf("%c%c%s", engineMessage, sioConnect, b), nil
}

// EncodeEvent builds a socket.io EVENT frame — "42" + JSON(["event",arg]).
func EncodeEvent(event string, arg any) (string, error) {
	b, err := json.Marshal([]any{event, arg})
	if err != nil {
		return "", fmt.Errorf("marshal event: %w", err)
	}
	return fmt.Sprintf("%c%c%s", engineMessage, sioEvent, b), nil
}
