<script setup lang="ts">
import AinoLoginLayout from './AinoLoginLayout.vue'
</script>

<template>
  <AinoLoginLayout>
    <slot />
    <template v-if="$slots.footer" #footer><slot name="footer" /></template>
  </AinoLoginLayout>
</template>
