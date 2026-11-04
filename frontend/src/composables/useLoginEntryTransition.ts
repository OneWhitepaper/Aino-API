import { onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'

/** Animate the destination once after leaving a sign-in flow successfully. */
export function useLoginEntryTransition(isAuthenticated: () => boolean) {
  const router = useRouter()
  const entering = ref(false)
  const removeHook = router.afterEach((to, from, failure) => {
    if (failure) return
    const fromSignIn = from.path === '/login' || from.path.startsWith('/auth/')
    entering.value = fromSignIn && to.meta.requiresAuth !== false && isAuthenticated()
  })
  onBeforeUnmount(removeHook)

  function finish() {
    entering.value = false
  }

  return { entering, finish }
}
