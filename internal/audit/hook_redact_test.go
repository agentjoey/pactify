package audit

import (
	"strings"
	"testing"
)

func TestRedactMasksBroadSecretShapes(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		leaked  string // must NOT survive
		visible string // must survive (introducer / context)
	}{
		{
			name:    "env var with SECRET+KEY",
			in:      "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG deploy",
			leaked:  "wJalrXUtnFEMI",
			visible: "AWS_SECRET_ACCESS_KEY=***",
		},
		{
			name:    "flag --api-key",
			in:      "curl --api-key=abc123def https://example.com",
			leaked:  "abc123def",
			visible: "--api-key=***",
		},
		{
			name:    "flag --password",
			in:      "mysql --password=hunter2 -u root",
			leaked:  "hunter2",
			visible: "--password=***",
		},
		{
			name:    "PGPASSWORD env",
			in:      "PGPASSWORD=s3cr3t psql -h db",
			leaked:  "s3cr3t",
			visible: "PGPASSWORD=***",
		},
		{
			name:    "pwd-suffixed var",
			in:      "DB_PWD=topsecret run",
			leaked:  "topsecret",
			visible: "DB_PWD=***",
		},
		{
			name:    "github classic token",
			in:      "git push https://example.com x ghp_abcdefghij0123456789KL",
			leaked:  "ghp_abcdefghij0123456789KL",
			visible: "git push",
		},
		{
			name:    "github fine-grained PAT",
			in:      "echo github_pat_11ABCDEFG0123456789_abcdef",
			leaked:  "github_pat_11ABCDEFG",
			visible: "echo",
		},
		{
			name:    "aws access key id",
			in:      "aws configure set aws_access_key_id AKIAIOSFODNN7EXAMPLE",
			leaked:  "AKIAIOSFODNN7EXAMPLE",
			visible: "aws configure",
		},
		{
			name:    "url basic auth",
			in:      "git clone https://alice:p4ssw0rd@github.com/x/y.git",
			leaked:  "alice:p4ssw0rd",
			visible: "https://***@github.com/x/y.git",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redact(c.in)
			if strings.Contains(got, c.leaked) {
				t.Fatalf("secret leaked: redact(%q) = %q", c.in, got)
			}
			if !strings.Contains(got, c.visible) {
				t.Fatalf("context lost: redact(%q) = %q, want it to contain %q", c.in, got, c.visible)
			}
		})
	}
}

func TestRedactLeavesBenignCommandsAlone(t *testing.T) {
	benign := []string{
		"git checkout -b feature/keyboard",
		"ls -la /home/me/keyboards",
		"grep -r monkey ./src",
		"go test ./internal/stats/",
		"curl https://api.example.com/v1/items",
	}
	for _, in := range benign {
		if got := redact(in); got != in {
			t.Fatalf("benign command mangled: redact(%q) = %q", in, got)
		}
	}
}
