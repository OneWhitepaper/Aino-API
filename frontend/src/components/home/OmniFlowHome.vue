<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import { resolveSiteLogo, resolveSiteName } from '@/utils/branding'
import OmniFlowGlyph from './OmniFlowGlyph.vue'
import OmniFlowSandbox from './OmniFlowSandbox.vue'
import { homeCopy } from './omniflowCopy'
import { codeLines, copyText, DEFAULT_MODEL, EXAMPLE_MODELS, MODEL_MEDIA, modelFor, protocolName, requestSnippet, selectCode, simulationAnswer } from './omniflowHome'
import type { CodeLanguage, ModelId, ProtocolFilter, SimulationState } from './omniflowHome'

const props = defineProps<{
  siteName: string
  siteLogo: string
  docUrl: string
  isAuthenticated: boolean
  dashboardPath: string
  registrationEnabled: boolean
  showModelPlaza: boolean
}>()
const { locale } = useI18n()
const chinese = computed(() => !locale?.value || locale.value.startsWith('zh'))
const copy = computed(() => homeCopy(chinese.value))
const brand = computed(() => resolveSiteName(props.siteName))
const localBrand = computed(() => chinese.value && brand.value === 'OmniFlow API' ? '万流 API' : brand.value)
const logo = computed(() => resolveSiteLogo(props.siteLogo, props.siteName))
const consolePath = computed(() => props.isAuthenticated ? props.dashboardPath : '/login')
const consoleLabel = computed(() => props.isAuthenticated ? copy.value.dashboard : copy.value.login)
const baseUrl = window.location.origin
const baseHost = window.location.host
const currentModel = ref<ModelId>(DEFAULT_MODEL)
const model = computed(() => modelFor(currentModel.value))
const language = ref<CodeLanguage>('curl')
const quickPrompt = computed(() => chinese.value ? '用一句话解释什么是模型网关。' : 'Explain what a model gateway is in one sentence.')
const snippet = computed(() => requestSnippet(currentModel.value, language.value, baseUrl, quickPrompt.value))
const lines = computed(() => codeLines(snippet.value))
const heroPrompt = ref(chinese.value ? `用一句话介绍${localBrand.value}。` : `Introduce ${localBrand.value} in one sentence.`)
const heroState = ref<SimulationState>('idle')
const heroError = ref(false)
const heroAnswer = ref('')
const defaultAnswer = computed(() => chinese.value ? `${localBrand.value} 是连接你的应用与多种模型的 API 网关。` : `${localBrand.value} connects your app to different models through their native APIs.`)
const response = computed(() => heroState.value === 'idle' ? defaultAnswer.value : heroState.value === 'loading' ? copy.value.loading : heroAnswer.value)
const status = computed(() => heroState.value === 'idle' ? copy.value.preview : heroState.value === 'loading' ? copy.value.processing : copy.value.done)
const query = ref('')
const filter = ref<ProtocolFilter>('all')
const filters = computed(() => [
  { key: 'all' as const, label: copy.value.all },
  { key: 'chat' as const, label: 'Chat Completions' },
  { key: 'messages' as const, label: 'Messages' },
  { key: 'native' as const, label: chinese.value ? 'Gemini 原生' : 'Gemini native' },
])
const visibleModels = computed(() => {
  const terms = query.value.trim().toLowerCase().split(/\s+/).filter(Boolean)
  return EXAMPLE_MODELS.filter(item => (filter.value === 'all' || item.kind === filter.value)
    && terms.every(term => `${item.name} ${item.id} ${item.provider} ${item.search} ${item.path}`.toLowerCase().includes(term)))
})
const menuOpen = ref(false)
const activeSection = ref('hero')
const sandboxOpen = ref(false)
const sandboxTrigger = ref<HTMLElement | null>(null)
const codeCopied = ref<'idle' | 'success' | 'failed'>('idle')
const baseCopied = ref<'idle' | 'success' | 'failed'>('idle')
const copyMessage = ref('')
const idMessage = ref('')
const idCopied = reactive<Record<string, boolean>>({})
const root = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const promptInput = ref<HTMLTextAreaElement | null>(null)
const codeElement = ref<HTMLElement | null>(null)
const baseElement = ref<HTMLElement | null>(null)
const menuToggle = ref<HTMLButtonElement | null>(null)
const navigation = ref<HTMLElement | null>(null)
const closing = ref<HTMLElement | null>(null)
const closingGround = ref<HTMLElement | null>(null)
const closingStatement = ref<HTMLElement | null>(null)
const year = new Date().getFullYear()
let heroTimer: ReturnType<typeof setTimeout> | undefined
let codeTimer: ReturnType<typeof setTimeout> | undefined
let baseTimer: ReturnType<typeof setTimeout> | undefined
const idTimers = new Map<string, ReturnType<typeof setTimeout>>()
let heroEpoch = 0
let codeEpoch = 0
let baseEpoch = 0
let frame = 0
let closingEffect: { destroy: () => void } | undefined
let closingMoment: { destroy?: () => void } | undefined
let momentEpoch = 0
let savedTitle = ''
let homepageTitle = ''
let savedScrollBehavior = ''
let mounted = false
const metadata = new Map<string, { element: HTMLMetaElement; previous: string | null; created: boolean; written: string }>()

function updateMetadata() {
  if (!mounted) return
  homepageTitle = `${brand.value} · ${copy.value.gateway}`
  document.title = homepageTitle
  const description = `${localBrand.value}：${copy.value.heroDescription} ${copy.value.heroSupport}`
  const values: [string, string, boolean?][] = [
    ['description', description], ['theme-color', '#eff0f1'], ['page-type', 'software'], ['site-kind', 'software'],
    ['og:title', homepageTitle, true], ['og:description', description, true], ['og:type', 'website', true],
  ]
  for (const [name, value, property] of values) {
    let entry = metadata.get(name)
    if (!entry) {
      const existing = document.head.querySelector<HTMLMetaElement>(`meta[${property ? 'property' : 'name'}="${name}"]`)
      const element = existing || document.createElement('meta')
      if (!existing) { element.setAttribute(property ? 'property' : 'name', name); document.head.append(element) }
      entry = { element, previous: existing?.getAttribute('content') ?? null, created: !existing, written: '' }
      metadata.set(name, entry)
    }
    entry.element.content = value
    entry.written = value
  }
}
watch([brand, chinese], updateMetadata)
watch(brand, () => { if (mounted) void mountClosingMoment() })

async function mountClosingMoment() {
  const statement = closingStatement.value
  const host = closing.value
  if (!statement || !host) return
  const epoch = ++momentEpoch
  closingMoment?.destroy?.()
  delete host.dataset.momentMounted
  host.classList.remove('mo-wh', 'is-done')
  // This dedicated subtree is owned by the original text effect, not Vue's text patcher.
  const mark = document.createElement('strong')
  mark.textContent = `${brand.value}.`
  statement.replaceChildren(document.createTextNode('One gateway for your next idea. Choose a model, keep its protocol, and start building with '), mark)
  statement.removeAttribute('aria-label')
  try {
    const moduleUrl = '/omniflow-home/moments/word-highlight/moment.js'
    const libraryUrl = '/omniflow-home/moments/lib.js'
    const [moment, library] = await Promise.all([import(/* @vite-ignore */ moduleUrl), import(/* @vite-ignore */ libraryUrl)])
    if (!mounted || epoch !== momentEpoch) return
    closingMoment = moment.default(host, {}, library) as { destroy?: () => void }
    host.dataset.momentMounted = '1'
    if (library.reduced()) host.dataset.momentStill = 'reduced'
  } catch { /* Unsplit text remains fully readable when an optional effect cannot load. */ }
}

async function mountClosingGround() {
  if (!closingGround.value || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
  try {
    // The approved prototype's unmodified local shader; scoped to this one background.
    const source = '/omniflow-home/effects/fx.js'
    const { default: fx } = await import(/* @vite-ignore */ source)
    if (!mounted || !closingGround.value) return
    const effect = await fx.mount(closingGround.value, 'grain-gradient', {
      palette: { bg: '#eff0f1', surface: '#dedfe0', ink: '#18191b', accent: '#ff4a1f', muted: '#5b5c5e' },
      params: { speed: 0, grainMotion: 0, grain: .18, tint: .16, contrast: .2 }, seed: 7,
    }) as { destroy: () => void }
    if (!mounted) effect.destroy()
    else closingEffect = effect
  } catch { /* The neutral CSS background remains visible if WebGL is unavailable. */ }
}

function resetHero() {
  clearTimeout(heroTimer)
  heroEpoch++
  heroState.value = 'idle'
  heroError.value = false
}

function resetCodeCopy() {
  clearTimeout(codeTimer)
  codeEpoch++
  codeCopied.value = 'idle'
  copyMessage.value = ''
}

function setModel(id: string) {
  currentModel.value = modelFor(id).id
  resetHero(); resetCodeCopy()
}

function chooseQuickModel(event: Event) { setModel((event.target as HTMLSelectElement).value) }
function setLanguage(value: CodeLanguage) { language.value = value; resetCodeCopy() }

function runHero() {
  if (heroState.value === 'loading') return
  const input = heroPrompt.value.trim()
  if (!input) { heroError.value = true; promptInput.value?.focus(); return }
  heroError.value = false
  heroState.value = 'loading'
  const epoch = ++heroEpoch
  heroTimer = setTimeout(() => {
    if (epoch !== heroEpoch) return
    heroAnswer.value = simulationAnswer(input, localBrand.value, chinese.value)
    heroState.value = 'done'
  }, 700)
}

function heroKey(event: KeyboardEvent) {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); runHero() }
}

function openSandbox(id: ModelId, event: Event) {
  setModel(id)
  menuOpen.value = false
  sandboxTrigger.value = event.currentTarget instanceof HTMLElement ? event.currentTarget : null
  sandboxOpen.value = true
}

async function copyCode() {
  const epoch = ++codeEpoch
  clearTimeout(codeTimer)
  const ok = await copyText(snippet.value, root.value || undefined)
  if (epoch !== codeEpoch) return
  codeCopied.value = ok ? 'success' : 'failed'
  copyMessage.value = ok ? copy.value.codeCopied : copy.value.codeFailed
  if (!ok) selectCode(codeElement.value)
  codeTimer = setTimeout(resetCodeCopy, 2200)
}

async function copyBase() {
  const epoch = ++baseEpoch
  clearTimeout(baseTimer)
  const ok = await copyText(baseUrl, root.value || undefined)
  if (epoch !== baseEpoch) return
  baseCopied.value = ok ? 'success' : 'failed'
  if (!ok) selectCode(baseElement.value)
  baseTimer = setTimeout(() => { baseCopied.value = 'idle' }, 2200)
}

async function copyModelId(id: ModelId, event: Event) {
  const button = event.currentTarget as HTMLButtonElement
  clearTimeout(idTimers.get(id))
  const ok = await copyText(id, root.value || undefined)
  idCopied[id] = ok
  idMessage.value = ok ? `${copy.value.copied} ${id}` : copy.value.copyFailed
  if (!ok) selectCode(button.querySelector('code'))
  idTimers.set(id, setTimeout(() => { delete idCopied[id] }, 2100))
}

function clearFilters() { query.value = ''; filter.value = 'all'; nextTick(() => searchInput.value?.focus({ preventScroll: true })) }

function closeMenu(restore = false) { menuOpen.value = false; if (restore) menuToggle.value?.focus() }
function toggleMenu() {
  menuOpen.value = !menuOpen.value
  if (menuOpen.value) nextTick(() => navigation.value?.querySelector<HTMLAnchorElement>('a')?.focus())
}

function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && menuOpen.value) { event.preventDefault(); closeMenu(true) }
  const element = event.target instanceof Element ? event.target : null
  if (event.key === '/' && !event.altKey && !event.ctrlKey && !event.metaKey && !sandboxOpen.value && !element?.closest('input,textarea,select,[contenteditable="true"]')) {
    event.preventDefault(); closeMenu()
    searchInput.value?.scrollIntoView({ block: 'center' }); searchInput.value?.focus({ preventScroll: true })
  }
}

function updateNavigation() {
  frame = 0
  if (!root.value) return
  const cutoff = 100 + Math.min(190, window.innerHeight * .2)
  let selected = 'hero'
  for (const id of ['hero', 'models', 'quickstart']) if ((root.value.querySelector(`#${id}`)?.getBoundingClientRect().top ?? Infinity) < cutoff) selected = id
  activeSection.value = selected
}

function scheduleNavigation() {
  if (window.innerWidth > 1000) menuOpen.value = false
  if (!frame) frame = requestAnimationFrame(updateNavigation)
}

onMounted(() => {
  savedTitle = document.title
  // The app's global smooth scrolling can leave a new Tab stop outside the viewport
  // while a keyboard user is already typing. The approved homepage uses immediate scroll.
  savedScrollBehavior = document.documentElement.style.scrollBehavior
  document.documentElement.style.scrollBehavior = 'auto'
  mounted = true
  updateMetadata()
  void mountClosingGround()
  void mountClosingMoment()
  document.addEventListener('keydown', keydown)
  window.addEventListener('scroll', scheduleNavigation, { passive: true })
  window.addEventListener('resize', scheduleNavigation)
  updateNavigation()
})
onBeforeUnmount(() => {
  mounted = false
  if (document.documentElement.style.scrollBehavior === 'auto') document.documentElement.style.scrollBehavior = savedScrollBehavior
  if (document.title === homepageTitle) document.title = savedTitle
  for (const entry of metadata.values()) {
    if (entry.element.getAttribute('content') !== entry.written) continue
    if (entry.created) entry.element.remove()
    else if (entry.previous === null) entry.element.removeAttribute('content')
    else entry.element.content = entry.previous
  }
  document.removeEventListener('keydown', keydown)
  window.removeEventListener('scroll', scheduleNavigation)
  window.removeEventListener('resize', scheduleNavigation)
  cancelAnimationFrame(frame)
  momentEpoch++
  closingMoment?.destroy?.()
  closingEffect?.destroy()
  clearTimeout(heroTimer); clearTimeout(codeTimer); clearTimeout(baseTimer)
  idTimers.forEach(clearTimeout)
  heroEpoch++; codeEpoch++; baseEpoch++
})
</script>

<template>
  <div ref="root" class="omniflow-home" :lang="chinese ? 'zh-CN' : 'en'">
    <a class="of-skip" href="#omniflow-main">{{ copy.skip }}</a>
    <header class="of-nav nav nav--cards">
      <div class="of-wrap wrap nav-bar">
        <router-link to="/" class="of-brand" :aria-label="brand"><img :src="logo" alt="" width="32" height="32" /><span>{{ brand }}</span></router-link>
        <nav id="omniflow-navigation" ref="navigation" class="of-links" :class="{ 'is-open': menuOpen }" :aria-label="copy.navigation" @click="closeMenu()">
          <a class="of-nav-link" href="#hero" :aria-current="activeSection === 'hero' ? 'location' : undefined">{{ copy.home }}</a>
          <a class="of-nav-link" href="#models" :aria-current="activeSection === 'models' ? 'location' : undefined">{{ copy.models }}</a>
          <a v-if="docUrl" class="of-nav-link" :href="docUrl" target="_blank" rel="noopener noreferrer">{{ copy.docs }}</a>
          <a v-else class="of-nav-link" href="#quickstart" :aria-current="activeSection === 'quickstart' ? 'location' : undefined">{{ copy.docs }}</a>
          <router-link v-if="showModelPlaza" class="of-nav-link" to="/model-plaza">{{ copy.modelPlaza }}</router-link>
          <router-link class="of-mobile-console" :to="consolePath">{{ consoleLabel }}<OmniFlowGlyph name="east" /></router-link>
          <router-link v-if="registrationEnabled && !isAuthenticated" class="of-mobile-console" to="/register">{{ copy.register }}<OmniFlowGlyph name="arrow" /></router-link>
        </nav>
        <div class="nav-actions">
          <LocaleSwitcher class="of-locale" />
          <router-link :to="consolePath" class="of-login">{{ consoleLabel }}<OmniFlowGlyph name="east" /></router-link>
          <button ref="menuToggle" type="button" class="of-menu-toggle" :aria-label="copy.menu" aria-controls="omniflow-navigation" :aria-expanded="menuOpen" @click="toggleMenu"><OmniFlowGlyph :name="menuOpen ? 'close' : 'menu'" /></button>
        </div>
      </div>
    </header>

    <main id="omniflow-main" tabindex="-1" :inert="menuOpen || undefined">
      <section id="hero" class="of-hero hero hero--centred" data-enter aria-labelledby="hero-title">
        <div class="of-wrap wrap hero-frame">
          <div class="hero-message">
            <p class="hero-eyebrow"><span class="brand-pip" aria-hidden="true"></span>{{ localBrand }} · {{ copy.gateway }}</p>
            <h1 id="hero-title"><span>{{ copy.heroLine1 }}</span><span>{{ copy.heroLine2 }}<span class="title-accent">{{ copy.heroAccent }}</span>{{ copy.heroStop }}</span></h1>
            <p class="hero-desc">{{ copy.heroDescription }}<br />{{ copy.heroSupport }}</p>
            <div class="hero-actions"><button type="button" id="hero-try" class="of-button of-primary" :data-sandbox="currentModel" @click="openSandbox(currentModel, $event)">{{ copy.tryApi }}<OmniFlowGlyph name="arrow" /></button><a class="of-button of-secondary" href="#models">{{ copy.browse }}<OmniFlowGlyph name="down" /></a></div>
          </div>
          <div class="workspace-stage hero__window" data-enter-media>
            <div id="route-art" class="workspace app-frame" data-ui :data-route="model.short.toLowerCase()" role="group" aria-labelledby="route-title">
              <div class="workspace-top"><span id="route-title" class="workspace-label"><OmniFlowGlyph name="term" />API Playground<span class="workspace-divider">/</span><span class="workspace-subtitle">{{ copy.workbench }}</span></span><span class="local-badge">{{ copy.local }}</span></div>
              <div class="workspace-body">
                <aside class="model-rail"><span class="rail-label">{{ copy.selectModel }}<span class="rc-count"><span id="route-index">{{ String(EXAMPLE_MODELS.findIndex(item => item.id === currentModel) + 1).padStart(2, '0') }}</span> / 03</span></span>
                  <div class="rc-controls" role="group" :aria-label="copy.selectModel"><button v-for="item in EXAMPLE_MODELS" :key="item.id" type="button" class="route-choice" :data-model="item.id" :aria-pressed="currentModel === item.id" @click="setModel(item.id)"><img :src="MODEL_MEDIA + item.icon" alt="" width="24" height="24" /><span>{{ item.short }}<small>{{ item.version }}</small></span><OmniFlowGlyph name="check" class="choice-check" /></button></div>
                  <a class="rail-link" href="#models">{{ copy.explore }}<OmniFlowGlyph name="arrow" /></a>
                </aside>
                <form id="hero-form" class="work-request" novalidate :aria-busy="heroState === 'loading'" @submit.prevent="runHero">
                  <div class="pane-heading"><span>{{ copy.request }}<span class="pane-eng">REQUEST</span></span><span id="hero-protocol" class="request-format">{{ protocolName(currentModel, chinese) }}</span></div>
                  <div class="rc-endpoint" :title="'POST ' + model.path"><span class="method">POST</span><code id="route-path">{{ model.path }}</code></div>
                  <label for="hero-prompt">{{ copy.input }}</label><textarea id="hero-prompt" ref="promptInput" v-model="heroPrompt" rows="2" maxlength="600" :aria-invalid="heroError || undefined" :aria-describedby="heroError ? 'hero-error' : undefined" @input="resetHero" @keydown="heroKey"></textarea>
                  <p v-if="heroError" id="hero-error" class="hero-error" role="alert">{{ copy.promptError }}</p>
                  <button id="hero-send" type="submit" class="run-button" :disabled="heroState === 'loading'"><OmniFlowGlyph name="play" /><span>{{ heroState === 'loading' ? copy.sending : heroState === 'done' ? copy.sendAgain : copy.send }}</span><kbd aria-hidden="true">↵</kbd></button>
                </form>
                <div class="work-response"><div class="pane-heading"><span>{{ copy.response }}<span class="pane-eng">RESPONSE</span></span><span id="hero-status" class="response-state" :data-done="heroState === 'done' ? true : undefined">{{ status }}</span></div>
                  <div class="response-meta"><img id="response-logo" :src="MODEL_MEDIA + model.icon" width="20" height="20" alt="" /><code id="hero-model-id">{{ currentModel }}</code></div><p id="hero-answer" class="response-answer" aria-live="polite" aria-atomic="true">{{ response }}</p><div class="response-footer"><span>{{ copy.noCost }}</span><button type="button" class="open-workbench" :data-sandbox="currentModel" :aria-label="copy.openWorkbench" @click="openSandbox(currentModel, $event)"><OmniFlowGlyph name="east" /></button></div>
                </div>
              </div>
            </div>
          </div>
          <div class="hero-bottom"><div class="base-address"><span>BASE URL</span><button id="copy-base" type="button" :aria-label="copy.copyBase + ' ' + baseUrl" :title="copy.copyBase" @click="copyBase"><code ref="baseElement">{{ baseHost }}</code><OmniFlowGlyph :name="baseCopied === 'success' ? 'check' : 'copy'" /></button><span id="base-feedback" role="status">{{ baseCopied === 'success' ? copy.copied : baseCopied === 'failed' ? copy.copyFailed : '' }}</span></div><span class="protocol-rail">Chat Completions<i>·</i>Messages<i>·</i>{{ chinese ? 'Gemini 原生' : 'Gemini native' }}</span></div>
        </div>
      </section>

      <section id="models" class="of-catalog" aria-labelledby="models-title">
        <div class="of-wrap wrap catalog-frame">
          <div class="catalog-intro"><div><span class="eyebrow">MODEL EXPLORER</span><h2 id="models-title">{{ copy.modelsLine1 }}<br />{{ copy.modelsLine2 }}</h2><p>{{ copy.modelsDescription }}</p></div><label class="search-box" for="model-search"><OmniFlowGlyph name="search" /><input id="model-search" ref="searchInput" v-model="query" type="search" :placeholder="copy.search" :aria-label="copy.search" autocomplete="off" spellcheck="false" /><kbd aria-hidden="true">/</kbd></label></div>
          <div class="catalog-tools"><div class="filters" role="group" :aria-label="copy.filters"><button v-for="item in filters" :key="item.key" type="button" :data-filter="item.key" :aria-pressed="filter === item.key" @click="filter = item.key">{{ item.label }}</button></div><span id="result-count" role="status">{{ visibleModels.length }} {{ copy.sampleCount }}</span></div>
          <div class="model-table" data-ui><div class="rows-header" aria-hidden="true"><span>{{ copy.modelColumn }}</span><span>{{ copy.protocolColumn }}</span><span>{{ copy.idColumn }}</span><span>{{ copy.tryColumn }}</span></div>
            <div id="model-list"><article v-for="item in visibleModels" :key="item.id" class="model-row" :data-model-row="item.id" :data-kind="item.kind"><div class="model-name"><span class="provider-tile"><img loading="lazy" decoding="async" :src="MODEL_MEDIA + item.icon" width="32" height="32" alt="" /></span><div><h3>{{ item.name }}</h3><p>{{ item.provider }}</p></div></div><span class="model-protocol">{{ protocolName(item.id, chinese) }}</span><button type="button" class="id-copy" :data-copy-id="item.id" :data-copied="idCopied[item.id] || undefined" :title="idCopied[item.id] ? copy.copied : copy.copyId" :aria-label="copy.copyId + ' ' + item.id" @click="copyModelId(item.id, $event)"><code class="model-id">{{ item.id }}</code><OmniFlowGlyph :name="idCopied[item.id] ? 'check' : 'copy'" /></button><button type="button" class="row-action" :data-sandbox="item.id" :aria-label="copy.tryModel + ' ' + item.name" @click="openSandbox(item.id, $event)"><OmniFlowGlyph name="east" /></button></article></div>
            <div v-if="visibleModels.length === 0" id="empty-state" class="empty-state"><OmniFlowGlyph name="search" /><strong>{{ copy.noResults }}</strong><p>{{ copy.noResultsHint }}</p><button id="clear-filters" type="button" class="quiet-btn" @click="clearFilters">{{ copy.clear }}</button></div>
          </div>
          <span id="id-copy-msg" role="status" class="of-sr">{{ idMessage }}</span><p class="catalog-note">{{ copy.catalogNote }} <router-link v-if="showModelPlaza" to="/model-plaza">{{ copy.modelPlaza }}<OmniFlowGlyph name="east" /></router-link></p>
        </div>
      </section>

      <section id="quickstart" class="of-quick" aria-labelledby="qs-title">
        <div class="of-wrap wrap quick-frame"><div class="section-kicker"><span>DEVELOPER QUICKSTART</span><span>01 / CONNECT</span></div>
          <div class="quick-grid"><div class="quick-copy-col"><h2 id="qs-title">{{ copy.quickLine1 }}<br /><em>{{ copy.quickLine2 }}</em></h2><p class="quick-desc">{{ copy.quickDescription }}</p><label class="model-label" for="quick-model">{{ copy.sampleModel }}</label><div class="model-select"><select id="quick-model" :value="currentModel" @change="chooseQuickModel"><option v-for="item in EXAMPLE_MODELS" :key="item.id" :value="item.id">{{ item.name }}</option></select><OmniFlowGlyph name="chevron" /></div><p class="env-note">{{ copy.envBefore }} <code>OMNIFLOW_API_KEY</code>{{ copy.envAfter }}</p><button id="quick-try" type="button" class="of-button of-primary bench-trial" :data-sandbox="currentModel" @click="openSandbox(currentModel, $event)">{{ copy.localTry }}<OmniFlowGlyph name="arrow" /></button></div>
            <div class="code-window" data-ui><div class="cw-top"><span><OmniFlowGlyph name="term" /><span id="code-filename">{{ language === 'curl' ? 'request.sh' : 'request.py' }}</span></span><span class="code-meta">{{ baseUrl.startsWith('https:') ? 'HTTPS' : 'HTTP' }}</span></div><div class="cw-tools"><div class="lang-tabs" role="group" :aria-label="copy.language"><button v-for="item in (['curl', 'python'] as const)" :key="item" type="button" :data-lang="item" :aria-pressed="language === item" @click="setLanguage(item)">{{ item === 'curl' ? 'cURL' : 'Python' }}</button></div><button id="quick-copy" type="button" class="copy-code" @click="copyCode"><OmniFlowGlyph :name="codeCopied === 'success' ? 'check' : 'copy'" /><span>{{ codeCopied === 'success' ? copy.copied : codeCopied === 'failed' ? copy.manualCopy : copy.copyCode }}</span></button></div><pre class="quick-pre" tabindex="0" :aria-label="copy.code"><code id="quick-code" ref="codeElement"><span v-for="line in lines" :key="line.number" class="code-line" :data-line="line.number"><span v-for="(part, index) in line.parts" :key="index" :class="part.kind">{{ part.text }}</span>{{ '\n' }}</span></code></pre><p id="copy-msg" class="of-sr" role="status">{{ copyMessage }}</p></div>
          </div>
        </div>
      </section>
    </main>

    <footer id="connect" ref="closing" class="connect-section closing footer--statement" data-moment="word-highlight" data-visual aria-labelledby="connect-title" :inert="menuOpen || undefined">
      <div ref="closingGround" class="closing-ground layer" data-fx="grain-gradient" data-fx-params='{"speed":0,"grainMotion":0,"grain":0.18,"tint":0.16,"contrast":0.2}' aria-hidden="true"></div>
      <div class="of-wrap wrap connect-frame">
        <div class="closing-cta closing__cta"><h2 id="connect-title">{{ copy.connectTitle }}</h2><p ref="closingStatement" class="connect-statement mo-wh__text"></p><div class="closing-actions"><router-link :to="consolePath" class="of-button of-primary">{{ consoleLabel }}<OmniFlowGlyph name="east" /></router-link><router-link v-if="registrationEnabled && !isAuthenticated" data-testid="home-primary-action" to="/register" class="of-button of-secondary">{{ copy.register }}<OmniFlowGlyph name="arrow" /></router-link></div></div>
        <div class="footer-legal closing__links"><span>{{ localBrand }} · {{ copy.gateway }}<small>© {{ year }} {{ brand }}</small></span><div class="footer-nav"><a href="#quickstart">{{ copy.viewCode }}<OmniFlowGlyph name="arrow" /></a><a class="to-top" href="#hero">{{ copy.toTop }}<OmniFlowGlyph name="east" /></a></div></div>
      </div>
    </footer>
    <OmniFlowSandbox :open="sandboxOpen" :model-id="currentModel" :base-url="baseUrl" :brand="localBrand" :chinese="chinese" :copy="copy" :trigger="sandboxTrigger" @update:model-id="setModel" @close="sandboxOpen = false" />
  </div>
</template>

<style src="./omniflow-home.css"></style>
<style src="./omniflow-sandbox.css"></style>
