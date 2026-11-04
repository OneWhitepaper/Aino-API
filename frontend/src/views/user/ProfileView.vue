<template>
  <AppLayout>
    <div
      data-testid="profile-shell"
      class="user-page space-y-6"
    >
      <UserPageHeader :title="t('userPages.profileTitle')" :description="t('userPages.profileDescription')" />
      <div class="user-profile-layout">
      <UserSectionNav v-model="activeSection" :items="sections" :label="t('userPages.profileTitle')" />
      <div class="user-profile-content flex flex-col gap-6">
      <ProfileInfoCard
        v-show="activeSection === 'basics' || activeSection === 'bindings'"
        :section="activeSection === 'bindings' ? 'bindings' : 'basics'"
        :user="user"
        :linuxdo-enabled="linuxdoOAuthEnabled"
        :dingtalk-enabled="dingtalkOAuthEnabled"
        :oidc-enabled="oidcOAuthEnabled"
        :oidc-provider-name="oidcOAuthProviderName"
        :wechat-enabled="wechatOAuthEnabled"
        :wechat-open-enabled="wechatOAuthOpenEnabled"
        :wechat-mp-enabled="wechatOAuthMPEnabled"
      />

      <div
        v-if="contactInfo"
        class="card border-primary-200 bg-primary-50 p-6 dark:bg-primary-900/20"
      >
        <div class="flex items-center gap-4">
          <div class="rounded-xl bg-primary-100 p-3 text-primary-600">
            <Icon name="chat" size="lg" />
          </div>
          <div>
            <h3 class="font-semibold text-primary-800 dark:text-primary-200">
              {{ t('common.contactSupport') }}
            </h3>
            <p class="text-sm font-medium">{{ contactInfo }}</p>
          </div>
        </div>
      </div>

      <div v-show="activeSection === 'security'" class="space-y-6">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('userPages.securityHint') }}</p>
        <ProfilePasswordForm />
        <ProfileTotpCard />
        <ProfilePasskeyCard :enabled="passkeyEnabled" />
      </div>

      <ProfileBalanceNotifyCard
        v-if="user && balanceLowNotifyEnabled"
        v-show="activeSection === 'notifications'"
        :enabled="user.balance_notify_enabled ?? true"
        :threshold="user.balance_notify_threshold"
        :extra-emails="user.balance_notify_extra_emails ?? []"
        :system-default-threshold="systemDefaultThreshold"
        :user-email="user.email"
      />

      </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon } from '@/components/icons'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserPageHeader from '@/components/user/workspace/UserPageHeader.vue'
import UserSectionNav from '@/components/user/workspace/UserSectionNav.vue'
import ProfileBalanceNotifyCard from '@/components/user/profile/ProfileBalanceNotifyCard.vue'
import ProfileInfoCard from '@/components/user/profile/ProfileInfoCard.vue'
import ProfilePasswordForm from '@/components/user/profile/ProfilePasswordForm.vue'
import ProfileTotpCard from '@/components/user/profile/ProfileTotpCard.vue'
import ProfilePasskeyCard from '@/components/user/profile/ProfilePasskeyCard.vue'
import { isWeChatWebOAuthEnabled } from '@/api/auth'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const user = computed(() => authStore.user)

const contactInfo = ref('')
const balanceLowNotifyEnabled = ref(false)
const activeSection = ref('basics')
const sections = computed(() => [
  { value: 'basics', label: t('userPages.basics') },
  { value: 'bindings', label: t('userPages.bindings') },
  { value: 'security', label: t('userPages.security') },
  ...(balanceLowNotifyEnabled.value ? [{ value: 'notifications', label: t('userPages.notifications') }] : []),
])
const systemDefaultThreshold = ref(0)
const linuxdoOAuthEnabled = ref(false)
const dingtalkOAuthEnabled = ref(false)
const wechatOAuthEnabled = ref(false)
const wechatOAuthOpenEnabled = ref<boolean | undefined>(undefined)
const wechatOAuthMPEnabled = ref<boolean | undefined>(undefined)
const oidcOAuthEnabled = ref(false)
const oidcOAuthProviderName = ref('OIDC')
const passkeyEnabled = ref(false)

onMounted(async () => {
  const profileRefresh = authStore.refreshUser().catch((error) => {
    console.error('Failed to refresh profile:', error)
  })

  const settingsLoad = appStore.fetchPublicSettings()
    .then((settings) => {
      if (!settings) {
        return
      }
      contactInfo.value = settings.contact_info || ''
      balanceLowNotifyEnabled.value = settings.balance_low_notify_enabled ?? false
      systemDefaultThreshold.value = settings.balance_low_notify_threshold ?? 0
      linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled ?? false
      dingtalkOAuthEnabled.value = settings.dingtalk_oauth_enabled ?? false
      wechatOAuthEnabled.value = isWeChatWebOAuthEnabled(settings)
      wechatOAuthOpenEnabled.value = typeof settings.wechat_oauth_open_enabled === 'boolean'
        ? settings.wechat_oauth_open_enabled
        : undefined
      wechatOAuthMPEnabled.value = typeof settings.wechat_oauth_mp_enabled === 'boolean'
        ? settings.wechat_oauth_mp_enabled
        : undefined
      oidcOAuthEnabled.value = settings.oidc_oauth_enabled ?? false
      oidcOAuthProviderName.value = settings.oidc_oauth_provider_name || 'OIDC'
      passkeyEnabled.value = settings.passkey_enabled === true
    })
    .catch((error) => {
      console.error('Failed to load settings:', error)
    })

  await Promise.all([profileRefresh, settingsLoad])
})
</script>
