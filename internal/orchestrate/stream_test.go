package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenStreamSinkAppends(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenStreamSink(dir, "t-cache")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("line one\n"))
	w.Write([]byte("line two\n"))
	w.Close()

	got, _ := os.ReadFile(StreamPath(dir, "t-cache"))
	if string(got) != "line one\nline two\n" {
		t.Fatalf("stream file = %q", got)
	}
	if StreamPath(dir, "t-cache") != filepath.Join(dir, ".pact", "orchestrate", "streams", "t-cache.log") {
		t.Fatalf("path = %s", StreamPath(dir, "t-cache"))
	}
}
