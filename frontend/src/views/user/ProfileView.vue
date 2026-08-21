<template>
  <AppLayout>
    <div
      data-testid="profile-shell"
      class="mx-auto max-w-[950px] space-y-6"
    >
      <ProfileInfoCard :user="user" />

      <div
        v-if="contactInfo"
        class="card border-primary-200 bg-primary-50 p-6 dark:bg-primary-900/20"
      >
        <div class="flex items-center gap-4">
          <div class="rounded-xl bg-primary-100 p-3 text-primary-600">
            <Icon name="chat" size="lg" />
          </div>
          <div>
            <h3 class="font-semibold text-primary-800 dark:text-primary-200">
              {{ t('common.contactSupport') }}
            </h3>
            <p class="text-sm font-medium">{{ contactInfo }}</p>
          </div>
        </div>
      </div>

      <ProfilePasswordForm />

      <ProfileTotpCard />
      <ProfilePasskeyCard :enabled="passkeyEnabled" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Icon } from '@/components/icons'
import AppLayout from '@/components/layout/AppLayout.vue'
import ProfileInfoCard from '@/components/user/profile/ProfileInfoCard.vue'
import ProfilePasswordForm from '@/components/user/profile/ProfilePasswordForm.vue'
import ProfileTotpCard from '@/components/user/profile/ProfileTotpCard.vue'
import ProfilePasskeyCard from '@/components/user/profile/ProfilePasskeyCard.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const user = computed(() => authStore.user)

const contactInfo = ref('')
const passkeyEnabled = ref(false)

onMounted(async () => {
  const profileRefresh = authStore.refreshUser().catch((error) => {
    console.error('Failed to refresh profile:', error)
  })

  const settingsLoad = appStore.fetchPublicSettings()
    .then((settings) => {
      if (!settings) {
        return
      }
      contactInfo.value = settings.contact_info || ''
      passkeyEnabled.value = settings.passkey_enabled === true
    })
    .catch((error) => {
      console.error('Failed to load settings:', error)
    })

  await Promise.all([profileRefresh, settingsLoad])
})
</script>
