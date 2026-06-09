package serve

import (
	"bufio"
	"os"

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

// drainNew reads lines appended since the last offset and broadcasts them.
func (s *Server) drainNew(id, lp string) {
	f, err := os.Open(lp)
	if err != nil {
		return
	}
	defer f.Close()
	off := s.offsets[id]
	if _, err := f.Seek(off, 0); err != nil {
		return
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var read int64
	for sc.Scan() {
		line := sc.Text()
		read += int64(len(line)) + 1
		if line != "" {
			s.hub.broadcast(id, line)
		}
	}
	s.offsets[id] = off + read
}

// Stop closes the fsnotify watcher.
func (s *Server) Stop() {
	if s.watcher != nil {
		_ = s.watcher.Close()
	}
}
