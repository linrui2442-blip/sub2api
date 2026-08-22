import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

describe('Personal startup network boundary', () => {
  const here = dirname(fileURLToPath(import.meta.url))

  it('does not mount cloud update checks from the sidebar', () => {
    const source = readFileSync(resolve(here, '../AppSidebar.vue'), 'utf8')
    expect(source).not.toContain('VersionBadge')
    expect(source).not.toContain('fetchVersion(')
  })

  it('does not request removed SaaS settings from account editors', () => {
    for (const file of ['../../account/CreateAccountModal.vue', '../../account/EditAccountModal.vue']) {
      const source = readFileSync(resolve(here, file), 'utf8')
      expect(source).not.toContain('getWebSearchEmulationConfig(')
    }
    const quotaState = readFileSync(resolve(here, '../../../composables/useQuotaNotifyState.ts'), 'utf8')
    expect(quotaState).not.toContain('.getSettings(')
  })
})
