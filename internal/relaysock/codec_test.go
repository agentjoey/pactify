package relaysock

import (
	"encoding/json"
	"testing"
)

func TestDecodeFrame_Kinds(t *testing.T) {
	cases := []struct {
		in   string
		kind FrameKind
	}{
		{`0{"sid":"abc","pingInterval":25000,"pingTimeout":20000}`, FrameOpen},
		{`2`, FramePing},
		{`40{"sid":"xyz"}`, FrameConnect},
		{`40`, FrameConnect}, // connect ack with no payload
		{`44{"message":"auth failed"}`, FrameConnectError},
		{`42["rpc",{"type":"pact.accept"}]`, FrameEvent},
		{`3probe`, FrameOther}, // engine.io upgrade-ish, ignored
		{`6`, FrameOther},
	}
	for _, c := range cases {
		f, err := DecodeFrame(c.in)
		if err != nil {
			t.Fatalf("DecodeFrame(%q) error: %v", c.in, err)
		}
		if f.Kind != c.kind {
			t.Fatalf("DecodeFrame(%q) kind = %d, want %d", c.in, f.Kind, c.kind)
		}
	}
}

func TestDecodeFrame_Event(t *testing.T) {
	f, err := DecodeFrame(`42["rpc",{"type":"pact.merge","machineId":"m1","project":"p","feature":"f1"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != FrameEvent || f.Event != "rpc" {
		t.Fatalf("got kind=%d event=%q", f.Kind, f.Event)
	}
	if len(f.Args) != 1 {
		t.Fatalf("want 1 arg, got %d", len(f.Args))
	}
	var m map[string]string
	if err := json.Unmarshal(f.Args[0], &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "pact.merge" || m["feature"] != "f1" {
		t.Fatalf("arg decoded wrong: %v", m)
	}
}

func TestDecodeFrame_EventWithNamespaceAndAckId(t *testing.T) {
	// A leading ack-id / namespace token before the array must be skipped.
	f, err := DecodeFrame(`4212["rpc",{"type":"pact.accept"}]`)
	if err != nil {
		t.Fatalf("ack-id prefixed event: %v", err)
	}
	if f.Kind != FrameEvent || f.Event != "rpc" {
		t.Fatalf("got kind=%d event=%q", f.Kind, f.Event)
	}
}

func TestDecodeFrame_Errors(t *testing.T) {
	if _, err := DecodeFrame(""); err == nil {
		t.Fatal("empty frame should error")
	}
	if _, err := DecodeFrame(`42["rpc"`); err == nil {
		t.Fatal("truncated event array should error")
	}
	if _, err := DecodeFrame(`42[]`); err == nil {
		t.Fatal("empty event array should error")
	}
	if _, err := DecodeFrame(`42[123]`); err == nil {
		t.Fatal("non-string event name should error")
	}
}

func TestEncode(t *testing.T) {
	c, err := EncodeConnect(map[string]string{"token": "t", "role": "machine", "machineId": "m1"})
	if err != nil {
		t.Fatal(err)
	}
	if c[:2] != "40" {
		t.Fatalf("connect frame prefix = %q, want 40", c[:2])
	}
	// Round-trips back to a CONNECT frame's payload.
	f, err := DecodeFrame(c)
	if err != nil || f.Kind != FrameConnect {
		t.Fatalf("connect round-trip: kind=%d err=%v", f.Kind, err)
	}

	e, err := EncodeEvent("rpc", map[string]string{"type": "pact.accept", "task": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if e[:2] != "42" {
		t.Fatalf("event frame prefix = %q, want 42", e[:2])
	}
	f2, err := DecodeFrame(e)
	if err != nil || f2.Kind != FrameEvent || f2.Event != "rpc" {
		t.Fatalf("event round-trip: %+v err=%v", f2, err)
	}
}

func TestPongConst(t *testing.T) {
	if Pong != "3" {
		t.Fatalf("Pong = %q, want 3", Pong)
	}
}
