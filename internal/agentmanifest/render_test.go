package agentmanifest

import (
	"reflect"
	"testing"

	"github.com/agentjoey/pactify/internal/agent"
)

func TestRenderArgs(t *testing.T) {
	perm := Permission{Blanket: []string{"--yolo"}, Scoped: []string{"--allowed-tools", "{tools}"}}
	args := []string{"run", "-m", "{model}", "{permission}", "{briefing}"}

	got := RenderArgs(args, perm, agent.PermPosture{}, "gpt-5", "BRIEF")
	want := []string{"run", "-m", "gpt-5", "--yolo", "BRIEF"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blanket = %v, want %v", got, want)
	}

	got = RenderArgs(args, perm, agent.PermPosture{}, "", "BRIEF")
	want = []string{"run", "--yolo", "BRIEF"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty-model = %v, want %v", got, want)
	}

	got = RenderArgs(args, perm, agent.PermPosture{Scoped: true, AllowedTools: []string{"Read", "Edit"}}, "gpt-5", "BRIEF")
	want = []string{"run", "-m", "gpt-5", "--allowed-tools", "Read,Edit", "BRIEF"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped = %v, want %v", got, want)
	}

	got = RenderArgs([]string{"run", "{permission}", "{briefing}"}, Permission{}, agent.PermPosture{}, "", "B")
	want = []string{"run", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no-perm = %v, want %v", got, want)
	}

	got = RenderArgs([]string{"run", "--id", "{seat}", "{briefing}"}, Permission{}, agent.PermPosture{}, "", "B")
	want = []string{"run", "--id", "{seat}", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("seat = %v, want %v", got, want)
	}

	got = RenderArgs([]string{"run", "--dir", "{repoDir}", "{briefing}"}, Permission{}, agent.PermPosture{}, "", "B")
	want = []string{"run", "--dir", "{repoDir}", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repoDir = %v, want %v", got, want)
	}
}

