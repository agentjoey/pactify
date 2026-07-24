package serve

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/registry"
	"github.com/fsnotify/fsnotify"
)

// StartWatchers opens a single fsnotify watcher and registers each currently
// known project's .pact/ dir. On each append to log.jsonl it broadcasts the new
// lines to that project's SSE subs. Per-project watches are added/removed at
// runtime by AddProject/RemoveProject via watchProject/unwatchProject — this
// shared watcher and its watchLoop persist for the server's lifetime.
func (s *Server) StartWatchers() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.watcher = w
	// Watch the registry file's directory so a CLI register / auto-register
	// (which only writes ~/.pactify/projects.json) reconciles live, no restart
	// (backlog B). Directory-level watch survives atomic saves (write-temp+rename
	// replaces the inode); the loop filters events to the registry file.
	if regDir := filepath.Dir(registry.Path()); regDir != "" {
		if err := os.MkdirAll(regDir, 0o755); err == nil {
			if werr := w.Add(regDir); werr != nil {
				fmt.Fprintf(os.Stderr, "pactify serve: watch registry dir %s: %v\n", regDir, werr)
			}
		}
	}
	s.pmu.Lock()
	for _, id := range s.order {
		s.watchProjectLocked(id, s.projects[id].Path)
	}
	// Snapshot (id, logPath, seededOffset) so the relay can replay the existing
	// ledger up to the same offset the SSE watch was seeded at — replay and the
	// live drain then partition each file with no overlapping seq.
	type seed struct {
		id, lp string
		off    int64
	}
	var seeds []seed
	if s.relay != nil {
		for id, lp := range s.watchPaths {
			seeds = append(seeds, seed{id, lp, s.offsets[id]})
		}
	}
	s.pmu.Unlock()
	// Full-ledger replay BEFORE the watch loop starts, so all historical events
	// get their line-index seq before any live append is enqueued.
	for _, sd := range seeds {
		s.relay.replayProject(sd.id, sd.lp, sd.off)
	}
	go s.watchLoop()
	return nil
}

// watchProjectLocked registers an fsnotify watch on the project's .pact/ dir and
// seeds its log offset to the current file size (so only NEW appends stream).
// Caller must hold s.pmu (write).
func (s *Server) watchProjectLocked(id, root string) {
	lp := logPath(root)
	if fi, err := os.Stat(lp); err == nil {
		s.offsets[id] = fi.Size()
	} else {
		s.offsets[id] = 0
	}
	s.watchPaths[id] = lp
	if s.watcher != nil {
		// A failed watch (inotify limit, permissions, missing dir) means this
		// project's SSE never streams — surface it instead of going silently dark.
		if err := s.watcher.Add(root + "/.pact"); err != nil {
			fmt.Fprintf(os.Stderr, "pactify serve: watch %s/.pact: %v\n", root, err)
		}
	}
}

// unwatchProjectLocked removes the fsnotify watch and the per-project
// bookkeeping. Caller must hold s.pmu (write).
func (s *Server) unwatchProjectLocked(id, root string) {
	if s.watcher != nil {
		if err := s.watcher.Remove(root + "/.pact"); err != nil {
			fmt.Fprintf(os.Stderr, "pactify serve: unwatch %s/.pact: %v\n", root, err)
		}
	}
	delete(s.watchPaths, id)
	delete(s.offsets, id)
}

func (s *Server) watchLoop() {
	for {
		select {
		case ev, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			// Registry file changed (CLI register/unregister/auto-register):
			// reconcile the live watched set. Rename fires on atomic saves.
			if filepath.Clean(ev.Name) == filepath.Clean(registry.Path()) {
				s.reconcileRegistry()
				continue
			}
			// Snapshot the (id,path) whose log changed under the read lock, then
			// drain outside the lock (drainNew touches only s.offsets[id], which
			// it re-guards).
			s.pmu.RLock()
			var hitID, hitLP string
			for id, lp := range s.watchPaths {
				if ev.Name == lp {
					hitID, hitLP = id, lp
					break
				}
			}
			s.pmu.RUnlock()
			if hitID != "" {
				s.relay.setWatermarkPath(hitID, hitLP)
				s.drainNew(hitID, hitLP)
			}
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "pactify serve: watcher error: %v\n", err)
		}
	}
}

// drainNew reads complete (newline-terminated) lines appended since the last
// offset and broadcasts them. It advances the offset only past complete lines —
// a trailing partial line is left for the next drain (avoids overshoot). If the
// file shrank since last read (truncation / branch switch swapped log.jsonl), it
// re-reads from the start so the stream doesn't silently freeze.
func (s *Server) drainNew(id, lp string) {
	fi, err := os.Stat(lp)
	if err != nil {
		return
	}
	s.pmu.Lock()
	off := s.offsets[id]
	s.pmu.Unlock()
	if fi.Size() < off {
		off = 0 // truncated/swapped to a shorter file: re-read from the start
	}
	f, err := os.Open(lp)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(off, 0); err != nil {
		return
	}
	rd := bufio.NewReader(f)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			// EOF without a trailing newline: a partial line. Do NOT advance past
			// it — it will be re-read complete on the next drain.
			break
		}
		off += int64(len(line)) // len includes the '\n'
		if t := strings.TrimRight(line, "\n"); t != "" {
			s.hub.broadcast(id, t)
			s.relay.enqueue(id, t, int64(len(line)))
		}
	}
	s.pmu.Lock()
	// Only persist the advanced offset if the project is still watched: a
	// concurrent RemoveProject may have deleted it, and resurrecting the entry
	// here would leak a stale offset.
	if _, ok := s.watchPaths[id]; ok {
		s.offsets[id] = off
	}
	s.pmu.Unlock()
}

// Stop closes the fsnotify watcher and the relay.
func (s *Server) Stop() {
	if s.watcher != nil {
		_ = s.watcher.Close()
	}
	if s.relay != nil {
		s.relay.stop()
	}
}
