package procx

import (
	"os"
	"reflect"
	"syscall"
	"testing"
)

func TestDescendantsFromSnapshotBindsBirthIdentity(t *testing.T) {
	root := Identity{PID: 10, Start: "root"}
	child, grand := Identity{PID: 20, Start: "child"}, Identity{PID: 30, Start: "grand"}
	snapshot := []processEntry{{Identity: grand, parent: 20}, {Identity: root, parent: 1}, {Identity: child, parent: 10}, {Identity: Identity{PID: 40, Start: "unrelated"}, parent: 1}}
	if got := descendantsFromSnapshot(root, snapshot); !reflect.DeepEqual(got, []Identity{grand, child}) {
		t.Fatalf("descendants=%+v", got)
	}
	root.Start = "reused"
	if got := descendantsFromSnapshot(root, snapshot); len(got) != 0 {
		t.Fatalf("reused parent adopted unrelated descendants: %+v", got)
	}
}
func TestIdentitySignalRejectsWrongBirth(t *testing.T) {
	p, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	p.Start += "-wrong"
	if alive, err := p.Alive(); err != nil || alive {
		t.Fatalf("wrong identity alive=%t err=%v", alive, err)
	}
	if err = p.Signal(syscall.SIGTERM, false); err != nil {
		t.Fatal(err)
	}
}
