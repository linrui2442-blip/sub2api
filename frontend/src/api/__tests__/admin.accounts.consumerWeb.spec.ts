import { beforeEach, describe, expect, it, vi } from 'vitest'

const { put } = vi.hoisted(() => ({ put: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { put } }))

import { updateConsumerWebCredentials } from '@/api/admin/accounts'

describe('consumer web account credentials API', () => {
  beforeEach(() => {
    put.mockReset()
    put.mockResolvedValue({
      data: {
        consumer_web_credentials_present: true,
        has_secure_1psid: true,
        has_secure_1psidts: true
      }
    })
  })

  it.each([2, 5])('uses the selected account id %s without a global target', async (accountId) => {
    await updateConsumerWebCredentials(accountId, { secure_1psidts: 'synthetic-psidts' })

    expect(put).toHaveBeenCalledWith(
      `/admin/accounts/${accountId}/consumer-web-credentials`,
      { secure_1psidts: 'synthetic-psidts' }
    )
  })
})
