import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue'), 'utf8')

describe('Personal AppSidebar', () => {
  it('exposes the private gateway control plane', () => {
    for (const path of ['/admin/accounts', '/admin/users', '/admin/groups', '/admin/proxies', '/admin/settings', '/admin/audit-logs']) {
      expect(source).toContain(path)
    }
  })

  it('exposes member access without SaaS pages', () => {
    for (const path of ['/keys', '/usage', '/profile']) expect(source).toContain(path)
    for (const path of ['/admin/billing', '/admin/subscriptions', '/admin/redeem']) expect(source).not.toContain(path)
  })
})
