import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { updateCredentials, showSuccess, showError } = vi.hoisted(() => ({
  updateCredentials: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { accounts: { updateConsumerWebCredentials: updateCredentials } }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import ConsumerWebCredentialsSection from '../ConsumerWebCredentialsSection.vue'

function account(id = 2, platform = 'antigravity', type = 'oauth') {
  return {
    id,
    platform,
    type,
    credentials: {},
    credentials_status: { has_secure_1psid: false, has_secure_1psidts: false }
  } as any
}

function mountSection(value = account()) {
  return mount(ConsumerWebCredentialsSection, { props: { account: value } })
}

describe('ConsumerWebCredentialsSection', () => {
  beforeEach(() => {
    updateCredentials.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    updateCredentials.mockResolvedValue({
      consumer_web_credentials_present: true,
      has_secure_1psid: true,
      has_secure_1psidts: true
    })
  })

  it('shows only for an Antigravity OAuth account and never prefills secrets', () => {
    const eligible = mountSection()
    expect(eligible.find('[data-testid="consumer-web-credentials-section"]').exists()).toBe(true)
    expect(eligible.get('[data-testid="consumer-web-psid-input"]').attributes('type')).toBe('password')
    expect(eligible.get('[data-testid="consumer-web-psidts-input"]').attributes('type')).toBe('password')
    expect((eligible.get('[data-testid="consumer-web-psid-input"]').element as HTMLInputElement).value).toBe('')
    expect((eligible.get('[data-testid="consumer-web-psidts-input"]').element as HTMLInputElement).value).toBe('')
    expect(mountSection(account(5, 'openai', 'oauth')).find('[data-testid="consumer-web-credentials-section"]').exists()).toBe(false)
    expect(mountSection(account(9, 'antigravity', 'apikey')).find('[data-testid="consumer-web-credentials-section"]').exists()).toBe(false)
  })

  it('does not send when both fields are empty', async () => {
    const wrapper = mountSection()
    await wrapper.get('[data-testid="consumer-web-save"]').trigger('click')
    expect(updateCredentials).not.toHaveBeenCalled()
  })

  it.each([
    ['consumer-web-psid-input', { secure_1psid: 'synthetic-psid' }],
    ['consumer-web-psidts-input', { secure_1psidts: 'synthetic-psidts' }]
  ])('sends only the populated field from %s', async (testId, expectedPatch) => {
    const wrapper = mountSection(account(5))
    await wrapper.get(`[data-testid="${testId}"]`).setValue(Object.values(expectedPatch)[0])
    await wrapper.get('[data-testid="consumer-web-save"]').trigger('click')
    await flushPromises()
    expect(updateCredentials).toHaveBeenCalledWith(5, expectedPatch)
  })

  it('sends a full pair, consumes only status flags, and clears both fields on success', async () => {
    const wrapper = mountSection()
    const psid = wrapper.get('[data-testid="consumer-web-psid-input"]')
    const psidts = wrapper.get('[data-testid="consumer-web-psidts-input"]')
    await psid.setValue('synthetic-psid')
    await psidts.setValue('synthetic-psidts')
    await wrapper.get('[data-testid="consumer-web-save"]').trigger('click')
    await flushPromises()
    expect(updateCredentials).toHaveBeenCalledWith(2, {
      secure_1psid: 'synthetic-psid',
      secure_1psidts: 'synthetic-psidts'
    })
    expect((psid.element as HTMLInputElement).value).toBe('')
    expect((psidts.element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-testid="consumer-web-psid-status"]').text()).toContain('configured')
    expect(wrapper.get('[data-testid="consumer-web-psidts-status"]').text()).toContain('configured')
  })

  it('clears secrets and exposes only a generic error on failure', async () => {
    updateCredentials.mockRejectedValue(new Error('request debug body synthetic-psid'))
    const wrapper = mountSection()
    const input = wrapper.get('[data-testid="consumer-web-psid-input"]')
    await input.setValue('synthetic-psid')
    await wrapper.get('[data-testid="consumer-web-save"]').trigger('click')
    await flushPromises()
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(showError).toHaveBeenCalledWith('admin.accounts.consumerWebCredentials.saveFailed')
    expect(JSON.stringify(showError.mock.calls)).not.toContain('request debug body')
  })
})
