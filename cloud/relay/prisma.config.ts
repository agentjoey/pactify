import process from 'node:process'
import { defineConfig } from 'prisma/config'

/**
 * Prisma 7 config. Connection URLs live here (no longer in schema.prisma).
 *
 * Runtime (Neon) and tests (PGlite) pass their own driver adapter to the
 * PrismaClient constructor (see src/db.ts); this file only feeds the Prisma
 * CLI (generate / migrate diff). DATABASE_URL is a GitHub env secret in CI; a
 * placeholder is fine locally because `migrate diff --from-empty --to-schema`
 * is a pure schema-to-schema diff and never opens a connection.
 */
export default defineConfig({
  schema: 'prisma/schema.prisma',
  migrations: {
    path: 'prisma/migrations',
  },
  datasource: {
    url:
      process.env.DATABASE_URL ?? 'postgresql://placeholder:placeholder@localhost:5432/placeholder',
  },
})
