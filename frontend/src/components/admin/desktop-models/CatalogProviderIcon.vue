<template>
  <span
    class="dm-provider"
    :class="`dm-provider-${entry.platform}`"
    aria-hidden="true"
  >
    <img
      v-if="image"
      :src="image"
      alt=""
      width="24"
      height="24"
      decoding="async"
    />
    <PlatformIcon
      v-else
      :platform="entry.platform"
      size="md"
    />
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { DesktopModelEntry } from '@/api/admin/settings'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import openaiIcon from './assets/openai.svg'
const props = defineProps<{ entry: DesktopModelEntry }>()
const icons: Record<string, string> = { openai: openaiIcon, anthropic: '/omniflow-home/claude.svg', deepseek: '/omniflow-home/deepseek.svg', gemini: '/omniflow-home/gemini.svg' }
const image = computed(() => icons[props.entry.platform])
</script>
