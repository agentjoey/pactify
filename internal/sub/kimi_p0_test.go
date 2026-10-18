package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestKimiP0WireFailureRetries(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "retry"}[retry], func(t *testing.T) {
			store, s, _, h, _, home := kimiJobFixture(t, false)
			n := 0
			s.script = func(s *fakeSession) {
				n++
				end := "{\"type\":\"turn.ended\",\"reason\":\"failed\",\"error\":{\"code\":\"provider.connection_error\",\"message\":\"connection failed\"}}\n"
				if n > 1 {
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Actual result"}
					end = "{\"type\":\"turn.ended\",\"reason\":\"completed\"}\n"
				}
				writeKimiJobRecord(t, home, s.ThreadID(), end)
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			st, _ := store.Load("j1")
			limit := 0
			if retry {
				limit = 1
			}
			st.Request.Retries = &limit
			store.Save(st)
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }, KimiWireTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if retry {
				if r.Status != StatusSucceeded || len(r.Retries) != 1 || n != 2 {
					t.Fatalf("%+v prompts=%d", r, n)
				}
			} else if r.Status != StatusFailed || r.FailureClass != "transient" {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestKimiP0ACPModelsNoPrompt(t *testing.T) {
	source := fakeKimiUserHome(t)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte("default_model=\"offline\"\n[models.offline]\n"), 0600)
	bin := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("AJ712_TRACE", trace)
	script := `#!/usr/bin/env python3
import json,os,sys
with open(os.environ['AJ712_TRACE'],'a') as trace:
 for line in sys.stdin:
  q=json.loads(line); trace.write(q['method']+'\n');trace.flush()
  if q['method']=='initialize':r={'protocolVersion':1,'agentCapabilities':{}}
  elif q['method']=='session/new':r={'sessionId':'discovery','configOptions':[{'id':'model','currentValue':'live/b','options':[{'value':'live/a'},{'value':'live/b'}]},{'id':'thinking','category':'thought_level','options':[{'value':'low'}]}]}
  else:sys.exit(9)
  print(json.dumps({'jsonrpc':'2.0','id':q['id'],'result':r}),flush=True)
`
	os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	got, err := kimiModelDetails(context.Background(), cockpit.StartOpts{Env: []string{"KIMI_CODE_HOME=" + source}, RepoDir: t.TempDir()})
	if err != nil || len(got.Items) != 2 || got.Items[0] != "live/a" || got.DefaultModel != "live/b" {
		t.Fatalf("%+v %v", got, err)
	}
	calls, _ := os.ReadFile(trace)
	if string(calls) != "initialize\nsession/new\n" {
		t.Fatalf("calls=%s", calls)
	}
	if _, err := os.Stat(filepath.Join(source, "session_index.jsonl")); !os.IsNotExist(err) {
		t.Fatal("discovery left user session")
	}
	h, _ := Lookup("kimi")
	h.Version = nil
	c := newHarnessCatalog()
	c.lookupPath = func(string) (string, error) { return "fake", nil }
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	for range 2 {
		row := c.harness(context.Background(), store, h, HarnessDefault{}, false)
		if len(row.Models.Items) != 2 || row.Models.Error != "" || row.Default.Model != "live/b" {
			t.Fatal(row)
		}
	}
	calls, _ = os.ReadFile(trace)
	if string(calls) != "initialize\nsession/new\ninitialize\nsession/new\n" {
		t.Fatalf("catalog cache sent extra calls: %s", calls)
	}

}

func TestKimiP0OfflineWarning(t *testing.T) {
	source := fakeKimiUserHome(t)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte("default_model=\"offline\"\n[models.offline]\n"), 0600)
	got, err := kimiModelDetails(context.Background(), cockpit.StartOpts{Env: []string{"KIMI_CODE_HOME=" + source}})
	if err != nil || len(got.Items) != 1 || got.Warning == "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestKimiP0QuotaFallback(t *testing.T) {
	store, s, _, h, _, home := kimiJobFixture(t, false)
	st, _ := store.Load("j1")
	st.Routing = &Routing{Remaining: []Route{{Harness: "codex"}}}
	store.Save(st)
	s.script = func(s *fakeSession) {
		writeKimiJobRecord(t, home, s.ThreadID(), "{\"type\":\"turn.ended\",\"reason\":\"failed\",\"error\":{\"code\":\"provider.rate_limit\",\"message\":\"subscription quota exhausted\"}}\n")
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || r.FailureClass != "quota" || r.Status != StatusFailed || r.FallbackTo == "" {
		t.Fatalf("%+v %v", r, err)
	}
	child, err := store.Load(r.FallbackTo)
	if err != nil || child.Request.Harness != "codex" || child.FallbackFrom != "j1" {
		t.Fatal(child, err)
	}
}

func TestKimiP0FakeACPWireFailure(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "retry"}[retry], func(t *testing.T) {
			store, _, _, h, _, _ := kimiJobFixture(t, false)
			h.Backend = func() cockpit.Backend { return cockpit.NewACPBackend("kimi-cli") }
			bin := t.TempDir()
			script := `#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
home=Path(os.environ['KIMI_CODE_HOME']);sd=home/'sessions'/'fake-acp';(sd/'agents'/'main').mkdir(parents=True)
(home/'session_index.jsonl').write_text(json.dumps({'sessionId':'fake-acp','sessionDir':str(sd)})+'\n')
n=0
for line in sys.stdin:
 q=json.loads(line);m=q['method']
 if 'id' not in q:continue
 if m=='initialize':r={'protocolVersion':1,'agentCapabilities':{'loadSession':True}}
 elif m=='session/new':r={'sessionId':'fake-acp'}
 elif m in ('session/set_mode','session/set_config_option'):r={}
 elif m=='session/prompt':
  n+=1
  with (sd/'agents'/'main'/'wire.jsonl').open('a') as f:
   events=[{'type':'llm.request','model':'fake/model'},{'type':'usage.record','usageScope':'turn','usage':{'inputOther':10,'output':2}},{'type':'turn.ended','reason':'completed' if n>1 else 'failed'}]
   if n==1:events[-1]['error']={'code':'provider.connection_error','message':'Connection error.'}
   for e in events:f.write(json.dumps(e)+'\n')
  if n>1:print(json.dumps({'jsonrpc':'2.0','method':'session/update','params':{'sessionId':'fake-acp','update':{'sessionUpdate':'agent_message_chunk','content':{'type':'text','text':'Actual result'}}}}),flush=True)
  r={'stopReason':'end_turn'}
 else:sys.exit(8)
 print(json.dumps({'jsonrpc':'2.0','id':q['id'],'result':r}),flush=True)
`
			os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700)
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			st, _ := store.Load("j1")
			limit := 0
			if retry {
				limit = 1
			}
			st.Request.Retries = &limit
			store.Save(st)
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }})
			if err != nil {
				t.Fatal(err)
			}
			if retry {
				if r.Status != StatusSucceeded || len(r.Retries) != 1 || r.InputTokens != 20 {
					t.Fatalf("%+v", r)
				}
			} else if r.Status != StatusFailed || r.FailureClass != "transient" {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestKimiP0RetryCancellationRecoversCurrentUsage(t *testing.T) {
	store, s, _, h, _, home := kimiJobFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	turns := 0
	s.script = func(s *fakeSession) {
		turns++
		if turns == 1 {
			writeKimiJobRecord(t, home, s.ThreadID(), "{\"type\":\"turn.ended\",\"reason\":\"failed\",\"error\":{\"code\":\"provider.connection_error\",\"message\":\"lost\"}}\n")
			s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
		} else {
			writeKimiJobRecord(t, home, s.ThreadID(), "{\"type\":\"turn.ended\",\"reason\":\"cancelled\"}\n")
			cancel()
		}
	}
	r, err := RunJob(ctx, store, "j1", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return 0 }, KimiWireTimeout: time.Second})
	if err != nil || r.Status != StatusCanceled || r.InputTokens != 234 || r.OutputTokens != 18 {
		t.Fatalf("%+v %v", r, err)
	}
}
