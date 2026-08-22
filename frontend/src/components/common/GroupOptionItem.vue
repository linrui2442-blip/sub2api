<template>
  <div class="flex min-w-0 flex-1 items-start justify-between gap-3">
    <div class="flex min-w-0 flex-1 flex-col items-start" :title="description || undefined">
      <GroupBadge :name="name" :platform="platform" class="groupOptionItemBadge" />
      <span v-if="description" class="mt-1.5 w-full whitespace-pre-line [overflow-wrap:anywhere] text-left text-xs leading-relaxed text-gray-500 dark:text-gray-400 line-clamp-3">
        {{ description }}
      </span>
    </div>
    <svg v-if="showCheckmark && selected" class="mt-0.5 h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
    </svg>
  </div>
</template>

<script setup lang="ts">
import GroupBadge from './GroupBadge.vue'
import type { GroupPlatform } from '@/types'

withDefaults(defineProps<{
  name: string
  platform: GroupPlatform
  description?: string | null
  selected?: boolean
  showCheckmark?: boolean
}>(), {
  selected: false,
  showCheckmark: true
})
</script>

<style scoped>
.groupOptionItemBadge :deep(span.truncate) { font-weight: 600; }
</style>
