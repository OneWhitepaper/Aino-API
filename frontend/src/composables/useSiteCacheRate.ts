import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSiteCacheRate, type SiteCacheRateResponse } from '@/api/usage'
import { updateSiteCacheRateVisibility } from '@/api/admin/settings'
import { useAppStore } from '@/stores/app'

export function useSiteCacheRate(startDate: Ref<string>, endDate: Ref<string>, manage = false) {
  const { t } = useI18n()
  const appStore = useAppStore()
  const siteCacheRate = ref<SiteCacheRateResponse | null>(null)
  const siteCacheRateLoading = ref(false)
  const siteCacheRateSaving = ref(false)
  const showSiteCacheRate = computed(() => manage || siteCacheRate.value?.visible_to_users === true)
  let requestRevision = 0
  let visibilityRevision = 0
  let savedVisibility = false
  let controller: AbortController | null = null

  async function refreshSiteCacheRate() {
    const revision = ++requestRevision
    const visibilityAtRequest = visibilityRevision
    controller?.abort()
    controller = new AbortController()
    siteCacheRateLoading.value = true
    if (!manage) siteCacheRate.value = null
    try {
      const data = await getSiteCacheRate({ start_date: startDate.value, end_date: endDate.value }, { signal: controller.signal })
      if (revision !== requestRevision) return
      siteCacheRate.value = visibilityAtRequest === visibilityRevision ? data : { ...data, visible_to_users: savedVisibility }
    } catch {
      if (revision === requestRevision) siteCacheRate.value = null
    } finally {
      if (revision === requestRevision) siteCacheRateLoading.value = false
    }
  }

  async function setSiteCacheVisibility(visible: boolean) {
    if (!manage || !siteCacheRate.value || siteCacheRateSaving.value) return
    siteCacheRateSaving.value = true
    try {
      const settings = await updateSiteCacheRateVisibility(visible)
      savedVisibility = settings.visible_to_users
      visibilityRevision++
      if (siteCacheRate.value) siteCacheRate.value = { ...siteCacheRate.value, visible_to_users: savedVisibility }
    } catch {
      appStore.showError(t('usage.siteCacheRateSaveFailed'))
    } finally {
      siteCacheRateSaving.value = false
    }
  }

  watch([startDate, endDate], refreshSiteCacheRate, { immediate: true })
  onScopeDispose(() => { requestRevision++; controller?.abort() })

  return { siteCacheRate, siteCacheRateLoading, siteCacheRateSaving, showSiteCacheRate, refreshSiteCacheRate, setSiteCacheVisibility }
}
