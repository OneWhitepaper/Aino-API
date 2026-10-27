<template>
  <section class="iq-panel iq-result" :aria-busy="task?.status === 'running'">
    <div class="iq-section-top">
      <div>
        <span class="iq-overline">02 / OBSERVE</span>
        <h2>{{ t('iqTest.resultTitle') }}</h2>
      </div>
      <span v-if="task" class="iq-task-id"># {{ task.id.slice(0, 8) }}</span>
    </div>
    <div v-if="!task" class="iq-empty">
      <div class="iq-orbit" aria-hidden="true"><span /><Icon name="brain" /></div>
      <span class="iq-overline">READY WHEN YOU ARE</span>
      <h3>{{ t('iqTest.emptyTitle') }}</h3>
      <p>{{ t('iqTest.emptyDescription') }}</p>
      <div class="iq-empty-steps">{{ t('iqTest.emptySteps') }}</div>
      <small><Icon name="lock" size="xs" />{{ t('iqTest.emptyFooter') }}</small>
    </div>
    <template v-else>
      <div class="iq-verdict" :class="'iq-verdict-' + label" role="status" aria-live="polite">
        <span class="iq-verdict-icon"
          ><Icon
            :name="label === 'passed' ? 'check' : task.status === 'running' ? 'refresh' : 'brain'"
            :class="{ 'animate-spin motion-reduce:animate-none': task.status === 'running' }"
        /></span>
        <div>
          <span class="iq-overline">{{ t('iqTest.singleResult') }}</span>
          <h3>{{ t(`iqTest.verdict_${label}`) }}</h3>
        </div>
        <span class="iq-state-dot">{{ t(`iqTest.label_${label}`) }}</span>
      </div>
      <div class="iq-model-line">
        <span
          ><Icon name="radar" size="sm" /><strong>{{ task.config.model }}</strong
          ><code v-if="task.config.effort">{{ task.config.effort }}</code></span
        ><small>{{ t('iqTest.' + task.config.source) }}</small>
      </div>
      <dl class="iq-metrics">
        <div>
          <dt>{{ t('iqTest.elapsed') }}</dt>
          <dd>
            {{ (task.result.duration_ms / 1000).toFixed(1) }}<small>{{ t('iqTest.seconds') }}</small>
          </dd>
        </div>
        <div :title="t('iqTest.speedNote')">
          <dt>{{ t('iqTest.speed') }}</dt>
          <dd>{{ speed == null ? '—' : speed.toFixed(1) }}<small>tokens/s</small></dd>
          <small>{{ t('iqTest.speedNote') }}</small>
        </div>
        <div>
          <dt>{{ t('iqTest.tokens') }}</dt>
          <dd>{{ metricNumber(totalTokens) }}</dd>
        </div>
        <div>
          <dt>{{ t('iqTest.referenceCost') }}</dt>
          <dd>{{ task.reference_cost_usd == null ? '—' : '$' + task.reference_cost_usd.toFixed(4) }}</dd>
        </div>
      </dl>
      <p v-if="task.error_code" class="iq-error" role="status">
        {{ t(`iqTest.failure_${task.error_code}`) }}
      </p>
      <div class="iq-result-toolbar">
        <div role="tablist" :aria-label="t('iqTest.resultTitle')">
          <button
            v-for="item in tabs"
            :id="'iq-tab-' + item"
            :key="item"
            type="button"
            role="tab"
            :aria-selected="tab === item"
            :aria-controls="'iq-panel-' + item"
            :tabindex="tab === item ? 0 : -1"
            :class="{ active: tab === item }"
            @click="tab = item"
            @keydown.right.prevent="moveTab(1)"
            @keydown.left.prevent="moveTab(-1)"
          >
            {{ t('iqTest.' + item) }}
          </button>
        </div>
        <button v-if="task.result.output" type="button" class="iq-text-button" @click="copy">
          <Icon name="copy" size="sm" />{{ t(copied ? 'iqTest.copied' : 'iqTest.copy') }}
        </button>
      </div>
      <div
        :id="'iq-panel-' + tab"
        class="iq-result-body"
        role="tabpanel"
        :aria-labelledby="'iq-tab-' + tab"
        tabindex="0"
      >
        <template v-if="tab === 'answer'">
          <template v-if="task.config.kind === 'pelican' && task.status === 'completed'">
            <div class="iq-artifact-toolbar">
              <div class="iq-segment">
                <button type="button" :aria-pressed="!showCode" @click="showCode = false">
                  {{ t('iqTest.preview') }}</button
                ><button type="button" :aria-pressed="showCode" @click="showCode = true">
                  {{ t('iqTest.code') }}
                </button>
              </div>
              <button v-if="preview" type="button" class="iq-text-button" @click="download">
                <Icon name="download" size="xs" />{{ t('iqTest.download') }}
              </button>
            </div>
            <iframe
              v-if="preview && !showCode"
              :title="t('iqTest.preview')"
              :srcdoc="preview"
              sandbox=""
              referrerpolicy="no-referrer"
              class="iq-artifact"
            />
            <pre v-else-if="showCode" class="iq-output iq-output-code">{{ task.result.output }}</pre>
            <p v-else class="iq-note">{{ t('iqTest.noArtifact') }}</p>
            <p class="iq-note iq-preview-note">{{ t('iqTest.previewNote') }}</p>
          </template>
          <template v-else
            ><p v-if="finalAnswer && task.status === 'completed'" class="iq-final-number">
              {{ finalAnswer }}
            </p>
            <pre class="iq-output">{{ task.result.output || t('iqTest.waiting') }}</pre>
          </template>
        </template>
        <div v-else-if="tab === 'basis'" class="iq-explanation">
          <Icon name="infoCircle" />
          <p>{{ t(task.basis ? 'iqTest.basis_' + task.basis : 'iqTest.noBasis') }}</p>
        </div>
        <template v-else
          ><dl class="iq-usage">
            <div v-for="item in usageRows" :key="item.key">
              <dt>{{ t('iqTest.' + item.key) }}</dt>
              <dd>{{ metricNumber(item.value) }}</dd>
            </div>
            <div>
              <dt>{{ t('iqTest.actualTier') }}</dt>
              <dd>{{ task.result.service_tier || '—' }}</dd>
            </div>
          </dl>
          <p class="iq-note">{{ t('iqTest.costNote') }}</p>
          <RouterLink v-if="task.config.source === 'site'" to="/usage" class="iq-text-button"
            >{{ t('iqTest.usageLink') }}<Icon name="arrowRight" size="xs" /></RouterLink
        ></template>
      </div>
      <p v-if="copyError" class="iq-note" role="status">{{ t('iqTest.copyFailed') }}</p>
      <div class="iq-result-footer">
        <span><Icon name="lock" size="xs" />{{ t('iqTest.private') }}</span
        ><button
          v-if="task.status === 'running'"
          type="button"
          class="iq-text-button"
          :disabled="cancelling"
          @click="$emit('cancel')"
        >
          {{ t(cancelling ? 'iqTest.cancelling' : 'iqTest.cancel') }}</button
        ><button v-else type="button" class="iq-text-button" :disabled="busy" @click="$emit('rerun', task)">
          {{ t('iqTest.rerun') }}<Icon name="refresh" size="sm" />
        </button>
      </div>
      <p v-if="task.status === 'running' || task.status === 'cancelled'" class="iq-note">
        {{ t('iqTest.cancelNote') }}
      </p>
      <p class="iq-disclaimer"><Icon name="infoCircle" size="sm" />{{ t('iqTest.disclaimer') }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IQTest } from '@/api/iqTest'
import { artifactDocument, metricNumber, resultLabel, tokenSpeed } from './artifact'
const props = defineProps<{ task: IQTest | null; cancelling: boolean; busy: boolean }>()
defineEmits<{ cancel: []; rerun: [task: IQTest] }>()
const { t } = useI18n()
const tabs = ['answer', 'basis', 'usage'] as const
const tab = ref<(typeof tabs)[number]>('answer')
const showCode = ref(false)
const copied = ref(false)
const copyError = ref(false)
watch(
  () => props.task?.id,
  () => {
    tab.value = 'answer'
    showCode.value = false
    copied.value = false
    copyError.value = false
  }
)
const label = computed(() => (props.task ? resultLabel(props.task) : ''))
const speed = computed(() =>
  tokenSpeed(props.task?.result.usage?.output_tokens, props.task?.result.duration_ms ?? 0)
)
const totalTokens = computed(() =>
  props.task?.result.usage
    ? props.task.result.usage.input_tokens + props.task.result.usage.output_tokens
    : null
)
const finalAnswer = computed(() =>
  props.task?.config.kind === 'candy'
    ? props.task.result.output
        .trim()
        .split('\n')
        .at(-1)
        ?.match(/^(?:(?:最终答案|final answer)\s*[:：]\s*)?(\d+)\s*[。.]?$/i)?.[1]
    : null
)
const preview = computed(() =>
  props.task?.config.kind === 'pelican' && props.task.status === 'completed'
    ? artifactDocument(props.task.result.output)
    : ''
)
const usageRows = computed(() => [
  { key: 'inputTokens', value: props.task?.result.usage?.input_tokens },
  { key: 'outputTokens', value: props.task?.result.usage?.output_tokens },
  { key: 'cachedTokens', value: props.task?.result.usage?.cached_tokens },
  { key: 'reasoningTokens', value: props.task?.result.usage?.reasoning_tokens }
])
async function moveTab(delta: number) {
  tab.value = tabs[(tabs.indexOf(tab.value) + delta + tabs.length) % tabs.length]
  await nextTick()
  document.getElementById('iq-tab-' + tab.value)?.focus()
}
async function copy() {
  try {
    await navigator.clipboard.writeText(props.task?.result.output ?? '')
    copied.value = true
    copyError.value = false
  } catch {
    copyError.value = true
  }
}
function download() {
  if (!preview.value) return
  const url = URL.createObjectURL(new Blob([preview.value], { type: 'text/html;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = 'aino-pelican-' + props.task?.id.slice(0, 8) + '.html'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
</script>
