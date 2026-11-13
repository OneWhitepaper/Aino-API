<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { displayAmount, sharedModes, validSpendLimit, type TeamMember, type MemberSettingsInput, type SharedMode, type FundingMode } from '@/api/teamFunding'
import FundingModeSelect from './FundingModeSelect.vue'
const props = defineProps<{ member: TeamMember; busy: boolean }>()
const emit = defineEmits<{ save: [input: MemberSettingsInput]; remove: [] }>()
const { t } = useI18n()
const unlimited = ref(true)
const limit = ref('')
const mode = ref<SharedMode | null>(null)
const suggested = ref<FundingMode | null>(null)
const invalid = ref(false)
watch(() => props.member, member => {
  unlimited.value = member.spend_limit === null
  limit.value = member.spend_limit ?? ''
  mode.value = member.shared_mode
  suggested.value = member.suggested_mode
}, { immediate: true })
function save() {
  invalid.value = !unlimited.value && !validSpendLimit(limit.value)
  if (!invalid.value) emit('save', { spend_limit: unlimited.value ? null : limit.value, shared_mode: mode.value, suggested_mode: suggested.value })
}
</script>
<template>
  <article class="rounded-xl border border-gray-200 p-4 dark:border-dark-600" :data-member="member.user_id">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div><h4 class="font-medium">{{ member.username }} <span class="text-xs text-gray-500">#{{ member.user_id }}</span></h4><p class="text-sm text-gray-500">{{ t(`teamFunding.status.${member.status}`) }}</p></div>
      <div class="flex gap-2">
        <button class="btn btn-secondary btn-sm" type="button" :disabled="busy || member.status === 'removed'" @click="emit('save', { status: member.status === 'active' ? 'paused' : 'active' })">{{ t(member.status === 'active' ? 'teamFunding.pause' : 'teamFunding.resume') }}</button>
        <button class="btn btn-ghost btn-sm text-red-600" type="button" :disabled="busy || member.status === 'removed'" @click="emit('remove')">{{ t('teamFunding.remove') }}</button>
      </div>
    </header>
    <p class="my-3 text-sm">{{ t('teamFunding.memberUsage', { spent: displayAmount(member.spent, t('teamFunding.credits')), reserved: displayAmount(member.reserved, t('teamFunding.credits')) }) }}</p>
    <form v-if="member.status !== 'removed'" class="space-y-4" @submit.prevent="save">
      <fieldset :disabled="busy" class="space-y-4">
        <div>
          <label :for="`unlimited-${member.user_id}`" class="flex items-center gap-2 text-sm"><input :id="`unlimited-${member.user_id}`" v-model="unlimited" type="checkbox" class="checkbox" />{{ t('teamFunding.unlimited') }}</label>
          <label v-if="!unlimited" :for="`limit-${member.user_id}`" class="mt-3 block space-y-2 text-sm"><span>{{ t('teamFunding.spendLimit') }}</span><input :id="`limit-${member.user_id}`" v-model="limit" class="input w-full" inputmode="decimal" :aria-invalid="invalid" /></label>
          <p class="mt-2 text-xs text-gray-500">{{ t('teamFunding.limitHelp') }}</p>
          <p v-if="invalid" role="alert" class="mt-2 text-sm text-red-600">{{ t('teamFunding.invalidLimit') }}</p>
        </div>
        <label :for="`member-mode-${member.user_id}`" class="block space-y-2 text-sm"><span>{{ t('teamFunding.memberSharedMode') }}</span><select :id="`member-mode-${member.user_id}`" v-model="mode" class="input w-full"><option :value="null">{{ t('teamFunding.inherit') }}</option><option v-for="item in sharedModes" :key="item" :value="item">{{ t(`teamFunding.sharedModes.${item}`) }}</option></select></label>
        <FundingModeSelect v-model="suggested" :id="`member-suggested-${member.user_id}`" :label="t('teamFunding.suggestedMode')" optional />
        <button class="btn btn-primary btn-sm" type="submit">{{ t('teamFunding.saveMember') }}</button>
      </fieldset>
    </form>
  </article>
</template>
