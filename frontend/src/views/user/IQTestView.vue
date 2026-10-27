<template>
  <AppLayout>
    <div class="iq-lab">
      <div class="iq-breadcrumb">
        {{ t('iqTest.tools') }}<span>/</span><strong>{{ t('iqTest.title') }}</strong>
      </div>
      <header class="iq-page-heading">
        <div>
          <div class="iq-kicker"><span />MODEL INTELLIGENCE LAB</div>
          <h1>{{ t('iqTest.heading') }}</h1>
          <p>{{ t('iqTest.description') }}</p>
        </div>
        <button type="button" class="iq-button" @click="showHistory">
          <Icon name="clock" size="sm" />{{ t('iqTest.historyLink') }}<Icon name="arrowRight" size="sm" />
        </button>
      </header>
      <div v-if="error" class="iq-error iq-page-error" role="alert">
        {{ t(`iqTest.error_${error}`)
        }}<button
          v-if="error !== 'start' && error !== 'cancel'"
          type="button"
          class="iq-text-button"
          @click="retry"
        >
          {{ t('iqTest.retry') }}
        </button>
      </div>
      <p v-if="pollError" class="iq-note" role="status">{{ t('iqTest.pollError') }}</p>
      <button
        v-if="runningID && current?.id !== runningID"
        type="button"
        class="iq-button iq-return"
        @click="openRunning"
      >
        {{ t('iqTest.returnRunning') }}<Icon name="arrowRight" size="sm" />
      </button>
      <div class="iq-detection-layout">
        <form class="iq-panel iq-configuration" @submit.prevent="run">
          <div class="iq-section-top">
            <div>
              <span class="iq-overline">01 / CONFIGURE</span>
              <h2>{{ t('iqTest.configureTitle') }}</h2>
            </div>
            <Icon name="cog" size="sm" class="iq-muted" />
          </div>
          <fieldset :disabled="starting || Boolean(runningID)">
            <legend class="iq-field-label">{{ t('iqTest.testKind') }}</legend>
            <div class="iq-task-choices">
              <button
                v-for="kind in kinds"
                :key="kind"
                type="button"
                class="iq-task-choice"
                :class="{ active: form.kind === kind }"
                :aria-pressed="form.kind === kind"
                @click="form.kind = kind"
              >
                <Icon :name="kind === 'candy' ? 'brain' : 'terminal'" class="iq-choice-icon" /><span
                  v-if="form.kind === kind"
                  class="iq-choice-check"
                  ><Icon name="check" size="xs" /></span
                ><strong>{{ t('iqTest.' + kind) }}</strong
                ><small>{{ t('iqTest.' + kind + 'Caption') }}</small>
              </button>
            </div>
            <p class="iq-question-hint">
              {{ t('iqTest.' + form.kind + 'Hint') }}
              <button
                type="button"
                class="iq-text-button"
                data-testid="view-prompt"
                @click="promptOpen = true"
              >
                {{ t('iqTest.viewPrompt') }}<Icon name="arrowRight" size="xs" />
              </button>
            </p>
            <div class="iq-divider" />
            <div class="iq-field-label">{{ t('iqTest.source') }}</div>
            <div class="iq-segment iq-source-switch">
              <button
                v-for="source in sources"
                :key="source"
                type="button"
                :data-testid="'source-' + source"
                :aria-pressed="form.source === source"
                @click="form.source = source"
              >
                {{ t('iqTest.' + source) }}
              </button>
            </div>
            <template v-if="form.source === 'site'">
              <label for="iq-key" class="iq-label"
                >{{ t('iqTest.key') }}<small>{{ t('iqTest.keyNameOnly') }}</small></label
              >
              <select id="iq-key" v-model="form.keyID" class="iq-input" :disabled="loading">
                <option :value="0">{{ t('iqTest.keyPlaceholder') }}</option>
                <option v-for="key in keys" :key="key.id" :value="key.id">
                  {{ key.name }} · {{ key.masked }}
                </option>
              </select>
              <p v-if="!loading && !keys.length" class="iq-note">
                {{ t('iqTest.noKeys') }}
                <RouterLink to="/keys" class="iq-text-button">{{ t('iqTest.manageKeys') }}</RouterLink>
              </p>
            </template>
            <template v-else>
              <label for="iq-base-url" class="iq-label">{{ t('iqTest.baseUrl') }}</label
              ><input
                id="iq-base-url"
                v-model="form.baseURL"
                type="url"
                class="iq-input"
                placeholder="https://api.example.com/v1"
                maxlength="2048"
                autocomplete="off"
                spellcheck="false"
                @change="discoverIfReady"
              />
              <label for="iq-custom-key" class="iq-label">{{ t('iqTest.apiKey') }}</label
              ><input
                id="iq-custom-key"
                v-model="form.apiKey"
                data-testid="custom-api-key"
                type="password"
                class="iq-input"
                placeholder="sk-…"
                maxlength="8192"
                autocomplete="off"
                spellcheck="false"
                @change="discoverIfReady"
              />
              <p class="iq-key-private"><Icon name="lock" size="xs" />{{ t('iqTest.keyPrivate') }}</p>
              <p class="iq-note">{{ t('iqTest.connectionHint') }}</p>
            </template>
            <label for="iq-model" class="iq-label"
              >{{ t('iqTest.model')
              }}<button
                type="button"
                class="iq-text-button"
                :disabled="!connectionReady || discovering"
                @click="discoverIfReady"
              >
                <Icon
                  name="refresh"
                  size="xs"
                  :class="{ 'animate-spin motion-reduce:animate-none': discovering }"
                />{{ t(discovering ? 'iqTest.discovering' : 'iqTest.discover') }}
              </button></label
            >
            <select v-if="models.length && !manualModel" id="iq-model" v-model="form.model" class="iq-input">
              <option v-for="model in models" :key="model.id" :value="model.id">
                {{ model.id }}
              </option></select
            ><input
              v-else
              id="iq-model"
              v-model="form.model"
              type="text"
              class="iq-input"
              :placeholder="t('iqTest.modelPlaceholder')"
              maxlength="200"
              autocomplete="off"
              spellcheck="false"
            />
            <div class="iq-model-hint">
              <span v-if="models.length">• {{ t('iqTest.modelCount', { count: models.length }) }}</span
              ><button v-if="models.length" type="button" class="iq-text-button" @click="toggleModelInput">
                {{ t(manualModel ? 'iqTest.chooseModel' : 'iqTest.manual') }}
              </button>
            </div>
            <p v-if="modelsError" class="iq-note" role="status">{{ t('iqTest.modelsFailed') }}</p>
            <div class="iq-label">{{ t('iqTest.effort') }}</div>
            <div class="iq-efforts">
              <button
                v-for="effort in ['', ...availableEfforts]"
                :key="effort"
                type="button"
                :data-testid="'effort-' + (effort || 'default')"
                :aria-pressed="form.effort === effort"
                @click="form.effort = effort"
              >
                {{ effort || t('iqTest.autoEffort') }}
              </button>
            </div>
            <p v-if="!availableEfforts.length" class="iq-note">
              {{ t(form.effort ? 'iqTest.explicitEffort' : 'iqTest.unknownEffort') }}
            </p>
            <details class="iq-advanced">
              <summary>{{ t('iqTest.advanced') }}<Icon name="chevronDown" size="xs" /></summary>
              <div class="iq-advanced-grid">
                <label
                  >{{ t('iqTest.tier')
                  }}<select v-model="form.tier" class="iq-input">
                    <option value="">{{ t('iqTest.defaultTier') }}</option>
                    <option value="priority">{{ t('iqTest.priorityTier') }}</option>
                  </select></label
                ><label
                  >{{ t('iqTest.delivery')
                  }}<select v-model="form.stream" class="iq-input">
                    <option :value="true">{{ t('iqTest.stream') }}</option>
                    <option :value="false">{{ t('iqTest.complete') }}</option>
                  </select></label
                ><label v-if="!availableEfforts.length" class="iq-advanced-full"
                  >{{ t('iqTest.manualEffort')
                  }}<select v-model="form.effort" class="iq-input">
                    <option value="">{{ t('iqTest.autoEffort') }}</option>
                    <option v-for="effort in allEfforts" :key="effort" :value="effort">{{ effort }}</option>
                  </select></label
                >
              </div>
            </details>
          </fieldset>
          <div class="iq-run-footer">
            <button
              type="submit"
              data-testid="start-iq-test"
              class="iq-button iq-button-primary"
              :disabled="!canStart"
            >
              <Icon
                :name="starting || runningID ? 'refresh' : 'play'"
                size="sm"
                :class="{ 'animate-spin motion-reduce:animate-none': starting || runningID }"
              /><span>{{
                t(starting ? 'iqTest.starting' : runningID ? 'iqTest.running' : 'iqTest.start')
              }}</span
              ><Icon name="arrowRight" size="sm" />
            </button>
            <p>
              <Icon name="infoCircle" size="xs" />{{
                t(form.source === 'site' ? 'iqTest.feeSite' : 'iqTest.feeCustom')
              }}
            </p>
            <p>{{ t('iqTest.limits') }}</p>
            <button
              v-if="runningID && current?.id !== runningID"
              type="button"
              class="iq-text-button iq-cancel-global"
              :disabled="cancelling"
              @click="cancel"
            >
              {{ t('iqTest.cancel') }}
            </button>
          </div>
        </form>
        <TestResult
          :task="current"
          :cancelling="cancelling"
          :busy="starting || Boolean(runningID)"
          @cancel="cancel"
          @rerun="rerun"
        />
      </div>
      <TestHistory
        :items="history"
        :selected="current?.id"
        :loading="historyLoading"
        :has-more="hasMore"
        @open="open"
        @refresh="loadHistory()"
        @more="loadHistory(true)"
      />
      <p class="iq-page-disclaimer">{{ t('iqTest.disclaimer') }}</p>
      <BaseDialog :show="promptOpen" :title="t('iqTest.' + form.kind)" @close="promptOpen = false"
        ><p class="whitespace-pre-wrap text-sm leading-7 text-slate-700 dark:text-slate-200">
          {{ options?.prompts[form.kind] || t('iqTest.error_options') }}
        </p>
        <template #footer
          ><button type="button" class="iq-button" @click="promptOpen = false">
            {{ t('common.close') }}
          </button></template
        ></BaseDialog
      >
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import TestResult from '@/features/iq-test/TestResult.vue'
import TestHistory from '@/features/iq-test/TestHistory.vue'
import { useIQTest } from '@/features/iq-test/useIQTest'
import type { IQConnection, IQTest } from '@/api/iqTest'
const { t } = useI18n()
const {
  current,
  history,
  hasMore,
  keys,
  options,
  models,
  runningID,
  loading,
  starting,
  cancelling,
  discovering,
  historyLoading,
  error,
  modelsError,
  pollError,
  start,
  cancel,
  open,
  loadHistory,
  discover,
  clearModels,
  initialize
} = useIQTest()
const kinds = ['candy', 'pelican'] as const
const sources = ['site', 'custom'] as const
const allEfforts = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra']
const form = reactive({
  kind: 'candy' as 'candy' | 'pelican',
  source: 'site' as 'site' | 'custom',
  keyID: 0,
  baseURL: '',
  apiKey: '',
  model: '',
  effort: '',
  tier: '',
  stream: true
})
const manualModel = ref(false)
const promptOpen = ref(false)
const connection = computed<IQConnection>(() =>
  form.source === 'site'
    ? { source: 'site', key_id: form.keyID }
    : { source: 'custom', base_url: form.baseURL.trim(), api_key: form.apiKey.trim() }
)
const connectionReady = computed(() =>
  form.source === 'site'
    ? form.keyID > 0
    : /^https:\/\//i.test(form.baseURL.trim()) && form.apiKey.trim().length >= 8
)
const availableEfforts = computed(() => models.value.find((model) => model.id === form.model)?.efforts ?? [])
const canStart = computed(
  () =>
    connectionReady.value &&
    Boolean(form.model.trim()) &&
    !starting.value &&
    !runningID.value &&
    !loading.value
)
watch(keys, (data) => {
  if (!form.keyID && data.length) form.keyID = data[0].id
})
watch(
  () => [form.source, form.keyID, form.baseURL, form.apiKey],
  () => {
    clearModels()
    form.model = ''
    form.effort = ''
    manualModel.value = false
    if (form.source === 'site' && form.keyID) void discoverIfReady()
  }
)
watch(models, (data) => {
  if (data.length && !manualModel.value && !data.some((model) => model.id === form.model))
    form.model = data[0].id
})
watch(
  () => form.model,
  () => {
    if (!availableEfforts.value.includes(form.effort)) form.effort = ''
  }
)
async function discoverIfReady() {
  if (connectionReady.value) await discover(connection.value)
}
function toggleModelInput() {
  manualModel.value = !manualModel.value
  if (!manualModel.value && models.value.length && !models.value.some((model) => model.id === form.model)) {
    form.model = models.value[0].id
  }
}
async function run() {
  if (canStart.value)
    await start({
      ...connection.value,
      kind: form.kind,
      model: form.model.trim(),
      effort: form.effort,
      service_tier: form.tier,
      stream: form.stream
    })
}
function showHistory() {
  const section = document.getElementById('iq-test-history')
  section?.scrollIntoView({
    behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
    block: 'start'
  })
  section?.focus({ preventScroll: true })
}
async function openRunning() {
  const task = history.value.find((row) => row.id === runningID.value)
  if (task) await open(task)
}
async function rerun(task: IQTest) {
  const config = task.config
  if (config.source === 'custom' && config.base_url !== form.baseURL.trim()) form.apiKey = ''
  Object.assign(form, {
    kind: config.kind,
    source: config.source,
    keyID: config.key_id ?? form.keyID,
    baseURL: config.base_url ?? '',
    model: config.model,
    effort: config.effort,
    tier: config.service_tier,
    stream: config.stream
  })
  await nextTick()
  manualModel.value = !models.value.some((model) => model.id === config.model)
  form.model = config.model
  await nextTick()
  form.effort = config.effort
  if (canStart.value) await run()
  else document.getElementById(form.source === 'custom' ? 'iq-custom-key' : 'iq-key')?.focus()
}
function retry() {
  error.value = ''
  void initialize()
}
onBeforeUnmount(() => {
  form.apiKey = ''
})
</script>
<style src="@/features/iq-test/iq-test.css"></style>
