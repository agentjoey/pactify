import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import { project, type PactEvent, type State } from '../src/index.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const testdata = resolve(__dirname, '..', 'testdata')

function readEvents(): PactEvent[] {
  const text = readFileSync(resolve(testdata, 'events-golden.jsonl'), 'utf-8')
  return text
    .split('\n')
    .filter((line) => line.trim() !== '')
    .map((line) => JSON.parse(line) as PactEvent)
}

function readGolden(): State {
  const text = readFileSync(resolve(testdata, 'state-golden.json'), 'utf-8')
  return JSON.parse(text) as State
}

describe('parity with Go projection (golden vector)', () => {
  it('project(events-golden) deep-equals state-golden', () => {
    const events = readEvents()
    const want = readGolden()
    const got = project(events)
    expect(got).toEqual(want)
  })

  it('awaiting_count equals manual count over projected state', () => {
    const events = readEvents()
    const got = project(events)
    let count = 0
    for (const f of got.features) {
      for (const t of f.tasks) {
        if (t.status === 'awaiting_review') count++
      }
    }
    expect(got.awaiting_count).toBe(count)
  })
})
