import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/admin/account/ReAuthAccountModal.vue'),
  'utf8'
)

describe('ReAuthAccountModal Antigravity reauthorization guard', () => {
  it('requires explicit confirmation and identifies manual force reauthorization', () => {
    expect(source).toContain("window.confirm(t('admin.accounts.oauth.antigravity.forceReauthWarning'))")
    expect(source).toContain("generateAuthUrl(props.account.proxy_id, 'manual_force', props.account.id)")
  })
})
