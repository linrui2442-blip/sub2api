<template>
  <PersonalSetupView v-if="mode === 'personal'" />
  <SetupWizardView v-else-if="mode === 'upstream'" />
  <div v-else class="flex min-h-screen items-center justify-center bg-gray-50 dark:bg-dark-900">
    <div class="text-center text-sm text-gray-500 dark:text-dark-400">正在准备初始化...</div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getSetupStatus } from '@/api/setup'
import PersonalSetupView from './PersonalSetupView.vue'
import SetupWizardView from './SetupWizardView.vue'

const mode = ref<'loading' | 'personal' | 'upstream'>('loading')

onMounted(async () => {
  try {
    const status = await getSetupStatus()
    mode.value = status.personal === true ? 'personal' : 'upstream'
  } catch {
    // Preserve the upstream setup path if an older backend does not expose the
    // Personal status flag or setup status is temporarily unavailable.
    mode.value = 'upstream'
  }
})
</script>
