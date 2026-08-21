<template>
  <BaseDialog :show="show" :title="t('admin.accounts.usageStatistics')" width="extra-wide" @close="emit('close')">
    <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
    <div v-else-if="stats" class="space-y-5">
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div v-for="item in summary" :key="item.label" class="rounded-xl border border-gray-200 p-4 dark:border-gray-700">
          <p class="text-xs text-gray-500">{{ item.label }}</p>
          <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ item.value }}</p>
        </div>
      </div>
      <div class="overflow-x-auto rounded-xl border border-gray-200 dark:border-gray-700">
        <table class="min-w-full text-sm">
          <thead class="bg-gray-50 text-left dark:bg-gray-800"><tr><th class="p-3">Date</th><th class="p-3 text-right">Requests</th><th class="p-3 text-right">Tokens</th></tr></thead>
          <tbody><tr v-for="row in stats.history" :key="row.date" class="border-t border-gray-100 dark:border-gray-700"><td class="p-3">{{ row.date }}</td><td class="p-3 text-right">{{ format(row.requests) }}</td><td class="p-3 text-right">{{ format(row.tokens) }}</td></tr></tbody>
        </table>
      </div>
    </div>
    <div v-else class="py-12 text-center text-gray-500">No usage data</div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { adminAPI } from '@/api/admin'
import type { Account, AccountUsageStatsResponse } from '@/types'

const { t } = useI18n()
const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const loading = ref(false)
const stats = ref<AccountUsageStatsResponse | null>(null)
const format = (value: number) => value.toLocaleString()
const summary = computed(() => stats.value ? [
  { label: t('admin.accounts.stats.requests'), value: format(stats.value.summary.total_requests) },
  { label: t('admin.accounts.stats.tokens'), value: format(stats.value.summary.total_tokens) },
  { label: t('admin.accounts.stats.avgDailyRequests'), value: format(Math.round(stats.value.summary.avg_daily_requests)) },
  { label: t('admin.accounts.stats.avgDuration'), value: `${Math.round(stats.value.summary.avg_duration_ms)}ms` }
] : [])

watch(() => props.show, async (show) => {
  if (!show || !props.account) { stats.value = null; return }
  loading.value = true
  try { stats.value = await adminAPI.accounts.getStats(props.account.id, 30) }
  catch { stats.value = null }
  finally { loading.value = false }
})
</script>
