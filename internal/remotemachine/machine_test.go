package remotemachine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentjoey/pactify/internal/remoteexec"
)

// fakeEngine records the verb the dispatched rpc reached.
type fakeEngine struct{ accepted chan string }

func (f *fakeEngine) Assign(_, _, _, _, _, _ string, _ []string) error { return nil }
func (f *fakeEngine) Accept(task string) error                          { f.accepted <- task; return nil }
func (f *fakeEngine) Changes(_, _ string) error                         { return nil }
func (f *fakeEngine) Merge(_ string) error                              { return nil }
func (f *fakeEngine) Checkpoint(_, _ string) error                      { return nil }

// startFakeRelay serves the socket.io v4 handshake then pushes `push` frames.
func startFakeRelay(t *testing.T, push []string) string {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"s1","pingInterval":25000,"pingTimeout":20000}`))
		if _, _, err := c.ReadMessage(); err != nil { // CONNECT
			return
		}
		_ = c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"s1"}`))
		for _, f := range push {
			_ = c.WriteMessage(websocket.TextMessage, []byte(f))
		}
		// Keep the connection open so the client's read loop stays alive.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "http" + strings.TrimPrefix(srv.URL, "http")
}

func TestRun_DispatchesPactRpcToEngine(t *testing.T) {
	fe := &fakeEngine{accepted: make(chan string, 1)}
	url := startFakeRelay(t, []string{
		`42["rpc",{"type":"pact.accept","machineId":"m1","project":"known","task":"t1"}]`,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		_ = Run(ctx, Config{
			RelayURL:  url,
			Account:   "acct1",
			MachineID: "m1",
			Token:     "tok",
			Resolve: func(project string) (remoteexec.PactEngine, error) {
				if project == "known" {
					return fe, nil
				}
				return nil, context.Canceled
			},
		})
	}()

	select {
	case task := <-fe.accepted:
		if task != "t1" {
			t.Fatalf("accepted task %q, want t1", task)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pact.accept rpc never reached the engine end-to-end")
	}
}

func TestRun_DialErrorReturns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := Run(ctx, Config{RelayURL: "http://127.0.0.1:1", Account: "a", MachineID: "m", Token: "t"})
	if err == nil {
		t.Fatal("Run should return the dial error")
	}
}
