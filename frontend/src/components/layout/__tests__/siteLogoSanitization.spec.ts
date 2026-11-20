import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const sidebarSource = readFileSync(resolve(dir, '../AppSidebar.vue'), 'utf8')
const homeViewSource = readFileSync(resolve(dir, '../../../views/HomeView.vue'), 'utf8')
const keyUsageViewSource = readFileSync(resolve(dir, '../../../views/KeyUsageView.vue'), 'utf8')

describe('site_logo sanitization', () => {
  it('all website entry points use the shared safe logo resolver', () => {
    for (const src of [sidebarSource, homeViewSource, keyUsageViewSource]) {
      expect(src).toContain("from '@/utils/branding'")
      expect(src).toContain('resolveSiteLogo(')
    }
  })
})
