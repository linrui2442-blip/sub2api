<template>
  <span :class="['inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium', badgeClass]">
    <PlatformIcon v-if="platform" :platform="platform" size="sm" />
    <span class="truncate">{{ name }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { GroupPlatform } from '@/types'
import PlatformIcon from './PlatformIcon.vue'

const props = defineProps<{
  name: string
  platform?: GroupPlatform
}>()

const badgeClass = computed(() => {
  const classes: Record<string, string> = {
    anthropic: 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400',
    openai: 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400',
    gemini: 'bg-sky-50 text-sky-700 dark:bg-sky-900/20 dark:text-sky-400',
    antigravity: 'bg-fuchsia-50 text-fuchsia-700 dark:bg-fuchsia-900/20 dark:text-fuchsia-400',
    grok: 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-200',
    kimi: 'bg-pink-50 text-pink-700 dark:bg-pink-900/20 dark:text-pink-400',
    zhipu: 'bg-indigo-50 text-indigo-700 dark:bg-indigo-900/20 dark:text-indigo-400',
    deepseek: 'bg-teal-50 text-teal-700 dark:bg-teal-900/20 dark:text-teal-400',
    composite: 'bg-cyan-50 text-cyan-800 dark:bg-cyan-900/20 dark:text-cyan-300'
  }
  return classes[props.platform || ''] || 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
})
</script>
