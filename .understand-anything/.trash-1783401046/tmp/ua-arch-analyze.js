#!/usr/bin/env node
// architecture analyzer: compute structural patterns from file nodes + import edges
const fs = require('fs');
const path = require('path');

const [, , inputPath, outputPath] = process.argv;
if (!inputPath || !outputPath) {
  console.error('usage: ua-arch-analyze.js <input.json> <output.json>');
  process.exit(1);
}

const data = JSON.parse(fs.readFileSync(inputPath, 'utf8'));
const fileNodes = data.fileNodes || [];
const importEdges = data.importEdges || [];
const allEdges = data.allEdges || [];

const nodeById = new Map(fileNodes.map(n => [n.id, n]));

// --- A. Directory Grouping ---
// Group by top-level directory (first path segment), with special-case grouping
// for internal/ (use second segment) since internal/* is the project's main code.
function topDir(n) {
  const p = n.filePath;
  if (!p.includes('/')) return '(root)';
  const segs = p.split('/');
  if (segs[0] === 'internal' && segs.length >= 2) return 'internal/' + segs[1];
  return segs[0];
}

const directoryGroups = {};
for (const n of fileNodes) {
  const d = topDir(n);
  (directoryGroups[d] = directoryGroups[d] || []).push(n.id);
}

// --- B. Node Type Grouping ---
const nodeTypeGroups = {};
for (const n of fileNodes) {
  (nodeTypeGroups[n.type] = nodeTypeGroups[n.type] || []).push(n.id);
}

// --- C. Import Adjacency Matrix ---
const fanIn = {};
const fanOut = {};
for (const e of importEdges) {
  fanOut[e.source] = (fanOut[e.source] || 0) + 1;
  fanIn[e.target] = (fanIn[e.target] || 0) + 1;
}

// --- D. Cross-Category Dependency Analysis ---
const crossCategoryEdges = [];
const ccKey = (a, b, t) => `${a}->${b}:${t}`;
const ccMap = new Map();
for (const e of allEdges) {
  const s = nodeById.get(e.source);
  const t = nodeById.get(e.target);
  if (!s || !t) continue;
  const k = ccKey(s.type, t.type, e.type);
  ccMap.set(k, (ccMap.get(k) || 0) + 1);
}
for (const [k, v] of ccMap) {
  const [pair, et] = k.split(':');
  const [ft, tt] = pair.split('->');
  crossCategoryEdges.push({ fromType: ft, toType: tt, edgeType: et, count: v });
}

// --- E. Inter-Group Import Frequency ---
const interGroupKey = (a, b) => `${a}__${b}`;
const interGroupMap = new Map();
for (const e of importEdges) {
  const s = nodeById.get(e.source);
  const t = nodeById.get(e.target);
  if (!s || !t) continue;
  const gs = topDir(s);
  const gt = topDir(t);
  if (gs === gt) continue;
  const k = interGroupKey(gs, gt);
  interGroupMap.set(k, (interGroupMap.get(k) || 0) + 1);
}
const interGroupImports = [];
for (const [k, v] of interGroupMap) {
  const [gs, gt] = k.split('__');
  interGroupImports.push({ from: gs, to: gt, count: v });
}
interGroupImports.sort((a, b) => b.count - a.count);

// --- F. Intra-Group Import Density ---
const intraStats = {};
for (const e of importEdges) {
  const s = nodeById.get(e.source);
  const t = nodeById.get(e.target);
  if (!s || !t) continue;
  const gs = topDir(s);
  const gt = topDir(t);
  if (!intraStats[gs]) intraStats[gs] = { internalEdges: 0, totalEdges: 0 };
  intraStats[gs].totalEdges++;
  if (gs === gt) intraStats[gs].internalEdges++;
}
const intraGroupDensity = {};
for (const [k, v] of Object.entries(intraStats)) {
  intraGroupDensity[k] = {
    internalEdges: v.internalEdges,
    totalEdges: v.totalEdges,
    density: v.totalEdges === 0 ? 0 : +(v.internalEdges / v.totalEdges).toFixed(3),
  };
}

// --- G. Directory Pattern Matching ---
const PATTERN_RULES = [
  [/^(routes|api|controllers?|endpoints?|handlers?|routers?|blueprints?|serializers)$/, 'api'],
  [/^(services?|core|lib|domain|logic|composables|signals|mailers|jobs|channels)$/, 'service'],
  [/^(models|db|data|persistence|repository|entities|migrations|sql|database|schema|entity)$/, 'data'],
  [/^(components?|views|pages|ui|layouts|screens)$/, 'ui'],
  [/^(middleware|plugins?|interceptors|guards)$/, 'middleware'],
  [/^(utils?|helpers?|common|shared|tools|pkg)$/, 'utility'],
  [/^(configs?|constants?|env|settings)$/, 'config'],
  [/^(__tests__|tests?|specs?)$/, 'test'],
  [/^(types|interfaces|schemas|contracts?|dtos?|dto|request|response)$/, 'types'],
  [/^hooks$/, 'hooks'],
  [/^(store|state|reducers|actions|slices)$/, 'state'],
  [/^(assets?|static|public)$/, 'assets'],
  [/^(management|commands?)$/, 'config'],
  [/^templatetags$/, 'utility'],
  [/^cmd$/, 'entry'],
  [/^internal$/, 'service'],
  [/^bin$/, 'entry'],
  [/^(docs?|documentation|wiki)$/, 'documentation'],
  [/^(deploy|deployment|infra|infrastructure)$/, 'infrastructure'],
  [/^(\.github|\.gitlab|\.circleci)$/, 'ci-cd'],
  [/^(k8s|kubernetes|helm|charts)$/, 'infrastructure'],
  [/^(terraform|tf)$/, 'infrastructure'],
  [/^docker$/, 'infrastructure'],
];
const patternMatches = {};
for (const d of Object.keys(directoryGroups)) {
  const seg = d.includes('/') ? d.split('/')[1] : d.split('/')[0];
  for (const [re, label] of PATTERN_RULES) {
    if (re.test(seg)) { patternMatches[d] = label; break; }
  }
}

// --- H. Deployment Topology Detection ---
const deploymentTopology = {
  hasDockerfile: false,
  hasCompose: false,
  hasK8s: false,
  hasTerraform: false,
  hasCI: false,
  infraFiles: [],
};
for (const n of fileNodes) {
  const p = n.filePath.toLowerCase();
  const base = path.basename(p);
  if (base === 'dockerfile') deploymentTopology.hasDockerfile = true, deploymentTopology.infraFiles.push(n.filePath);
  if (base.startsWith('docker-compose')) deploymentTopology.hasCompose = true, deploymentTopology.infraFiles.push(n.filePath);
  if (base === 'fly.toml' || base === 'fly.staging.toml') deploymentTopology.infraFiles.push(n.filePath);
  if (p.startsWith('.github/workflows/') || p.startsWith('.gitlab-ci') || base === 'jenkinsfile') deploymentTopology.hasCI = true, deploymentTopology.infraFiles.push(n.filePath);
  if (p.endsWith('.tf') || p.endsWith('.tfvars')) deploymentTopology.hasTerraform = true, deploymentTopology.infraFiles.push(n.filePath);
}

// --- I. Data Pipeline Detection ---
const dataPipeline = { schemaFiles: [], migrationFiles: [], dataModelFiles: [], apiHandlerFiles: [] };
for (const n of fileNodes) {
  const p = n.filePath.toLowerCase();
  if (p.endsWith('.sql') && (p.includes('migration') || /migrations?\//.test(p))) {
    dataPipeline.migrationFiles.push(n.filePath);
  } else if (p.endsWith('.sql') || p.endsWith('.prisma')) {
    dataPipeline.schemaFiles.push(n.filePath);
  }
}

// --- J. Documentation Coverage ---
const docCoverage = { groupsWithDocs: 0, totalGroups: 0, coverageRatio: 0, undocumentedGroups: [] };
for (const d of Object.keys(directoryGroups)) {
  docCoverage.totalGroups++;
  const hasDoc = directoryGroups[d].some(id => {
    const n = nodeById.get(id);
    return n && n.type === 'document';
  });
  if (hasDoc) docCoverage.groupsWithDocs++;
  else docCoverage.undocumentedGroups.push(d);
}
docCoverage.coverageRatio = docCoverage.totalGroups === 0 ? 0 : +(docCoverage.groupsWithDocs / docCoverage.totalGroups).toFixed(3);

// --- K. Dependency Direction ---
const depPairs = new Map();
for (const { from, to, count } of interGroupImports) {
  const k = from < to ? `${from}__${to}` : `${to}__${from}`;
  if (!depPairs.has(k)) depPairs.set(k, { a: from, b: to, ab: 0, ba: 0 });
  const ent = depPairs.get(k);
  if (from < to) ent.ab += count; else ent.ba += count;
}
const dependencyDirection = [];
for (const v of depPairs.values()) {
  if (v.ab > v.ba) dependencyDirection.push({ dependent: v.b, dependsOn: v.a, weight: v.ab - v.ba });
  else if (v.ba > v.ab) dependencyDirection.push({ dependent: v.a, dependsOn: v.b, weight: v.ba - v.ab });
}
dependencyDirection.sort((a, b) => b.weight - a.weight);

// --- fileStats ---
const filesPerGroup = Object.fromEntries(Object.entries(directoryGroups).map(([k, v]) => [k, v.length]));
const nodeTypeCounts = Object.fromEntries(Object.entries(nodeTypeGroups).map(([k, v]) => [k, v.length]));

const result = {
  scriptCompleted: true,
  directoryGroups,
  nodeTypeGroups,
  crossCategoryEdges,
  interGroupImports,
  intraGroupDensity,
  patternMatches,
  deploymentTopology,
  dataPipeline,
  docCoverage,
  dependencyDirection,
  fileStats: {
    totalFileNodes: fileNodes.length,
    filesPerGroup,
    nodeTypeCounts,
  },
  fileFanIn: Object.fromEntries(Object.entries(fanIn).sort((a, b) => b[1] - a[1]).slice(0, 50)),
  fileFanOut: Object.fromEntries(Object.entries(fanOut).sort((a, b) => b[1] - a[1]).slice(0, 50)),
};

fs.writeFileSync(outputPath, JSON.stringify(result, null, 2));
console.error('wrote', outputPath);
console.error('total file nodes:', fileNodes.length);
console.error('directory groups:', Object.keys(directoryGroups).length);