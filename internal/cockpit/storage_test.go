package cockpit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestStorageCodexDeleteThread(t *testing.T) {
	s := newFakeCodexServer(t)
	defer s.Close()
	s.handle("thread/delete", func(m wireMessage) any {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		json.Unmarshal(m.Params, &p)
		if p.ThreadID != "archived" {
			t.Errorf("thread=%q", p.ThreadID)
		}
		return map[string]any{}
	})
	if err := deleteCodexThreads(context.Background(), s.client, []string{"archived"}); err != nil {
		t.Fatal(err)
	}
	if !s.methodOf("thread/delete").isRequest {
		t.Fatal("delete must be a request")
	}
	s.handleErr("thread/delete", -32602, "thread not found")
	if err := deleteCodexThreads(context.Background(), s.client, []string{"gone"}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"not found", "already deleted", "no rollout found for thread id gone", "thread gone does not exist", "thread gone already deleted"} {
		s.handleErr("thread/delete", -32600, message)
		if err := deleteCodexThreads(context.Background(), s.client, []string{"gone"}); err != nil {
			t.Fatalf("missing thread should be idempotent: %v", err)
		}
	}
	s.handleErr("thread/delete", -32603, "database unavailable")
	if err := deleteCodexThreads(context.Background(), s.client, []string{"keep"}); err == nil {
		t.Fatal("unexpected failure must preserve job")
	}
	for _, failure := range []struct {
		code    int
		message string
	}{{-32601, "method not found: thread/delete"}, {-32603, "thread database file not found"}, {-32603, "not found"}} {
		s.handleErr("thread/delete", failure.code, failure.message)
		if err := deleteCodexThreads(context.Background(), s.client, []string{"keep"}); err == nil {
			t.Fatalf("failure mistaken for a missing thread: %s", failure.message)
		}
	}
}

func TestStorageOpencodeListsAllProjectsAndProtectsAncestors(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "v2"}[v2], func(t *testing.T) {
			var deleted []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					deleted = append(deleted, r.URL.Path)
					if v2 {
						w.Write([]byte(`{"data":true}`))
					} else {
						w.Write([]byte(`true`))
					}
					return
				}
				if r.URL.Query().Has("directory") {
					t.Error("global listing was filtered to one directory")
				}
				page := r.URL.Query().Get("cursor")
				if v2 {
					if r.URL.Path != "/api/session" {
						t.Errorf("path=%s", r.URL.Path)
					}
					if page == "" {
						w.Write([]byte(`{"data":[{"id":"parent"},{"id":"orphan"}],"cursor":{"next":"second"}}`))
					} else {
						w.Write([]byte(`{"data":[{"id":"keep-child","parentID":"parent"}],"cursor":{"next":null}}`))
					}
				} else {
					if r.URL.Path != "/experimental/session" {
						t.Errorf("path=%s", r.URL.Path)
					}
					if page == "" {
						w.Header().Set("x-next-cursor", "123")
						w.Write([]byte(`[{"id":"parent"},{"id":"orphan"}]`))
					} else {
						w.Write([]byte(`[{"id":"keep-child","parentID":"parent"}]`))
					}
				}
			}))
			defer server.Close()
			s := &opencodeServer{url: server.URL, http: server.Client(), v2: v2, directory: "/one-project"}
			n, err := s.pruneStorageSessions(context.Background(), nil, map[string]bool{"keep-child": true})
			if err != nil || n != 1 {
				t.Fatalf("n=%d err=%v", n, err)
			}
			want := []string{"/session/orphan"}
			if v2 {
				want = []string{"/api/session/orphan"}
			}
			if !reflect.DeepEqual(deleted, want) {
				t.Fatalf("deleted=%v", deleted)
			}
			if _, err = s.pruneStorageSessions(context.Background(), []string{"parent"}, map[string]bool{"keep-child": true}); err == nil {
				t.Fatal("deletion of a referenced ancestor must fail closed")
			}
		})
	}
}
func TestStorageNativeDeleteAPIs(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				t.Errorf("method=%s", r.Method)
			}
			if v2 && r.URL.Path != "/api/session/ses_1" || !v2 && r.URL.Path != "/session/ses_1" {
				t.Errorf("path=%s", r.URL.Path)
			}
			if v2 {
				w.Write([]byte(`{"data":true}`))
			} else {
				w.Write([]byte(`true`))
			}
		}))
		s := &opencodeServer{url: server.URL, http: server.Client(), v2: v2}
		if err := s.deleteStorageSession(context.Background(), "ses_1"); err != nil {
			t.Fatal(err)
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/sessions/session_1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"status":"deleted"}`))
	}))
	defer server.Close()
	if err := deleteKimiSessionHTTP(context.Background(), server.URL, "test-token", "session_1"); err != nil {
		t.Fatal(err)
	}
}
