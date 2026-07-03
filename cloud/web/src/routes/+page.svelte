<script lang="ts">
  import { PUBLIC_PACTIFY_RELAY_URL } from '$env/static/public'
  import { hexToBytes } from '@noble/hashes/utils.js'
  import { MissionControlRelay, type Project, type PactEvent } from '$lib/relay'
  import { boardColumns, COLUMNS, type PactEventHeader } from '$lib/board'

  let secretHex = $state('')
  let relay: MissionControlRelay | null = $state(null)
  let projects = $state<Project[]>([])
  let selected = $state<Project | null>(null)
  let events = $state<PactEventHeader[]>([])
  let error = $state('')

  const columns = $derived(events.length ? boardColumns(events) : null)

  async function connect() {
    error = ''
    try {
      const master = hexToBytes(secretHex.trim())
      const r = new MissionControlRelay(PUBLIC_PACTIFY_RELAY_URL, master)
      await r.login()
      relay = r
      projects = await r.listProjects()
      r.connect((e) => {
        // Live update: if it belongs to the open project, fold it in.
        if (selected && e.projectId === selected.id) {
          events = [...events.filter((x) => x.seq !== e.seq), e]
        }
        void refreshProjects()
      })
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    }
  }

  async function refreshProjects() {
    if (relay) projects = await relay.listProjects()
  }

  async function open(p: Project) {
    if (!relay) return
    selected = p
    const evs: PactEvent[] = await relay.projectEvents(p.id)
    events = evs
  }
</script>

<main>
  <h1>Pactify Mission Control</h1>

  {#if !relay}
    <p class="muted">Enter your account master secret (hex) to view your pact projects across machines. The secret never leaves this browser; the relay is a blind courier.</p>
    <form onsubmit={(e) => { e.preventDefault(); connect() }}>
      <input type="password" bind:value={secretHex} placeholder="master secret (64 hex chars)" size="48" />
      <button type="submit">Connect</button>
    </form>
    {#if error}<p class="error">{error}</p>{/if}
  {:else}
    <div class="layout">
      <aside>
        <h2>Projects</h2>
        <ul>
          {#each projects as p (p.id)}
            <li>
              <button class:active={selected?.id === p.id} onclick={() => open(p)}>
                {p.name}{#if p.feature} · <span class="muted">{p.feature}</span>{/if}
              </button>
            </li>
          {/each}
          {#if projects.length === 0}<li class="muted">no projects yet</li>{/if}
        </ul>
      </aside>

      <section>
        {#if selected && columns}
          <h2>{selected.name}</h2>
          <div class="board">
            {#each COLUMNS as col (col)}
              <div class="column">
                <h3>{col.replace('_', ' ')} <span class="count">{columns[col].length}</span></h3>
                {#each columns[col] as t (t.id)}
                  <div class="card">
                    <div class="task">{t.id}</div>
                    {#if t.feature}<div class="muted">{t.feature}</div>{/if}
                    <div class="muted small">{t.lastEventType}</div>
                  </div>
                {/each}
              </div>
            {/each}
          </div>
        {:else}
          <p class="muted">Select a project.</p>
        {/if}
      </section>
    </div>
  {/if}
</main>

<style>
  :global(body) { margin: 0; background: #07090d; color: #e6e9ef; font-family: ui-sans-serif, system-ui, sans-serif; }
  main { max-width: 1200px; margin: 0 auto; padding: 24px; }
  h1 { font-size: 20px; }
  .muted { color: #8a93a6; }
  .small { font-size: 12px; }
  .error { color: #ff6b6b; }
  input, button { font: inherit; padding: 8px 10px; border-radius: 6px; border: 1px solid #22283a; background: #11151f; color: inherit; }
  button { cursor: pointer; }
  .layout { display: grid; grid-template-columns: 220px 1fr; gap: 24px; margin-top: 16px; }
  aside ul { list-style: none; padding: 0; }
  aside li { margin: 4px 0; }
  aside button { width: 100%; text-align: left; background: transparent; border: none; padding: 6px 8px; border-radius: 6px; }
  aside button.active { background: #1a2030; }
  .board { display: grid; grid-template-columns: repeat(5, 1fr); gap: 12px; }
  .column h3 { font-size: 12px; text-transform: uppercase; letter-spacing: 0.04em; color: #8a93a6; }
  .count { color: #5b6478; }
  .card { background: #11151f; border: 1px solid #22283a; border-radius: 8px; padding: 10px; margin-bottom: 8px; }
  .task { font-weight: 600; font-size: 13px; }
</style>
