<script setup lang="ts">
import { RouterView, useRoute, useRouter } from 'vue-router'
import { onMounted, watch } from 'vue'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import { useAppStore } from '@/stores'
import { getSetupStatus } from '@/api/setup'
import { updateFavicon } from '@/utils/branding'

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()

function updateDocumentTitle() {
  const pageTitle = String(route.meta.title ?? 'Sub2 Personal')
  const siteName = appStore.siteName || 'Sub2 Personal'
  document.title = `${pageTitle} - ${siteName}`
}

watch(
  () => appStore.siteLogo,
  (logo) => {
    if (logo) updateFavicon(logo)
  },
  { immediate: true },
)

watch(
  [() => route.fullPath, () => route.meta.title, () => appStore.siteName],
  updateDocumentTitle,
)

onMounted(async () => {
  try {
    const status = await getSetupStatus()
    if (status.needs_setup && route.path !== '/setup') {
      await router.replace('/setup')
      return
    }
  } catch {
    // Personal startup may still be warming up. Keep the current route usable.
  }

  await appStore.fetchPublicSettings()
  updateDocumentTitle()
})
</script>

<template>
  <NavigationProgress />
  <RouterView />
  <Toast />
</template>
