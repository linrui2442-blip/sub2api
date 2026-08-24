import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('Token usage trend locale messages', () => {
  it('defines the chart legend labels in the namespace used by the component', () => {
    expect(zh.dashboard).toMatchObject({
      input: '输入',
      output: '输出',
      cacheCreation: '缓存创建',
      cacheRead: '缓存读取',
      cacheHitRate: '缓存命中率'
    })
    expect(en.dashboard).toMatchObject({
      input: 'Input',
      output: 'Output',
      cacheCreation: 'Cache Creation',
      cacheRead: 'Cache Read',
      cacheHitRate: 'Cache Hit Rate'
    })
  })
})
