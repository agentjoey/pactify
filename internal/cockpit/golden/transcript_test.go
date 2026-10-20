package golden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	raw := `{"Authorization":"Bearer secret-value","cookie":"session=private","password":"hidden","key":"provider-opaque-key","token":"opaque","device_id":"device-value","email":"person@example.com","path":"` + "/" + `Users/alice/repo","nested":"password=hunter2 token=abc /home/bob/repo Bearer abc.def"}`
	got := Sanitize(raw)
	for _, secret := range []string{"secret-value", "session=private", "hidden", "provider-opaque-key", "opaque", "device-value", "person@example.com", "/" + "Users/alice", "hunter2", "token=abc", "/home/bob", "abc.def"} {
		if strings.Contains(got, secret) {
			t.Errorf("leaked %q: %s", secret, got)
		}
	}
	if err := Scan(got); err != nil {
		t.Fatal(err)
	}
}

func TestScannerRejectsSensitiveData(t *testing.T) {
	for _, s := range []string{`{"token":"opaque"}`, `{"password":"secret"}`, `{"Authorization":"Basic xyz"}`, `{"cookie":"private"}`, `{"device_id":"abc"}`, `person@example.com`, "/" + "Users/alice/repo", `C:\Users\alice\repo`, `/home/bob/a`} {
		if Scan(s) == nil {
			t.Errorf("accepted %s", s)
		}
	}
}

func TestRedactsMultilineWireAndEmbeddedSecrets(t *testing.T) {
	raw := "{\"device_id\":\"unique-device\"}\n{\"token\":\"opaque-secret\"}\nlog sk-abcdefghijklmnopqrstuvwxyz123456 github_pat_123456789012345678901234567890\n"
	got := Sanitize(raw)
	for _, secret := range []string{"unique-device", "opaque-secret", "sk-abcdefghijklmnopqrstuvwxyz123456", "github_pat_123456789012345678901234567890"} {
		if strings.Contains(got, secret) {
			t.Errorf("leaked %q", secret)
		}
	}
}

func TestRecorderConcurrentSnapshot(t *testing.T) {
	r := &Recorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			r.Observe("stdout", []byte(`{"token":"private"}`))
		}
	}()
	for i := 0; i < 100; i++ {
		_ = r.Snapshot()
	}
	<-done
	frames := r.Snapshot()
	if len(frames) != 100 {
		t.Fatalf("lost frames: %d", len(frames))
	}
	frames[0].Data = "changed"
	if strings.Contains(r.Snapshot()[0].Data, "private") || r.Snapshot()[0].Data == "changed" {
		t.Fatal("snapshot leaked recorder state or sensitive data")
	}
}

func TestManagedDirectoryRejectsEscapingLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	inside := filepath.Join(root, "home")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ManagedDirectory(root, inside); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, outside, link} {
		if ManagedDirectory(root, path) == nil {
			t.Errorf("accepted unsafe record home %s", path)
		}
	}
}

func TestRedactsLocalUserName(t *testing.T) {
	re := localUser()
	if re == nil {
		t.Skip("no usable local user name")
	}
	name := re.String()[2 : len(re.String())-2]
	raw := "-rw-r--r--  1 " + name + "  wheel  5 Oct 17 golden.txt"
	if err := Scan(raw); err == nil {
		t.Fatal("scan accepted the local user name")
	}
	got := Sanitize(raw)
	if strings.Contains(got, name) || !strings.Contains(got, UserPlaceholder) {
		t.Fatalf("Sanitize = %q", got)
	}
	if err := Scan(got); err != nil {
		t.Fatal(err)
	}
}
