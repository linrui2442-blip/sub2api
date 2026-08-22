<template>
  <AccountStatsModal :show="show" :account="account" @close="emit('close')" />
</template>

<script setup lang="ts">
import AccountStatsModal from '@/components/account/AccountStatsModal.vue'
import type { Account } from '@/types'

defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ (e: 'close'): void }>()
</script>
