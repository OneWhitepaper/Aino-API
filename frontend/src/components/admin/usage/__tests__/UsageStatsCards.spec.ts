import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UsageStatsCards from '../UsageStatsCards.vue'

const messages: Record<string, string> = {
  'usage.totalRequests': 'Total Requests',
  'usage.inSelectedRange': 'in selected range',
  'usage.totalTokens': 'Total Tokens',
  'usage.in': 'In',
  'usage.out': 'Out',
  'usage.cacheTotal': 'Cache',
  'usage.cacheBreakdown': 'Cache Token Breakdown',
  'usage.cacheCreationTokensLabel': 'Cache Creation',
  'usage.cacheReadTokensLabel': 'Cache Read',
  'usage.totalCost': 'Total Cost',
  'usage.accountCost': 'Cost',
  'usage.standardCost': 'Standard',
  'usage.avgDuration': 'Avg Duration',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const stats = {
  total_requests: 1,
  total_input_tokens: 100,
  total_output_tokens: 50,
  total_cache_tokens: 34,
  total_cache_creation_tokens: 12,
  total_cache_read_tokens: 22,
  total_tokens: 184,
  total_cost: 0.001,
  total_actual_cost: 0.001,
  total_account_cost: 0.001,
  average_duration_ms: 250,
}

describe('UsageStatsCards', () => {
  it('shows a separate site-wide rate without using the personal summary tokens', async () => {
    const wrapper = mount(UsageStatsCards, {
      props: { stats, showSiteCacheRate: true, siteCacheRate: { cache_hit_rate: 81.23, visible_to_users: false }, manageSiteCacheRate: true },
      global: { stubs: { Icon: true } },
    })
    expect(wrapper.get('[data-testid="site-cache-rate-card"]').text()).toContain('81.2%')
    expect(wrapper.classes()).toContain('xl:grid-cols-5')
    const toggle = wrapper.get('[data-testid="site-cache-rate-visibility"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    expect(wrapper.emitted('updateSiteCacheVisibility')).toEqual([[true]])
  })

  it('hides the site card for users when disabled and never gives users a visibility toggle', async () => {
    const wrapper = mount(UsageStatsCards, {
      props: { stats, showSiteCacheRate: false, siteCacheRate: { cache_hit_rate: 98, visible_to_users: false } },
      global: { stubs: { Icon: true } },
    })
    expect(wrapper.find('[data-testid="site-cache-rate-card"]').exists()).toBe(false)
    await wrapper.setProps({ showSiteCacheRate: true, siteCacheRate: { cache_hit_rate: 0, visible_to_users: true } })
    expect(wrapper.get('[data-testid="site-cache-rate-card"]').text()).toContain('0.0%')
    expect(wrapper.find('[data-testid="site-cache-rate-visibility"]').exists()).toBe(false)
    await wrapper.setProps({ siteCacheRate: { visible_to_users: true } })
    expect(wrapper.get('[data-testid="site-cache-rate-card"]').text()).toContain('—')
  })

  it('shows cache token breakdown values', () => {
    const wrapper = mount(UsageStatsCards, {
      props: {
        stats,
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('Cache: 34')
    expect(text).toContain('Cache Token Breakdown')
    expect(text).toContain('Cache Creation')
    expect(text).toContain('12')
    expect(text).toContain('Cache Read')
    expect(text).toContain('22')
  })

  it('keeps the cache tooltip out of the layout while it is hidden', () => {
    const wrapper = mount(UsageStatsCards, {
      props: {
        stats,
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    const tooltip = wrapper.findAll('span').find((el) => el.classes().includes('group-hover:block'))

    expect(tooltip).toBeDefined()
    // `opacity-0` hides the tooltip visually but keeps it in the layout, so its
    // fixed width still widens the document and causes horizontal scrolling on
    // narrow screens. `hidden` (display: none) takes it out of the flow.
    expect(tooltip?.classes()).toContain('hidden')
    expect(tooltip?.classes()).not.toContain('opacity-0')
  })
})
