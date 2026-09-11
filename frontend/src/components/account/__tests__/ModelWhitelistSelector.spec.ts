import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { copyToClipboard, syncUpstreamModels, showError, showSuccess, showInfo } = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  syncUpstreamModels: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: { count?: number }) => key === 'common.copy' ? '复制' : key === 'admin.accounts.discoveredUpstreamModels' ? `Upstream discovered models (${params?.count})` : key
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo
  })
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModels,
    syncUpstreamModelsPreview: vi.fn()
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

function mountSelector(props: Record<string, unknown> = {}) {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai',
      ...props
    },
    global: {
      stubs: {
        ModelIcon: true
      }
    }
  })
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error(`Model row not found: ${modelId}`)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  beforeEach(() => {
    copyToClipboard.mockClear()
    syncUpstreamModels.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')

    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('reconciles successful authoritative sync instead of retaining stale direct models', async () => {
    syncUpstreamModels.mockResolvedValue({ models: ['live-b', 'live-a', 'live-a'], absence_authoritative: true })
    const wrapper = mountSelector({ accountId: 3, modelValue: ['live-a', 'stale-model'] })

    await wrapper.get('button.border-emerald-200').trigger('click')
    await flushPromises()

    expect(syncUpstreamModels).toHaveBeenCalledWith(3)
    expect(wrapper.emitted('update:modelValue')).toEqual([[['live-b', 'live-a']]])
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsSuccess')
  })

  it('keeps current supported entries and reports matching authoritative count', async () => {
    syncUpstreamModels.mockResolvedValue({ models: ['live-a', 'live-b'], absence_authoritative: true })
    const wrapper = mountSelector({ accountId: 3, modelValue: ['live-b', 'live-a'] })

    await wrapper.get('button.border-emerald-200').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['live-a', 'live-b']]])
    expect(showInfo).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsNoChanges')
  })

  it('reconciles an authoritative empty membership for a saved account', async () => {
    syncUpstreamModels.mockResolvedValue({ models: [], absence_authoritative: true })
    const wrapper = mountSelector({ accountId: 3, modelValue: ['stale-model'] })

    await wrapper.get('button.border-emerald-200').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
    expect(showInfo).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsEmpty')
  })

  it('shows six discovered OpenAI models without removing configured models absent from discovery', async () => {
    const models = ['a', 'b', 'c', 'd', 'e', 'f']
    syncUpstreamModels.mockResolvedValue({ models, absence_authoritative: false })
    const configured = ['configured-unknown', 'configured-alias', 'a']
    const wrapper = mountSelector({ accountId: 3, modelValue: configured })
    await wrapper.get('button.border-emerald-200').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="discovered-models"]').text()).toContain('Upstream discovered models (6)')
    expect(wrapper.findAll('[data-testid="discovered-model"]').map(model => model.text())).toEqual(models)
    expect(wrapper.emitted('update:modelValue')).toEqual([[['configured-unknown', 'configured-alias', ...models]]])
    expect(configured).toEqual(['configured-unknown', 'configured-alias', 'a'])
  })

  it.each([false, undefined])('preserves configured selections for empty discovery with authority %s', async (authority) => {
    syncUpstreamModels.mockResolvedValue({ models: [], absence_authoritative: authority })
    const wrapper = mountSelector({ accountId: 3, modelValue: ['configured-unknown'] })
    await wrapper.get('button.border-emerald-200').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('[data-testid="discovered-models"]').text()).toContain('(0)')
  })

  it('preserves selections and previous discovery when syncing fails', async () => {
    syncUpstreamModels.mockResolvedValueOnce({ models: ['a'], absence_authoritative: false })
    const wrapper = mountSelector({ accountId: 3, modelValue: ['configured-unknown'] })
    await wrapper.get('button.border-emerald-200').trigger('click')
    await flushPromises()
    syncUpstreamModels.mockRejectedValueOnce(new Error('Upstream unavailable'))
    await wrapper.get('button.border-emerald-200').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="discovered-model"]').map(model => model.text())).toEqual(['a'])
    expect(showError).toHaveBeenCalled()
  })
})
