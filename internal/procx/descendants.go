package procx

import "sort"

// processEntry captures ancestry and birth identity in the same kernel snapshot.
// Recording only a PID and inspecting it later could adopt an unrelated process
// if that PID were reused between enumeration and inspection.
type processEntry struct {
	Identity
	parent int
}

// Descendants snapshots the full descendant tree, including separate process
// groups/sessions, before the caller signals the root. Leaf processes come first.
func Descendants(root Identity) ([]Identity, error) {
	entries, err := processSnapshot(root)
	return descendantsFromSnapshot(root, entries), err
}
func descendantsFromSnapshot(root Identity, entries []processEntry) []Identity {
	children := map[int][]Identity{}
	found := false
	for _, p := range entries {
		if p.Identity == root && root.PID > 1 && root.Start != "" {
			found = true
		}
		if p.PID > 1 && p.Start != "" {
			children[p.parent] = append(children[p.parent], p.Identity)
		}
	}
	if !found {
		return nil
	}
	queue := []Identity{root}
	seen := map[int]bool{root.PID: true}
	var descendants []Identity
	for i := 0; i < len(queue); i++ {
		next := children[queue[i].PID]
		sort.Slice(next, func(i, j int) bool { return next[i].PID < next[j].PID })
		for _, p := range next {
			if !seen[p.PID] {
				seen[p.PID] = true
				descendants = append(descendants, p)
				queue = append(queue, p)
			}
		}
	}
	for i, j := 0, len(descendants)-1; i < j; i, j = i+1, j-1 {
		descendants[i], descendants[j] = descendants[j], descendants[i]
	}
	return descendants
}
