// Copy the repo-root installer into public/ so it ships at /install.sh.
// public/install.sh is gitignored — the repo root stays the single source of truth.
import { copyFileSync } from 'node:fs';

copyFileSync(new URL('../../install.sh', import.meta.url), new URL('../public/install.sh', import.meta.url));
console.log('synced install.sh -> public/');
