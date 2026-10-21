package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/agentjoey/pactify/internal/buildinfo"
)

// Overridden at release time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	info, _ := debug.ReadBuildInfo()
	version, commit, date = buildIdentity(version, commit, date, info)
	buildinfo.Version = version
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func buildIdentity(version, commit, date string, info *debug.BuildInfo) (string, string, string) {
	if version != "dev" || info == nil {
		return version, commit, date
	}
	// Module versions carry a "v" prefix; release ldflags inject the bare semver.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		version = strings.TrimPrefix(v, "v")
	}
	for _, setting := range info.Settings {
		if setting.Value == "" {
			continue
		}
		switch setting.Key {
		case "vcs.revision":
			if commit == "none" {
				commit = setting.Value
			}
		case "vcs.time":
			if date == "unknown" {
				date = setting.Value
			}
		}
	}
	return version, commit, date
}
