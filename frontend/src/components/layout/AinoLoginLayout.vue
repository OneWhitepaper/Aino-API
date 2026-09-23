<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import ainoMark from '@/assets/aino/aino-mark.svg'

const { t } = useI18n()
const appStore = useAppStore()
const brand = computed(() => {
  const name = appStore.cachedPublicSettings?.site_name || appStore.siteName
  return !name || name === 'Sub2API' ? 'Aino API' : name
})
const logo = computed(() => sanitizeUrl(
  appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '',
  { allowRelative: true, allowDataUrl: true },
) || ainoMark)
const isDark = ref(document.documentElement.classList.contains('dark'))
const year = new Date().getFullYear()
const description = computed(() => {
  const subtitle = appStore.cachedPublicSettings?.site_subtitle
  return subtitle && subtitle !== 'Subscription to API Conversion Platform'
    ? subtitle
    : t('home.aino.loginDescription')
})

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

onMounted(() => {
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
})
</script>

<template>
  <div class="aino-login" :class="{ 'aino-login--dark': isDark }">
    <header class="login-header login-shell">
      <nav class="login-nav" :aria-label="t('home.aino.navigation')">
        <router-link to="/home" class="login-brand">
          <img :src="logo" alt="" width="28" height="28" />
          <span>{{ brand }}</span>
        </router-link>
        <div class="login-nav-actions">
          <LocaleSwitcher class="login-locale" />
          <button class="login-theme" type="button" :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
            <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
          </button>
          <router-link to="/home" class="login-home-link" :aria-label="t('home.aino.loginBack')" :title="t('home.aino.loginBack')">
            <span>{{ t('home.aino.loginBack') }}</span>
            <Icon name="arrowRight" size="sm" aria-hidden="true" />
          </router-link>
        </div>
      </nav>
    </header>

    <main class="login-main login-shell">
      <section class="login-story" aria-labelledby="login-story-title">
        <div class="login-story-copy">
          <img :src="ainoMark" alt="" width="44" height="44" />
          <h2 id="login-story-title">{{ t('home.aino.loginLine1') }}<br />{{ t('home.aino.loginLine2') }}</h2>
          <p>{{ description }}</p>
          <div class="login-story-caption">
            <span>{{ t('home.aino.models') }}</span><span>{{ t('home.aino.keys') }}</span><span>{{ t('home.aino.usage') }}</span>
          </div>
        </div>
        <div class="login-art" aria-hidden="true">
          <img class="login-art-background" src="https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260826_125039_45a71f04-36dd-4620-99d8-7526316d439e.png" alt="" decoding="async" />
          <div class="login-art-brand"><img :src="ainoMark" width="54" height="54" alt="" /><span>Aino<span class="login-brand-dot">.</span></span></div>
        </div>
      </section>

      <section class="login-form-region">
        <div class="login-form-content">
          <slot />
          <div v-if="$slots.footer" class="login-form-footer"><slot name="footer" /></div>
        </div>
      </section>
    </main>

    <footer class="login-footer login-shell">
      <span>© {{ year }} {{ brand }}</span>
      <span>{{ t('home.aino.footerDescription') }}</span>
    </footer>
  </div>
</template>

<style scoped>
.aino-login {
  --login-paper: #fff;
  --login-ink: #092e48;
  --login-muted: #63676d;
  --login-line: #e5e7e9;
  --login-mint: #e9f9f3;
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  background: var(--login-paper);
  color: var(--login-ink);
  font-family: 'Figtree', -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif;
  -webkit-font-smoothing: antialiased;
  line-height: 1.65;
}
.aino-login--dark {
  --login-paper: #0c202e;
  --login-ink: #e8f0f4;
  --login-muted: #a9b9c3;
  --login-line: #304959;
  --login-mint: #143739;
}
.login-shell { width: min(1160px, calc(100% - 80px)); margin-inline: auto; }
.login-header { padding-top: 24px; }
.login-nav { display: flex; align-items: center; justify-content: space-between; gap: 24px; min-height: 60px; padding: 8px 10px 8px 20px; border-radius: 32px; background: #06283f; color: #fff; }
.login-brand { display: inline-flex; align-items: center; gap: 9px; min-width: 0; font-size: 25px; font-weight: 750; line-height: 1.15; letter-spacing: -1px; }
.login-brand span { overflow-wrap: anywhere; }
.login-brand img { flex: none; border-radius: 8px; object-fit: contain; }
.login-nav-actions { display: flex; gap: 8px; align-items: center; flex: none; }
.login-theme { display: grid; place-items: center; width: 34px; height: 36px; border-radius: 50%; color: #dce7ed; }
.login-theme:hover { background: #ffffff18; }
.login-locale :deep(> button) { color: #dce7ed; font-size: 11px; }
.login-locale :deep(> button:hover) { background: #ffffff18; }
.login-locale :deep(> button > span:first-child) { display: none; }
.login-locale :deep(> button > span:nth-child(2)) { display: inline; }
.login-home-link { display: inline-flex; align-items: center; gap: 10px; padding: 10px 19px; min-height: 42px; border-radius: 24px; color: #092e48; background: white; font-size: 13px; font-weight: 550; }
.aino-login :is(a, button):focus-visible { outline: 3px solid #19b8a6; outline-offset: 4px; }
.login-main { flex: 1; display: grid; grid-template-columns: 1fr 1fr; align-items: center; gap: 60px; padding-block: 50px; }
.login-story { border-radius: 20px; overflow: hidden; background: var(--login-mint); }
.login-story-copy { padding: 36px 40px 32px; }
.login-story-copy > img { border-radius: 12px; margin-bottom: 25px; }
.login-story h2 { margin: 0; font-size: clamp(32px, 3.25vw, 44px); line-height: 1.25; letter-spacing: -.045em; font-weight: 560; text-wrap: balance; }
.login-story p { margin: 20px 0 23px; color: var(--login-muted); font-size: 14px; line-height: 1.9; max-width: 380px; }
.login-story-caption { display: flex; align-items: center; flex-wrap: wrap; gap: 14px; color: var(--login-muted); font-size: 11px; }
.login-story-caption span + span::before { content: '/'; color: #9bb3b0; margin-right: 14px; }
.login-art { min-height: 190px; display: grid; place-items: center; isolation: isolate; position: relative; background: #12bcb8; }
.login-art-background { position: absolute; inset: 0; height: 100%; width: 100%; object-fit: cover; z-index: -1; }
.login-art-brand { display: flex; align-items: center; gap: 15px; padding: 24px 42px; border-radius: 18px; background: #fff; box-shadow: 0 8px 35px #03446120; }
.login-art-brand > span { font-size: 42px; font-weight: 750; letter-spacing: -2px; color: #092e48; line-height: 1; }
.login-brand-dot { color: #ffc63d; }
.login-form-region { min-width: 0; padding: 20px 22px; }
.login-form-content { max-width: 380px; margin-inline: auto; }
.login-form-footer { margin-top: 26px; font-size: 13px; text-align: center; }
.login-footer { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 12px 24px; padding-block: 22px; border-top: 1px solid var(--login-line); color: var(--login-muted); font-size: 11px; }
.login-form-content :deep(.login-heading) { margin-bottom: 30px; }
.login-form-content :deep(h1) { color: var(--login-ink); font-size: 32px; font-weight: 560; letter-spacing: -.045em; line-height: 1.3; text-align: left; }
.login-form-content :deep(h2) { color: var(--login-ink); font-size: 28px; font-weight: 560; letter-spacing: -.035em; line-height: 1.3; }
.login-form-content :deep(.text-center:not(.login-heading) h1) { text-align: center; }
.login-form-content :deep(.login-heading > p) { color: var(--login-muted); text-align: left; font-size: 14px; margin-top: 12px; }
.login-form-content :deep(.input-label) { color: var(--login-ink); font-size: 13px; font-weight: 550; margin-bottom: 9px; }
.login-form-content :deep(.input) { min-height: 48px; border-radius: 10px; border: 1px solid var(--login-line); box-shadow: none; background: var(--login-paper); color: var(--login-ink); font-size: 14px; transition: border-color .2s, box-shadow .2s; }
.login-form-content :deep(.input:focus) { border-color: #19b8a6; outline: none; box-shadow: 0 0 0 3px #19b8a61a; }
.login-form-content :deep(.input-error) { border-color: #ef4444; }
.login-form-content :deep(.input:disabled) { opacity: .65; }
.login-form-content :deep(.btn-primary) { min-height: 48px; border: 0; border-radius: 24px; background: #092e48; color: #fff; box-shadow: none; font-size: 14px; font-weight: 550; transition: background .2s; }
.login-form-content :deep(.btn-primary:hover:not(:disabled)) { background: #124962; }
.aino-login--dark .login-form-content :deep(.btn-primary) { background: #e8f0f4; color: #092e48; }
.aino-login--dark .login-form-content :deep(.btn-primary:hover:not(:disabled)) { background: #cbdde7; }
.login-form-content :deep(.btn-primary:disabled) { opacity: .5; cursor: not-allowed; }
.login-form-content :deep(.btn-primary .animate-spin) { color: inherit; }
.login-form-content :deep(.btn-secondary) { border-color: var(--login-line); background: var(--login-paper); color: var(--login-ink); min-height: 46px; border-radius: 24px; box-shadow: none; }
.login-form-content :deep(a) { color: var(--login-ink); text-underline-offset: 4px; }
.login-form-content :deep(a:hover) { text-decoration: underline; }
@media (max-width: 950px) {
  .login-shell { width: calc(100% - 48px); }
  .login-main { gap: 26px; }
  .login-story-copy { padding: 30px; }
  .login-story h2 { font-size: 34px; }
  .login-form-region { padding-inline: 12px; }
}
@media (max-width: 720px) {
  .login-header { padding-top: 14px; }
  .login-header.login-shell { width: calc(100% - 24px); }
  .login-nav { gap: 8px; padding-left: 14px; min-height: 56px; }
  .login-brand { font-size: 20px; gap: 7px; }
  .login-brand img { width: 24px; height: 24px; }
  .login-nav-actions { gap: 1px; }
  .login-home-link { min-height: 36px; padding: 8px 13px; font-size: 12px; }
  .login-home-link svg { display: none; }
  .login-locale :deep(> button) { gap: 2px; padding-inline: 5px; }
  .login-theme { width: 29px; }
  .login-main { display: flex; flex-direction: column; justify-content: center; padding-block: 52px; }
  .login-story { display: none; }
  .login-form-region { width: 100%; padding: 0; }
  .login-form-content { max-width: 400px; }
  .login-form-content :deep(h1) { font-size: 30px; }
  .login-footer { justify-content: center; text-align: center; }
}
@media (max-width: 360px) {
  .login-home-link { width: 36px; padding: 8px; justify-content: center; }
  .login-home-link > span { display: none; }
  .login-home-link svg { display: block; }
}
@media (prefers-reduced-motion: reduce) {
  .aino-login *, .aino-login :deep(*) { transition: none !important; animation: none !important; }
}
</style>
