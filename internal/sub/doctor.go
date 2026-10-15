package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type DoctorCheck struct {
	Name   string
	Status string
	Reason string
}

type DoctorReport struct {
	Harness string
	Version string
	Checks  []DoctorCheck
}

func (r DoctorReport) Passed() bool {
	if len(r.Checks) == 0 {
		return false
	}
	for _, c := range r.Checks {
		if c.Status == "FAIL" {
			return false
		}
	}
	return true
}

type doctorDeps struct {
	lookPath   func(string) (string, error)
	run        func(context.Context, Store, string, Harness, WorkerConfig) (Result, error)
	now        func() time.Time
	tempParent string
}

// Doctor uses disposable repositories and job storage; only the verified-version
// table is written to the caller's home. No process-global environment is changed.
func Doctor(ctx context.Context, store Store, names []string, out io.Writer) (bool, error) {
	if len(names) == 0 {
		names = Names()
	}
	harnesses := make([]Harness, 0, len(names))
	for _, name := range names {
		h, ok := Lookup(name)
		if !ok {
			return false, fmt.Errorf("unknown harness %q; available: %s", name, strings.Join(Names(), ", "))
		}
		harnesses = append(harnesses, h)
	}
	reports, err := runDoctor(ctx, store, harnesses, doctorDeps{})
	passed := err == nil
	for _, r := range reports {
		if _, e := fmt.Fprintf(out, "%s %s\n", r.Harness, r.Version); e != nil {
			return false, e
		}
		for _, c := range r.Checks {
			if _, e := fmt.Fprintf(out, "  %s %s: %s\n", c.Status, c.Name, c.Reason); e != nil {
				return false, e
			}
		}
		passed = passed && r.Passed()
	}
	return passed, err
}

func runDoctor(ctx context.Context, store Store, harnesses []Harness, d doctorDeps) ([]DoctorReport, error) {
	if d.lookPath == nil {
		d.lookPath = exec.LookPath
	}
	if d.run == nil {
		d.run = RunJob
	}
	if d.now == nil {
		d.now = time.Now
	}
	root, err := os.MkdirTemp(d.tempParent, "pactify-sub-doctor-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	var reports []DoctorReport
	for _, h := range harnesses {
		r := DoctorReport{Harness: h.Name}
		add := func(name string, ok bool, reason string) {
			status := "FAIL"
			if ok {
				status = "PASS"
			}
			r.Checks = append(r.Checks, DoctorCheck{name, status, reason})
		}
		skip := func(name, reason string) { r.Checks = append(r.Checks, DoctorCheck{name, "SKIP", reason}) }
		_, installErr := d.lookPath(h.Name)
		if h.Name == "fake" && os.Getenv("PACTIFY_SUB_FAKE") == "1" {
			installErr = nil
		}
		if installErr == nil {
			if h.Version == nil {
				installErr = fmt.Errorf("no version probe")
			} else {
				r.Version, installErr = h.Version(ctx)
				if installErr == nil && strings.TrimSpace(r.Version) == "" {
					installErr = fmt.Errorf("version is empty")
				}
			}
		} else {
			installErr = fmt.Errorf("not installed (PATH): %w", installErr)
		}
		if installErr != nil {
			add("Installed", false, installErr.Error())
			for _, name := range []string{"Write access", "Model", "Read-only access", "Network", "Session hiding"} {
				skip(name, "CLI unavailable; not run")
			}
			reports = append(reports, r)
			continue
		}
		add("Installed", true, "CLI version "+r.Version)
		jobStore := Store{Root: filepath.Join(root, h.Name, "home", "jobs")}
		network := h.Name == "codex"
		model := "gpt-6-luna"
		if h.Kind == "kimi-cli" {
			model = "kimi-code/kimi-for-coding"
		}
		run := func(access, brief string) (Result, string, error) {
			repo := filepath.Join(root, h.Name, access)
			if err := initDoctorRepo(repo, network && access == "write"); err != nil {
				return Result{}, repo, err
			}
			id := access
			if _, err := jobStore.Create(id, Request{Harness: h.Name, Model: model, Access: access, Cwd: repo, Brief: brief}); err != nil {
				return Result{}, repo, err
			}
			jobCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			result, err := d.run(jobCtx, jobStore, id, h, WorkerConfig{ToolCallLimit: 40})
			return result, repo, err
		}
		brief := "Run a shell command to create out.txt containing doctor-ok in the current directory. Do not just describe it. Return a short final reply."
		if network {
			brief += " Also run exactly `python3 doctor-network.py`. Do not edit that script or create doctor-network.json yourself; the script records the real local port bind and external HTTPS results."
		}
		writable, repo, writeErr := run("write", brief)
		goodWrite := writeErr == nil && writable.Status == StatusSucceeded && writable.CLIVersion == r.Version
		info, fileErr := os.Lstat(filepath.Join(repo, "out.txt"))
		wrote := fileErr == nil && info.Mode().IsRegular()
		add("Write access", goodWrite && wrote && strings.TrimSpace(writable.FinalText) != "" && writable.InputTokens+writable.OutputTokens > 0,
			fmt.Sprintf("status=%s, file=%t, final reply=%t, tokens=%d, version=%s%s", writable.Status, wrote, strings.TrimSpace(writable.FinalText) != "", writable.InputTokens+writable.OutputTokens, writable.CLIVersion, doctorError(writeErr)))
		add("Model", goodWrite && writable.ModelVerified && writable.ModelActual == model, fmt.Sprintf("requested=%s, actual=%s, independently verified=%t", model, writable.ModelActual, writable.ModelVerified))
		results := []Result{writable}
		ranAll := writeErr == nil && writable.Status == StatusSucceeded
		if h.NoReadOnly != "" {
			skip("Read-only access", h.NoReadOnly)
		} else {
			readonly, roRepo, roErr := run("read-only", "Use your shell execution tool to execute this exact command once in the current directory, even if you expect the sandbox to deny the write:\n\n```sh\nsh -c 'echo hi > out.txt'; echo EXIT=$?\n```\n\nReport the command's stdout and stderr verbatim, including the EXIT line. Do not predict the result or claim a denial without invoking the tool. Do not request elevated permissions, retry, or seek a workaround; report the actual output and stop.")
			results = append(results, readonly)
			_, statErr := os.Lstat(filepath.Join(roRepo, "out.txt"))
			ok := roErr == nil && readonly.Status == StatusSucceeded && readonly.CLIVersion == r.Version && readonly.ToolCalls > 0 && strings.TrimSpace(readonly.FinalText) != "" && os.IsNotExist(statErr) && len(readonly.Diff.Files) == 0
			add("Read-only access", ok, fmt.Sprintf("status=%s, write target absent=%t, tool calls=%d%s", readonly.Status, os.IsNotExist(statErr), readonly.ToolCalls, doctorError(roErr)))
			ranAll = ranAll && roErr == nil && readonly.Status == StatusSucceeded
		}
		if !network {
			skip("Network", "only codex write access checks local ports and external networking")
		} else {
			var evidence struct {
				Bind     bool `json:"bind"`
				External bool `json:"external"`
			}
			data, e := os.ReadFile(filepath.Join(repo, "doctor-network.json"))
			valid := e == nil && json.Unmarshal(data, &evidence) == nil
			add("Network", goodWrite && valid && evidence.Bind && evidence.External, fmt.Sprintf("local port bind=%t, external HTTPS=%t, valid evidence=%t", evidence.Bind, evidence.External, valid))
		}
		hidden := ranAll && (h.HidesItself || h.Conceal != nil)
		var warnings []string
		for _, result := range results {
			for _, w := range result.Warnings {
				if strings.Contains(w, sessionHidingFailed) || strings.Contains(w, sessionArchivingFailed) {
					hidden = false
					warnings = append(warnings, w)
				}
			}
		}
		reason := "All probe sessions were hidden through the normal job lifecycle; no hiding or archiving failures reported"
		if !hidden {
			reason = "Cannot confirm session hiding"
			if len(warnings) > 0 {
				reason = strings.Join(warnings, "; ")
			}
		}
		add("Session hiding", hidden, reason)
		if r.Passed() {
			if err := recordVerifiedVersion(ctx, filepath.Dir(store.Root), h.Name, r.Version, d.now()); err != nil {
				return append(reports, r), fmt.Errorf("save verified version for %s: %w", h.Name, err)
			}
		}
		reports = append(reports, r)
	}
	return reports, nil
}

func doctorError(err error) string {
	if err != nil {
		return ", error=" + err.Error()
	}
	return ""
}

func initDoctorRepo(dir string, network bool) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if network {
		if err := os.WriteFile(filepath.Join(dir, "doctor-network.py"), []byte(doctorNetworkScript), 0600); err != nil {
			return err
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=Pactify Doctor", "-c", "user.email=doctor@localhost", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "doctor baseline"}} {
		if _, err := runGit(dir, args...); err != nil {
			return err
		}
	}
	return nil
}

const doctorNetworkScript = `import json
import socket
import urllib.request

with socket.socket() as listener:
    listener.bind(("127.0.0.1", 0))
    listener.listen(1)
    with urllib.request.urlopen("https://example.com", timeout=20) as response:
        if response.status != 200:
            raise RuntimeError("external HTTPS failed")
        response.read(1)
    with open("doctor-network.json", "w") as evidence:
        json.dump({"bind": True, "external": True}, evidence)
print("local bind and external HTTPS succeeded")
`
