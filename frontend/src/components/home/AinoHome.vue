<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ainoMark from '@/assets/aino/aino-mark.svg'

const props = defineProps<{
  siteName: string
  siteLogo: string
  docUrl: string
  isAuthenticated: boolean
  dashboardPath: string
  registrationEnabled: boolean
  showModelPlaza: boolean
  isDark: boolean
}>()
defineEmits<{ 'toggle-theme': [] }>()
const { t } = useI18n()
const brand = computed(() => props.siteName === 'Sub2API' ? 'Aino API' : props.siteName)
const primaryPath = computed(() => props.isAuthenticated ? props.dashboardPath : props.registrationEnabled ? '/register' : '/login')
const primaryLabel = computed(() => props.isAuthenticated ? t('home.goToDashboard') : props.registrationEnabled ? t('home.aino.createAccount') : t('home.getStarted'))
const activeTab = ref(0)
const menuOpen = ref(false)
const tabs = ['models', 'keys', 'usage'] as const
const tabIcons = ['grid', 'key', 'chart'] as const
const year = new Date().getFullYear()

function navigateTab(event: KeyboardEvent, index: number) {
  const next = event.key === 'ArrowRight' ? (index + 1) % tabs.length
    : event.key === 'ArrowLeft' ? (index + tabs.length - 1) % tabs.length
      : event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : null
  if (next === null) return
  event.preventDefault()
  activeTab.value = next
  const group = (event.currentTarget as HTMLElement).parentElement
  group?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus()
}
</script>

<template>
  <div class="aino-home" :class="{ 'aino-home--dark': isDark }">
    <a class="aino-skip" href="#aino-main">{{ t('home.aino.skip') }}</a>
    <header class="aino-header">
      <nav class="aino-nav aino-shell" :aria-label="t('home.aino.navigation')">
        <router-link to="/" class="aino-brand" :aria-label="brand">
          <img :src="siteLogo || ainoMark" alt="" width="28" height="28" />
          <span>{{ brand }}</span>
        </router-link>
        <div id="aino-navigation" class="aino-nav-links" :class="{ 'is-open': menuOpen }" @click="menuOpen = false">
          <a href="#aino-platform">{{ t('home.aino.platform') }}</a>
          <router-link v-if="showModelPlaza" to="/model-plaza">{{ t('nav.modelPlaza') }}</router-link>
          <a href="#aino-start">{{ t('home.aino.quickStart') }}</a>
          <a href="#aino-faq">{{ t('home.aino.faq') }}</a>
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer">{{ t('home.docs') }}</a>
        </div>
        <div class="aino-nav-actions">
          <LocaleSwitcher class="aino-locale" />
          <button class="aino-icon-button" :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="$emit('toggle-theme')">
            <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
          </button>
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="aino-button aino-nav-cta">
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
            <Icon name="arrowRight" size="sm" aria-hidden="true" />
          </router-link>
          <button class="aino-icon-button aino-menu-toggle" :aria-label="t('home.aino.menu')" :aria-expanded="menuOpen" aria-controls="aino-navigation" @click="menuOpen = !menuOpen" @keydown.esc="menuOpen = false">
            <Icon :name="menuOpen ? 'x' : 'menu'" size="md" />
          </button>
        </div>
      </nav>
    </header>

    <main id="aino-main" tabindex="-1">
      <section class="aino-hero" aria-labelledby="aino-title">
        <div class="aino-hero-copy aino-shell">
          <h1 id="aino-title">{{ t('home.aino.heroLine1') }}<br />{{ t('home.aino.heroLine2') }}</h1>
          <p class="aino-lead">{{ t('home.aino.heroDescription') }}<br class="aino-desktop-break" /> {{ t('home.aino.heroSupport') }}</p>
          <div class="aino-actions">
            <router-link data-testid="home-primary-action" :to="primaryPath" class="aino-button aino-button-dark">
              {{ primaryLabel }}<Icon name="arrowRight" size="sm" aria-hidden="true" />
            </router-link>
            <a class="aino-text-link" href="#aino-platform">{{ t('home.aino.explore') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></a>
          </div>
        </div>

        <div class="aino-showcase">
          <!-- The same decorative artwork used by the official Aino website. -->
          <img class="aino-showcase-art" src="https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260826_125039_45a71f04-36dd-4620-99d8-7526316d439e.png" alt="" decoding="async" />
          <div class="aino-preview">
            <aside class="aino-preview-sidebar" aria-hidden="true">
              <span class="aino-preview-brand">Aino<span>.</span></span>
              <span class="aino-sidebar-label">{{ t('home.aino.workspace') }}</span>
              <span v-for="(tab, index) in tabs" :key="tab" class="aino-sidebar-item" :class="{ selected: activeTab === index }">
                <Icon :name="tabIcons[index]" size="sm" />{{ t(`home.aino.${tab}`) }}
              </span>
              <span class="aino-sidebar-note">{{ t('home.aino.sidebarNote') }}</span>
            </aside>
            <div class="aino-preview-main">
              <div class="aino-preview-topline"><span>{{ t('home.aino.workspaceIntro') }}</span><span class="aino-sample-label">{{ t('home.aino.demo') }}</span></div>
              <h2>{{ t('home.aino.previewTitle') }}</h2>
              <div class="aino-tabs" role="tablist" :aria-label="t('home.aino.previewTitle')">
                <button v-for="(tab, index) in tabs" :id="`aino-tab-${index}`" :key="tab" type="button" role="tab" :aria-selected="activeTab === index" :tabindex="activeTab === index ? 0 : -1" aria-controls="aino-preview-panel" @click="activeTab = index" @keydown="navigateTab($event, index)">{{ t(`home.aino.${tab}`) }}</button>
              </div>
              <div id="aino-preview-panel" class="aino-preview-panel" role="tabpanel" :aria-labelledby="`aino-tab-${activeTab}`" tabindex="0">
                <template v-if="activeTab === 0">
                  <div class="aino-provider-row" v-for="(provider, index) in ['OpenAI', 'Claude', 'Gemini']" :key="provider">
                    <span class="aino-provider-monogram" :class="`provider-${index}`">{{ provider.charAt(0) }}</span>
                    <div><strong>{{ provider }}</strong><span>{{ t(`home.aino.provider${index}`) }}</span></div>
                    <Icon name="arrowRight" size="sm" aria-hidden="true" />
                  </div>
                </template>
                <template v-else-if="activeTab === 1">
                  <div class="aino-key-demo"><Icon name="key" size="lg" /><strong>{{ t('home.aino.keyTitle') }}</strong><code>Authorization: Bearer YOUR_API_KEY</code></div>
                  <p>{{ t('home.aino.keyDescription') }}</p>
                </template>
                <template v-else>
                  <div v-for="item in ['model', 'tokens', 'cost']" :key="item" class="aino-usage-row"><span>{{ t(`home.aino.usage${item}`) }}</span><strong>{{ t(`home.aino.usage${item}Detail`) }}</strong></div>
                </template>
              </div>
              <p class="aino-preview-caption">{{ t('home.aino.demoNote') }}</p>
            </div>
          </div>
        </div>
      </section>

      <section id="aino-platform" class="aino-section aino-shell" aria-labelledby="aino-platform-title">
        <div class="aino-section-heading">
          <span class="aino-eyebrow">{{ t('home.aino.platformEyebrow') }}</span>
          <h2 id="aino-platform-title">{{ t('home.aino.platformLine1') }}<br />{{ t('home.aino.platformLine2') }}</h2>
          <p>{{ t('home.aino.platformDescription') }}</p>
        </div>
        <div class="aino-feature-row">
          <div class="aino-feature-copy">
            <span class="aino-eyebrow">01 / {{ t('home.aino.accessEyebrow') }}</span>
            <h3>{{ t('home.aino.accessTitle') }}</h3>
            <p>{{ t('home.aino.accessDescription') }}</p>
            <router-link class="aino-text-link" :to="showModelPlaza ? '/model-plaza' : primaryPath">{{ showModelPlaza ? t('nav.modelPlaza') : primaryLabel }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></router-link>
          </div>
          <div class="aino-feature-visual">
            <div class="aino-connection-card">
              <img :src="ainoMark" width="40" height="40" alt="" />
              <h4>{{ t('home.aino.connectionTitle') }}</h4>
              <div class="aino-detail-row"><span>{{ t('home.aino.client') }}</span><strong>Aino · SDK · {{ t('home.aino.yourApp') }}</strong></div>
              <div class="aino-detail-row"><span>{{ t('home.aino.access') }}</span><strong>API Key</strong></div>
              <div class="aino-detail-row"><span>{{ t('home.aino.models') }}</span><strong>{{ t('home.aino.availableModels') }}</strong></div>
              <p class="aino-connection-note"><Icon name="checkCircle" size="sm" />{{ t('home.aino.connectionNote') }}</p>
            </div>
          </div>
        </div>
        <div class="aino-feature-row aino-feature-reverse">
          <div class="aino-feature-copy">
            <span class="aino-eyebrow">02 / {{ t('home.aino.controlEyebrow') }}</span>
            <h3>{{ t('home.aino.controlTitle') }}</h3>
            <p>{{ t('home.aino.controlDescription') }}</p>
            <router-link class="aino-text-link" :to="isAuthenticated ? dashboardPath : '/login'">{{ t('home.goToDashboard') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></router-link>
          </div>
          <div class="aino-feature-visual aino-control-visual">
            <div class="aino-control-list">
              <div v-for="(item, index) in ['keys', 'quota', 'billing']" :key="item" class="aino-control-item">
                <span class="aino-step-number">0{{ index + 1 }}</span>
                <div><strong>{{ t(`home.aino.control${item}`) }}</strong><span>{{ t(`home.aino.control${item}Description`) }}</span></div>
                <Icon name="check" size="sm" aria-hidden="true" />
              </div>
            </div>
          </div>
        </div>
      </section>

      <section id="aino-start" class="aino-start-section" aria-labelledby="aino-start-title">
        <div class="aino-shell">
          <span class="aino-eyebrow">{{ t('home.aino.startEyebrow') }}</span>
          <h2 id="aino-start-title">{{ t('home.aino.startTitle') }}</h2>
          <div class="aino-steps">
            <article v-for="(step, index) in ['account', 'key', 'connect']" :key="step">
              <span class="aino-step-number">0{{ index + 1 }}</span>
              <h3>{{ t(`home.aino.step${step}`) }}</h3>
              <p>{{ t(`home.aino.step${step}Description`) }}</p>
            </article>
          </div>
        </div>
      </section>

      <section id="aino-faq" class="aino-faq aino-section aino-shell" aria-labelledby="aino-faq-title">
        <div><span class="aino-eyebrow">{{ t('home.aino.faqEyebrow') }}</span><h2 id="aino-faq-title">{{ t('home.aino.faqTitle') }}</h2><a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="aino-text-link">{{ t('home.viewDocs') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></a></div>
        <div class="aino-faq-list">
          <details v-for="topic in ['aino', 'models', 'billing', 'keys']" :key="topic">
            <summary>{{ t(`home.aino.faq${topic}`) }}<Icon name="plus" size="sm" aria-hidden="true" /></summary>
            <p>{{ t(`home.aino.faq${topic}Answer`) }}</p>
          </details>
        </div>
      </section>

      <section class="aino-final-cta">
        <div class="aino-shell"><img :src="ainoMark" width="54" height="54" alt="" /><h2>{{ t('home.aino.ctaLine1') }}<br />{{ t('home.aino.ctaLine2') }}</h2><p>{{ t('home.aino.ctaDescription') }}</p><router-link :to="primaryPath" class="aino-button aino-button-dark">{{ primaryLabel }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></router-link></div>
      </section>
    </main>

    <footer class="aino-footer aino-shell">
      <div class="aino-footer-main"><div><router-link to="/" class="aino-brand"><img :src="siteLogo || ainoMark" width="28" height="28" alt="" /><span>{{ brand }}</span></router-link><p>{{ t('home.aino.footerDescription') }}</p></div><div class="aino-footer-links"><a href="#aino-platform">{{ t('home.aino.platform') }}</a><a href="#aino-start">{{ t('home.aino.quickStart') }}</a><a href="#aino-faq">{{ t('home.aino.faq') }}</a><a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer">{{ t('home.docs') }}</a></div></div>
      <div class="aino-footer-bottom"><span>© {{ year }} {{ brand }}</span><a href="https://github.com/Ablankpaper/Aino-API" target="_blank" rel="noopener noreferrer">Aino API · GitHub<Icon name="externalLink" size="xs" aria-hidden="true" /></a></div>
    </footer>
  </div>
</template>

<style scoped src="./aino-home.css"></style>
