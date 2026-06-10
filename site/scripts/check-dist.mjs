// Post-build assertions: fails (exit 1) on any drift between dist and the canon.
import { readFileSync } from 'node:fs';

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8');
const fail = (msg) => { console.error(`✗ ${msg}`); process.exitCode = 1; };
const ok = (msg) => console.log(`✓ ${msg}`);

// 1. install.sh in dist is byte-identical to the repo root installer
const canon = read('../../install.sh');
const served = read('../dist/install.sh');
canon === served ? ok('dist/install.sh matches repo root') : fail('dist/install.sh drifted from repo root install.sh');

// 2. landing carries the curl one-liner and both rules
const index = read('../dist/index.html');
index.includes('curl -fsSL https://pactify.dev/install.sh | sh') ? ok('landing has curl CTA') : fail('landing missing curl CTA');
index.includes('self-accept') && index.includes('every task is accepted') ? ok('landing has the two rules') : fail('landing missing rule text');

// 3. docs pages render the canonical titles
read('../dist/protocol/index.html').includes('Pact Protocol v1') ? ok('/protocol renders the spec') : fail('/protocol missing spec title');
read('../dist/onboarding/index.html').includes('Agent onboarding') ? ok('/onboarding renders the doc') : fail('/onboarding missing doc title');

// 4. v2 landing structure
const personas = ['product agent', 'design agent', 'dev agent A'];
personas.every((p) => index.includes(p)) ? ok('landing has persona seats') : fail('landing missing persona seat names');
const anchors = (index.match(/onboarding#[a-z-]+/g) || []).length;
anchors >= 12 ? ok(`marquee onboarding anchors (${anchors})`) : fail(`marquee anchors missing (found ${anchors}, want >=12)`);
index.includes('What we didn&#39;t build') || index.includes("What we didn't build") ? ok('landing has philosophy section') : fail('landing missing philosophy section');
index.includes('in development') ? ok('ladder has status badges') : fail('ladder missing in-development badge');
index.includes('href="#"') ? fail('placeholder links remain on the landing') : ok('no placeholder links');

// 5. role tokens shipped in the built CSS
import { readdirSync } from 'node:fs';
const cssDir = new URL('../dist/_astro/', import.meta.url);
const cssFiles = readdirSync(cssDir).filter((f) => f.endsWith('.css'));
const css = cssFiles.map((f) => readFileSync(new URL(f, cssDir), 'utf8')).join('');
css.includes('--role-product') ? ok('role tokens in built CSS') : fail('role tokens missing from built CSS');
