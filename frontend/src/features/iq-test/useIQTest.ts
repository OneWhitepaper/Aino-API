import { onBeforeUnmount, onMounted, ref } from 'vue'
import {
  cancelIQTest,
  discoverIQModels,
  getIQTest,
  getIQTestKeys,
  getIQTestOptions,
  listIQTests,
  startIQTest,
  type IQConnection,
  type IQKey,
  type IQModel,
  type IQOptions,
  type IQTest,
  type IQTestInput
} from '@/api/iqTest'

export function useIQTest() {
  const current = ref<IQTest | null>(null)
  const history = ref<IQTest[]>([])
  const hasMore = ref(false)
  const keys = ref<IQKey[]>([])
  const options = ref<IQOptions | null>(null)
  const models = ref<IQModel[]>([])
  const runningID = ref<string | null>(null)
  const loading = ref(true)
  const starting = ref(false)
  const cancelling = ref(false)
  const discovering = ref(false)
  const historyLoading = ref(false)
  const error = ref('')
  const modelsError = ref(false)
  const pollError = ref(false)
  const controller = new AbortController()
  let alive = true
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let modelController: AbortController | undefined
  let modelEpoch = 0
  let detailEpoch = 0
  let pendingAttempt: { fingerprint: string; id: string } | undefined

  function upsert(task: IQTest) {
    const index = history.value.findIndex((row) => row.id === task.id)
    if (index >= 0) history.value[index] = task
    else history.value.unshift(task)
    if (current.value?.id === task.id) current.value = task
  }
  function schedule(delay = 2000) {
    clearTimeout(pollTimer)
    if (alive && runningID.value)
      pollTimer = setTimeout(() => {
        void poll()
      }, delay)
  }
  async function poll() {
    const id = runningID.value
    if (!id || !alive) return
    try {
      const task = await getIQTest(id, controller.signal)
      if (!alive || runningID.value !== id) return
      pollError.value = false
      upsert(task)
      if (task.status !== 'running') runningID.value = null
      schedule()
    } catch {
      if (alive) {
        pollError.value = true
        schedule(5000)
      }
    }
  }
  async function open(task: IQTest) {
    const epoch = ++detailEpoch
    try {
      const detail = await getIQTest(task.id, controller.signal)
      if (alive && epoch === detailEpoch) {
        current.value = detail
        error.value = ''
        upsert(detail)
      }
    } catch {
      if (alive && epoch === detailEpoch) error.value = 'detail'
    }
  }
  async function loadHistory(more = false) {
    if (historyLoading.value) return
    historyLoading.value = true
    try {
      const page = await listIQTests(more ? history.value.length : 0, controller.signal)
      if (!alive) return
      history.value = more
        ? [...new Map([...history.value, ...page.items].map((row) => [row.id, row])).values()]
        : page.items
      hasMore.value = page.has_more
      const active = page.items.find((row) => row.status === 'running')
      if (active && !runningID.value) {
        runningID.value = active.id
        schedule()
      }
      if (!current.value && (active || page.items[0])) await open(active || page.items[0])
    } catch {
      if (alive) error.value = 'history'
    } finally {
      if (alive) historyLoading.value = false
    }
  }
  async function initialize() {
    loading.value = true
    await Promise.allSettled([
      loadHistory(),
      getIQTestKeys(controller.signal)
        .then((data) => {
          if (alive) keys.value = data
        })
        .catch(() => {
          if (alive) error.value = 'keys'
        }),
      getIQTestOptions(controller.signal)
        .then((data) => {
          if (alive) options.value = data
        })
        .catch(() => {
          if (alive) error.value = 'options'
        })
    ])
    if (alive) loading.value = false
  }
  async function start(input: IQTestInput) {
    if (!alive || starting.value || runningID.value) return
    starting.value = true
    error.value = ''
    ++detailEpoch
    const fingerprint = JSON.stringify(input)
    if (!pendingAttempt || pendingAttempt.fingerprint !== fingerprint)
      pendingAttempt = { fingerprint, id: crypto.randomUUID() }
    try {
      const task = await startIQTest({ ...input, request_id: pendingAttempt.id }, controller.signal)
      if (!alive) return
      pendingAttempt = undefined
      current.value = task
      upsert(task)
      runningID.value = task.status === 'running' ? task.id : null
      schedule()
    } catch (cause) {
      if (alive) {
        const reason = cause && typeof cause === 'object' && 'reason' in cause ? String(cause.reason) : ''
        if (reason === 'IQ_TEST_BUSY') {
          await loadHistory()
          if (alive) error.value = 'busy'
        } else {
          const messages: Record<string, string> = {
            IQ_TEST_INVALID: 'settings',
            IQ_TEST_KEY_NOT_FOUND: 'keyUnavailable',
            IQ_TEST_KEY_UNAVAILABLE: 'keyUnavailable',
            IQ_TEST_CAPACITY: 'capacity',
            IQ_TEST_IDEMPOTENCY: 'requestConflict'
          }
          error.value = messages[reason] || 'start'
          if (reason === 'IQ_TEST_IDEMPOTENCY') pendingAttempt = undefined
        }
      }
    } finally {
      if (alive) starting.value = false
    }
  }
  async function cancel() {
    const id = runningID.value
    if (!id || cancelling.value) return
    cancelling.value = true
    try {
      const task = await cancelIQTest(id, controller.signal)
      if (!alive) return
      upsert(task)
      runningID.value = task.status === 'running' ? task.id : null
      schedule()
    } catch {
      if (alive) error.value = 'cancel'
    } finally {
      if (alive) cancelling.value = false
    }
  }
  function clearModels() {
    ++modelEpoch
    modelController?.abort()
    models.value = []
    modelsError.value = false
    discovering.value = false
  }
  async function discover(connection: IQConnection) {
    clearModels()
    const epoch = modelEpoch
    modelController = new AbortController()
    discovering.value = true
    try {
      const data = await discoverIQModels(connection, modelController.signal)
      if (alive && epoch === modelEpoch) models.value = data
    } catch {
      if (alive && epoch === modelEpoch) modelsError.value = true
    } finally {
      if (alive && epoch === modelEpoch) discovering.value = false
    }
  }
  onMounted(initialize)
  onBeforeUnmount(() => {
    alive = false
    ++detailEpoch
    pendingAttempt = undefined
    clearTimeout(pollTimer)
    controller.abort()
    modelController?.abort()
  })
  return {
    current,
    history,
    hasMore,
    keys,
    options,
    models,
    runningID,
    loading,
    starting,
    cancelling,
    discovering,
    historyLoading,
    error,
    modelsError,
    pollError,
    start,
    cancel,
    open,
    loadHistory,
    discover,
    clearModels,
    initialize
  }
}
