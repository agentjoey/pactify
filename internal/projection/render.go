package projection

import (
	"fmt"
	"strings"
)

// Render produces STATE.yml text byte-identical to the bash reference
// (_pact_render_state_to). Evidence newlines/tabs are folded to spaces;
// nil evidence renders as the literal "null".
func Render(st State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "project: %s\n", st.Project)
	b.WriteString("working_tree_holder: null\n")
	b.WriteString("agents:\n")
	for _, a := range st.Agents {
		fmt.Fprintf(&b, "  - { id: %s, roles: [%s] }\n", a.ID, strings.Join(a.Roles, ", "))
	}
	b.WriteString("features:\n")
	for _, f := range st.Features {
		fmt.Fprintf(&b, "  - id: %s\n", f.ID)
		fmt.Fprintf(&b, "    branch: %s\n", f.Branch)
		fmt.Fprintf(&b, "    status: %s\n", f.Status)
		b.WriteString("    tasks:\n")
		for _, t := range f.Tasks {
			ev := "null"
			if t.Evidence != nil {
				ev = strings.ReplaceAll(strings.ReplaceAll(*t.Evidence, "\n", " "), "\t", " ")
			}
			fmt.Fprintf(&b, "      - id: %s\n", t.ID)
			fmt.Fprintf(&b, "        owner: %s\n", t.Owner)
			fmt.Fprintf(&b, "        status: %s\n", t.Status)
			fmt.Fprintf(&b, "        reviewer: %s\n", t.Reviewer)
			fmt.Fprintf(&b, "        spec: %s\n", t.Spec)
			fmt.Fprintf(&b, "        evidence: %s\n", ev)
			// deps line is emitted ONLY when the task carried deps; deps-free
			// tasks render byte-identically to the bash reference (which has no
			// deps concept). Mirrors the agents `roles: [a, b]` list style.
			if len(t.Deps) > 0 {
				fmt.Fprintf(&b, "        deps: [%s]\n", strings.Join(t.Deps, ", "))
			}
		}
	}
	return b.String()
}

// WriteState atomically writes Render(st) to path (tmp + rename).
func WriteState(path string, st State) error {
	return atomicWrite(path, Render(st))
}
