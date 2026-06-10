import { defineCollection } from 'astro:content';
import { glob } from 'astro/loaders';

// Single source of truth: render the canonical repo docs, no copies.
const docs = defineCollection({
  loader: glob({ pattern: ['specs/pact-protocol.md', 'agent-onboarding.md'], base: '../docs' }),
});

export const collections = { docs };
