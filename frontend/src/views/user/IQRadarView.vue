<template>
  <AppLayout>
    <div class="radar-page mx-auto max-w-[1480px] space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-5">
        <div class="min-w-0">
          <div class="mb-3 inline-flex items-center gap-2 rounded-full border border-teal-200/70 bg-teal-50 px-3 py-1 text-xs font-semibold text-teal-700 dark:border-teal-800 dark:bg-teal-950/50 dark:text-teal-300">
            <Icon name="radar" size="sm" aria-hidden="true" /> INTELLIGENCE RADAR
          </div>
          <h1 class="text-2xl font-bold tracking-tight text-slate-900 sm:text-3xl dark:text-white">{{ t('iqRadar.title') }}</h1>
          <p class="mt-2 max-w-2xl text-sm leading-relaxed text-slate-500 dark:text-dark-400">{{ t('iqRadar.description') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2 sm:pt-2">
          <a href="https://codexradar.com/" target="_blank" rel="noopener noreferrer" class="radar-source">
            <span class="h-1.5 w-1.5 rounded-full bg-teal-500" aria-hidden="true" /> Codex Radar <Icon name="link" size="xs" aria-hidden="true" />
          </a>
          <button type="button" data-testid="refresh-radar" class="radar-refresh" :disabled="pending || snapshot?.refreshing" @click="load">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin motion-reduce:animate-none': pending || snapshot?.refreshing }" aria-hidden="true" />{{ t('iqRadar.refresh') }}
          </button>
        </div>
      </header>
      <div v-if="failed" role="alert" class="rounded-2xl border border-rose-200 bg-rose-50 px-5 py-4 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300">{{ t('iqRadar.loadFailed') }}</div>
      <div v-if="snapshot && snapshot.partial_failures > 0 && snapshot.points.length" role="status" class="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300">{{ t('iqRadar.stale') }}</div>
      <section v-if="!snapshot?.points.length" class="radar-card flex min-h-64 flex-col items-center justify-center px-6 py-12 text-center" role="status" :aria-busy="pending || snapshot?.refreshing">
        <span class="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-teal-50 text-teal-600 dark:bg-teal-950/40"><Icon name="radar" size="lg" :class="{ 'animate-pulse motion-reduce:animate-none': pending || snapshot?.refreshing }" aria-hidden="true" /></span>
        <h2 class="font-semibold text-slate-900 dark:text-white">{{ t('iqRadar.empty') }}</h2>
        <p class="mt-2 max-w-md text-sm leading-6 text-slate-500 dark:text-dark-400">{{ pending || snapshot?.refreshing ? t('iqRadar.emptyDescription') : t('iqRadar.unavailable') }}</p>
      </section>
      <template v-if="snapshot?.points.length">
        <section class="radar-card overflow-hidden">
          <div class="flex flex-wrap items-center justify-between gap-4 border-b border-slate-100 px-5 py-5 sm:px-6 dark:border-dark-700">
            <div><div class="mb-1 flex flex-wrap items-center gap-2"><h2 class="text-base font-bold text-slate-900 dark:text-white">{{ t('iqRadar.benchmarkTitle') }}</h2><span class="radar-badge">{{ t('iqRadar.scale', { max: snapshot.score_max }) }}</span></div><p class="text-xs leading-5 text-slate-500 dark:text-dark-400">{{ t('iqRadar.benchmarkDescription') }}</p></div>
            <label class="relative w-full sm:w-52"><span class="sr-only">{{ t('iqRadar.search') }}</span><Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-slate-400" aria-hidden="true" /><input v-model="search" type="search" :placeholder="t('iqRadar.search')" class="w-full rounded-xl border border-slate-200 bg-slate-50 py-2 pl-9 pr-3 text-sm text-slate-800 outline-none focus:border-teal-500 focus:ring-2 focus:ring-teal-500/15 dark:border-dark-600 dark:bg-dark-900 dark:text-slate-200" /></label>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-x-5 gap-y-2 px-5 py-3 text-xs sm:px-6">
            <div class="flex flex-wrap gap-x-5 gap-y-2 text-slate-500 dark:text-dark-400"><span><strong class="mr-1 text-slate-900 dark:text-white">{{ modelGroups.length }}</strong>{{ t('iqRadar.models') }}</span><span><strong class="mr-1 text-slate-900 dark:text-white">{{ snapshot.points.length }}</strong>{{ t('iqRadar.tiers') }}</span><span>{{ t('iqRadar.taskCount', { count: snapshot.required_tasks }) }}</span></div>
            <span v-if="best?.score != null" class="text-teal-700 dark:text-teal-300">{{ t('iqRadar.topScore') }} <strong class="ml-1 tabular-nums">{{ formatScore(best.score) }}</strong></span>
          </div>
          <div class="hidden px-4 pb-4 xl:block">
            <table class="w-full table-fixed border-separate border-spacing-1.5 text-left">
              <caption class="sr-only">{{ t('iqRadar.benchmarkTitle') }}</caption>
              <thead><tr><th scope="col" class="w-[24%] px-3 py-2 text-xs font-medium text-slate-400">{{ t('iqRadar.model') }}</th><th v-for="effort in efforts" :key="effort" scope="col" class="px-1 py-2 text-center text-xs font-medium text-slate-500 dark:text-dark-400">{{ effort }}</th></tr></thead>
              <tbody><tr v-for="group in filteredGroups" :key="group.model"><th scope="row" class="px-3 py-3"><span class="block break-words text-sm font-semibold text-slate-800 dark:text-slate-100">{{ group.label }}</span><span class="mt-1 block text-[11px] font-normal text-slate-400">{{ group.model }}</span></th><td v-for="effort in efforts" :key="effort" class="py-0.5">
                <button v-if="pointFor(group.points, effort)" type="button" :data-testid="'radar-' + group.model + '-' + effort" class="radar-cell w-full" :class="cellClass(pointFor(group.points, effort)!)" :aria-pressed="isSelected(pointFor(group.points, effort)!)" :aria-label="selectionLabel(pointFor(group.points, effort)!)" @click="select(pointFor(group.points, effort)!)"><span class="text-base font-bold tabular-nums">{{ formatScore(pointFor(group.points, effort)!.score) }}</span><span v-if="pointFor(group.points, effort)!.stale" class="absolute right-2 top-2 h-1.5 w-1.5 rounded-full bg-amber-500" :title="t(pointFor(group.points, effort)!.fetched_at ? 'iqRadar.stalePoint' : 'iqRadar.noScore')" /></button>
                <span v-else class="block py-4 text-center text-sm text-slate-300 dark:text-dark-600" :title="t('iqRadar.noTier')">—</span>
              </td></tr></tbody>
            </table>
          </div>
          <div class="grid gap-3 px-4 pb-4 sm:grid-cols-2 xl:hidden">
            <article v-for="group in filteredGroups" :key="group.model" class="min-w-0 rounded-2xl border border-slate-100 p-4 dark:border-dark-700"><h3 class="mb-3 break-words text-sm font-semibold text-slate-800 dark:text-slate-100">{{ group.label }}</h3><div class="grid grid-cols-3 gap-2"><button v-for="point in group.points" :key="point.effort" type="button" :data-testid="'radar-' + group.model + '-' + point.effort" class="radar-cell" :class="cellClass(point)" :aria-pressed="isSelected(point)" :aria-label="selectionLabel(point)" @click="select(point)"><span class="block text-[10px] opacity-75">{{ point.effort }}</span><span class="mt-0.5 block text-base font-bold tabular-nums">{{ formatScore(point.score) }}</span><span v-if="point.stale" class="absolute right-1.5 top-1.5 h-1.5 w-1.5 rounded-full bg-amber-500" :title="t(point.fetched_at ? 'iqRadar.stalePoint' : 'iqRadar.noScore')" /></button></div></article>
          </div>
          <p v-if="!filteredGroups.length" class="px-6 py-10 text-center text-sm text-slate-500">{{ t('iqRadar.noResults') }}</p>
        </section>
        <div v-if="selected" class="grid min-w-0 gap-6 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.4fr)]">
          <section class="radar-card min-w-0 p-5 sm:p-6" aria-labelledby="radar-selected-title">
            <div class="mb-4 flex flex-wrap items-center justify-between gap-2"><span class="radar-eyebrow">{{ t('iqRadar.selected') }}</span><span v-if="selected.stale && selectedHasData" class="text-xs text-amber-600 dark:text-amber-400">{{ t('iqRadar.stalePoint') }}</span></div>
            <h2 id="radar-selected-title" class="break-words text-xl font-bold text-slate-900 dark:text-white">{{ modelLabel(selected.model) }} <span class="ml-1 text-base font-medium text-teal-600 dark:text-teal-300">{{ selected.effort }}</span></h2>
            <div class="mt-5 rounded-2xl bg-slate-900 p-5 text-white dark:bg-dark-950">
              <div class="flex flex-wrap items-end justify-between gap-3"><div><p class="text-xs text-slate-400">{{ t('iqRadar.score') }}</p><p data-testid="selected-score" class="mt-2 text-5xl font-semibold tracking-tight tabular-nums">{{ formatScore(selected.score) }}<span class="ml-2 text-base font-normal text-slate-400">/ {{ snapshot.score_max }}</span></p></div><span class="rounded-full bg-white/10 px-2.5 py-1 text-[11px] text-teal-200">{{ selected.score == null ? t('iqRadar.noScore') : selected.score_status === 'complete' ? t('iqRadar.complete') : t('iqRadar.incomplete') }}</span></div>
              <p v-if="selectedHasData" class="mt-4 text-xs text-slate-400">{{ t('iqRadar.coverage', { count: selected.coverage, total: selected.required_tasks }) }}<span class="mx-2">·</span>{{ t('iqRadar.events', { count: selected.valid_grade_events }) }}</p>
            </div>
            <p v-if="!selectedHasData" class="mt-4 rounded-2xl bg-amber-50 p-4 text-sm leading-6 text-amber-800 dark:bg-amber-950/30 dark:text-amber-300">{{ t('iqRadar.unavailable') }}</p>
            <dl v-if="selectedHasData" class="mt-4 grid grid-cols-2 gap-3"><div class="rounded-2xl bg-slate-50 p-3.5 dark:bg-dark-900"><dt class="text-xs text-slate-500 dark:text-dark-400">{{ t('iqRadar.cost') }}</dt><dd data-testid="selected-cost" class="mt-2 text-xl font-semibold tabular-nums text-slate-900 dark:text-white">{{ formatPrice(selected.average_price_usd) }}</dd><dd class="mt-1 text-[11px] leading-5 text-slate-400">{{ t('iqRadar.costNote', { count: selected.price_samples }) }}</dd></div><div class="rounded-2xl bg-slate-50 p-3.5 dark:bg-dark-900"><dt class="text-xs text-slate-500 dark:text-dark-400">{{ t('iqRadar.duration') }}</dt><dd class="mt-2 text-xl font-semibold tabular-nums text-slate-900 dark:text-white">{{ selected.average_minutes == null ? '—' : t('iqRadar.minutes', { value: formatNumber(selected.average_minutes) }) }}</dd><dd class="mt-1 text-[11px] leading-5 text-slate-400">{{ t('iqRadar.durationNote', { count: selected.duration_samples }) }}</dd></div></dl>
            <dl v-if="selectedHasData" class="mt-4 space-y-2 text-xs"><div class="flex flex-wrap justify-between gap-2"><dt class="text-slate-400">{{ t('iqRadar.lastGraded') }}</dt><dd class="text-slate-600 dark:text-dark-300">{{ formatTime(selected.latest_graded_at) }}</dd></div><div class="flex flex-wrap justify-between gap-2"><dt class="text-slate-400">{{ t('iqRadar.pointFetched') }}</dt><dd class="text-slate-600 dark:text-dark-300">{{ formatTime(selected.fetched_at) }}</dd></div></dl>
          </section>
          <section class="radar-card min-w-0 p-5 sm:p-6">
            <div class="mb-5"><span class="radar-eyebrow">HISTORY · UTC</span><h2 class="mt-2 text-base font-bold text-slate-900 dark:text-white">{{ t('iqRadar.history') }}</h2><p class="mt-1 text-xs leading-5 text-slate-500 dark:text-dark-400">{{ t('iqRadar.historyNote') }}</p></div>
            <RadarTrendChart v-if="selected.history.length" :history="selected.history" :score-max="snapshot.score_max" />
            <div v-else class="flex h-64 items-center justify-center text-sm text-slate-400">{{ t('iqRadar.noHistory') }}</div>
            <details v-if="selected.history.length" class="mt-4 border-t border-slate-100 pt-3 dark:border-dark-700"><summary class="cursor-pointer text-xs font-medium text-slate-500 hover:text-teal-600 dark:text-dark-300">{{ t('iqRadar.historyTable') }}</summary><div class="mt-3 max-h-48 overflow-y-auto"><table class="w-full text-left text-xs"><thead><tr class="text-slate-400"><th scope="col" class="pb-2 font-medium">{{ t('iqRadar.date') }}</th><th scope="col" class="pb-2 text-right font-medium">{{ t('iqRadar.score') }}</th></tr></thead><tbody><tr v-for="day in selected.history" :key="day.date" class="border-t border-slate-50 text-slate-600 dark:border-dark-700 dark:text-dark-300"><td class="py-2">{{ day.date }}</td><td class="py-2 text-right tabular-nums">{{ formatScore(day.score) }}</td></tr></tbody></table></div></details>
          </section>
        </div>
      </template>
      <section v-if="snapshot" class="radar-card p-5 sm:p-6">
        <div class="mb-5 flex flex-wrap items-start justify-between gap-3"><div><span class="radar-eyebrow">{{ t('iqRadar.community') }}</span><h2 class="mt-2 text-lg font-bold text-slate-900 dark:text-white">{{ t('iqRadar.communityTitle') }}</h2><p class="mt-1 text-xs leading-5 text-slate-500 dark:text-dark-400">{{ t('iqRadar.communityDescription') }}</p></div><span class="radar-badge">24H · / 10</span></div>
        <p v-if="snapshot.community.stale" class="mb-4 text-xs text-amber-600 dark:text-amber-400">{{ t(snapshot.community.fetched_at ? 'iqRadar.stalePoint' : 'iqRadar.communityEmpty') }}</p>
        <div v-if="communityGroups.length" class="grid gap-4 md:grid-cols-2 2xl:grid-cols-3"><article v-for="group in communityGroups" :key="group.name" class="min-w-0 rounded-2xl border border-slate-100 p-4 dark:border-dark-700"><div class="mb-4 flex flex-wrap items-center justify-between gap-2"><h3 class="break-words text-sm font-semibold text-slate-800 dark:text-slate-100">{{ group.name }}</h3><span class="text-[11px] text-slate-400">{{ t('iqRadar.votes', { count: group.votes }) }}</span></div><dl class="space-y-3"><div v-for="rating in group.models" :key="rating.id" class="grid grid-cols-[3rem_minmax(0,1fr)_2rem_3.5rem] items-center gap-2 text-xs"><dt class="text-slate-500 dark:text-dark-400">{{ communityEffort(rating) }}</dt><dd class="h-1.5 overflow-hidden rounded-full bg-slate-100 dark:bg-dark-700" aria-hidden="true"><span class="block h-full rounded-full bg-teal-500" :style="{ width: (rating.average == null ? 0 : rating.average * 10) + '%' }" /></dd><dd class="text-right font-semibold tabular-nums text-slate-700 dark:text-slate-200">{{ formatScore(rating.average) }}</dd><dd class="text-right text-[10px] text-slate-400">{{ rating.count ? t('iqRadar.votes', { count: rating.count }) : t('iqRadar.unrated') }}</dd></div></dl></article></div>
        <p v-else class="py-7 text-center text-sm text-slate-400">{{ t('iqRadar.communityEmpty') }}</p>
        <p class="mt-5 text-[11px] text-slate-400">{{ t('iqRadar.communityUpdated') }} · {{ formatTime(snapshot.community.updated_at) }}</p>
      </section>
      <footer class="space-y-2 px-1 pb-4 text-[11px] leading-5 text-slate-400 dark:text-dark-500"><p role="status" aria-live="polite">{{ snapshot?.refreshing ? t('iqRadar.syncing') : t('iqRadar.synced') + ' · ' + formatTime(snapshot?.fetched_at ?? null) }}<span class="mx-2">/</span>{{ t('iqRadar.cacheNote') }}</p><p>{{ t('iqRadar.disclaimer') }}</p></footer>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import RadarTrendChart from '@/features/iq-radar/RadarTrendChart.vue'
import { communityEffort, EFFORTS, groupCommunityModels, groupRadarModels, modelLabel, selectRadarPoint } from '@/features/iq-radar/radarData'
import { getIQRadar, type RadarPoint, type RadarSnapshot } from '@/api/iqRadar'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const snapshot = ref<RadarSnapshot | null>(null)
const pending = ref(false)
const failed = ref(false)
const search = ref('')
const modelGroups = computed(() => groupRadarModels(snapshot.value?.points ?? []))
const filteredGroups = computed(() => modelGroups.value.filter(group => (group.label + ' ' + group.model).toLowerCase().includes(search.value.trim().toLowerCase())))
const communityGroups = computed(() => groupCommunityModels(snapshot.value?.community.models ?? []))
const efforts = computed(() => [...EFFORTS, ...new Set((snapshot.value?.points ?? []).map(point => point.effort).filter(effort => !EFFORTS.includes(effort)))])
const selected = computed(() => selectRadarPoint(snapshot.value?.points ?? [], typeof route.query.model === 'string' ? route.query.model : undefined, typeof route.query.effort === 'string' ? route.query.effort : undefined))
const selectedHasData = computed(() => selected.value?.fetched_at != null && selected.value.score_status !== 'unavailable')
const best = computed(() => selectRadarPoint(snapshot.value?.points ?? []))
const formatScore = (value: number | null) => value == null ? '—' : value.toFixed(1)
const formatNumber = (value: number) => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(value)
const formatPrice = (value: number | null) => value == null ? '—' : new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', minimumFractionDigits: 1, maximumFractionDigits: 2 }).format(value)
const formatTime = (value: string | null) => value ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '—'
const pointFor = (points: RadarPoint[], effort: string) => points.find(point => point.effort === effort)
const isSelected = (point: RadarPoint) => point.model === selected.value?.model && point.effort === selected.value?.effort
const selectionLabel = (point: RadarPoint) => t('iqRadar.select', { model: modelLabel(point.model), effort: point.effort, score: point.score == null ? t('iqRadar.noScore') : formatScore(point.score) })
function select(point: RadarPoint) { void router.replace({ query: { ...route.query, model: point.model, effort: point.effort } }) }
function cellClass(point: RadarPoint): string {
  if (isSelected(point)) return 'border-slate-900 bg-slate-900 text-white shadow-sm dark:border-teal-400 dark:bg-teal-500/15 dark:text-teal-200'
  if (point.score == null) return 'border-transparent bg-slate-50 text-slate-400 dark:bg-dark-900'
  const ratio = point.score / (snapshot.value?.score_max || 150)
  if (ratio >= 0.65) return 'border-transparent bg-teal-50 text-teal-800 hover:border-teal-300 dark:bg-teal-900/20 dark:text-teal-300'
  if (ratio >= 0.45) return 'border-transparent bg-sky-50 text-sky-800 hover:border-sky-300 dark:bg-sky-900/20 dark:text-sky-300'
  return 'border-transparent bg-slate-100 text-slate-600 hover:border-slate-300 dark:bg-dark-700 dark:text-slate-300'
}

let active = true
let timer: ReturnType<typeof setTimeout> | undefined
let controller: AbortController | undefined
async function load() {
  if (!active || pending.value) return
  clearTimeout(timer)
  pending.value = true
  controller = new AbortController()
  try {
    const data = await getIQRadar(controller.signal)
    if (active) { snapshot.value = data; failed.value = false }
  } catch {
    if (active && !controller.signal.aborted) failed.value = true
  } finally {
    if (active) {
      pending.value = false
      if (!document.hidden) timer = setTimeout(load, snapshot.value?.refreshing ? 5000 : failed.value ? 30000 : 60000)
    }
  }
}
function onVisibilityChange() { clearTimeout(timer); if (!document.hidden) void load() }
onMounted(() => { document.addEventListener('visibilitychange', onVisibilityChange); void load() })
onBeforeUnmount(() => { active = false; clearTimeout(timer); controller?.abort(); document.removeEventListener('visibilitychange', onVisibilityChange) })
</script>

<style scoped>
.radar-card { @apply rounded-3xl border border-slate-200/70 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800; }
.radar-source { @apply inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-medium text-slate-600 transition hover:border-teal-300 hover:text-teal-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300; }
.radar-refresh { @apply inline-flex items-center gap-2 rounded-xl bg-slate-900 px-4 py-2.5 text-xs font-semibold text-white transition hover:bg-slate-700 disabled:cursor-wait disabled:opacity-50 dark:bg-teal-500 dark:text-slate-950 dark:hover:bg-teal-400; }
.radar-badge { @apply inline-flex rounded-full bg-slate-100 px-2.5 py-1 text-[10px] font-semibold text-slate-500 dark:bg-dark-700 dark:text-dark-300; }
.radar-eyebrow { @apply text-[10px] font-bold uppercase tracking-[0.15em] text-teal-600 dark:text-teal-400; }
.radar-cell { @apply relative min-h-14 rounded-xl border px-2 py-2.5 text-center transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-teal-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800; }
</style>
