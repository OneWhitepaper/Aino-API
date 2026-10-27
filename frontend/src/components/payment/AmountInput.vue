<template>
  <div class="recharge-amount-input">
    <div class="recharge-section-heading">
      <h3>{{ t('payment.recharge.selectAmount') }}</h3>
      <span class="recharge-currency">{{ t('payment.recharge.currency', { currency: paymentCurrency }) }}</span>
    </div>
    <p class="recharge-section-description">{{ t('payment.recharge.amountHint') }}</p>
    <div :id="amountInputId + '-presets'" class="recharge-amount-grid" :aria-label="t('payment.quickAmounts')">
      <button
        v-for="amt in displayedAmounts"
        :key="amt"
        type="button"
        class="recharge-amount"
        :class="{ 'is-selected': modelValue === amt, 'has-bonus': quoteFor(amt).percent > 0 }"
        :aria-pressed="modelValue === amt"
        :aria-label="formatPaymentAmount(amt, currency)"
        :data-testid="`quick-amount-${amt}`"
        @click="selectAmount(amt)"
      >
        <span v-if="quoteFor(amt).percent > 0" class="recharge-bonus-badge" data-testid="quick-amount-bonus-badge">{{ badgeText(amt) }}</span>
        <span v-else-if="modelValue === amt" class="recharge-amount-check" aria-hidden="true"><Icon name="check" size="xs" /></span>
        <span class="recharge-amount-number"><span>{{ currencySymbol(currency) }}</span>{{ amt.toLocaleString() }}</span>
        <span v-if="showSecondLine" class="recharge-amount-credited" data-testid="quick-amount-credited">{{ secondLine(amt) }}</span>
      </button>
    </div>
    <div class="recharge-amount-toolbar">
      <label :for="amountInputId">{{ t('payment.customAmount') }}</label>
      <button
        v-if="filteredAmounts.length > 6"
        type="button"
        class="recharge-more-amounts"
        data-testid="toggle-quick-amounts"
        :aria-expanded="amountsExpanded"
        :aria-controls="amountInputId + '-presets'"
        @click="amountsExpanded = !amountsExpanded"
      >
        {{ t(amountsExpanded ? 'payment.recharge.fewerAmounts' : 'payment.recharge.moreAmounts') }}
        <Icon name="chevronDown" size="xs" :class="{ 'rotate-180': amountsExpanded }" />
      </button>
    </div>
    <div class="recharge-custom-input">
      <span data-testid="amount-currency-symbol" aria-hidden="true">{{ currencySymbol(currency) }}</span>
      <input :id="amountInputId" type="text" inputmode="decimal" :value="customText" :placeholder="placeholderText" @input="handleInput" />
      <span data-testid="amount-currency-code">{{ paymentCurrency }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RechargeBonusTier } from '@/types/payment'
import { formatRechargeBonusNumber, quoteRechargeBonus, type RechargeBonusMode } from '@/utils/rechargeBonus'
import { currencySymbol, formatPaymentAmount, normalizePaymentCurrency } from './currency'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  amounts?: number[]
  modelValue: number | null
  min?: number
  max?: number
  /** 充值优惠阶梯（按 min_amount 升序）；为空时不显示价签与第二行 */
  bonusTiers?: RechargeBonusTier[]
  /** 阶梯模式：bonus 赠金 / discount 折扣 */
  bonusMode?: RechargeBonusMode
  /** 充值倍率（1 支付币种 = multiplier USD），用于计算到账金额 */
  multiplier?: number
  /** 支付币种（折扣模式第二行实付金额的币种与精度） */
  currency?: string
}>(), {
  amounts: () => [10, 20, 50, 100, 200, 500, 1000, 2000, 5000],
  min: 0,
  max: 0,
  bonusTiers: () => [],
  bonusMode: 'bonus',
  multiplier: 1,
  currency: undefined,
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
}>()

const { t } = useI18n()

const customText = ref('')
const amountInputId = useId()
const amountsExpanded = ref(false)
const paymentCurrency = computed(() => normalizePaymentCurrency(props.currency))

// 0 = no limit
const filteredAmounts = computed(() =>
  props.amounts.filter((a) => (props.min <= 0 || a >= props.min) && (props.max <= 0 || a <= props.max))
)

const displayedAmounts = computed(() => amountsExpanded.value ? filteredAmounts.value : filteredAmounts.value.slice(0, 6))

// Keep an externally restored amount visible without changing the selection.
watch([() => props.modelValue, filteredAmounts], ([value]) => {
  if (filteredAmounts.value.findIndex(amount => amount === value) >= 6) amountsExpanded.value = true
}, { immediate: true })

const showSecondLine = computed(() => props.bonusTiers.length > 0)

function currencyDigits(): number {
  if (!props.currency) return 2
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency: props.currency }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

function quoteFor(amt: number) {
  return quoteRechargeBonus(props.bonusTiers, amt, {
    multiplier: props.multiplier,
    mode: props.bonusMode,
    currencyDigits: currencyDigits(),
  })
}

// 价签文案：赠金「+20%」，折扣「20% OFF」
function badgeText(amt: number): string {
  const percent = formatRechargeBonusNumber(quoteFor(amt).percent)
  return props.bonusMode === 'discount' ? `${percent}% OFF` : `+${percent}%`
}

function secondLine(amt: number): string {
  const quote = quoteFor(amt)
  if (props.bonusMode === 'discount') {
    return t('payment.rechargeBonus.payShort', { amount: formatPaymentAmount(quote.payBase, props.currency) })
  }
  return t('payment.rechargeBonus.creditedShort', { amount: '$' + quote.credited.toFixed(2) })
}

const placeholderText = computed(() => {
  if (props.min > 0 && props.max > 0) return `${props.min} - ${props.max}`
  if (props.min > 0) return `≥ ${props.min}`
  if (props.max > 0) return `≤ ${props.max}`
  return t('payment.enterAmount')
})

const AMOUNT_PATTERN = /^\d*(\.\d{0,2})?$/

function selectAmount(amt: number) {
  customText.value = String(amt)
  emit('update:modelValue', amt)
}

function handleInput(e: Event) {
  const input = e.target as HTMLInputElement
  const val = input.value
  if (!AMOUNT_PATTERN.test(val)) {
    input.value = customText.value
    return
  }
  customText.value = val
  if (val === '') {
    emit('update:modelValue', null)
    return
  }
  const num = parseFloat(val)
  if (!isNaN(num) && num > 0) {
    emit('update:modelValue', num)
  } else {
    emit('update:modelValue', null)
  }
}

watch(() => props.modelValue, (v) => {
  if (v !== null && String(v) !== customText.value) {
    customText.value = String(v)
  }
}, { immediate: true })
</script>
