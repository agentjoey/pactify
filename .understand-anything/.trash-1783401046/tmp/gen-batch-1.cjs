// Batch-1 generator for pactify web/ knowledge graph
const fs = require('fs');
const path = require('path');

const ROOT = '/Users/xtation/AgentWorks/Code_Claude/pactify';
const TMP = `${ROOT}/.understand-anything/tmp`;
const OUT_DIR = `${ROOT}/.understand-anything/intermediate`;

const input = JSON.parse(fs.readFileSync(`${TMP}/ua-file-analyzer-input-1.json`, 'utf8'));
const extract = JSON.parse(fs.readFileSync(`${TMP}/ua-file-extract-results-1.json`, 'utf8'));
const neighborMap = JSON.parse(fs.readFileSync(`${TMP}/ua-neighbor-map-1.json`, 'utf8'));

// ---- File metadata: manual summaries for high quality ----
// Each entry: filePath -> { summary, tags, complexity, languageNotes? }
const FILE_META = {
  'web/src/App.test.tsx': {
    summary: 'Vitest test suite for the App root component, mocking EventSource and asserting keyboard shortcut routing across views.',
    tags: ['test', 'react', 'app', 'keyboard'],
    complexity: 'moderate',
  },
  'web/src/App.tsx': {
    summary: 'Root React component that bootstraps the application: manages active project, subscribes to the data source, renders the shell, toasts, modals, and routes keyboard shortcuts.',
    tags: ['entry-point', 'react', 'state-management', 'app-shell'],
    complexity: 'complex',
    languageNotes: 'Massive AppContent component (430 lines) holding 30+ useState/useRef hooks for cross-cutting UI state.',
  },
  'web/src/components/Agents.test.tsx': {
    summary: 'Tests for the Agents settings table covering load, toggle, and reload interactions.',
    tags: ['test', 'react', 'agents'],
    complexity: 'simple',
  },
  'web/src/components/Agents.tsx': {
    summary: 'Settings panel listing registered agents with toggle controls for enabling/disabling each agent kind.',
    tags: ['react', 'settings', 'agents', 'component'],
    complexity: 'moderate',
  },
  'web/src/components/AuditLens.test.tsx': {
    summary: 'Minimal smoke test for the AuditLens component.',
    tags: ['test', 'react', 'audit'],
    complexity: 'simple',
  },
  'web/src/components/AuditLens.tsx': {
    summary: 'Side panel rendering risk-prioritized audit recommendations for the selected task, fetched from the audit API.',
    tags: ['react', 'audit', 'risk', 'component'],
    complexity: 'moderate',
  },
  'web/src/components/Board.test.tsx': {
    summary: 'Vitest suite for the Board component covering rendering, column layout, and verb interactions.',
    tags: ['test', 'react', 'board'],
    complexity: 'moderate',
  },
  'web/src/components/Board.tsx': {
    summary: 'Kanban-style board view grouping tasks into feature columns with status filters, role chips, and inline verb actions.',
    tags: ['react', 'board', 'kanban', 'component', 'feature'],
    complexity: 'complex',
  },
  'web/src/components/Board.verb.test.tsx': {
    summary: 'Focused tests for verb-handling paths on the Board component, exercising accept/changes flows.',
    tags: ['test', 'react', 'board', 'verbs'],
    complexity: 'simple',
  },
  'web/src/components/Canvas.test.tsx': {
    summary: 'Tests for the Canvas office-layout view verifying node placement and toolbar interactions.',
    tags: ['test', 'react', 'canvas'],
    complexity: 'moderate',
  },
  'web/src/components/Canvas.tsx': {
    summary: 'Canvas view that renders an office-style spatial layout of features and seats with drag, dispatch, and edit affordances.',
    tags: ['react', 'canvas', 'layout', 'component'],
    complexity: 'complex',
  },
  'web/src/components/CommandK.test.tsx': {
    summary: 'Test suite for the Cmd+K command palette covering keyboard open, search filtering, and verb dispatch.',
    tags: ['test', 'react', 'command-palette'],
    complexity: 'moderate',
  },
  'web/src/components/CommandK.tsx': {
    summary: 'Global Cmd+K command palette modal providing fuzzy task search, view switching, project switching, and recipe execution.',
    tags: ['react', 'command-palette', 'modal', 'keyboard'],
    complexity: 'complex',
  },
  'web/src/components/DispatchModal.test.ts': {
    summary: 'Vitest suite (TypeScript) for the DispatchModal focusing on dispatch payload construction.',
    tags: ['test', 'react', 'dispatch'],
    complexity: 'simple',
  },
  'web/src/components/DispatchModal.test.tsx': {
    summary: 'React-testing-library suite for the DispatchModal, exercising modal lifecycle and dispatch payload validation.',
    tags: ['test', 'react', 'dispatch'],
    complexity: 'simple',
  },
  'web/src/components/DispatchModal.tsx': {
    summary: 'Modal for dispatching a task to a worker seat with optional dependency wiring, returning a payload ready to POST.',
    tags: ['react', 'modal', 'dispatch', 'component'],
    complexity: 'complex',
  },
  'web/src/components/ElementsGallery.tsx': {
    summary: 'Visual design-system catalogue rendering swatches, badges, status pills, ant avatars, buttons, inputs, and skeleton states for review.',
    tags: ['react', 'design-system', 'catalogue', 'documentation'],
    complexity: 'complex',
  },
  'web/src/components/LiveOrchestrate.test.tsx': {
    summary: 'Extensive Vitest suite for LiveOrchestrate covering status polling, run/resume, diff/ship, review-gate, and reject flows.',
    tags: ['test', 'react', 'live', 'orchestrate'],
    complexity: 'complex',
  },
  'web/src/components/LiveOrchestrate.tsx': {
    summary: 'Live orchestration view streaming parallel pipeline status with feature lanes, review gates, and inline ship/diff actions.',
    tags: ['react', 'live', 'orchestrate', 'pipeline', 'component'],
    complexity: 'complex',
  },
  'web/src/components/Machines.test.tsx': {
    summary: 'Tests for the Machines settings page verifying machine list load and rendering.',
    tags: ['test', 'react', 'machines'],
    complexity: 'simple',
  },
  'web/src/components/Machines.tsx': {
    summary: 'Settings panel listing registered worker machines with their agent kinds and last-seen activity timestamps.',
    tags: ['react', 'settings', 'machines', 'component'],
    complexity: 'moderate',
  },
  'web/src/components/NoProjects.test.tsx': {
    summary: 'Tests for the NoProjects empty-state component covering local and hosted variants.',
    tags: ['test', 'react', 'empty-state'],
    complexity: 'simple',
  },
  'web/src/components/NoProjects.tsx': {
    summary: 'Empty-state component shown when no project is registered, dispatching to a local- or hosted-specific onboarding form.',
    tags: ['react', 'empty-state', 'onboarding', 'component'],
    complexity: 'moderate',
  },
  'web/src/components/Recipes.test.tsx': {
    summary: 'Smoke tests for the Recipes expand flow.',
    tags: ['test', 'react', 'recipes'],
    complexity: 'simple',
  },
  'web/src/components/Recipes.tsx': {
    summary: 'Recipe browser letting users pick a recipe template, supply a goal, and expand it into a concrete draft task list.',
    tags: ['react', 'recipes', 'expand', 'component'],
    complexity: 'moderate',
  },
  'web/src/components/RelayConnect.test.tsx': {
    summary: 'Tests for the RelayConnect onboarding screen covering form validation and relay connection submission.',
    tags: ['test', 'react', 'relay'],
    complexity: 'simple',
  },
  'web/src/components/RelayConnect.tsx': {
    summary: 'Onboarding form for connecting to a remote relay server, capturing URL and shared secret with validation.',
    tags: ['react', 'relay', 'onboarding', 'component'],
    complexity: 'simple',
  },
  'web/src/components/RightRail.test.tsx': {
    summary: 'Tests for the RightRail task detail panel covering task stat load, verb actions, and changes request flow.',
    tags: ['test', 'react', 'right-rail'],
    complexity: 'moderate',
  },
  'web/src/components/RightRail.tsx': {
    summary: 'Right-rail task detail panel showing per-task stats, recent timeline events, role avatars, and review/merge/changes verb controls.',
    tags: ['react', 'right-rail', 'task-detail', 'component'],
    complexity: 'complex',
  },
  'web/src/components/Setup.test.tsx': {
    summary: 'Tests for the project setup wizard covering suggestion load, role toggles, and apply commands.',
    tags: ['test', 'react', 'setup'],
    complexity: 'moderate',
  },
  'web/src/components/Setup.tsx': {
    summary: 'First-run setup wizard for a project: suggests seat/role bindings, lets the user edit roles, and emits copy-pasteable wire commands.',
    tags: ['react', 'setup', 'wizard', 'component'],
    complexity: 'complex',
  },
  'web/src/components/Skeleton.test.tsx': {
    summary: 'Tests for the skeleton loading placeholders.',
    tags: ['test', 'react', 'skeleton'],
    complexity: 'simple',
  },
  'web/src/components/Skeleton.tsx': {
    summary: 'Pure-presentational skeleton loading states for the four primary views (default, Board, Canvas, Ops).',
    tags: ['react', 'skeleton', 'loading', 'component'],
    complexity: 'simple',
  },
  'web/src/components/TaskCard.test.tsx': {
    summary: 'Tests for the TaskCard component covering status rendering and lifecycle styling.',
    tags: ['test', 'react', 'task-card'],
    complexity: 'simple',
  },
  'web/src/components/TaskCard.tsx': {
    summary: 'Task card rendering status pill, role caste, lifecycle stage, and review-flow indicator with optional draft styling.',
    tags: ['react', 'task-card', 'component', 'status'],
    complexity: 'moderate',
  },
};

// ---- Function-level metadata: only significant ones ----
const FUNC_META = {
  'web/src/App.test.tsx': {
    makeFakeESClass: { summary: 'Test helper building a fake EventSource class that records pact listeners and exposes a fire() method for event simulation.', tags: ['test-helper', 'mock', 'event-source'] },
  },
  'web/src/App.tsx': {
    AppContent: { summary: 'Inner root component holding 30+ React state hooks for project list, selection, worktrees, modal/toast state, data source subscription, and live event polling.', tags: ['react', 'state-management', 'lifecycle'] },
    App: { summary: 'Outer App wrapper that chooses between hosted-mode (URL-driven) and local-source data sources before rendering AppContent.', tags: ['react', 'entry-point', 'wrapper'] },
  },
  'web/src/components/Agents.tsx': {
    Agents: { summary: 'Agents settings panel with load(), toggle(), and a rows-table renderer that lets the user enable or disable each registered agent kind.', tags: ['react', 'settings', 'agents'] },
  },
  'web/src/components/AuditLens.tsx': {
    AuditLens: { summary: 'Renders audit recommendations for a task, color-coded by risk, with a side fetcher effect.', tags: ['react', 'audit', 'risk'] },
    riskColor: { summary: 'Maps a risk level string to a CSS color token.', tags: ['utility', 'color', 'risk'] },
  },
  'web/src/components/Board.tsx': {
    Board: { summary: 'Kanban board component: groups tasks by feature, renders role chips, supports status filters, and dispatches verb actions with humanized errors.', tags: ['react', 'board', 'kanban'] },
    FilterChip: { summary: 'Filter pill component showing a feature label, active state, and progress bar; emits onClick when toggled.', tags: ['react', 'filter', 'pill'] },
  },
  'web/src/components/Board.verb.test.tsx': {
    fx: { summary: 'Test fixture builder producing a synthetic state object with one task in a given status.', tags: ['test-helper', 'fixture', 'mock'] },
  },
  'web/src/components/Canvas.tsx': {
    nextId: { summary: 'Generates a unique draft feature id by walking taken ids and appending a numeric suffix.', tags: ['utility', 'id-generation'] },
    Canvas: { summary: 'Canvas office-layout view: loads layout, supports drag-to-move, opens the dispatch modal on node click, and opens the TaskEditor on draft edits.', tags: ['react', 'canvas', 'layout'] },
  },
  'web/src/components/CommandK.tsx': {
    statusIcon: { summary: 'Returns the emoji icon used to represent a task status in the command palette result list.', tags: ['utility', 'icon', 'status'] },
    CommandK: { summary: 'Cmd+K palette modal that fuzzy-filters tasks, supports view/project switching, and runs actions via postVerb.', tags: ['react', 'command-palette', 'modal'] },
    CheatSheet: { summary: 'Compact help dialog listing global keyboard shortcuts, opened from CommandK.', tags: ['react', 'help', 'shortcuts'] },
  },
  'web/src/components/DispatchModal.test.tsx': {
    renderModal: { summary: 'Test helper mounting DispatchModal with a fake data source and helpers.', tags: ['test-helper', 'mock', 'mount'] },
    hostedSource: { summary: 'Test helper constructing a hosted-mode data source stub with mocked postTask/verb methods.', tags: ['test-helper', 'mock', 'data-source'] },
  },
  'web/src/components/DispatchModal.tsx': {
    dispatchPayload: { summary: 'Pure builder that turns the modal form state into a wire-ready dispatch payload object.', tags: ['utility', 'dispatch', 'payload'] },
    DispatchModal: { summary: 'Modal form for dispatching a task to a worker seat with reviewer and dependency selection.', tags: ['react', 'modal', 'dispatch'] },
  },
  'web/src/components/ElementsGallery.tsx': {
    Section: { summary: 'Labelled section wrapper used inside the gallery to group related element samples.', tags: ['react', 'layout', 'section'] },
    ElementsGallery: { summary: 'Full design-system catalogue rendering swatches, badges, status pills, ants, buttons, inputs, skeletons, and tool rows for visual review.', tags: ['react', 'design-system', 'catalogue'] },
    NodeCard: { summary: 'Sample card showing node styling variants used in the design system.', tags: ['react', 'card', 'design-system'] },
    ToolRow: { summary: 'Row component rendering a tool icon, label, and inline help text for the tools section.', tags: ['react', 'tool', 'row'] },
    LinksDemo: { summary: 'Demo row showing how links inherit colour and hover affordance in the design system.', tags: ['react', 'links', 'demo'] },
    GridLensDemo: { summary: 'Demo grid showing the responsive card-layout lens used across the app.', tags: ['react', 'grid', 'demo'] },
    Swatch: { summary: 'Color swatch row showing a token name and its computed background.', tags: ['react', 'swatch', 'color'] },
  },
  'web/src/components/LiveOrchestrate.test.tsx': {
    hostedSource: { summary: 'Test helper constructing a hosted-mode data source stub with mocked orchestrate APIs.', tags: ['test-helper', 'mock', 'data-source'] },
  },
  'web/src/components/LiveOrchestrate.tsx': {
    LiveOrchestrate: { summary: 'Live orchestration view: polls single and parallel orchestrate status, renders feature lanes with review gates, and exposes run/resume/ship/diff actions.', tags: ['react', 'live', 'orchestrate', 'pipeline'] },
    RunControl: { summary: 'Top control bar with run, resume, and ship buttons plus a status pill.', tags: ['react', 'control', 'actions'] },
    FeatureLane: { summary: 'Lane component rendering one feature and its pipeline chips with merge and reject affordances.', tags: ['react', 'lane', 'pipeline'] },
    PipeChip: { summary: 'Pipeline status chip showing an agent kind and its current state.', tags: ['react', 'chip', 'pipeline'] },
    MergeNode: { summary: 'Visual merge node rendered between feature lanes at dependency junctions.', tags: ['react', 'node', 'merge'] },
    ReviewGate: { summary: 'Modal-style review gate displayed when a feature requires reviewer sign-off before merge.', tags: ['react', 'review', 'gate'] },
    EventStream: { summary: 'Bottom event log streaming recent orchestration events with timestamp and actor.', tags: ['react', 'events', 'log'] },
  },
  'web/src/components/Machines.test.tsx': {
    makeSource: { summary: 'Test helper building a stub data source with a getMachines() promise.', tags: ['test-helper', 'mock', 'data-source'] },
  },
  'web/src/components/Machines.tsx': {
    Machines: { summary: 'Machines settings list showing worker machines, their agent kinds, and last-seen timestamps.', tags: ['react', 'settings', 'machines'] },
  },
  'web/src/components/NoProjects.tsx': {
    NoProjectsHosted: { summary: 'Hosted-mode empty state: lets the user register a remote project path via the registry API.', tags: ['react', 'empty-state', 'hosted'] },
    NoProjectsLocal: { summary: 'Local-mode empty state: lets the user register a local project by absolute path on disk.', tags: ['react', 'empty-state', 'local'] },
  },
  'web/src/components/Recipes.tsx': {
    Recipes: { summary: 'Recipe browser with select/expand controls and a draft task preview list.', tags: ['react', 'recipes', 'expand'] },
  },
  'web/src/components/RelayConnect.tsx': {
    RelayConnect: { summary: 'Form for connecting to a remote relay server with URL and secret fields and basic validation.', tags: ['react', 'relay', 'onboarding'] },
  },
  'web/src/components/RightRail.test.tsx': {
    ev: { summary: 'Test fixture builder producing a synthetic pact event with a configurable timestamp.', tags: ['test-helper', 'fixture', 'event'] },
  },
  'web/src/components/RightRail.tsx': {
    RightRail: { summary: 'Right-rail task detail panel: per-task stats, timeline, role castes, and accept/changes/merge verb controls with focus management.', tags: ['react', 'right-rail', 'task-detail'] },
  },
  'web/src/components/Setup.tsx': {
    Setup: { summary: 'Setup wizard that fetches seat/role suggestions, lets the user toggle roles, validates bindings, and emits copy-pasteable wire commands.', tags: ['react', 'setup', 'wizard'] },
  },
  'web/src/components/Skeleton.tsx': {
    Skeleton: { summary: 'Generic skeleton block accepting a className and a row count, used as the default placeholder.', tags: ['react', 'skeleton', 'loading'] },
    BoardSkeleton: { summary: 'Specialised skeleton mirroring the Board view layout while data loads.', tags: ['react', 'skeleton', 'board'] },
    CanvasSkeleton: { summary: 'Specialised skeleton mirroring the Canvas view layout while data loads.', tags: ['react', 'skeleton', 'canvas'] },
    OpsSkeleton: { summary: 'Specialised skeleton mirroring the Ops view layout while data loads.', tags: ['react', 'skeleton', 'ops'] },
  },
  'web/src/components/TaskCard.tsx': {
    TaskCard: { summary: 'Task card showing lifecycle stage, status colour, role caste, and review-flow indicator.', tags: ['react', 'task-card', 'status'] },
  },
};

// ---- Helpers ----
const extractByPath = (p) => extract.results.find(r => r.path === p);
const isTestFile = (p) => /\.test\.(tsx?|ts)$/.test(p) || /\.spec\.(tsx?|ts)$/.test(p);
const isExported = (filePath, name) => {
  const r = extractByPath(filePath);
  if (!r || !r.exports) return false;
  return r.exports.some(e => e.name === name);
};

// Determine production counterpart for a test file (best-effort path mapping)
function prodForTest(testPath) {
  return testPath.replace(/\.test\.(tsx?|ts)$/, '.$1');
}

// ---- Build nodes ----
const nodes = [];
const functionNodesByPath = {}; // path -> [{funcName, nodeId, lineRange, exported, summary, tags, complexity}]

for (const f of input.batchFiles) {
  const meta = FILE_META[f.path] || { summary: '', tags: [], complexity: 'simple' };
  const r = extractByPath(f.path);
  const nonEmpty = r ? r.nonEmptyLines : f.sizeLines;
  const complexity = meta.complexity || (nonEmpty < 50 ? 'simple' : nonEmpty < 200 ? 'moderate' : 'complex');
  const fileNode = {
    id: `file:${f.path}`,
    type: 'file',
    name: path.basename(f.path),
    filePath: f.path,
    summary: meta.summary,
    tags: meta.tags,
    complexity,
  };
  if (meta.languageNotes) fileNode.languageNotes = meta.languageNotes;
  nodes.push(fileNode);

  // Function nodes
  if (r && r.functions) {
    functionNodesByPath[f.path] = [];
    const funcMeta = FUNC_META[f.path] || {};
    for (const fn of r.functions) {
      const lines = fn.endLine - fn.startLine;
      const exported = isExported(f.path, fn.name);
      const significant = lines >= 10 || exported;
      if (!significant) continue;
      const fm = funcMeta[fn.name] || { summary: `${fn.name} function in ${path.basename(f.path)}.`, tags: ['react', 'function'] };
      const nodeId = `function:${f.path}:${fn.name}`;
      const fnNode = {
        id: nodeId,
        type: 'function',
        name: fn.name,
        filePath: f.path,
        lineRange: [fn.startLine, fn.endLine],
        summary: fm.summary,
        tags: fm.tags,
        complexity: lines < 30 ? 'simple' : lines < 100 ? 'moderate' : 'complex',
      };
      nodes.push(fnNode);
      functionNodesByPath[f.path].push({ name: fn.name, nodeId, exported, lineRange: [fn.startLine, fn.endLine] });
    }
  }
}

// ---- Build edges ----
const edges = [];
let importEdgeCount = 0;

// 1. Imports edges (1 per entry in batchImportData)
const importData = input.batchImportData || {};
for (const [srcFile, targets] of Object.entries(importData)) {
  for (const tgt of targets) {
    edges.push({
      source: `file:${srcFile}`,
      target: `file:${tgt}`,
      type: 'imports',
      direction: 'forward',
      weight: 0.7,
    });
    importEdgeCount++;
  }
}

// 2. Contains edges (file -> function nodes defined in it)
for (const [fp, fns] of Object.entries(functionNodesByPath)) {
  for (const fn of fns) {
    edges.push({
      source: `file:${fp}`,
      target: fn.nodeId,
      type: 'contains',
      direction: 'forward',
      weight: 1.0,
    });
  }
}

// 3. Exports edges (file -> exported function nodes defined in it)
for (const [fp, fns] of Object.entries(functionNodesByPath)) {
  for (const fn of fns) {
    if (fn.exported) {
      edges.push({
        source: `file:${fp}`,
        target: fn.nodeId,
        type: 'exports',
        direction: 'forward',
        weight: 0.8,
      });
    }
  }
}

// 4. tested_by edges: production -> test file (when test file imports production)
for (const f of input.batchFiles) {
  if (!isTestFile(f.path)) continue;
  const prod = prodForTest(f.path);
  const imports = importData[f.path] || [];
  if (imports.includes(prod)) {
    edges.push({
      source: `file:${prod}`,
      target: `file:${f.path}`,
      type: 'tested_by',
      direction: 'forward',
      weight: 0.5,
    });
  }
}

// 5. Cross-batch calls edges (only when a callee name matches a neighbor symbol)
const knownSymbols = new Map(); // calleeName -> [nodeId,...]
for (const [srcFile, neighbors] of Object.entries(neighborMap)) {
  for (const n of neighbors) {
    for (const sym of (n.symbols || [])) {
      if (!knownSymbols.has(sym)) knownSymbols.set(sym, []);
      knownSymbols.get(sym).push(`function:${n.path}:${sym}`);
    }
  }
}

// Build a set of our local function names per source file (for caller lookup)
const localFuncNamesBySrcFile = {};
for (const [fp, fns] of Object.entries(functionNodesByPath)) {
  localFuncNamesBySrcFile[fp] = new Set(fns.map(f => f.name));
}

// Walk callGraph for cross-batch calls where callee is in knownSymbols
const seenCallsEdges = new Set();
function addCallEdge(sourceId, targetId) {
  if (sourceId === targetId) return;
  const key = sourceId + '|' + targetId;
  if (seenCallsEdges.has(key)) return;
  seenCallsEdges.add(key);
  edges.push({
    source: sourceId,
    target: targetId,
    type: 'calls',
    direction: 'forward',
    weight: 0.8,
  });
}

for (const r of extract.results) {
  if (!r.callGraph) continue;
  const localSet = localFuncNamesBySrcFile[r.path] || new Set();
  // Build callerId candidates: if caller is a known local function, emit from that function; else from file
  for (const cg of r.callGraph) {
    const caller = cg.caller;
    let callee = cg.callee;
    // Trim trailing method chains / async wrappers
    callee = callee.replace(/\(.+\)$/, '').trim();
    // The script's `callee` may include `parent.child` — keep first segment
    if (!knownSymbols.has(callee)) continue;
    // Resolve source: prefer function node if caller is local function; else file
    let sourceId;
    if (localSet.has(caller)) {
      sourceId = `function:${r.path}:${caller}`;
    } else {
      // Only emit from file when caller matches a local helper or is AppContent/etc that is also a local fn
      continue; // skip pure React-hooks / non-local callers to limit noise
    }
    for (const targetId of knownSymbols.get(callee)) {
      addCallEdge(sourceId, targetId);
    }
  }
}

// ---- Compute totals and decide split ----
const totalNodes = nodes.length;
const totalEdges = edges.length;
const parts = Math.ceil(Math.max(totalNodes / 60, totalEdges / 120));
console.error(`nodeCount=${totalNodes} edgeCount=${totalEdges} importEdges=${importEdgeCount} parts=${parts}`);

if (parts === 1) {
  fs.writeFileSync(`${OUT_DIR}/batch-1.json`, JSON.stringify({ nodes, edges }, null, 2));
  console.error('Wrote batch-1.json');
} else {
  // Sort files alphabetically, chunk sequentially
  const allFiles = [...input.batchFiles].map(f => f.path).sort();
  const chunkSize = Math.ceil(allFiles.length / parts);
  for (let k = 1; k <= parts; k++) {
    const start = (k - 1) * chunkSize;
    const end = Math.min(start + chunkSize, allFiles.length);
    const partFiles = new Set(allFiles.slice(start, end));
    const partNodes = nodes.filter(n => {
      if (!n.filePath) return false;
      return partFiles.has(n.filePath);
    });
    const nodeIds = new Set(partNodes.map(n => n.id));
    const partEdges = edges.filter(e => nodeIds.has(e.source));
    const outPath = `${OUT_DIR}/batch-1-part-${k}.json`;
    fs.writeFileSync(outPath, JSON.stringify({ nodes: partNodes, edges: partEdges }, null, 2));
    console.error(`Wrote ${outPath} nodes=${partNodes.length} edges=${partEdges.length}`);
  }
}

// ---- Validation ----
console.error('\n=== Validation ===');
console.error(`Total nodes: ${nodes.length}, edges: ${edges.length}, imports: ${importEdgeCount}`);
const dupCheck = new Set();
for (const n of nodes) {
  if (dupCheck.has(n.id)) console.error('DUPLICATE NODE:', n.id);
  dupCheck.add(n.id);
}
const expectedImportTotal = Object.values(importData).reduce((s, a) => s + a.length, 0);
console.error(`Expected imports total = ${expectedImportTotal}, emitted = ${importEdgeCount}, match = ${importEdgeCount === expectedImportTotal}`);