// slugify turns a free-text goal into a kebab-case feature id matching the
// backend's ^[a-z0-9][a-z0-9-]*$ rule (≤40 chars). May return "" for input with
// no alphanumerics — callers treat empty as "needs a manual feature id".
export function slugify(goal: string): string {
  return goal
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/g, "");
}
