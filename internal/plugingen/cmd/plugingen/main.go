// Command plugingen regenerates distributions from plugins/src. Run at repo root.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentjoey/pactify/internal/plugingen"
)

func main() {
	check := flag.Bool("check", false, "list stale generated files and exit nonzero without writing")
	agensi := flag.String("agensi", "", "write a deterministic Agensi skill zip to this path (no regeneration)")
	flag.Parse()
	if flag.NArg() != 0 || (*check && *agensi != "") {
		fail(fmt.Errorf("usage: plugingen [-check | -agensi output.zip]"))
	}
	files, err := plugingen.Generate(".")
	if err != nil {
		fail(err)
	}
	if *agensi != "" {
		data, err := plugingen.Agensi(files)
		if err != nil {
			fail(err)
		}
		if err := os.MkdirAll(filepath.Dir(*agensi), 0755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(*agensi, data, 0644); err != nil {
			fail(err)
		}
		fmt.Println(*agensi)
		return
	}
	names, err := plugingen.Sync(".", files, *check)
	if err != nil {
		fail(err)
	}
	for _, name := range names {
		fmt.Println(name)
	}
	if *check && len(names) != 0 {
		os.Exit(1)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, "plugingen:", err); os.Exit(1) }
