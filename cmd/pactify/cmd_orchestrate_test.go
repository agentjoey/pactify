package main

import (
	"testing"

	"github.com/agentjoey/pactify/internal/orchestrate"
)

func TestTransportModesFromFlags_DefaultsToAcp(t *testing.T) {
	modes, err := transportModesFromFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if modes["opencode"] != "acp" {
		t.Fatalf("want opencode=acp default, got %v", modes)
	}
	// Defaults must match the canonical source of truth.
	want := orchestrate.DefaultTransportModes()
	if len(modes) != len(want) {
		t.Fatalf("default modes length mismatch: got %d want %d", len(modes), len(want))
	}
}

func TestTransportModesFromFlags_Override(t *testing.T) {
	modes, err := transportModesFromFlags([]string{"opencode=cmd"})
	if err != nil {
		t.Fatal(err)
	}
	if modes["opencode"] != "cmd" {
		t.Fatalf("want opencode=cmd override, got %v", modes)
	}
}

func TestTransportModesFromFlags_AdditionalKind(t *testing.T) {
	modes, err := transportModesFromFlags([]string{"kimi-cli=acp"})
	if err != nil {
		t.Fatal(err)
	}
	if modes["opencode"] != "acp" {
		t.Fatalf("want opencode=acp preserved, got %v", modes)
	}
	if modes["kimi-cli"] != "acp" {
		t.Fatalf("want kimi-cli=acp, got %v", modes)
	}
}

func TestTransportModesFromFlags_Invalid(t *testing.T) {
	_, err := transportModesFromFlags([]string{"opencode=invalid"})
	if err == nil {
		t.Fatal("expected error for invalid transport value")
	}
}
