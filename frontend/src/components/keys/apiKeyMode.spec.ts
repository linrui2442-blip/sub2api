import { describe, expect, it } from 'vitest'
import { getApiKeyMode } from './apiKeyMode'

describe('getApiKeyMode', () => {
  it('resolves an ungrouped Personal key as Unified', () => {
    expect(getApiKeyMode({ group_id: null }, true)).toBe('UNIFIED')
  })

  it('resolves a grouped Personal key as Group-Pinned', () => {
    expect(getApiKeyMode({ group_id: 7 }, true)).toBe('GROUP_PINNED')
  })

  it('preserves the legacy ungrouped non-Personal state', () => {
    expect(getApiKeyMode({ group_id: null }, false)).toBeNull()
  })
})
