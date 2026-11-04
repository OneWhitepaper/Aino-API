<template>
  <section id="iq-test-history" class="iq-panel iq-history" tabindex="-1">
    <div class="iq-section-top">
      <div>
        <span class="iq-overline">03 / COMPARE</span>
        <h2>
          {{ t('iqTest.historyTitle') }}<small>{{ t('iqTest.historyHint') }}</small>
        </h2>
      </div>
      <button type="button" class="iq-text-button" :disabled="loading" @click="$emit('refresh')">
        <Icon name="refresh" size="sm" :class="{ 'animate-spin motion-reduce:animate-none': loading }" />{{
          t('iqTest.historyRefresh')
        }}
      </button>
    </div>
    <p v-if="!items.length" class="iq-history-empty" role="status">
      {{ t(loading ? 'iqTest.historyLoading' : 'iqTest.historyEmpty') }}
    </p>
    <div v-else class="iq-history-scroll">
      <table>
        <caption class="sr-only">
          {{
            t('iqTest.historyTitle')
          }}
        </caption>
        <thead>
          <tr>
            <th scope="col">{{ t('iqTest.historyModel') }}</th>
            <th scope="col">{{ t('iqTest.historyResult') }}</th>
            <th scope="col">{{ t('iqTest.elapsed') }}</th>
            <th scope="col">{{ t('iqTest.historyTime') }}</th>
            <th scope="col">
              <span class="sr-only">{{ t('iqTest.historyOpen') }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="task in items" :key="task.id" :class="{ selected: task.id === selected }">
            <td>
              <span class="iq-history-task"
                ><span class="iq-history-icon"
                  ><Icon :name="task.config.kind === 'candy' ? 'brain' : 'terminal'" size="sm" /></span
                ><span
                  ><strong>{{ t('iqTest.' + task.config.kind) }}</strong
                  ><small>{{ task.config.model }} {{ task.config.effort }}</small></span
                ></span
              >
            </td>
            <td>
              <span class="iq-history-status" :class="'iq-history-' + resultLabel(task)">{{
                t(`iqTest.label_${resultLabel(task)}`)
              }}</span>
            </td>
            <td class="iq-nowrap">
              {{ (task.result.duration_ms / 1000).toFixed(1) }} {{ t('iqTest.seconds') }}
            </td>
            <td class="iq-nowrap iq-muted">{{ formatTime(task.created_at) }}</td>
            <td>
              <button
                type="button"
                class="iq-icon-button"
                :aria-label="
                  t('iqTest.historyOpen') + ' · ' + task.config.model + ' · ' + formatTime(task.created_at)
                "
                @click="$emit('open', task)"
              >
                <Icon name="chevronRight" size="sm" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <button
      v-if="hasMore"
      type="button"
      class="iq-button iq-load-more"
      :disabled="loading"
      @click="$emit('more')"
    >
      {{ t('iqTest.loadMore') }}
    </button>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IQTest } from '@/api/iqTest'
import { resultLabel } from './artifact'
defineProps<{ items: IQTest[]; selected?: string; loading: boolean; hasMore: boolean }>()
defineEmits<{ open: [task: IQTest]; refresh: []; more: [] }>()
const { t, locale } = useI18n()
const formatTime = (value: string) =>
  new Intl.DateTimeFormat(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(new Date(value))
</script>
