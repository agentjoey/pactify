package audit

import (
	"strings"
	"testing"
)

func TestRedactMasksBroadSecretShapes(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		leaked     string // must NOT survive (empty skips)
		visible    string // must survive (introducer / context) (empty skips)
		notContain string // must NOT survive (for negative cases)
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
		{
			name:    "slack token",
			in:      "echo xoxb-1234567890-abcdef",
			leaked:  "1234567890-abcdef",
			visible: "xoxb-***",
		},
		{
			name:    "google api key",
			in:      "echo AIzaSyA12345678901234567890123456789012",
			leaked:  "SyA12345678901234567890123456789012",
			visible: "AIza***",
		},
		{
			name:    "stripe live key",
			in:      "stripe charge sk_live_abcd1234efgh",
			leaked:  "abcd1234efgh",
			visible: "sk_live_***",
		},
		{
			name:    "gcp oauth token",
			in:      "echo ya29.a0AfB_byCycQ2F3",
			leaked:  "a0AfB_byCycQ2F3",
			visible: "ya29.***",
		},
		{
			name:    "jwt",
			in:      "echo eyJhbGciOi.eyJzdWIi.SflKxwRJ",
			leaked:  "eyJhbGciOi",
			visible: "***",
		},
		{
			name:    "pem private key header",
			in:      "echo -----BEGIN RSA PRIVATE KEY----- MIIEpAIBAAKCAQEA...",
			leaked:  "BEGIN RSA PRIVATE KEY",
			visible: "-----BEGIN PRIVATE KEY----- ***",
		},
		{
			name:       "slack token too short",
			in:         "echo xoxb-123",
			visible:    "echo xoxb-123",
			notContain: "***",
		},
		{
			name:       "google api key too short",
			in:         "echo AIza12345",
			visible:    "echo AIza12345",
			notContain: "***",
		},
		{
			name:       "stripe key missing value",
			in:         "echo sk_live_",
			visible:    "echo sk_live_",
			notContain: "***",
		},
		{
			name:       "gcp oauth missing value",
			in:         "echo ya29.",
			visible:    "echo ya29.",
			notContain: "***",
		},
		{
			name:       "jwt missing segments",
			in:         "echo eyJhbGciOi",
			visible:    "echo eyJhbGciOi",
			notContain: "***",
		},
		{
			name:       "pem keyword not a header",
			in:         "openssl genrsa -out private.key 2048",
			visible:    "openssl genrsa -out private.key 2048",
			notContain: "***",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redact(c.in)
			if c.leaked != "" && strings.Contains(got, c.leaked) {
				t.Fatalf("secret leaked: redact(%q) = %q", c.in, got)
			}
			if c.visible != "" && !strings.Contains(got, c.visible) {
				t.Fatalf("context lost: redact(%q) = %q, want it to contain %q", c.in, got, c.visible)
			}
			if c.notContain != "" && strings.Contains(got, c.notContain) {
				t.Fatalf("false positive: redact(%q) = %q, must not contain %q", c.in, got, c.notContain)
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
		"git status",
		"cat README.md",
	}
	for _, in := range benign {
		if got := redact(in); got != in {
			t.Fatalf("benign command mangled: redact(%q) = %q", in, got)
		}
	}
}
