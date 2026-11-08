import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ImageQualityMappingEditor from '../ImageQualityMappingEditor.vue'
import type { ImageQualityMappingRow } from '../imageQualityMapping'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('ImageQualityMappingEditor', () => {
  it('starts collapsed and supports adding, changing and removing an explicit mapping', async () => {
    const wrapper = mount(defineComponent({
      components: { ImageQualityMappingEditor },
      setup: () => ({ rows: ref<ImageQualityMappingRow[]>([]) }),
      template: '<ImageQualityMappingEditor v-model:rows="rows" />'
    }))
    expect(wrapper.get('details').attributes('open')).toBeUndefined()
    await wrapper.get('[data-testid="image-quality-add"]').trigger('click')
    await wrapper.get('[data-testid="image-quality-model"]').setValue('Nano Banana 2')
    await wrapper.get('[data-testid="image-quality-upstream"]').setValue('2k')
    await wrapper.get('[data-testid="image-quality-requested"]').setValue('high')
    expect(wrapper.getComponent(ImageQualityMappingEditor).props('rows')).toEqual([
      { model: 'Nano Banana 2', requestedQuality: 'high', upstreamQuality: '2k' }
    ])
    await wrapper.get('[data-testid="image-quality-remove"]').trigger('click')
    expect(wrapper.findAll('[data-testid="image-quality-row"]')).toHaveLength(0)
    expect(wrapper.getComponent(ImageQualityMappingEditor).props('rows')).toEqual([])
  })
})
