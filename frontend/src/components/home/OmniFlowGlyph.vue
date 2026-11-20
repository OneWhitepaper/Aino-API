<script setup lang="ts">
defineProps<{ name: 'arrow' | 'east' | 'down' | 'search' | 'copy' | 'check' | 'menu' | 'chevron' | 'play' | 'close' | 'term' | 'reply' | 'grid' }>()
</script>

<template>
  <svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path v-if="name === 'arrow'" d="M4 12h15m-6-6 6 6-6 6" />
    <path v-else-if="name === 'east'" d="M6 18 18 6M7 6h11v11" />
    <path v-else-if="name === 'down'" d="M12 4v15m-6-6 6 6 6-6" />
    <template v-else-if="name === 'search'"><circle cx="10.8" cy="10.8" r="6.65" /><path d="m16 16 4.2 4.2" /></template>
    <template v-else-if="name === 'copy'"><rect x="8.2" y="8.2" width="11.6" height="12.5" rx="1.8" /><path d="M15.6 8V5.2c0-1-.7-1.8-1.7-1.8H5.8c-1 0-1.8.8-1.8 1.8v9c0 1 .8 1.8 1.8 1.8h2.4" /></template>
    <path v-else-if="name === 'check'" d="m4.5 12 5.1 5.1L20 6.7" />
    <path v-else-if="name === 'menu'" d="M3 8h18M3 16h18" />
    <path v-else-if="name === 'chevron'" d="m6.5 9 5.5 5.5L17.5 9" />
    <path v-else-if="name === 'play'" d="m7.5 4.5 12 7.5-12 7.5z" />
    <path v-else-if="name === 'close'" d="m6 6 12 12M18 6 6 18" />
    <path v-else-if="name === 'term'" d="m4 6 6 6-6 6m9 0h7" />
    <path v-else-if="name === 'reply'" d="m10 7-5 5 5 5m-5-5h9a5 5 0 0 0 5-5V4" />
    <path v-else d="m3 7 9-5 9 5-9 5zM3 12l9 5 9-5M3 17l9 5 9-5" />
  </svg>
</template>
