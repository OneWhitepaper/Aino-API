import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import { useLoginEntryTransition } from '../useLoginEntryTransition'

async function setup(initial = '/login', authenticated = false) {
  const auth = ref(authenticated)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: ['/login', '/auth/oidc/callback', '/home', '/dashboard', '/keys'].map(path => ({
      path, component: { template: '<div />' },
      meta: { requiresAuth: ['/dashboard', '/keys'].includes(path) },
    })),
  })
  await router.push(initial)
  await router.isReady()
  const wrapper = mount(defineComponent({
    setup() { return useLoginEntryTransition(() => auth.value) },
    template: '<div :data-entering="entering" @animationend="finish" />',
  }), { global: { plugins: [router] } })
  return { wrapper, router, auth }
}

describe('login entry transition', () => {
  it('animates successful login once and keeps ordinary navigation immediate', async () => {
    const { wrapper, router, auth } = await setup()
    auth.value = true
    await router.push('/dashboard')
    expect(wrapper.attributes('data-entering')).toBe('true')
    await wrapper.trigger('animationend')
    expect(wrapper.attributes('data-entering')).toBe('false')
    await router.push('/keys')
    expect(wrapper.attributes('data-entering')).toBe('false')
    wrapper.unmount()
  })

  it('covers third-party sign-in and respects its destination', async () => {
    const { wrapper, router, auth } = await setup('/auth/oidc/callback')
    auth.value = true
    await router.push('/keys')
    expect(wrapper.attributes('data-entering')).toBe('true')
    expect(router.currentRoute.value.path).toBe('/keys')
    wrapper.unmount()
  })

  it('does not animate refresh, unauthenticated navigation or aborted navigation', async () => {
    const { wrapper, router, auth } = await setup('/dashboard', true)
    expect(wrapper.attributes('data-entering')).toBe('false')
    await router.push('/login')
    auth.value = false
    await router.push('/dashboard')
    expect(wrapper.attributes('data-entering')).toBe('false')
    await router.push('/login')
    auth.value = true
    const removeGuard = router.beforeEach(() => false)
    await router.push('/dashboard')
    expect(wrapper.attributes('data-entering')).toBe('false')
    removeGuard()
    wrapper.unmount()
  })
})
