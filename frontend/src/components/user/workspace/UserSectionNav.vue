<script setup lang="ts">
const props = defineProps<{ modelValue: string; label: string; items: Array<{ value: string; label: string }> }>()
const emit = defineEmits<{ (event: 'update:modelValue', value: string): void }>()
function move(event: KeyboardEvent, index: number) {
  const offset = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
  if (!offset && event.key !== 'Home' && event.key !== 'End') return
  event.preventDefault()
  const target = event.key === 'Home' ? 0 : event.key === 'End' ? props.items.length - 1 : (index + offset + props.items.length) % props.items.length
  emit('update:modelValue', props.items[target].value)
  const buttons = (event.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLButtonElement>('button')
  buttons?.[target]?.focus()
}
</script>

<template>
  <nav class="user-section-nav" :aria-label="label">
    <button v-for="(item, index) in items" :key="item.value" type="button" :aria-pressed="modelValue === item.value" :class="{ 'is-current': modelValue === item.value }" @click="emit('update:modelValue', item.value)" @keydown="move($event, index)">{{ item.label }}</button>
  </nav>
</template>
