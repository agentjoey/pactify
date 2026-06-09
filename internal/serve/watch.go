package serve

import (
	"bufio"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// StartWatchers opens an fsnotify watch on each project's .pact/ dir and, on
// each append to log.jsonl, broadcasts the new lines to that project's SSE subs.
func (s *Server) StartWatchers() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.watcher = w
	s.offsets = map[string]int64{}
	for _, id := range s.order {
		p := s.projects[id]
		lp := logPath(p.Path)
		if fi, err := os.Stat(lp); err == nil {
			s.offsets[id] = fi.Size()
		}
		_ = w.Add(p.Path + "/.pact")
		s.watchPaths = append(s.watchPaths, struct{ id, lp string }{id, lp})
	}
	go s.watchLoop()
	return nil
}

func (s *Server) watchLoop() {
	for {
		select {
		case ev, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			for _, wp := range s.watchPaths {
				if ev.Name == wp.lp {
					s.drainNew(wp.id, wp.lp)
				}
			}
		case _, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
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
	off := s.offsets[id]
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
		}
	}
	s.offsets[id] = off
}

// Stop closes the fsnotify watcher.
func (s *Server) Stop() {
	if s.watcher != nil {
		_ = s.watcher.Close()
	}
}
