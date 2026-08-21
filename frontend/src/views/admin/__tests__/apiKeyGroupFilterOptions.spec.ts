import { describe, expect, it } from 'vitest'
import { buildApiKeyGroupFilterOptions } from '../apiKeyGroupFilterOptions'
import type { AdminGroup } from '@/types'

const labels = {
  all: 'All',
  exclusive: 'Exclusive',
  public: 'Public',
  disabled: 'Disabled',
}

function g(partial: Partial<AdminGroup>): AdminGroup {
  return {
    id: 0,
    name: '',
    status: 'active',
    is_exclusive: false,
    ...partial,
  } as AdminGroup
}

describe('buildApiKeyGroupFilterOptions', () => {
  it('partitions active groups into exclusive and public sections', () => {
    const groups = [
      g({ id: 1, name: 'Excl', is_exclusive: true }),
      g({ id: 2, name: 'Pub', is_exclusive: false }),
    ]

    expect(buildApiKeyGroupFilterOptions(groups, labels)).toEqual([
      { value: null, label: 'All' },
      { value: -1, label: 'Exclusive', kind: 'group', disabled: true },
      { value: 1, label: 'Excl' },
      { value: -2, label: 'Public', kind: 'group', disabled: true },
      { value: 2, label: 'Pub' },
    ])
  })

  it('skips empty section headers', () => {
    const opts = buildApiKeyGroupFilterOptions(
      [g({ id: 2, name: 'Pub', is_exclusive: false })],
      labels,
    )
    expect(opts.find((o) => o.label === 'Exclusive')).toBeUndefined()
    expect(opts).toContainEqual({
      value: -2,
      label: 'Public',
      kind: 'group',
      disabled: true,
    })
  })

  it('places non-active groups in a separate disabled section', () => {
    const opts = buildApiKeyGroupFilterOptions([
      g({ id: 1, name: 'Active', is_exclusive: true }),
      g({ id: 2, name: 'Inactive', status: 'inactive' }),
    ], labels)

    expect(opts).toContainEqual({ value: 1, label: 'Active' })
    expect(opts).toContainEqual({ value: 2, label: 'Inactive' })
    expect(opts).toContainEqual({
      value: -3,
      label: 'Disabled',
      kind: 'group',
      disabled: true,
    })
  })

  it('returns only the all-option when there are no groups', () => {
    expect(buildApiKeyGroupFilterOptions([], labels)).toEqual([
      { value: null, label: 'All' },
    ])
  })
})
