<template>
  <AppLayout>
    <div class="workspace-page">
      <header class="workspace-intro">
        <div>
          <h2>{{ t('workspace.title') }}</h2>
          <p>{{ t('workspace.description') }}</p>
        </div>
        <button class="btn btn-secondary" @click="showGuide = true"><Icon name="book" size="sm" />{{ t('workspace.guide') }}</button>
      </header>

      <div class="workspace-overview">
        <section class="workspace-connect" aria-labelledby="workspace-start">
          <div>
            <h2 id="workspace-start">{{ t('workspace.start') }}</h2>
            <p>{{ t('workspace.startDescription') }}</p>
          </div>
          <div class="workspace-connect-actions">
            <router-link to="/keys?create=1" class="workspace-primary-link">{{ t('workspace.create') }}<Icon name="arrowRight" size="sm" /></router-link>
            <router-link to="/keys" class="workspace-secondary-link">{{ t('workspace.manage') }}</router-link>
          </div>
          <ol class="workspace-steps">
            <li><span>01</span><router-link to="/keys?create=1">{{ t('workspace.keyStep') }}</router-link></li>
            <li><span>02</span><router-link :to="catalog?.to || '/keys?create=1'">{{ t('workspace.modelStep') }}</router-link></li>
            <li><span>03</span><button @click="showGuide = true">{{ t('workspace.requestStep') }}</button></li>
          </ol>
        </section>
        <section class="workspace-account" aria-labelledby="workspace-account-title">
          <h2 id="workspace-account-title">{{ t('workspace.account') }}</h2>
          <template v-if="!authStore.isSimpleMode">
            <span class="workspace-account-label">{{ t('workspace.availableBalance') }}</span>
            <strong class="workspace-balance">{{ user ? money(user.balance, 2) : '—' }}</strong>
            <p v-if="user?.frozen_balance" class="text-xs text-gray-500 dark:text-dark-400">{{ t('workspace.frozenBalance') }} {{ money(user.frozen_balance, 2) }}</p>
            <router-link v-if="billing" :to="billing.to" class="workspace-text-link">{{ t(billing.label) }}<Icon name="arrowRight" size="sm" /></router-link>
          </template>
          <template v-else>
            <span class="workspace-account-label">{{ t('workspace.activeKeys') }}</span>
            <strong class="workspace-balance">{{ stats?.active_api_keys ?? '—' }}</strong>
            <router-link to="/keys" class="workspace-text-link">{{ t('workspace.manage') }}<Icon name="arrowRight" size="sm" /></router-link>
          </template>
        </section>
      </div>

      <section class="workspace-endpoint" :aria-label="t('workspace.endpoint')">
        <div class="min-w-0 flex-1">
          <h2>{{ t('workspace.endpoint') }}</h2>
          <code>{{ endpoint }}</code>
          <p>{{ t('workspace.endpointHint') }}</p>
        </div>
        <button class="btn btn-secondary shrink-0" @click="copyToClipboard(endpoint)"><Icon :name="copied ? 'check' : 'copy'" size="sm" />{{ t(copied ? 'common.copied' : 'workspace.copyEndpoint') }}</button>
      </section>

      <div v-if="statsError" class="workspace-error" role="status">
        <p>{{ t('workspace.loadError') }}</p><button class="btn btn-secondary btn-sm" @click="loadStats">{{ t('workspace.retry') }}</button>
      </div>
      <dl class="workspace-metrics" :aria-busy="loading">
        <div><dt>{{ t('workspace.activeKeys') }}</dt><dd>{{ stats?.active_api_keys ?? '—' }}</dd><router-link to="/keys">{{ t('workspace.manage') }}<Icon name="arrowRight" size="xs" /></router-link></div>
        <div><dt>{{ t('workspace.todayRequests') }}</dt><dd>{{ stats?.today_requests.toLocaleString() ?? '—' }}</dd><router-link v-if="!authStore.isSimpleMode" to="/usage">{{ t('workspace.usage') }}<Icon name="arrowRight" size="xs" /></router-link></div>
        <div><dt>{{ t('workspace.todaySpend') }}</dt><dd>{{ stats ? money(stats.today_actual_cost, 4) : '—' }}</dd><span>{{ t('dashboard.actual') }}</span></div>
      </dl>

      <WorkspaceModels />

      <section class="workspace-analytics">
        <div class="workspace-section-heading">
          <div><h2>{{ t('workspace.analytics') }}</h2><p>{{ t('workspace.analyticsDescription') }}</p></div>
          <button class="btn btn-secondary" :aria-expanded="showAnalytics" aria-controls="workspace-analytics-content" @click="toggleAnalytics">
            {{ t(showAnalytics ? 'workspace.collapseAnalytics' : 'workspace.expandAnalytics') }}<Icon name="chevronDown" size="sm" :class="{ 'rotate-180': showAnalytics }" />
          </button>
        </div>
        <div v-if="showAnalytics" id="workspace-analytics-content" class="space-y-6">
          <UserDashboardStats v-if="stats" :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
          <div v-if="chartsError" class="workspace-error" role="status"><p>{{ t('workspace.usageError') }}</p><button class="btn btn-secondary btn-sm" @click="loadCharts">{{ t('workspace.retry') }}</button></div>
          <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="refreshAll" />
        </div>
      </section>
      <template v-if="!authStore.isSimpleMode">
        <div v-if="recentError" class="workspace-error" role="status"><p>{{ t('workspace.usageError') }}</p><button class="btn btn-secondary btn-sm" @click="loadRecent">{{ t('workspace.retry') }}</button></div>
        <UserDashboardRecentUsage v-else :data="recentUsage" :loading="loadingUsage" />
      </template>
    </div>
    <BaseDialog :show="showGuide" :title="t('workspace.guide')" width="wide" @close="showGuide = false"><ConnectionGuide /></BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import WorkspaceModels from '@/components/user/workspace/WorkspaceModels.vue'
import ConnectionGuide from '@/components/user/workspace/ConnectionGuide.vue'
import { useWorkspaceLinks } from '@/composables/useWorkspaceLinks'
import { useClipboard } from '@/composables/useClipboard'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { formatDateLocalInput } from '@/utils/format'

const { t } = useI18n()
const authStore = useAuthStore()
const { catalog, endpoint, billing } = useWorkspaceLinks()
const { copied, copyToClipboard } = useClipboard()
const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null)
const loading = ref(true)
const statsError = ref(false)
const chartsError = ref(false)
const recentError = ref(false)
const loadingUsage = ref(true)
const loadingCharts = ref(false)
const showGuide = ref(false)
const showAnalytics = ref(false)
const analyticsLoaded = ref(false)
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)
const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatDateLocalInput(new Date()))
const granularity = ref('day')
const money = (value: number, digits: number) => '$' + value.toFixed(digits)

async function loadStats() {
  loading.value = true
  statsError.value = false
  try {
    stats.value = await usageAPI.getDashboardStats()
  } catch {
    statsError.value = true
  } finally {
    loading.value = false
  }
}

async function loadCharts() {
  loadingCharts.value = true
  chartsError.value = false
  try {
    const params = { start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as 'day' | 'hour' }
    const [trend, models] = await Promise.all([usageAPI.getDashboardTrend(params), usageAPI.getDashboardModels(params)])
    trendData.value = trend.trend || []
    modelStats.value = models.models || []
  } catch {
    chartsError.value = true
  } finally {
    loadingCharts.value = false
  }
}

async function loadRecent() {
  loadingUsage.value = true
  recentError.value = false
  try {
    const result = await usageAPI.getByDateRange(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)), formatDateLocalInput(new Date()))
    recentUsage.value = result.items.slice(0, 5)
  } catch {
    recentError.value = true
  } finally {
    loadingUsage.value = false
  }
}

async function loadPlatformQuotas() {
  try {
    const data = await getMyPlatformQuotas()
    platformQuotas.value = data.platform_quotas ?? []
  } catch {
    platformQuotas.value = []
  }
}

function toggleAnalytics() {
  showAnalytics.value = !showAnalytics.value
  if (showAnalytics.value && !analyticsLoaded.value) {
    analyticsLoaded.value = true
    void loadCharts()
    void loadPlatformQuotas()
  }
}

function refreshAll() {
  void loadStats()
  if (!authStore.isSimpleMode) void loadRecent()
  if (showAnalytics.value) {
    void loadCharts()
    void loadPlatformQuotas()
  }
  void authStore.refreshUser().catch(() => {})
}

onMounted(refreshAll)
</script>
