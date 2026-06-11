// docTitle composes the browser tab title (spec §6.5). With a project selected
// the name is wrapped in guillemets; a non-zero awaiting count appends an
// "N awaiting ●" marker so a background tab signals pending reviews. With no
// project the bare product name is shown.
export function docTitle(project: string, awaiting: number): string {
  if (!project) return "pactify";
  return awaiting > 0 ? `«${project}» · ${awaiting} awaiting ●` : `«${project}»`;
}
