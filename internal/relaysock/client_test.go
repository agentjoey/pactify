package relaysock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeRelay is a minimal socket.io v4 server for testing the client: it does the
// handshake, then runs a scripted exchange the test drives via channels.
type fakeRelay struct {
	srv       *httptest.Server
	gotAuth   chan json.RawMessage // the CONNECT auth payload the client sent
	gotEmit   chan Frame           // events the client emitted
	toClient  chan string          // raw frames to push to the client
	rejectMsg string               // if set, reply CONNECT_ERROR instead of ack
}

func newFakeRelay(t *testing.T, rejectMsg string) *fakeRelay {
	f := &fakeRelay{
		gotAuth:   make(chan json.RawMessage, 1),
		gotEmit:   make(chan Frame, 8),
		toClient:  make(chan string, 8),
		rejectMsg: rejectMsg,
	}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// engine.io OPEN.
		_ = c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s1","pingInterval":25000,"pingTimeout":20000}`))
		// Read socket.io CONNECT with auth.
		_, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		fr, _ := DecodeFrame(string(data))
		if fr.Kind == FrameConnect {
			f.gotAuth <- fr.Data
		}
		if f.rejectMsg != "" {
			_ = c.WriteMessage(websocket.TextMessage, []byte(`44{"message":"`+f.rejectMsg+`"}`))
			return
		}
		_ = c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s1"}`))
		// Pump: push scripted frames to the client, and record what it emits.
		go func() {
			for msg := range f.toClient {
				_ = c.WriteMessage(websocket.TextMessage, []byte(msg))
			}
		}()
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			fr, derr := DecodeFrame(string(data))
			if derr == nil && (fr.Kind == FrameEvent) {
				f.gotEmit <- fr
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelay) url() string { return "http" + strings.TrimPrefix(f.srv.URL, "http") }

func TestClient_HandshakeAuthAndEvents(t *testing.T) {
	f := newFakeRelay(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, err := Dial(ctx, f.url(), map[string]string{"token": "tok", "role": "machine", "machineId": "m1"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	// The relay saw our auth.
	var auth map[string]string
	select {
	case raw := <-f.gotAuth:
		_ = json.Unmarshal(raw, &auth)
	case <-time.After(2 * time.Second):
		t.Fatal("relay never received auth")
	}
	if auth["role"] != "machine" || auth["machineId"] != "m1" || auth["token"] != "tok" {
		t.Fatalf("auth wrong: %v", auth)
	}

	// Handler receives a pushed rpc event.
	got := make(chan string, 1)
	c.On("rpc", func(args []json.RawMessage) {
		var m map[string]string
		if len(args) == 1 {
			_ = json.Unmarshal(args[0], &m)
		}
		got <- m["type"]
	})
	go c.Run(ctx)

	f.toClient <- `42["rpc",{"type":"pact.accept","task":"t1"}]`
	select {
	case typ := <-got:
		if typ != "pact.accept" {
			t.Fatalf("handler got type %q", typ)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rpc event never dispatched")
	}

	// Client Emit reaches the relay.
	if err := c.Emit("rpc", map[string]string{"type": "pact.merge", "feature": "f1"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	select {
	case ev := <-f.gotEmit:
		if ev.Event != "rpc" {
			t.Fatalf("relay got event %q", ev.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay never received emit")
	}
}

func TestClient_PingPong(t *testing.T) {
	f := newFakeRelay(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, f.url(), map[string]string{"role": "machine"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	go c.Run(ctx)

	<-f.gotAuth
	f.toClient <- `2` // engine ping
	// The client should PONG ("3"), which the relay records as... not an event.
	// We assert indirectly: after a ping, the connection stays alive and an event
	// still dispatches (a dead/paniced loop would fail the next assertion).
	got := make(chan struct{}, 1)
	c.On("rpc", func([]json.RawMessage) { got <- struct{}{} })
	f.toClient <- `42["rpc",{}]`
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("connection did not survive ping/pong")
	}
}

func TestClient_ConnectRejected(t *testing.T) {
	f := newFakeRelay(t, "bad token")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Dial(ctx, f.url(), map[string]string{"role": "machine"}); err == nil {
		t.Fatal("Dial should fail on CONNECT_ERROR")
	}
}
