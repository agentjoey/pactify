package projection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"

	"github.com/agentjoey/pactify/internal/event"
)

// SnapshotVersion is the on-disk schema version of state-snapshot.json. A reader
// whose version differs treats the snapshot as absent and falls back to a full
// fold — so bumping this here is a safe, self-healing invalidation.
const SnapshotVersion = 1

// NoSnapshotEnv, when set to "1", bypasses the snapshot cache entirely (both read
// and write). It is the escape hatch for the correctness-critical fold: if the
// cache is ever suspected of diverging from a full replay, exporting it forces
// every fold back onto the ground truth.
const NoSnapshotEnv = "PACT_NO_SNAPSHOT"

// snapshotFile is the persisted ledger snapshot: a cache, never the source of
// truth (which is log.jsonl). It stores the byte prefix of the log it already
// folded (LedgerBytes) and a hash of exactly those bytes (LedgerHeadHash) so a
// reader can prove the prefix is intact before resuming, plus the PRE-retirement
// accumulator (State + cancelled/withdrawn) that lets the fold resume at the
// offset with a result identical to a full replay.
//
// State here is the Folder's pre-retirement state — it may still contain cancelled
// tasks / withdrawn features; that is intentional and required for correctness (see
// Folder). The cancelled/withdrawn maps are omitted when empty so the common case
// stays compact; their \x00-joined keys round-trip through JSON unchanged.
type snapshotFile struct {
	Version        int             `json:"version"`
	LedgerBytes    int64           `json:"ledger_bytes"`
	LedgerHeadHash string          `json:"ledger_head_hash"`
	State          State           `json:"state"`
	Cancelled      map[string]bool `json:"cancelled,omitempty"`
	Withdrawn      map[string]bool `json:"withdrawn,omitempty"`
}

// headHash is the validity fingerprint: sha256 over the exact log prefix the
// snapshot folded. Hashing the WHOLE prefix (not a fixed-size head) is deliberate —
// it detects any rewrite anywhere in the folded region, not just truncation, which
// is what makes the cache safe to trust.
func headHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// WriteSnapshot atomically rewrites snapPath to describe the fold of logBytes (the
// entire current log) captured by f. It is a cache write: callers treat any error
// as non-fatal and never let it fail the verb. Honors NoSnapshotEnv.
func WriteSnapshot(snapPath string, logBytes []byte, f *Folder) error {
	if os.Getenv(NoSnapshotEnv) == "1" {
		return nil
	}
	snap := snapshotFile{
		Version:        SnapshotVersion,
		LedgerBytes:    int64(len(logBytes)),
		LedgerHeadHash: headHash(logBytes),
		State:          f.st,
		Cancelled:      f.cancelled,
		Withdrawn:      f.withdrawn,
	}
	if len(snap.Cancelled) == 0 {
		snap.Cancelled = nil
	}
	if len(snap.Withdrawn) == 0 {
		snap.Withdrawn = nil
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return atomicWrite(snapPath, string(b))
}

// LoadSnapshotState folds the log at logPath into the finalized public State. When
// a valid snapshot at snapPath covers a prefix of the current log, only the bytes
// AFTER that prefix are parsed and folded onto the snapshot's accumulator
// (incremental replay). Any mismatch — version, size, or head hash — silently falls
// back to a full fold, so the snapshot can NEVER change the result. NoSnapshotEnv=1
// bypasses the cache read as well.
//
// The result is guaranteed byte-for-byte equal to Project(event.ReadAll(logPath)):
// the resume rebuilds the exact fold accumulator (pre-retirement state + sticky
// cancelled/withdrawn sets + feature/task indices) that a full fold would hold at
// the offset, then applies the tail and finalizes identically.
func LoadSnapshotState(logPath, snapPath string) (State, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			data = nil
		} else {
			return State{}, err
		}
	}
	if os.Getenv(NoSnapshotEnv) == "1" {
		return foldBytes(data)
	}
	snap, ok := readValidSnapshot(snapPath, data)
	if !ok {
		return foldBytes(data)
	}
	tail := data[snap.LedgerBytes:]
	tailEvs, err := event.ParseAll(tail)
	if err != nil {
		// A tail that won't parse means the log changed under us in a way the hash
		// prefix didn't cover (or is corrupt); fall back to a full fold, which will
		// surface the same parse error deterministically.
		return foldBytes(data)
	}
	for _, e := range tailEvs {
		// A second init after the snapshot offset would re-seed project/seats via the
		// last-init scan that only a full fold performs; resume can't replicate it, so
		// bail to a full fold. (In practice init is the very first event, never in a tail.)
		if e.EventType == "init" {
			return foldBytes(data)
		}
	}
	f := restoreFolder(snap)
	f.apply(tailEvs)
	return f.finalize(), nil
}

func foldBytes(data []byte) (State, error) {
	evs, err := event.ParseAll(data)
	if err != nil {
		return State{}, err
	}
	return Project(evs), nil
}

// readValidSnapshot loads and validates the snapshot against the current log bytes.
// It returns ok=false (not an error) on any absence/parse/version/size/hash
// mismatch — the caller treats every such case as "no usable cache" and full-folds.
func readValidSnapshot(snapPath string, logData []byte) (snapshotFile, bool) {
	b, err := os.ReadFile(snapPath)
	if err != nil {
		return snapshotFile{}, false
	}
	var snap snapshotFile
	if err := json.Unmarshal(b, &snap); err != nil {
		return snapshotFile{}, false
	}
	if snap.Version != SnapshotVersion {
		return snapshotFile{}, false
	}
	// Offset must lie within the current log (guards truncation / rewrite-shorter).
	if snap.LedgerBytes < 0 || snap.LedgerBytes > int64(len(logData)) {
		return snapshotFile{}, false
	}
	// The folded prefix must be byte-identical to what the snapshot hashed
	// (guards any in-place rewrite of the prefix, not just truncation).
	if headHash(logData[:snap.LedgerBytes]) != snap.LedgerHeadHash {
		return snapshotFile{}, false
	}
	return snap, true
}

// restoreFolder reconstructs a resumable Folder from a validated snapshot. The
// feature/task indices are rebuilt from the stored (pre-retirement) State in fold
// order — faithful because the accumulator retains every feature/task the full fold
// held at the offset — while the sticky cancelled/withdrawn sets are carried over
// verbatim (they are NOT reconstructible from a finalized State, which is exactly
// why the snapshot stores the pre-retirement accumulator).
func restoreFolder(snap snapshotFile) *Folder {
	f := &Folder{
		st:        snap.State,
		fIdx:      map[string]int{},
		tIdx:      map[string]map[string]int{},
		cancelled: map[string]bool{},
		withdrawn: map[string]bool{},
	}
	for i := range f.st.Features {
		feat := &f.st.Features[i]
		f.fIdx[feat.ID] = i
		m := map[string]int{}
		for ti := range feat.Tasks {
			m[feat.Tasks[ti].ID] = ti
		}
		f.tIdx[feat.ID] = m
	}
	for k, v := range snap.Cancelled {
		if v {
			f.cancelled[k] = true
		}
	}
	for k, v := range snap.Withdrawn {
		if v {
			f.withdrawn[k] = true
		}
	}
	return f
}
