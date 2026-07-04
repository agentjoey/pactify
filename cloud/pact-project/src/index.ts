export interface Seat {
  id: string
  roles: string[]
  kind?: string
}

export interface Task {
  id: string
  owner: string
  status: string
  reviewer: string
  spec: string
  evidence: string
  deps?: string[]
}

export interface Feature {
  id: string
  branch: string
  status: string
  tasks: Task[]
}

export interface State {
  project: string
  agents: Seat[]
  features: Feature[]
  awaiting_count: number
}

export interface PactEvent {
  event_id: string
  ts: string
  agent_id: string
  role: string
  event_type: string
  task_id: string
  feature: string
  payload: Record<string, unknown>
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : ''
}

function payloadStr(payload: Record<string, unknown>, key: string): string {
  return str(payload[key])
}

function payloadArr(payload: Record<string, unknown>, key: string): unknown[] {
  const v = payload[key]
  return Array.isArray(v) ? v : []
}

function seatFromPayload(payload: Record<string, unknown>): Seat {
  const seat: Seat = { id: payloadStr(payload, 'id'), roles: [] }
  const kind = payloadStr(payload, 'kind')
  if (kind !== '') seat.kind = kind
  for (const r of payloadArr(payload, 'roles')) {
    const s = str(r)
    if (s !== '') seat.roles.push(s)
  }
  return seat
}

function findTask(feature: Feature, taskId: string): Task | undefined {
  return feature.tasks.find((t) => t.id === taskId)
}

function buildTask(t: Omit<Task, 'evidence'> & { evidence: string }): Task {
  if (t.deps !== undefined && t.deps.length === 0) {
    const { deps: _d, ...rest } = t
    return rest as Task
  }
  return t as Task
}

export function project(events: PactEvent[]): State {
  const st: State = { project: 'unknown', agents: [], features: [], awaiting_count: 0 }
  const fIdx: Record<string, number> = {}
  const tIdx: Record<string, Record<string, number>> = {}
  const cancelled = new Set<string>()
  const withdrawn = new Set<string>()

  function find(feature: string, task: string): Task | undefined {
    const fi = fIdx[feature]
    if (fi === undefined) return undefined
    const ft = tIdx[feature]
    if (!ft) return undefined
    const ti = ft[task]
    if (ti === undefined) return undefined
    const feat = st.features[fi]
    if (!feat) return undefined
    return feat.tasks[ti]
  }

  // Priming: scan backwards for the last init event to get project + initial seats.
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i]!
    if (e.event_type !== 'init') continue
    const projectName = payloadStr(e.payload, 'project')
    if (projectName !== '') st.project = projectName
    for (const s of payloadArr(e.payload, 'seats')) {
      if (typeof s === 'object' && s !== null) {
        st.agents.push(seatFromPayload(s as Record<string, unknown>))
      }
    }
    break
  }

  // Event fold.
  for (const e of events) {
    switch (e.event_type) {
      case 'add-seat': {
        const seat = seatFromPayload(e.payload)
        if (!st.agents.some((a) => a.id === seat.id)) {
          st.agents.push(seat)
        }
        break
      }
      case 'assign': {
        let fi = fIdx[e.feature]
        if (fi === undefined) {
          st.features.push({ id: e.feature, branch: payloadStr(e.payload, 'branch'), status: 'in_progress', tasks: [] })
          fi = st.features.length - 1
          fIdx[e.feature] = fi
          tIdx[e.feature] = {}
        }
        const depsRaw = payloadArr(e.payload, 'deps')
        const deps: string[] | undefined =
          depsRaw.length > 0
            ? depsRaw.map((d) => str(d)).filter((s) => s !== '')
            : undefined
        const task: Task = buildTask({
          id: e.task_id,
          owner: payloadStr(e.payload, 'owner'),
          status: 'assigned',
          reviewer: payloadStr(e.payload, 'reviewer'),
          spec: payloadStr(e.payload, 'spec'),
          evidence: '',
          deps,
        })
        const ft = tIdx[e.feature]
        if (ft !== undefined && ft[e.task_id] !== undefined) {
          const ti = ft[e.task_id]!
          st.features[fi]!.tasks[ti] = task
        } else {
          st.features[fi]!.tasks.push(task)
          if (tIdx[e.feature] === undefined) tIdx[e.feature] = {}
          tIdx[e.feature]![e.task_id] = st.features[fi]!.tasks.length - 1
        }
        break
      }
      case 'join': {
        // Seat-scoped: lift only the FIRST owned + assigned + dep-ready task.
        let lifted = false
        for (const f of st.features) {
          if (lifted) break
          for (const t of f.tasks) {
            if (t.owner !== e.agent_id || t.status !== 'assigned' || cancelled.has(`${f.id}\x00${t.id}`)) continue
            const ready =
              !t.deps ||
              t.deps.every((d) => {
                const dep = findTask(f, d)
                return !dep || cancelled.has(`${f.id}\x00${d}`) || dep.status === 'accepted'
              })
            if (ready) {
              t.status = 'in_progress'
              lifted = true
              break
            }
          }
        }
        break
      }
      case 'start': {
        const t = find(e.feature, e.task_id)
        if (t && t.status === 'assigned') {
          t.status = 'in_progress'
        }
        break
      }
      case 'checkpoint': {
        const t = find(e.feature, e.task_id)
        if (t) {
          t.status = 'awaiting_review'
          t.evidence = payloadStr(e.payload, 'evidence')
        }
        break
      }
      case 'accept': {
        const t = find(e.feature, e.task_id)
        if (t) {
          t.status = 'accepted'
        }
        break
      }
      case 'changes_requested': {
        const t = find(e.feature, e.task_id)
        if (t) {
          t.status = 'changes_requested'
        }
        break
      }
      case 'merge': {
        const fi = fIdx[e.feature]
        if (fi !== undefined) {
          st.features[fi]!.status = 'shipped'
        }
        break
      }
      case 'cancel': {
        cancelled.add(`${e.feature}\x00${e.task_id}`)
        break
      }
      case 'withdraw': {
        withdrawn.add(e.feature)
        break
      }
    }
  }

  // Retire cancelled tasks and withdrawn features.
  if (cancelled.size > 0 || withdrawn.size > 0) {
    const kept: Feature[] = []
    for (const f of st.features) {
      if (withdrawn.has(f.id)) continue
      f.tasks = f.tasks.filter((t) => !cancelled.has(`${f.id}\x00${t.id}`))
      kept.push(f)
    }
    st.features = kept
  }

  // Compute awaiting_count.
  let awaitingCount = 0
  for (const f of st.features) {
    for (const t of f.tasks) {
      if (t.status === 'awaiting_review') awaitingCount++
    }
  }
  st.awaiting_count = awaitingCount

  return st
}
