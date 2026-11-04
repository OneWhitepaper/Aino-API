<script setup lang="ts">
defineProps<{ title: string; description: string }>()
</script>

<template>
  <header class="user-page-heading">
    <div class="min-w-0"><h2>{{ title }}</h2><p>{{ description }}</p></div>
    <div v-if="$slots.default" class="user-page-actions"><slot /></div>
  </header>
</template>
