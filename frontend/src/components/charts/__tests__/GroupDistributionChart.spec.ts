import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import GroupDistributionChart from '../GroupDistributionChart.vue'

const messages: Record<string, string> = {
  'admin.dashboard.groupDistribution': 'Group Distribution',
  'admin.dashboard.group': 'Group',
  'admin.dashboard.noGroup': 'No Group',
  'admin.dashboard.requests': 'Requests',
  'admin.dashboard.tokens': 'Tokens',
  'admin.dashboard.actual': 'Actual',
  'admin.dashboard.accountCost': 'Account Cost',
  'admin.dashboard.standard': 'Standard',
  'admin.dashboard.metricTokens': 'By Tokens',
  'admin.dashboard.metricActualCost': 'By Actual Cost',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Doughnut: {
    props: ['data'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

describe('GroupDistributionChart', () => {
  const groupStats = [
    {
      group_id: 1,
      group_name: 'group-a',
      requests: 9,
      total_tokens: 1200,
    },
    {
      group_id: 2,
      group_name: 'group-b',
      requests: 4,
      total_tokens: 600,
    },
  ]

  it('uses total_tokens and token ordering by default', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['group-a', 'group-b'])
    expect(chartData.datasets[0].data).toEqual([1200, 600])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('group-a')
    expect(rows[1].text()).toContain('group-b')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'group-a',
      raw: 1200,
      dataset: { data: [1200, 600] },
    })
    expect(label).toBe('group-a: 1.20K (66.7%)')
  })

  it('uses requests and reorders rows in request mode', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
        metric: 'requests',
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    expect(chartData.labels).toEqual(['group-a', 'group-b'])
    expect(chartData.datasets[0].data).toEqual([9, 4])

    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].text()).toContain('group-a')
    expect(rows[1].text()).toContain('group-b')

    const options = (wrapper.vm as any).$?.setupState.doughnutOptions
    const label = options.plugins.tooltip.callbacks.label({
      label: 'group-a',
      raw: 9,
      dataset: { data: [9, 4] },
    })
    expect(label).toBe('group-a: 9 (69.2%)')
  })

  it('can hide account cost for user usage stats without account_cost', () => {
    const wrapper = mount(GroupDistributionChart, {
      props: {
        groupStats,
        showAccountCost: false,
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    expect(wrapper.text()).not.toContain('Account Cost')
    expect(wrapper.findAll('thead th')).toHaveLength(3)
    expect(wrapper.findAll('tbody tr')[0].findAll('td')).toHaveLength(3)
  })
})
