package cockpit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// rpcConn is a generic JSON-RPC 2.0 connection over newline-delimited JSON.
// It is intentionally transport-agnostic: callers provide the stdio pipes and
// a close function.
type rpcConn struct {
	w       io.WriteCloser
	r       io.Reader
	closeFn func() error

	writeCh chan []byte
	done    chan struct{}
	once    sync.Once

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResponse
	dead    bool
	deadErr error

	onRequest      func(id json.RawMessage, method string, params json.RawMessage)
	onNotification func(method string, params json.RawMessage)
}

type rpcResponse struct {
	Result json.RawMessage
	Err    *rpcError
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// rpcMessage is the wire envelope used for dispatch. ID is kept as raw JSON so
// that both integer client ids and string server ids are preserved exactly when
// replies are sent back.
type rpcMessage struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
	Result json.RawMessage  `json:"result"`
	Error  *rpcError        `json:"error"`
}

// request is the outgoing envelope. ID uses omitempty so that id 0 (used by
// mistake) is dropped and not misinterpreted as a notification; ids therefore
// start at 1.
type request struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func newRPCConn(
	w io.WriteCloser,
	r io.Reader,
	closeFn func() error,
	onRequest func(id json.RawMessage, method string, params json.RawMessage),
	onNotification func(method string, params json.RawMessage),
) *rpcConn {
	c := &rpcConn{
		w:              w,
		r:              r,
		closeFn:        closeFn,
		writeCh:        make(chan []byte),
		done:           make(chan struct{}),
		pending:        map[int]chan rpcResponse{},
		onRequest:      onRequest,
		onNotification: onNotification,
		// Request ids MUST start at 1: id 0 is dropped by the `id,omitempty`
		// JSON tag, so the peer would see the request as an id-less notification
		// and never reply — calls like initialize would then hang until the ctx
		// deadline.
		nextID: 1,
	}
	go c.writeLoop()
	go c.readLoop()
	return c
}

func (c *rpcConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.dead {
		err := c.deadErr
		c.mu.Unlock()
		return nil, err
	}
	id := c.nextID
	c.nextID++
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	b, err := json.Marshal(request{Jsonrpc: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		c.dropPending(id)
		return nil, fmt.Errorf("marshal %s: %w", method, err)
	}
	if err := c.write(b); err != nil {
		c.dropPending(id)
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.dropPending(id)
		return nil, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		err := c.deadErr
		c.mu.Unlock()
		return nil, err
	case resp := <-ch:
		if resp.Err != nil {
			return nil, resp.Err
		}
		return resp.Result, nil
	}
}

func (c *rpcConn) notify(method string, params any) error {
	b, err := json.Marshal(request{Jsonrpc: "2.0", Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("marshal %s: %w", method, err)
	}
	return c.write(b)
}

func (c *rpcConn) reply(id json.RawMessage, result any) error {
	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	if err != nil {
		return err
	}
	return c.write(b)
}

func (c *rpcConn) replyError(id json.RawMessage, code int, message string, data any) error {
	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
			"data":    data,
		},
	})
	if err != nil {
		return err
	}
	return c.write(b)
}

func (c *rpcConn) Close() error {
	c.terminate(errors.New("rpc connection closed"))
	return nil
}

func (c *rpcConn) dropPending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *rpcConn) write(b []byte) error {
	select {
	case <-c.done:
		c.mu.Lock()
		err := c.deadErr
		c.mu.Unlock()
		return err
	case c.writeCh <- b:
		return nil
	}
}

func (c *rpcConn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.writeCh:
			frame := append(b, '\n')
			if _, err := c.w.Write(frame); err != nil {
				c.terminate(fmt.Errorf("write: %w", err))
				return
			}
		}
	}
}

func (c *rpcConn) readLoop() {
	br := bufio.NewReader(c.r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			c.dispatch(line)
		}
		if err != nil {
			c.terminate(fmt.Errorf("connection closed: %w", err))
			return
		}
	}
}

func (c *rpcConn) dispatch(line []byte) {
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}

	switch {
	case m.Method != "" && m.ID != nil:
		if c.onRequest != nil {
			c.onRequest(*m.ID, m.Method, m.Params)
		}
	case m.Method != "":
		if c.onNotification != nil {
			c.onNotification(m.Method, m.Params)
		}
	case m.ID != nil:
		var id int
		if json.Unmarshal(*m.ID, &id) != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch != nil {
			ch <- rpcResponse{Result: m.Result, Err: m.Error}
		}
	}
}

func (c *rpcConn) terminate(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.dead = true
		c.deadErr = err
		c.mu.Unlock()
		close(c.done)
		if c.closeFn != nil {
			_ = c.closeFn()
		}
	})
}

// filteredEnviron returns the current environment with pactify/relay secrets
// removed. A denylist is used so that vendor authentication (PATH, HOME, shell
// config, Claude OAuth, etc.) still reaches the child process.
func filteredEnviron() []string {
	var out []string
	for _, e := range os.Environ() {
		if key, _, ok := strings.Cut(e, "="); ok {
			if key == "PACT_RELAY_TOKEN" || strings.HasPrefix(key, "PACTIFY_") {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return
	}
	_ = cmd.Process.Kill()
}
