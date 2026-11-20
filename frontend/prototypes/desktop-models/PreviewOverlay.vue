<template>
  <dialog ref="element" class="preview-overlay" :class="`overlay-${variant}`" :aria-label="label" @cancel.prevent="$emit('close')" @click="backdrop">
    <div class="overlay-content"><slot /></div>
  </dialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
const props = withDefaults(defineProps<{ open: boolean; label: string; variant?: 'drawer' | 'modal' }>(), { variant: 'modal' })
const emit = defineEmits<{ close: [] }>()
const element = ref<HTMLDialogElement>()
watch(() => props.open, open => {
  if (open && !element.value?.open) element.value?.showModal()
  else if (!open && element.value?.open) element.value.close()
}, { flush: 'post' })
function backdrop(event: MouseEvent): void {
  if (event.target !== element.value) return
  const box = element.value.getBoundingClientRect()
  if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) emit('close')
}
onBeforeUnmount(() => element.value?.close())
</script>
