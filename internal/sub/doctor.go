package sub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/orchestrate"
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

type DoctorOptions struct {
	Model string // Empty selects the harness's default doctor model.
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
	model      string
}

// Doctor uses disposable repositories and job storage; only the verified-version
// table is written to the caller's home. No process-global environment is changed.
func Doctor(ctx context.Context, store Store, names []string, out io.Writer, options ...DoctorOptions) (bool, error) {
	var opts DoctorOptions
	if len(options) > 1 {
		return false, fmt.Errorf("expected at most one set of doctor options")
	}
	if len(options) == 1 {
		opts = options[0]
	}
	if opts.Model != "" && len(names) != 1 {
		return false, fmt.Errorf("--model requires exactly one harness")
	}
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
	if _, err := fmt.Fprintln(out, "Storage"); err != nil {
		return false, err
	}
	for _, check := range doctorStorageChecks(store, nil) {
		if _, err := fmt.Fprintf(out, "  %s %s: %s\n", check.Status, check.Name, check.Reason); err != nil {
			return false, err
		}
	}
	reports, err := runDoctor(ctx, store, harnesses, doctorDeps{model: opts.Model})
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
		abortProbe := func(name, reason string) {
			add(name, false, reason)
			for _, check := range []string{"Write access", "Model", "Read-only access", "Network", "Session hiding"} {
				if check != name {
					skip(check, "preflight failed; not run")
				}
			}
			if h.Name == "agy" {
				skip("Read-only hook", "preflight failed; not run")
			}
			if h.Home != nil && name != "Managed home" {
				skip("Managed home", "preflight failed; not run")
			}
		}

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
				if installErr == nil {
					installErr = h.unsupported(r.Version)
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
			if h.Name == "agy" {
				skip("Read-only hook", "CLI unavailable; not run")
			}
			if h.Home != nil {
				skip("Managed home", "CLI unavailable; not run")
			}
			reports = append(reports, alignDoctorChecks(r))
			continue
		}
		add("Installed", true, "CLI version "+r.Version)
		jobStore := Store{Root: filepath.Join(root, h.Name, "home", "jobs")}
		network := h.Name == "codex" || h.Name == "opencode" || h.Name == "agy" || h.Name == "kimi"
		model := "gpt-6-luna"
		if h.Name == "agy" {
			model = cockpit.AgyDefaultModel
		}
		if h.DoctorModel != "" {
			model = h.DoctorModel
		}
		if d.model != "" {
			model = d.model
		}

		var checkUserHome func() error
		if h.Home != nil && h.Home.BeforeDoctor != nil {
			var e error
			checkUserHome, e = h.Home.BeforeDoctor()
			if e != nil {
				abortProbe("Managed home", e.Error())
				reports = append(reports, alignDoctorChecks(r))
				continue
			}
		}
		// Explicit models follow RunJob's backend validation, just like sub run.
		// In particular, agy can proceed when its catalog pre-check is unavailable.
		// Other harnesses retain fixed DoctorModel defaults; agy discovery is flash-only.
		if d.model == "" && (h.DoctorModel == "" || h.Name == "agy") && h.Models != nil {
			opts := cockpit.StartOpts{Env: orchestrate.CrossVendorStrip(h.Kind)}
			var modelHomes []*managedHome
			if h.Home != nil {
				_, _, env, layouts, e := prepareHarnessHomes(ctx, h.Home, "", nil, true, filepath.Join(filepath.Dir(store.Root), "credential-quarantine"))
				modelHomes = layouts
				opts.Env = append(opts.Env, env...)
				if e != nil {
					for _, layout := range modelHomes {
						if warnings := layout.Cleanup(); len(warnings) > 0 {
							e = fmt.Errorf("%w; %s", e, strings.Join(warnings, "; "))
						}
					}
					abortProbe("Managed home", e.Error())
					reports = append(reports, alignDoctorChecks(r))
					continue
				}
			}
			if h.Name == "opencode" {
				opts.Env = append(opts.Env, opencodeVendorStrip()...)
			}
			if h.Name == "opencode" {
				opts.MinimalConfig = true
				opts.RepoDir = filepath.Join(root, h.Name, "models")
				if e := os.MkdirAll(opts.RepoDir, 0700); e != nil {
					return reports, e
				}
			}
			models, e := h.Models(ctx, opts)
			for _, layout := range modelHomes {
				if warnings := layout.Cleanup(); len(warnings) > 0 {
					e = fmt.Errorf("model listing error=%v; %s", e, strings.Join(warnings, "; "))
				}
			}
			if e != nil || len(models) == 0 {
				abortProbe("Model", fmt.Sprintf("cannot list models: %v", e))
				reports = append(reports, alignDoctorChecks(r))
				continue
			}
			model = cheapestDoctorModel(models)
			if h.Name == "agy" {
				model = ""
				for _, m := range models {
					if strings.HasPrefix(m, "gemini-3.8-flash-") && (model == "" || strings.HasSuffix(m, "-low")) {
						model = m
					}
				}
			}
			if h.Name == "opencode" {
				model = opencodeDoctorModel(models)
			}
		}
		if h.Name == "agy" && !strings.HasPrefix(model, "gemini-3.8-flash-") {
			abortProbe("Model", "agy doctor requires an available gemini-3.8-flash-* model")
			reports = append(reports, alignDoctorChecks(r))
			continue
		}
		run := func(access, brief string) (Result, string, error) {
			repo := filepath.Join(root, h.Name, access)
			if err := initDoctorRepo(repo, network && access == "write"); err != nil {
				return Result{}, repo, err
			}
			id := access
			mode := access
			if strings.HasPrefix(access, "read-only") && access != "read-only-hook" {
				mode = "read-only"
			}
			if access == "read-only-hook" {
				mode = "write"
			}
			if _, err := jobStore.Create(id, Request{Harness: h.Name, Model: model, Access: mode, Cwd: repo, Brief: brief}); err != nil {
				return Result{}, repo, err
			}
			jobCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			result, err := d.run(jobCtx, jobStore, id, h, WorkerConfig{ToolCallLimit: 40, CacheHome: filepath.Dir(store.Root), ProcessMarker: fmt.Sprintf("doctor-%x-%s", sha256.Sum256([]byte(jobStore.Root)), id), ReadOnlyPolicy: access == "read-only-hook"})
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
		writeReason := fmt.Sprintf("status=%s, file=%t, final reply=%t, tokens=%d, version=%s%s", writable.Status, wrote, strings.TrimSpace(writable.FinalText) != "", writable.InputTokens+writable.OutputTokens, writable.CLIVersion, doctorError(writeErr))
		if writeErr == nil && writable.Status != StatusSucceeded && len(writable.Warnings) > 0 {
			writeReason += "; " + strings.Join(writable.Warnings, "; ")
		}
		add("Write access", goodWrite && wrote && strings.TrimSpace(writable.FinalText) != "" && writable.InputTokens+writable.OutputTokens > 0, writeReason)
		add("Model", goodWrite && writable.ModelVerified && writable.ModelActual == model, fmt.Sprintf("requested=%s, actual=%s, independently verified=%t", model, writable.ModelActual, writable.ModelVerified))
		results := []Result{writable}
		ranAll := writeErr == nil && writable.Status == StatusSucceeded
		if h.NoReadOnly != "" {
			skip("Read-only access", h.NoReadOnly)
		} else {
			readonlyBrief := "Use your shell execution tool to execute this exact command once in the current directory, even if you expect the sandbox to deny the write:\n\n```sh\nsh -c 'echo hi > out.txt'; echo EXIT=$?\n```\n\nReport the command's stdout and stderr verbatim, including the EXIT line. Do not predict the result or claim a denial without invoking the tool. Do not request elevated permissions, retry, or seek a workaround; report the actual output and stop."
			if h.Name == "opencode" || h.Name == "agy" {
				readonlyBrief = "This is an authorized read-only permission diagnostic in plan mode. Invoke the bash tool to run `ls` once. Then invoke the bash tool with exactly `echo x > probe.txt` once to verify that the permission policy denies it. You must actually invoke both tool calls; do not merely predict a denial. Report the tool result and stop. Do not retry, seek a workaround, or use another tool to write."
			}
			if h.Name == "agy" {
				readonlyBrief = strings.ReplaceAll(readonlyBrief, "bash tool", "run_command tool")
			}
			target := "out.txt"
			probe := func(id string) (Result, bool, error, error) {
				readonly, roRepo, roErr := run(id, readonlyBrief)
				denied := true
				if h.Name == "opencode" || h.Name == "agy" {
					target = "probe.txt"
					denied = doctorDeniedWrite(jobStore.LogPath(id), h.Name)
				}
				_, statErr := os.Lstat(filepath.Join(roRepo, target))
				return readonly, denied, roErr, statErr
			}
			readonly, denied, roErr, statErr := probe("read-only")
			// The model may decline to attempt the write at all; that proves
			// nothing about the permission layer. Ask once more before
			// reporting it as inconclusive.
			attempts := 1
			clean := func() bool {
				return roErr == nil && readonly.Status == StatusSucceeded && readonly.CLIVersion == r.Version && readonly.ToolCalls > 0 && strings.TrimSpace(readonly.FinalText) != "" && os.IsNotExist(statErr) && len(readonly.Diff.Files) == 0
			}
			if !denied && clean() {
				results = append(results, readonly)
				readonly, denied, roErr, statErr = probe("read-only-2")
				attempts++
			}
			results = append(results, readonly)
			readError := doctorError(roErr)
			if roErr != nil && writeErr != nil && roErr.Error() == writeErr.Error() {
				readError = ", error=same as above (Write access)"
			}
			evidence := fmt.Sprintf("status=%s, write target absent=%t, denied write verified=%t, tool calls=%d, attempts=%d%s", readonly.Status, os.IsNotExist(statErr), denied, readonly.ToolCalls, attempts, readError)
			if !denied && clean() {
				r.Checks = append(r.Checks, DoctorCheck{"Read-only access", "WARN", "inconclusive: the model did not attempt the denied write; nothing was written; " + evidence})
			} else {
				add("Read-only access", denied && clean(), evidence)
			}
			ranAll = ranAll && roErr == nil && readonly.Status == StatusSucceeded
		}
		if h.Name == "agy" {
			hookResult, hookRepo, hookErr := run("read-only-hook", "This is an authorized permission diagnostic. Use the write_to_file tool once to create hook-probe.txt in the current directory containing probe. You must invoke the tool, then report its actual rejection. Do not retry or use another tool. The managed hook should deny this write.")
			results = append(results, hookResult)
			_, statErr := os.Lstat(filepath.Join(hookRepo, "hook-probe.txt"))
			denied := doctorDeniedWrite(jobStore.LogPath("read-only-hook"), "agy-hook")
			clean := hookErr == nil && hookResult.Status == StatusSucceeded && os.IsNotExist(statErr) && len(hookResult.Diff.Files) == 0
			reason := fmt.Sprintf("status=%s, hook denial verified=%t, write target absent=%t%s", hookResult.Status, denied, os.IsNotExist(statErr), doctorError(hookErr))
			if clean && !denied {
				r.Checks = append(r.Checks, DoctorCheck{"Read-only hook", "WARN", "inconclusive: no managed hook rejection observed; " + reason})
			} else {
				add("Read-only hook", clean && denied, reason)
			}
			ranAll = ranAll && clean
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
		if h.Home != nil {
			homeErr := h.Home.Doctor(jobStore, "write", writable, goodWrite)

			if homeErr == nil && checkUserHome != nil {
				homeErr = checkUserHome()
			}
			homeReason := "Probe succeeded in the shared minimal home with intact links and private sessions"
			if homeErr != nil {
				homeReason = homeErr.Error()
				hidden = false
				reason = "Cannot confirm session hiding: " + homeReason
			}
			add("Session hiding", hidden, reason)
			add("Managed home", homeErr == nil, homeReason)
		} else {
			add("Session hiding", hidden, reason)
		}
		r = alignDoctorChecks(r)
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

func doctorKimiHome(store Store, id string, result Result, succeeded bool) error {
	if !succeeded || result.SessionID == "" {
		return fmt.Errorf("managed-home probe did not succeed with a session id")
	}
	home, err := managedKimiHome()
	if err != nil {
		return err
	}
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	if st.HarnessHome != home || st.EffectiveConfigMode() != "minimal" {
		return fmt.Errorf("probe did not use the shared minimal home")
	}
	source, err := kimiHomeDir()
	if err != nil {
		return err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	if err := checkKimiLinks(home, source); err != nil {
		return err
	}
	listed, err := cockpit.KimiSessionListed(source, result.SessionID)
	if err != nil {
		return fmt.Errorf("user session index: %w", err)
	}
	if listed {
		return fmt.Errorf("probe session appeared in the user session index")
	}
	record, err := cockpit.ReadKimiRecord(st.HarnessHome, result.SessionID)
	if err != nil {
		return fmt.Errorf("probe wire record: %w", err)
	}
	if record.MCPToolsDiscovered {
		return fmt.Errorf("probe wire record contains mcp.tools_discovered")
	}
	return nil
}

// cheapestDoctorModel keeps doctor probes on the cheapest tier (spec §11) when a
// harness lists effort tiers as "<model>-low/-medium/-high"; otherwise it keeps
// the harness's own first (default) entry. Names stay dynamic, never hardcoded.
func cheapestDoctorModel(models []string) string {
	for _, m := range models {
		if strings.HasSuffix(m, "-low") {
			return m
		}
	}
	return models[0]
}
