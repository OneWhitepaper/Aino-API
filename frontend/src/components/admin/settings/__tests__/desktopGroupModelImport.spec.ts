import { describe, expect, it } from 'vitest'
import { discoverDesktopGroupModels } from '../desktopModelImport'
import type { SyncUpstreamModelsResult } from '@/api/admin/accounts'
import type { AccountListItem, AdminGroup } from '@/types'

const group = { id: 17, platform: 'openai' } as AdminGroup
const account = (id: number, platform: AccountListItem['platform'] = 'openai', type: AccountListItem['type'] = 'apikey') => ({ id, platform, type })

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
}

describe('group model discovery', () => {
  it('unions sources and deduplicates routes in source order without binding an account', async () => {
    const first = deferred<SyncUpstreamModelsResult>()
    const second = deferred<SyncUpstreamModelsResult>()
    const pending = discoverDesktopGroupModels(group, [account(1), account(2)], 'auto', id => id === 1 ? first.promise : second.promise)
    second.resolve({ models: ['shared', 'second'], metadata: { shared: { id: 'shared', display_name: 'Second metadata' } } })
    await flush()
    first.resolve({ models: ['shared', 'first'], metadata: { shared: { id: 'shared', display_name: 'First metadata' } } })
    const result = await pending
    expect(result.models.map(model => model.model)).toEqual(['shared', 'first', 'second'])
    expect(result.models[0].display_name).toBe('First metadata')
    expect(result.models.every(model => model.group_id === 17 && !('account_id' in model))).toBe(true)
    expect(result).toMatchObject({ succeeded: 2, failed: 0 })
  })

  it('retains successful catalogs and metadata warnings when another account fails', async () => {
    const warning = { code: 'metadata_partial', message: 'Some model details are unavailable' }
    const result = await discoverDesktopGroupModels(group, [account(1), account(2)], 'responses', async id => {
      if (id === 1) throw new Error('unreachable')
      return { models: ['available'], warnings: [warning] }
    })
    expect(result).toMatchObject({ succeeded: 1, failed: 1, warnings: [warning] })
    expect(result.models.map(model => [model.model, model.api_mode])).toEqual([['available', 'responses']])
  })

  it('reports a complete fetch failure without producing models or unhandled rejections', async () => {
    const result = await discoverDesktopGroupModels(group, [account(1), account(2)], 'auto', async () => { throw new Error('offline') })
    expect(result).toEqual({ models: [], warnings: [], succeeded: 0, failed: 2 })
  })

  it('preserves platform and protocol distinctions for mixed group accounts', async () => {
    const mixed = { ...group, platform: 'composite' } as AdminGroup
    const sources = [account(1), account(2, 'openai', 'oauth'), account(3, 'anthropic')]
    const result = await discoverDesktopGroupModels(mixed, sources, 'auto', async () => ({ models: ['same-name'] }))
    expect(result.models.map(model => [model.platform, model.api_mode])).toEqual([
      ['openai', 'chat_completions'], ['openai', 'responses'], ['anthropic', 'anthropic_messages']
    ])
    const images = await discoverDesktopGroupModels(group, [account(1), account(2)], 'images_generations', async () => ({ models: ['image'] }))
    expect(images.models).toHaveLength(1)
    expect(images.models[0]).toMatchObject({ api_mode: 'images_generations', capabilities: { image_generation: true, tools: false } })
  })

  it('limits concurrent discovery to two sources while returning the complete union', async () => {
    let active = 0
    let peak = 0
    const waiting: Array<ReturnType<typeof deferred<SyncUpstreamModelsResult>>> = []
    const pending = discoverDesktopGroupModels(group, [1, 2, 3, 4, 5].map(id => account(id)), 'auto', async () => {
      active++
      peak = Math.max(peak, active)
      const result = deferred<SyncUpstreamModelsResult>()
      waiting.push(result)
      try { return await result.promise } finally { active-- }
    })
    await flush()
    expect(active).toBe(2)
    for (let index = 0; index < 5; index++) {
      waiting[index].resolve({ models: [`model-${index}`] })
      await flush()
    }
    const result = await pending
    expect(peak).toBe(2)
    expect(result.models).toHaveLength(5)
    expect(result.succeeded).toBe(5)
  })

  it('stops queued requests and discards stale results after switching groups', async () => {
    let current = true
    const started: number[] = []
    const first = deferred<SyncUpstreamModelsResult>()
    const second = deferred<SyncUpstreamModelsResult>()
    const pending = discoverDesktopGroupModels(group, [account(1), account(2), account(3)], 'auto', id => {
      started.push(id)
      return id === 1 ? first.promise : second.promise
    }, () => current)
    current = false
    first.reject(new Error('late failure'))
    second.resolve({ models: ['old-group'] })
    expect(await pending).toEqual({ models: [], warnings: [], succeeded: 0, failed: 0 })
    expect(started).toEqual([1, 2])
  })
})
