import { copyFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const here = fileURLToPath(new URL('.', import.meta.url));
copyFileSync(new URL('../../install.sh', `file://${here}`), new URL('../public/install.sh', `file://${here}`));
console.log('synced install.sh -> public/');
