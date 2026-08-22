import { describe, expect, it } from 'vitest'

import {
  PERSONAL_ACCOUNT_PLATFORM_IDS,
  PERSONAL_ACCOUNT_PROVIDERS
} from '../personalProviders'

describe('Personal account providers', () => {
  it('exposes Antigravity as the fourth peer provider', () => {
    expect(PERSONAL_ACCOUNT_PROVIDERS.map((provider) => provider.id)).toEqual([
      'openai',
      'gemini',
      'anthropic',
      'antigravity'
    ])
    expect(PERSONAL_ACCOUNT_PROVIDERS.at(-1)?.label).toContain('Experimental')
    expect(PERSONAL_ACCOUNT_PLATFORM_IDS.has('antigravity')).toBe(true)
  })
})
