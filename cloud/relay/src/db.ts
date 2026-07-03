import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { PGlite } from '@electric-sql/pglite'
import { PrismaPGlite } from 'pglite-prisma-adapter'
import { PrismaPg } from '@prisma/adapter-pg'
import pg from 'pg'
import { PrismaClient } from '@prisma/client'

/**
 * Build a {@link PrismaClient} backed by an in-memory PGlite instance with the
 * relay schema (`prisma/init.sql`) applied. For tests and local/self-host runs
 * — no network DB. Prod passes a Neon driver adapter instead (later slice).
 *
 * Prisma 7: driver adapters are GA; the adapter is passed to the PrismaClient
 * constructor. `pglite-prisma-adapter` exposes `PrismaPGlite`, a driver-adapter
 * factory wrapping a `PGlite` client.
 */
export async function createPgliteDb(): Promise<PrismaClient> {
  const pglite = new PGlite()
  const sql = readFileSync(fileURLToPath(new URL('../prisma/init.sql', import.meta.url)), 'utf8')
  await pglite.exec(sql)
  const adapter = new PrismaPGlite(pglite)
  return new PrismaClient({ adapter })
}

/**
 * Build a {@link PrismaClient} backed by a real Postgres (Neon in prod) via the
 * Prisma 7 GA driver adapter `@prisma/adapter-pg` (`PrismaPg`) over a long-lived
 * `pg` Pool. Neon's pooled connection string works with a `pg` Pool on Fly.
 *
 * Construction is lazy: the Pool does not dial out and no connection is opened
 * until the first query, so this can be constructed with any URL without I/O.
 */
export function createPostgresDb(connectionString: string): PrismaClient {
  const pool = new pg.Pool({ connectionString })
  const adapter = new PrismaPg(pool)
  return new PrismaClient({ adapter })
}
