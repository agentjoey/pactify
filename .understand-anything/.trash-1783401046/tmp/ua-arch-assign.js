#!/usr/bin/env node
// Assign all file nodes to layers, verify counts against input
const fs = require('fs');

const input = JSON.parse(fs.readFileSync('/Users/xtation/AgentWorks/Code_Claude/pactify/.understand-anything/tmp/ua-architecture-input.json', 'utf8'));

// Layer rule table: ordered, first match wins
function layerFor(n) {
  const p = n.filePath;
  const id = n.id;

  // --- layer:cli ---
  if (p.startsWith('cmd/pactify/') || p === 'cmd/pactify') return 'layer:cli';

  // --- layer:web-dashboard ---
  if (p === 'web' || p.startsWith('web/')) return 'layer:web-dashboard';

  // --- layer:cloud-monorepo (relay + shared libs + cloud configs + Dockerfile stages) ---
  if (p === 'cloud/fly.toml' || p === 'cloud/fly.staging.toml' || p === 'cloud/package.json' ||
      p === 'cloud/pnpm-workspace.yaml' || p === 'cloud/tsconfig.base.json' || p === 'cloud/.npmrc')
    return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/relay/') || p === 'cloud/relay') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/wire/') || p === 'cloud/wire') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/crypto/') || p === 'cloud/crypto') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/pact-project/') || p === 'cloud/pact-project') return 'layer:cloud-monorepo';
  if (p.startsWith('cloud/relay-client/') || p === 'cloud/relay-client') return 'layer:cloud-monorepo';

  // --- layer:core-protocol ---
  // internal/pact, internal/event, internal/gitx, internal/lockx, internal/paths, internal/projection, schemas/
  if (p.startsWith('internal/pact/') || p === 'internal/pact') return 'layer:core-protocol';
  if (p.startsWith('internal/event/') || p === 'internal/event') return 'layer:core-protocol';
  if (p.startsWith('internal/gitx/') || p === 'internal/gitx') return 'layer:core-protocol';
  if (p.startsWith('internal/lockx/') || p === 'internal/lockx') return 'layer:core-protocol';
  if (p.startsWith('internal/paths/') || p === 'internal/paths') return 'layer:core-protocol';
  if (p.startsWith('internal/projection/') || p === 'internal/projection') return 'layer:core-protocol';
  if (p.startsWith('schemas/') || p === 'schemas') return 'layer:core-protocol';

  // --- layer:orchestration ---
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

  // --- layer:agents-and-seats ---
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

  // --- layer:mcp-and-http-serving ---
  if (p.startsWith('internal/mcp/') || p === 'internal/mcp') return 'layer:mcp-and-http-serving';
  if (p.startsWith('internal/serve/') || p === 'internal/serve') return 'layer:mcp-and-http-serving';

  // --- layer:cloud-integration (Go-side cloud client) ---
  if (p.startsWith('internal/cloudauth/') || p === 'internal/cloudauth') return 'layer:cloud-integration';
  if (p.startsWith('internal/cloudclient/') || p === 'internal/cloudclient') return 'layer:cloud-integration';
  if (p.startsWith('internal/relaysock/') || p === 'internal/relaysock') return 'layer:cloud-integration';
  if (p.startsWith('internal/remoteexec/') || p === 'internal/remoteexec') return 'layer:cloud-integration';
  if (p.startsWith('internal/remotemachine/') || p === 'internal/remotemachine') return 'layer:cloud-integration';

  // --- layer:docs-and-protocol-specs (all document nodes) ---
  if (n.type === 'document') return 'layer:docs-and-protocol-specs';

  // --- Everything else goes to layer:tests-runtime-and-distribution ---
  return 'layer:tests-runtime-and-distribution';
}

const buckets = {};
const unassigned = [];
for (const n of input.fileNodes) {
  const layer = layerFor(n);
  if (!layer) { unassigned.push(n.id); continue; }
  (buckets[layer] = buckets[layer] || []).push(n.id);
}

console.log('=== Layer counts ===');
const ordered = [
  'layer:cli',
  'layer:core-protocol',
  'layer:orchestration',
  'layer:agents-and-seats',
  'layer:mcp-and-http-serving',
  'layer:cloud-integration',
  'layer:web-dashboard',
  'layer:cloud-monorepo',
  'layer:docs-and-protocol-specs',
  'layer:tests-runtime-and-distribution',
];
let total = 0;
for (const l of ordered) {
  const c = (buckets[l] || []).length;
  total += c;
  console.log(' ', l.padEnd(45), c);
}
console.log('Total assigned:', total);
console.log('Total input:', input.fileNodes.length);
console.log('Unassigned:', unassigned.length);
if (unassigned.length) console.log('UNASSIGNED:', unassigned);