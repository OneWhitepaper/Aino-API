<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import OmniFlowGlyph from './OmniFlowGlyph.vue'
import { codeLines, copyText, EXAMPLE_MODELS, modelFor, requestSnippet, selectCode, simulationAnswer, simulationPayload } from './omniflowHome'
import type { CodeLanguage, ModelId, SimulationState } from './omniflowHome'
import type { HomeCopy } from './omniflowCopy'

const props = defineProps<{ open: boolean; modelId: ModelId; baseUrl: string; brand: string; chinese: boolean; copy: HomeCopy; trigger: HTMLElement | null }>()
const emit = defineEmits<{ close: []; 'update:modelId': [value: ModelId] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const title = ref<HTMLElement | null>(null)
const promptInput = ref<HTMLTextAreaElement | null>(null)
const codeElement = ref<HTMLElement | null>(null)
const prompt = ref('')
const language = ref<CodeLanguage>('curl')
const state = ref<SimulationState>('idle')
const answer = ref('')
const error = ref(false)
const showJson = ref(false)
const copied = ref<'idle' | 'success' | 'failed'>('idle')
const copyMessage = ref('')
const model = computed(() => modelFor(props.modelId))
const snippet = computed(() => requestSnippet(props.modelId, language.value, props.baseUrl, prompt.value))
const lines = computed(() => codeLines(snippet.value))
const json = computed(() => state.value === 'done' ? JSON.stringify(simulationPayload(props.modelId, answer.value), null, 2) : '')
const status = computed(() => state.value === 'idle' ? props.copy.waiting : state.value === 'loading' ? props.copy.sandboxProcessing : props.copy.sandboxDone)
const response = computed(() => state.value === 'idle' ? props.copy.waitingHint : state.value === 'loading' ? props.copy.sandboxLoading : answer.value)
let responseTimer: ReturnType<typeof setTimeout> | undefined
let copyTimer: ReturnType<typeof setTimeout> | undefined
let responseEpoch = 0
let copyEpoch = 0
let previousFocus: HTMLElement | null = null
let previousOverflow = ''
let ownsScrollLock = false
let backdropStart = false

function resetResponse() {
  clearTimeout(responseTimer)
  responseEpoch++
  state.value = 'idle'
  answer.value = ''
  error.value = false
  showJson.value = false
}

function restorePage() {
  clearTimeout(responseTimer)
  clearTimeout(copyTimer)
  responseEpoch++; copyEpoch++
  if (ownsScrollLock) {
    document.documentElement.style.overflow = previousOverflow
    ownsScrollLock = false
  }
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
  previousFocus = null
}

async function openDialog() {
  if (!dialog.value || dialog.value.open || !props.open) return
  previousFocus = props.trigger || (document.activeElement instanceof HTMLElement ? document.activeElement : null)
  prompt.value = props.chinese ? `用一句话解释什么是模型网关。` : 'Explain what a model gateway is in one sentence.'
  language.value = 'curl'
  copied.value = 'idle'; copyMessage.value = ''
  resetResponse()
  previousOverflow = document.documentElement.style.overflow
  dialog.value.showModal()
  document.documentElement.style.overflow = 'hidden'
  ownsScrollLock = true
  dialog.value.scrollTop = 0
  await nextTick()
  title.value?.focus({ preventScroll: true })
}

function closeDialog() {
  if (dialog.value?.open) dialog.value.close()
  restorePage()
  emit('close')
}

function onNativeClose() { restorePage(); emit('close') }
watch(() => props.open, value => { if (value) void nextTick(openDialog); else if (dialog.value?.open) closeDialog() })
watch(() => props.modelId, resetResponse)
watch(prompt, resetResponse)
watch(snippet, () => { clearTimeout(copyTimer); copyEpoch++; copied.value = 'idle'; copyMessage.value = '' })
onMounted(() => { if (props.open) void openDialog() })
onBeforeUnmount(() => { if (dialog.value?.open) dialog.value.close(); restorePage() })

function chooseModel(event: Event) {
  emit('update:modelId', modelFor((event.target as HTMLSelectElement).value).id)
}

function run() {
  if (state.value === 'loading') return
  const input = prompt.value.trim()
  if (!input) { error.value = true; promptInput.value?.focus(); return }
  error.value = false
  state.value = 'loading'
  showJson.value = false
  const epoch = ++responseEpoch
  const id = props.modelId
  responseTimer = setTimeout(() => {
    if (epoch !== responseEpoch || !props.open || id !== props.modelId) return
    answer.value = simulationAnswer(input, props.brand, props.chinese)
    state.value = 'done'
  }, 560)
}

async function copyCode() {
  const epoch = ++copyEpoch
  clearTimeout(copyTimer)
  const ok = await copyText(snippet.value, dialog.value || undefined)
  if (epoch !== copyEpoch || !props.open) return
  copied.value = ok ? 'success' : 'failed'
  copyMessage.value = ok ? props.copy.codeCopied : props.copy.sandboxCopyFailed
  if (!ok) selectCode(codeElement.value)
  copyTimer = setTimeout(() => { copied.value = 'idle' }, 1900)
}

function navigateLanguage(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  language.value = event.key === 'Home' ? 'curl' : event.key === 'End' ? 'python' : language.value === 'curl' ? 'python' : 'curl'
  nextTick(() => dialog.value?.querySelector<HTMLButtonElement>(`[data-ofsb-lang="${language.value}"]`)?.focus())
}

function trapFocus(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !dialog.value) return
  const stops = [...dialog.value.querySelectorAll<HTMLElement>('button:not(:disabled),select,textarea,summary,[tabindex="0"]')]
    .filter(element => element.tabIndex >= 0 && !element.closest('[hidden]') && element.getClientRects().length > 0)
  const first = stops[0], last = stops[stops.length - 1], active = document.activeElement
  if (event.shiftKey && (active === first || active === title.value)) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && active === last) { event.preventDefault(); first?.focus() }
}

function outside(event: PointerEvent | MouseEvent) {
  if (event.target !== dialog.value || !dialog.value) return false
  const rect = dialog.value.getBoundingClientRect()
  return event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom
}

function onBackdrop(event: MouseEvent) { if (backdropStart && outside(event)) closeDialog(); backdropStart = false }
</script>

<template>
  <dialog ref="dialog" class="ofsb" aria-labelledby="ofsb-title" aria-describedby="ofsb-hint" @cancel.prevent="closeDialog" @close="onNativeClose" @keydown="trapFocus" @pointerdown="backdropStart = outside($event)" @click="onBackdrop">
    <div class="ofsb-frame">
      <header class="ofsb-head">
        <div class="ofsb-head-text"><span class="ofsb-mark"><OmniFlowGlyph name="grid" /></span><h2 id="ofsb-title" ref="title" tabindex="-1">{{ copy.sandboxTitle }}</h2></div>
        <button type="button" class="ofsb-iconbtn" data-ofsb="close" :aria-label="copy.sandboxClose" @click="closeDialog"><OmniFlowGlyph name="close" /></button>
      </header>
      <p id="ofsb-hint" class="ofsb-hint">{{ copy.sandboxHint }}</p>
      <div class="ofsb-columns">
        <section class="ofsb-inputs" :aria-label="copy.request">
          <form class="ofsb-form" novalidate :aria-busy="state === 'loading'" @submit.prevent="run">
            <label for="ofsb-model">{{ copy.selectModel }}</label>
            <div class="ofsb-select-wrap"><select id="ofsb-model" :value="modelId" @change="chooseModel"><option v-for="item in EXAMPLE_MODELS" :key="item.id" :value="item.id">{{ item.name }}</option></select><OmniFlowGlyph name="chevron" /></div>
            <div class="ofsb-fields-head"><label for="ofsb-prompt">{{ copy.prompt }}</label><span class="ofsb-count" aria-hidden="true">{{ prompt.length }} / 320</span></div>
            <textarea id="ofsb-prompt" ref="promptInput" v-model="prompt" rows="3" maxlength="320" :aria-describedby="error ? 'ofsb-error ofsb-limit' : 'ofsb-limit'" :aria-invalid="error || undefined" :placeholder="copy.promptPlaceholder" @keydown.enter="($event.ctrlKey || $event.metaKey) && !$event.isComposing && ($event.preventDefault(), run())"></textarea>
            <span id="ofsb-limit" class="ofsb-sr">{{ copy.promptLimit }}</span><span v-if="error" id="ofsb-error" class="ofsb-error" role="alert">{{ copy.sandboxError }}</span>
            <button type="submit" class="ofsb-send" :disabled="state === 'loading'"><span class="ofsb-send-text">{{ state === 'loading' ? copy.sending : state === 'done' ? copy.sendAgain : copy.send }}</span><span class="ofsb-send-icon"><span v-if="state === 'loading'" class="ofsb-spinner"></span><OmniFlowGlyph v-else name="arrow" /></span></button>
          </form>
          <section class="ofsb-response" :data-state="state" :aria-label="copy.simulationResponse">
            <div class="ofsb-response-head"><span><OmniFlowGlyph name="reply" />{{ copy.simulationResponse }}</span><span class="ofsb-status" aria-hidden="true">{{ status }}</span></div>
            <p class="ofsb-answer" aria-live="polite" aria-atomic="true">{{ response }}</p>
            <details v-if="state === 'done'" class="ofsb-details" :open="showJson" @toggle="showJson = ($event.target as HTMLDetailsElement).open"><summary>{{ copy.viewJson }}</summary><pre class="ofsb-json" tabindex="0" :aria-label="copy.json">{{ json }}</pre></details>
          </section>
        </section>
        <section class="ofsb-integration" :aria-label="copy.integration">
          <h3>{{ copy.integration }}</h3><div class="ofsb-path"><b>POST</b><code>{{ model.path }}</code></div>
          <div class="ofsb-codebar"><div role="tablist" :aria-label="copy.language" class="ofsb-tabs"><button v-for="item in (['curl', 'python'] as const)" :id="`ofsb-tab-${item}`" :key="item" type="button" role="tab" aria-controls="ofsb-codepanel" :data-ofsb-lang="item" :aria-selected="language === item" :tabindex="language === item ? 0 : -1" @click="language = item" @keydown="navigateLanguage">{{ item === 'curl' ? 'cURL' : 'Python' }}</button></div>
            <button type="button" class="ofsb-copy" :title="copy.copyCode" :aria-label="copy.copyCode" @click="copyCode"><OmniFlowGlyph :name="copied === 'success' ? 'check' : 'copy'" /><span>{{ copied === 'success' ? copy.copied : copied === 'failed' ? copy.manualCopy : copy.copyCode }}</span></button>
          </div>
          <pre id="ofsb-codepanel" class="ofsb-pre" role="tabpanel" tabindex="0" :aria-labelledby="`ofsb-tab-${language}`"><code ref="codeElement"><span v-for="line in lines" :key="line.number" class="ofsb-code-line"><span v-for="(part, index) in line.parts" :key="index" :class="part.kind">{{ part.text }}</span>{{ '\n' }}</span></code></pre>
          <p class="ofsb-env">{{ copy.sandboxEnvBefore }} <code>OMNIFLOW_API_KEY</code>{{ copy.sandboxEnvAfter }}</p><p class="ofsb-sr ofsb-copy-msg" role="status">{{ copyMessage }}</p>
        </section>
      </div>
    </div>
  </dialog>
</template>
