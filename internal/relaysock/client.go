package relaysock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// Client is a minimal Socket.IO v4 connection to the relay over websocket. It is
// single-connection (no auto-reconnect — the caller re-Dials on Run's error) and
// default-namespace only. Safe for concurrent Emit; Run must be called from one
// goroutine.
type Client struct {
	conn     *websocket.Conn
	writeMu  sync.Mutex
	mu       sync.RWMutex
	handlers map[string]func(args []json.RawMessage)
}

// Dial connects to the relay's socket.io endpoint as a machine and completes the
// engine.io + socket.io v4 handshake, sending `auth` (token/role/machineId) in
// the CONNECT packet. It returns an error if the transport fails or the relay
// rejects the connection (CONNECT_ERROR — e.g. bad token).
func Dial(ctx context.Context, relayURL string, auth any) (*Client, error) {
	u, err := url.Parse(relayURL)
	if err != nil {
		return nil, fmt.Errorf("parse relay url: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/socket.io/"
	u.RawQuery = "EIO=4&transport=websocket"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("dial relay: %w", err)
	}
	c := &Client{conn: conn, handlers: map[string]func([]json.RawMessage){}}

	// 1) engine.io OPEN.
	open, err := c.readFrame()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read open: %w", err)
	}
	if open.Kind != FrameOpen {
		conn.Close()
		return nil, fmt.Errorf("expected engine open, got kind %d", open.Kind)
	}
	// 2) socket.io CONNECT with auth.
	frame, err := EncodeConnect(auth)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.write(frame); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send connect: %w", err)
	}
	// 3) CONNECT ack or CONNECT_ERROR.
	ack, err := c.readFrame()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read connect ack: %w", err)
	}
	if ack.Kind == FrameConnectError {
		conn.Close()
		return nil, fmt.Errorf("relay rejected connect: %s", string(ack.Data))
	}
	if ack.Kind != FrameConnect {
		conn.Close()
		return nil, fmt.Errorf("expected connect ack, got kind %d", ack.Kind)
	}
	return c, nil
}

// On registers a handler for an event name (last registration wins). Safe to call
// before Run.
func (c *Client) On(event string, h func(args []json.RawMessage)) {
	c.mu.Lock()
	c.handlers[event] = h
	c.mu.Unlock()
}

// Emit sends a socket.io EVENT to the relay.
func (c *Client) Emit(event string, arg any) error {
	frame, err := EncodeEvent(event, arg)
	if err != nil {
		return err
	}
	return c.write(frame)
}

// Run reads frames until the connection errors or ctx is cancelled, answering
// engine.io PINGs with PONGs and dispatching EVENTs to their handlers. It returns
// the terminating error (e.g. websocket close) so the caller can reconnect.
func (c *Client) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		c.conn.Close() // unblock ReadMessage
	}()
	for {
		f, err := c.readFrame()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		switch f.Kind {
		case FramePing:
			_ = c.write(Pong)
		case FrameEvent:
			c.mu.RLock()
			h := c.handlers[f.Event]
			c.mu.RUnlock()
			if h != nil {
				h(f.Args)
			}
		}
	}
}

// Close terminates the connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) readFrame() (Frame, error) {
	mt, data, err := c.conn.ReadMessage()
	if err != nil {
		return Frame{}, err
	}
	if mt != websocket.TextMessage {
		return Frame{Kind: FrameOther}, nil // binary frames unused here
	}
	return DecodeFrame(string(data))
}

func (c *Client) write(frame string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, []byte(frame))
}
