<template>
  <div class="dm-field">
    <label :for="id">{{ label }}<span v-if="required" aria-hidden="true"> *</span>
    </label>
    <slot />
    <small
      v-if="error"
      :id="`${id}-hint`"
      class="dm-field-error"
    >{{ t(`admin.desktopModels.errors.${error}`) }}</small>
    <small v-else-if="hint" :id="`${id}-hint`">{{ hint }}</small>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ id: string; label: string; required?: boolean; error?: string; hint?: string }>()
const { t } = useI18n()
</script>
