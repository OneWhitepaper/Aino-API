<template>
  <dialog
    ref="element"
    class="dm-dialog dm-workspace"
    :class="{ 'dm-drawer': drawer }"
    :aria-label="label"
    @cancel.prevent="requestClose"
    @click="backdrop"
  >
    <div class="dm-dialog-content">
      <slot />
    </div>
  </dialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'

const props = withDefaults(defineProps<{ open: boolean; label: string; drawer?: boolean; busy?: boolean }>(), { drawer: false, busy: false })
const emit = defineEmits<{ close: [] }>()
const element = ref<HTMLDialogElement>()
let previouslyFocused: HTMLElement | null = null
watch([() => props.open, element], ([open, dialog]) => {
  if (!dialog) return
  if (open && !dialog.open) {
    previouslyFocused = document.activeElement as HTMLElement | null
    dialog.showModal()
  } else if (!open && dialog.open) {
    dialog.close()
    if (previouslyFocused?.isConnected) previouslyFocused.focus()
  }
}, { flush: 'post' })
function requestClose(): void { if (!props.busy) emit('close') }
function backdrop(event: MouseEvent): void {
  if (event.target !== element.value || !element.value) return
  const rect = element.value.getBoundingClientRect()
  if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) requestClose()
}
onBeforeUnmount(() => element.value?.close())
</script>
