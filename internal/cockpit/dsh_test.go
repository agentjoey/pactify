package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixture speaks rc.5 SDK only. No vendor executable or network is used.
func dshFixture(t *testing.T, mode string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/usr/bin/env python3
import json,sys,os,signal,time,subprocess
signal.signal(signal.SIGTERM, lambda *args: sys.exit(0))
def send(x): print(json.dumps(x),flush=True)
def event(s,t,d): send({'jsonrpc':'2.0','method':'session.event','params':{'sessionId':s,'event':{'type':t,'seq':1,'time':0,'data':d}}})
for line in sys.stdin:
 r=json.loads(line); m=r['method']; p=r.get('params',{}); result={}
 if os.environ.get('DSH_TEST_SIBLING_LEAK'): print(os.environ['ANTHROPIC_API_KEY'],file=sys.stderr,flush=True)
 if os.environ.get('DSH_TEST_LEAK'): print(os.environ['DEEPSEEK_API_KEY'],file=sys.stderr,flush=True)
 if m=='initialize':
  assert p['provider']==os.environ.get('DSH_TEST_PROVIDER','deepseek-official')
  check=os.environ.get('DSH_TEST_ENV')
  if check:
   keys=json.loads(os.environ['DSH_TEST_ENV_KEYS'])
   assert all(os.environ.get(k)=='fixture-'+k if check=='explicit' else os.environ.get(k)=='' for k in keys)
   assert os.environ['DEEPSEEK_API_KEY']=='fixture-deepseek'
   assert os.environ['DEEPSEEK_BASE_URL']=='https://fixture.invalid'
  assert os.environ['DSH_PERMISSION_MODE']=='read-only'
  assert os.environ['DSH_TELEMETRY_DISABLED']=='1'
  childmode=os.environ.get('DSH_TEST_CHILD')
  if childmode:
   childscript="import os,signal,sys,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); open(sys.argv[1],'w').write(str(os.getpid())); time.sleep(60)"
   subprocess.Popen([sys.executable,'-c',childscript,os.environ['DSH_TEST_CHILD_PID']],start_new_session=childmode=='detached',stdout=subprocess.DEVNULL if childmode=='group-closed' else None,stderr=subprocess.DEVNULL if childmode=='group-closed' else None)
   while not os.path.exists(os.environ['DSH_TEST_CHILD_PID']): time.sleep(.01)
  result={'serverInfo':{'name':os.environ.get('DSH_TEST_SERVER_NAME','deepseek-harness-sdk-runtime'),'version':'0.0.1'}}
 elif m=='test/pending': continue
 elif m=='session/prompt':
  sid=p['sessionId']; result={'messageId':'m1'}
 elif m=='session/wait':
  mode=os.environ['DSH_TEST_MODE']
  if mode=='large-stderr':
   sys.stderr.write('x'*(5*1024*1024)+'\n'+'y'*(256*1024)+'\n');sys.stderr.flush()
  if mode in ('server-request','server-stalled'):
   send({'jsonrpc':'2.0','id':'permission-1' if mode=='server-request' else 'x'*1048576,'method':os.environ.get('DSH_TEST_REQUEST_METHOD','approval/request'),'params':{}})
   if mode=='server-request':
    reply=json.loads(sys.stdin.readline())
    assert reply['id']=='permission-1' and reply['error']['code']==-32601
  if mode=='interrupt': time.sleep(60)
  if mode=='crash': sys.exit(7)
  event(sid,'unknown/future',{})
  if os.environ.get('DSH_TEST_LEAK'): event(sid,'tool/call',{'name':'bash','arguments':os.environ['DEEPSEEK_API_KEY']})
  event(sid,'tool/call',{'name':'bash','callId':'c','arguments':'{"command":"ls"}'})
  event(sid,'tool/result',{'message':{'toolCallId':'c','content':[{'type':'text','text':'ok'}]}})
  for s,txt in [(sid,'step'),('child','CHILD'),(sid,'DONE')]:
   event(s,'assistant/message',{'message':{'content':[{'type':'reasoning','text':'SECRET'},{'type':'text','text':txt}],'source':{'provider':'deepseek-official','model':'deepseek-v4-flash'}},'usage':{'inputTokens':10,'outputTokens':2,'totalTokens':12}})
  if mode!='missing': event(sid,'turn/end',{'reason':json.loads(os.environ['DSH_TEST_REASON']) if os.environ.get('DSH_TEST_REASON') else {'kind': 'completed' if mode in ('ok','server-request','server-stalled','large-stderr') else mode}})
 elif m=='session/resume': pass
 elif m=='shutdown':
  send({'jsonrpc':'2.0','id':r['id'],'result':{}}); sys.exit(0)
 else:
  send({'jsonrpc':'2.0','id':r['id'],'error':{'code':-32603,'message':os.environ.get('DSH_TEST_RPC_ERROR','unknown')}}); continue
 send({'jsonrpc':'2.0','id':r['id'],'result':result})
 if m=='session/wait' and mode=='server-stalled': time.sleep(60)
`
	if err := os.WriteFile(filepath.Join(dir, "dsh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DSH_TEST_MODE", mode)
}
func TestDSHTurn(t *testing.T) {
	for _, mode := range []string{"ok", "aborted", "future", "missing", "crash", "interrupt"} {
		t.Run(mode, func(t *testing.T) {
			dshFixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := NewDSHBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.Prompt(ctx, UserMessage{Text: "hello"}); err != nil {
				t.Fatal(err)
			}
			if mode == "interrupt" {
				time.Sleep(50 * time.Millisecond)
				if err = s.Interrupt(ctx); err != nil {
					t.Fatal(err)
				}
			}
			final := ""
			tokens := 0
			tools := 0
			for {
				select {
				case e := <-s.Events():
					if e.Kind == EventMessage && e.Final {
						final = e.Text
					}
					if e.Usage != nil {
						tokens = e.Usage.InputTokens + e.Usage.OutputTokens
					}
					if e.Tool != nil {
						if e.Tool.Name != "bash" {
							t.Fatalf("tool name missing: %+v", e.Tool)
						}
						tools++
					}
					if e.State == StateTurnCompleted || e.State == StateTurnFailed {
						if (e.State == StateTurnCompleted) != (mode == "ok") {
							t.Fatalf("state=%+v", e)
						}
						if mode == "ok" && (final != "DONE" || tokens != 36 || tools != 2) {
							t.Fatalf("text=%q tokens=%d tools=%d", final, tokens, tools)
						}
						if mode == "interrupt" && e.Err != "canceled" {
							t.Fatalf("interrupt=%+v", e)
						}
						return
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
		})
	}
}
func TestDSHResumeAndRPCError(t *testing.T) {
	dshFixture(t, "ok")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewDSHBackend().(OptsResumer).ResumeWith(ctx, "existing", StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.ThreadID() != "existing" {
		t.Fatal(s.ThreadID())
	}
	if _, err := s.(*dshSession).call(ctx, "session/query", map[string]any{}); err == nil {
		t.Fatal("unknown method succeeded")
	}
}

func TestDSHTranscriptSanitization(t *testing.T) {
	dshFixture(t, "ok")
	t.Setenv("DSH_TEST_LEAK", "1")
	t.Setenv("DEEPSEEK_API_KEY", "fixture-opaque-credential")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	recorder := &golden.Recorder{}
	s, err := NewDSHBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly, Transcript: recorder, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Prompt(ctx, UserMessage{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	for e := range s.Events() {
		if e.State == StateTurnCompleted {
			break
		}
	}
	s.Close()
	data, _ := json.Marshal(recorder.Snapshot())
	if strings.Contains(string(data), "fixture-opaque-credential") || strings.Contains(stderr.String(), "fixture-opaque-credential") {
		t.Fatal("credential leaked")
	}
	if err = golden.Scan(string(data)); err != nil {
		t.Fatal(err)
	}
}

func TestDSHSyntheticGoldenReplay(t *testing.T) {
	dshFixture(t, "ok")
	// Until the registry selects dsh, prevent the helper's default vendor from running.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	recorder := &golden.Recorder{}
	tr := golden.Transcript{Harness: "dsh", Scenario: "readonly", Resume: "existing", Prompt: "synthetic SDK replay"}
	got, err := goldenRun(ctx, tr, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly, Transcript: recorder}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	tr.Expected, _ = json.Marshal(got)
	tr.Frames = recorder.Snapshot()
	if err = replayGolden(t, tr); err != nil {
		t.Fatal(err)
	}
}

func TestDSHPromptAfterClose(t *testing.T) {
	dshFixture(t, "ok")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := NewDSHBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Prompt(ctx, UserMessage{Text: "late"}); err == nil {
		t.Fatal("closed session accepted prompt")
	}
}

func TestDSHRuntimeHandshake(t *testing.T) {
	for _, name := range []string{"deepseek-harness-sdk-runtime", "wrong-runtime"} {
		t.Run(name, func(t *testing.T) {
			dshFixture(t, "ok")
			t.Setenv("DSH_TEST_SERVER_NAME", name)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := NewDSHBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Access: AccessReadOnly})
			if name == "wrong-runtime" {
				if err == nil {
					s.Close()
					t.Fatal("wrong runtime accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
		})
	}
}
