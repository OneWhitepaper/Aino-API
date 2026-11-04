<template>
  <div class="h-64 min-w-0 sm:h-72">
    <Line :data="chartData" :options="options" role="img" :aria-label="t('iqRadar.history')" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, LinearScale, PointElement, LineElement, Tooltip, Filler, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import type { RadarHistoryPoint } from '@/api/iqRadar'

ChartJS.register(LinearScale, PointElement, LineElement, Tooltip, Filler)
const props = defineProps<{ history: RadarHistoryPoint[]; scoreMax: number }>()
const { t, locale } = useI18n()
const dark = ref(false)
let observer: MutationObserver | undefined
onMounted(() => {
  const update = () => { dark.value = document.documentElement.classList.contains('dark') }
  update()
  observer = new MutationObserver(update)
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})
onBeforeUnmount(() => observer?.disconnect())

const dateLabel = (value: number) => new Intl.DateTimeFormat(locale.value, { month: '2-digit', day: '2-digit', timeZone: 'UTC' }).format(value)
const chartData = computed(() => ({
  datasets: [{
    label: t('iqRadar.score'),
    data: props.history.map(point => ({ x: Date.parse(point.date + 'T00:00:00Z'), y: point.score })),
    borderColor: '#14b8a6', backgroundColor: 'rgba(20, 184, 166, 0.08)',
    pointBackgroundColor: '#14b8a6', pointBorderColor: dark.value ? '#172033' : '#ffffff',
    pointBorderWidth: 2, pointRadius: 3, pointHoverRadius: 5, pointHitRadius: 12,
    borderWidth: 2, fill: 'origin' as const, tension: 0,
    spanGaps: 86400000,
  }],
}))
const options = computed<ChartOptions<'line'>>(() => ({
  responsive: true, maintainAspectRatio: false, animation: false, normalized: true,
  interaction: { mode: 'nearest', intersect: false },
  plugins: {
    legend: { display: false },
    tooltip: {
      displayColors: false,
      callbacks: {
        title: items => items[0]?.parsed.x != null ? new Date(items[0].parsed.x).toISOString().slice(0, 10) + ' UTC' : '',
        label: item => t('iqRadar.score') + ': ' + item.parsed.y?.toFixed(1),
      },
    },
  },
  scales: {
    x: { type: 'linear', grid: { display: false }, border: { display: false }, ticks: { maxTicksLimit: 6, color: dark.value ? '#94a3b8' : '#64748b', callback: value => dateLabel(Number(value)) } },
    y: { min: 0, max: props.scoreMax, border: { display: false }, grid: { color: dark.value ? 'rgba(148,163,184,0.12)' : '#f1f5f9' }, ticks: { color: dark.value ? '#94a3b8' : '#64748b', maxTicksLimit: 6, padding: 10 } },
  },
}))
</script>
