import { useEffect, useState } from "react";
import type { RecipeItem, ExpandedTaskItem } from "../lib/types";
import { getRecipes, expandRecipe } from "../lib/api";
import { Badge } from "./ui/Badge";
import { Alert } from "./ui/Alert";
import { EmptyState } from "./ui/EmptyState";
import { Spinner } from "./ui/Spinner";

// Recipes (#11) — the low-friction entry into orchestration: pick a named recipe,
// type a one-line goal, and preview the pact task graph it expands to (owner work
// gets filled in by the planner/orchestrate later). Read + preview only here
// (consumes GET /api/recipes + POST .../expand); writing the plan manifest
// (generate → Plan review) is the next step and a deliberate follow-up since it
// mutates the repo over HTTP.
export function Recipes() {
  const [recipes, setRecipes] = useState<RecipeItem[] | null>(null);
  const [selected, setSelected] = useState<string>("");
  const [goal, setGoal] = useState("");
  const [tasks, setTasks] = useState<ExpandedTaskItem[] | null>(null);
  const [error, setError] = useState("");
  const [expanding, setExpanding] = useState(false);

  function loadRecipes() {
    getRecipes()
      .then((rs) => {
        setRecipes(rs);
        if (rs.length > 0 && !selected) setSelected(rs[0].name);
        setError("");
      })
      .catch(() => setError("Failed to load recipes"));
  }

  useEffect(() => {
    loadRecipes();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Debounced live preview: expand the selected recipe as the goal is typed.
  useEffect(() => {
    if (!selected || goal.trim() === "") {
      setTasks(null);
      return;
    }
    const handle = setTimeout(() => {
      setExpanding(true);
      expandRecipe(selected, goal)
        .then((ts) => {
          setTasks(ts);
          setError("");
        })
        .catch((e) => setError(e instanceof Error ? e.message : "expand failed"))
        .finally(() => setExpanding(false));
    }, 350);
    return () => clearTimeout(handle);
  }, [selected, goal]);

  const current = recipes?.find((r) => r.name === selected);

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5 view-enter" data-testid="recipes-view">
      <h2 className="mb-1 text-[15px] font-[650] text-[var(--color-text-1)]">Recipes</h2>
      <p className="mb-4 text-[12px] text-[var(--color-text-3)]">
        Pick a recipe and state a one-line goal to preview the task graph it expands to. No more hand-writing the graph — just pick a recipe.
      </p>

      {error && (
        <Alert tone="danger" title="Something went wrong" className="mb-3">
          {error}
        </Alert>
      )}

      {recipes && recipes.length === 0 && (
        <EmptyState title="No recipes available" hint="Recipes are provided by internal/recipe (add-tests / review-harden / spec-to-plan)." />
      )}

      {recipes && recipes.length > 0 && (
        <div className="grid grid-cols-[200px_1fr] gap-4">
          {/* recipe list */}
          <div className="flex flex-col gap-1.5" data-testid="recipe-list">
            {recipes.map((r) => (
              <button
                key={r.name}
                type="button"
                data-testid={`recipe-${r.name}`}
                aria-pressed={r.name === selected}
                onClick={() => setSelected(r.name)}
                className={[
                  "rounded-md border px-3 py-2 text-left press transition-colors duration-[var(--motion-micro)] outline-none focus-visible:ring-2",
                  r.name === selected
                    ? "border-[color-mix(in_srgb,var(--color-role-design)_45%,transparent)] bg-[color-mix(in_srgb,var(--color-role-design)_12%,transparent)]"
                    : "border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] hover:border-[var(--color-border-strong)]",
                ].join(" ")}
              >
                <div className="mono text-[12px] font-medium text-[var(--color-text-1)]">{r.name}</div>
                <div className="mt-0.5 text-[10.5px] text-[var(--color-text-3)]">{r.description}</div>
              </button>
            ))}
          </div>

          {/* goal + preview */}
          <div>
            <label className="mb-1 block text-[11px] uppercase tracking-[.5px] text-[var(--color-text-3)]">
              Goal (one line)
            </label>
            <input
              data-testid="recipe-goal"
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              placeholder={current ? `Use ${current.name} to…` : "Goal…"}
              className="w-full rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-3 py-2 text-[13px] text-[var(--color-text-1)] outline-none focus-visible:ring-2"
            />

            <div className="mt-3 flex items-center gap-2">
              <span className="text-[11px] uppercase tracking-[.5px] text-[var(--color-text-3)]">Preview</span>
              {expanding && <Spinner size="xs" />}
              {tasks && <Badge color="role-design">{tasks.length} tasks</Badge>}
            </div>

            {!goal.trim() && (
              <div className="mt-2 text-[12px] text-[var(--color-text-3)]">Enter a goal to preview the task graph live.</div>
            )}

            {tasks && (
              <ol className="mt-2 flex flex-col gap-2 fade-rise">
                {tasks.map((t, i) => (
                  <li
                    key={t.id}
                    data-testid="recipe-task"
                    style={{ animationDelay: `${i * 40}ms` }}
                    className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5 hover-lift fade-rise"
                  >
                    <div className="flex items-center gap-2">
                      <span className="mono text-[12px] font-medium text-[var(--color-text-1)]">{t.id}</span>
                      {t.deps && t.deps.length > 0 && (
                        <span className="text-[10.5px] text-[var(--color-text-3)]">deps: {t.deps.join(", ")}</span>
                      )}
                    </div>
                    <pre className="mt-1 whitespace-pre-wrap mono text-[10.5px] text-[var(--color-text-3)] [overflow-wrap:anywhere]">
                      {t.spec}
                    </pre>
                  </li>
                ))}
              </ol>
            )}

            {tasks && tasks.length > 0 && (
              <div className="mt-3 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-3 py-2 text-[11px] text-[var(--color-text-2)]">
                Looks good? Next, run{" "}
                <span className="mono text-[var(--color-text-1)]">pactify plan {selected}</span>{" "}
                to generate the task-graph manifest, review it in the Plan view → run orchestrate.
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
