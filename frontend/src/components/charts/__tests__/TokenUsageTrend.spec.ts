import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import TokenUsageTrend from '../TokenUsageTrend.vue'

let currentLocale: 'zh' | 'en' = 'en'
const messages = {
  zh: {
    'admin.dashboard.tokenUsageTrend': 'Token 使用趋势',
    'admin.dashboard.noDataAvailable': '暂无数据',
    'admin.dashboard.input': '输入',
    'admin.dashboard.output': '输出',
    'admin.dashboard.cacheCreation': '缓存创建',
    'admin.dashboard.cacheRead': '缓存读取',
    'admin.dashboard.cacheHitRate': '缓存命中率'
  },
  en: {
    'admin.dashboard.tokenUsageTrend': 'Token Usage Trend',
    'admin.dashboard.noDataAvailable': 'No data available',
    'admin.dashboard.input': 'Input',
    'admin.dashboard.output': 'Output',
    'admin.dashboard.cacheCreation': 'Cache Creation',
    'admin.dashboard.cacheRead': 'Cache Read',
    'admin.dashboard.cacheHitRate': 'Cache Hit Rate'
  }
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: keyof typeof messages.en) => messages[currentLocale][key] ?? key
    })
  }
})

vi.mock('vue-chartjs', () => ({
  Line: defineComponent({
    name: 'ChartLine',
    props: {
      data: { type: Object, required: true },
      options: { type: Object, required: true }
    },
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>'
  })
}))

function mountTrend(inputTokens: number, cacheCreationTokens: number, cacheReadTokens: number) {
  return mount(TokenUsageTrend, {
    props: {
      trendData: [{
        date: '2026-08-24 09:00',
        requests: 1,
        input_tokens: inputTokens,
        output_tokens: 100,
        cache_creation_tokens: cacheCreationTokens,
        cache_read_tokens: cacheReadTokens,
        total_tokens: inputTokens + 100 + cacheCreationTokens + cacheReadTokens
      }]
    },
    global: { stubs: { LoadingSpinner: true } }
  })
}

describe('TokenUsageTrend', () => {
  it('calculates cache hit rate against all prompt tokens', () => {
    currentLocale = 'en'
    const wrapper = mountTrend(500, 0, 1500)
    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find((dataset: any) => dataset.label === 'Cache Hit Rate')
    expect(hitRateDataset.data[0]).toBe(75)
  })

  it('returns 0 hit rate when all prompt tokens are zero', () => {
    currentLocale = 'en'
    const wrapper = mountTrend(0, 0, 0)
    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find((dataset: any) => dataset.label === 'Cache Hit Rate')
    expect(hitRateDataset.data[0]).toBe(0)
  })

  it('includes cache_creation_tokens in denominator for Anthropic models', () => {
    currentLocale = 'en'
    const wrapper = mountTrend(200, 300, 500)
    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find((dataset: any) => dataset.label === 'Cache Hit Rate')
    expect(hitRateDataset.data[0]).toBe(50)
  })

  it.each([
    ['zh', ['输入', '输出', '缓存创建', '缓存读取', '缓存命中率']],
    ['en', ['Input', 'Output', 'Cache Creation', 'Cache Read', 'Cache Hit Rate']]
  ] as const)('localizes legend and tooltip labels for %s', (locale, expectedLabels) => {
    currentLocale = locale
    const wrapper = mountTrend(100, 10, 50)
    const chart = wrapper.findComponent({ name: 'ChartLine' })
    const data = chart.props('data') as { datasets: Array<{ label: string; yAxisID?: string }> }
    const options = chart.props('options') as any

    expect(data.datasets.map((dataset) => dataset.label)).toEqual(expectedLabels)
    const tooltipLabel = options.plugins.tooltip.callbacks.label
    expect(tooltipLabel({ dataset: data.datasets[0], raw: 100 })).toBe(`${expectedLabels[0]}: 100`)
    expect(tooltipLabel({ dataset: data.datasets[4], raw: 25 })).toBe(`${expectedLabels[4]}: 25.0%`)
  })
})
