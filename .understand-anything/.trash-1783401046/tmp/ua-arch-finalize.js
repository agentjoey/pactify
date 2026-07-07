#!/usr/bin/env node
// Final: build layers.json, verify all node IDs exist in assembled graph
const fs = require('fs');
const path = require('path');

const ROOT = '/Users/xtation/AgentWorks/Code_Claude/pactify';
const input = JSON.parse(fs.readFileSync(path.join(ROOT, '.understand-anything/tmp/ua-architecture-input.json'), 'utf8'));
const graph = JSON.parse(fs.readFileSync(path.join(ROOT, '.understand-anything/intermediate/assembled-graph.json'), 'utf8'));

const graphNodeIds = new Set(graph.nodes.map(n => n.id));
const inputNodeIds = new Set(input.fileNodes.map(n => n.id));

// Layer assignment rules (same as ua-arch-assign.js)
function layerFor(n) {
  const p = n.filePath;
  if (p.startsWith('cmd/pactify/') || p === 'cmd/pactify') return 'layer:cli';
  if (p === 'web' || p.startsWith('web/')) return 'layer:web-dashboard';
  if (p === 'cloud/fly.toml' || p === 'cloud/fly.staging.toml' || p === 'cloud/package.json' ||
      p === 'cloud/pnpm-workspace.yaml' || p === 'cloud/tsconfig.base.json' || p === 'cloud/.npmrc')
    return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/relay/') || p === 'cloud/relay') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/wire/') || p === 'cloud/wire') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/crypto/') || p === 'cloud/crypto') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/pact-project/') || p === 'cloud/pact-project') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/relay-client/') || p === 'cloud/relay-client') return 'layer:cloud-monorepo';
  if (p.startsWith('internal/pact/') || p === 'internal/pact') return 'layer:core-protocol';
  if (p.startsWith('internal/event/') || p === 'internal/event') return 'layer:core-protocol';
  if (p.startsWith('internal/gitx/') || p === 'internal/gitx') return 'layer:core-protocol';
  if (p.startsWith('internal/lockx/') || p === 'internal/lockx') return 'layer:core-protocol';
  if (p.startsWith('internal/paths/') || p === 'internal/paths') return 'layer:core-protocol';
  if (p.startsWith('internal/projection/') || p === 'internal/projection') return 'layer:core-protocol';
  if (p.startsWith('schemas/') || p === 'schemas') return 'layer:core-protocol';
  if (p.startsWith('internal/orchestrate/') || p === 'internal/orchestrate') return 'layer:orchestration';
  if (p.startsWith('internal/planner/') || p === 'internal/planner') return 'layer:orchestration';
  if (p.startsWith('internal/audit/') || p === 'internal/audit') return 'layer:orchestration';
  if (p.startsWith('internal/doctor/') || p === 'internal/doctor') return 'layer:orchestration';
  if (p.startsWith('internal/recipe/') || p === 'internal/recipe') return 'layer:orchestration';
  if (p.startsWith('internal/registry/') || p === 'internal/registry') return 'layer:orchestration';
  if (p.startsWith('internal/schedule/') || p === 'internal/schedule') return 'layer:orchestration';
  if (p.startsWith('internal/wizard/') || p === 'internal/wizard') return 'layer:orchestration';
  if (p.startsWith('internal/finish/') || p === 'internal/finish') return 'layer:orchestration';
  if (p.startsWith('internal/acp/') || p === 'internal/acp') return 'layer:orchestration';
  if (p.startsWith('internal/agent/') || p === 'internal/agent') return 'layer:agents-and-seats';
  if (p.startsWith('internal/agentcfg/') || p === 'internal/agentcfg') return 'layer:agents-and-seats';
  if (p.startsWith('internal/agentmanifest/') || p === 'internal/agentmanifest') return 'layer:agents-and-seats';
  if (p.startsWith('internal/agentreg/') || p === 'internal/agentreg') return 'layer:agents-and-seats';
  if (p.startsWith('internal/sessions/') || p === 'internal/sessions') return 'layer:agents-and-seats';
  if (p.startsWith('internal/stats/') || p === 'internal/stats') return 'layer:agents-and-seats';
  if (p.startsWith('internal/diffstat/') || p === 'internal/diffstat') return 'layer:agents-and-seats';
  if (p.startsWith('internal/machineid/') || p === 'internal/machineid') return 'layer:agents-and-seats';
  if (p.startsWith('internal/tokens/') || p === 'internal/tokens') return 'layer:agents-and-seats';
  if (p.startsWith('internal/secret/') || p === 'internal/secret') return 'layer:agents-and-seats';
  if (p.startsWith('internal/mcp/') || p === 'internal/mcp') return 'layer:mcp-and-http-serving';
  if (p.startsWith('internal/serve/') || p === 'internal/serve') return 'layer:mcp-and-http-serving';
  if (p.startsWith('internal/cloudauth/') || p === 'internal/cloudauth') return 'layer:cloud-integration';
  if (p.startsWith('internal/cloudclient/') || p === 'internal/cloudclient') return 'layer:cloud-integration';
  if (p.startsWith('internal/relaysock/') || p === 'internal/relaysock') return 'layer:cloud-integration';
  if (p.startsWith('internal/remoteexec/') || p === 'internal/remoteexec') return 'layer:cloud-integration';
  if (p.startsWith('internal/remotemachine/') || p === 'internal/remotemachine') return 'layer:cloud-integration';
  if (n.type === 'document') return 'layer:docs-and-protocol-specs';
  return 'layer:tests-build-and-distribution';
}

const buckets = {};
const orphans = [];
for (const n of input.fileNodes) {
  const l = layerFor(n);
  (buckets[l] = buckets[l] || []).push(n.id);
  // Verify each node ID exists in the assembled graph
  if (!graphNodeIds.has(n.id)) orphans.push(n.id);
}
if (orphans.length) {
  console.error('WARNING: input node IDs missing from assembled graph:', orphans.length);
  for (const o of orphans.slice(0, 20)) console.error(' ', o);
}

const layers = [
  {
    id: 'layer:cli',
    name: 'CLI Command Layer',
    description: 'Cobra-based pactify CLI entry points at cmd/pactify/ — every user-facing verb (init, join, assign, checkpoint, accept, changes, merge, status, serve, mcp, doctor, etc.) is wired here and dispatched to the internal packages below.',
    nodeIds: buckets['layer:cli'] || [],
  },
  {
    id: 'layer:core-protocol',
    name: 'Core Protocol Engine',
    description: 'Append-only log event engine, role/rule enforcement, event types, and the state projection that regenerates STATE.yml from log.jsonl — plus JSON Schemas and filesystem/git/lock primitives that the rest of the system depends on.',
    nodeIds: buckets['layer:core-protocol'] || [],
  },
  {
    id: 'layer:orchestration',
    name: 'Orchestration Engine',
    description: 'Multi-agent driver loop (orchestrate + planner + ACP runner), fix-until-green critic, recipe/audit/registry/schedule/wizard/finish workflows — the package that turns the protocol log into a self-driving dev team.',
    nodeIds: buckets['layer:orchestration'] || [],
  },
  {
    id: 'layer:agents-and-seats',
    name: 'Agents & Seats',
    description: 'Agent-kind registry, launch profiles, MCP wiring into per-agent config files, session/diffstat/stats/machine-id/token/secret bookkeeping — everything pactify needs to recognise and steer every supported agent.',
    nodeIds: buckets['layer:agents-and-seats'] || [],
  },
  {
    id: 'layer:mcp-and-http-serving',
    name: 'MCP Server & HTTP Serving',
    description: 'External protocol adapters: the MCP server (internal/mcp) that exposes every pact verb as tools for AI agents, and the HTTP serving layer (internal/serve) that backs the Mission Control dashboard and remote clients.',
    nodeIds: buckets['layer:mcp-and-http-serving'] || [],
  },
  {
    id: 'layer:cloud-integration',
    name: 'Cloud Relay Client (Go-side)',
    description: 'Go-side client for the hosted zero-knowledge cloud relay — authentication, websocket transport, remote execution, and remote-machine registration that lets the CLI collaborate across machines.',
    nodeIds: buckets['layer:cloud-integration'] || [],
  },
  {
    id: 'layer:web-dashboard',
    name: 'Web Dashboard (Mission Control)',
    description: 'React 19 + Vite + xyflow Mission Control frontend at web/ — canvas visualisation of seats/features/tasks, command palette, agents panel, and other operator-facing UI.',
    nodeIds: buckets['layer:web-dashboard'] || [],
  },
  {
    id: 'layer:cloud-monorepo',
    name: 'Cloud Monorepo (Relay & Shared TS Modules)',
    description: 'pnpm workspace at cloud/ containing the Fastify zero-knowledge relay server (with Prisma schema and migrations), the shared wire/crypto/pact-project/relay-client libraries, Dockerfile stages, and Fly.io deploy manifests.',
    nodeIds: buckets['layer:cloud-monorepo'] || [],
  },
  {
    id: 'layer:docs-and-protocol-specs',
    name: 'Documentation & Protocol Specs',
    description: 'All project documentation: top-level README and agent context files, architecture/operations/deployment/onboarding guides, frozen Pact Protocol v1 spec and draft specs, task spec stubs, and the project charter (PROJECT.md).',
    nodeIds: buckets['layer:docs-and-protocol-specs'] || [],
  },
  {
    id: 'layer:tests-build-and-distribution',
    name: 'Tests, Build & Distribution',
    description: 'Bats CLI end-to-end tests, CI/release pipelines, root build configs (go.mod/.goreleaser/.mcp.json/opencode.json), the Claude/Gemini plugin distribution, the .pact runtime artifacts of the dev repo, the dogfood showcase, and the install/gitattributes scripts.',
    nodeIds: buckets['layer:tests-build-and-distribution'] || [],
  },
];

// Final integrity checks
let totalIds = 0;
const seen = new Set();
const missingInGraph = [];
for (const l of layers) {
  if (!l.nodeIds.length) { console.error('WARN: empty layer', l.id); }
  for (const id of l.nodeIds) {
    totalIds++;
    if (seen.has(id)) console.error('DUPLICATE:', id);
    seen.add(id);
    if (!graphNodeIds.has(id)) missingInGraph.push(id);
  }
}
console.error('Total nodeIds across layers:', totalIds);
console.error('Unique IDs:', seen.size);
console.error('Missing in graph:', missingInGraph.length);
if (missingInGraph.length) for (const m of missingInGraph) console.error(' ', m);

// Check coverage
const missingFromLayers = [];
for (const id of inputNodeIds) {
  if (!seen.has(id)) missingFromLayers.push(id);
}
console.error('Input IDs missing from layers:', missingFromLayers.length);
if (missingFromLayers.length) for (const m of missingFromLayers) console.error(' ', m);

if (missingInGraph.length || missingFromLayers.length) process.exit(2);

// Write output
fs.writeFileSync(path.join(ROOT, '.understand-anything/intermediate/layers.json'), JSON.stringify(layers, null, 2));
console.error('Wrote layers.json with', layers.length, 'layers');

// Summary
for (const l of layers) console.error(' ', l.id.padEnd(45), l.nodeIds.length);