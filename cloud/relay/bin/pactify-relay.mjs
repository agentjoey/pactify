#!/usr/bin/env node
// Thin launcher: defer to the compiled bootstrap so the bin has no build step.
import('../dist/server-main.js').then((m) => m.main())
