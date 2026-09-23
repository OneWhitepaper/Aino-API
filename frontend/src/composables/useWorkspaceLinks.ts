import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import { workspaceCatalog } from '@/utils/workspace'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'
import { sanitizeUrl } from '@/utils/url'

export function useWorkspaceLinks() {
  const app = useAppStore()
  const auth = useAuthStore()
  const catalog = computed(() => workspaceCatalog(app.cachedPublicSettings, auth.isSimpleMode))
  const endpoint = computed(() => app.cachedPublicSettings?.api_base_url?.trim() || window.location.origin)
  const docs = computed(() => sanitizeUrl(app.cachedPublicSettings?.doc_url || ''))
  const billing = computed(() => {
    if (auth.isSimpleMode) return null
    if (!resolveFeatureFlag(app.cachedPublicSettings, FeatureFlags.payment)) {
      return { to: '/redeem', label: 'workspace.redeem' }
    }
    const subscriptionOnly = resolveSiteBillingMode(app.cachedPublicSettings) === 'subscription_only'
    return { to: '/purchase', label: subscriptionOnly ? 'workspace.subscribe' : 'workspace.addBalance' }
  })
  return { catalog, endpoint, docs, billing }
}
