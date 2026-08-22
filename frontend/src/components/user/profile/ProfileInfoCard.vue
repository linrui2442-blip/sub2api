<template>
  <div class="space-y-6">
    <section
      data-testid="profile-overview-hero"
      class="card overflow-hidden border border-primary-100/80 bg-gradient-to-br from-primary-50 via-white to-amber-50/70 dark:border-primary-900/40 dark:from-primary-950/40 dark:via-dark-900 dark:to-dark-950"
    >
      <div class="px-6 py-6 md:px-8">
        <div class="flex flex-col gap-6 lg:items-start">
          <div class="min-w-0 flex-1 space-y-5">
            <div class="space-y-3">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="truncate text-2xl font-semibold text-gray-900 dark:text-white">
                  {{ displayName }}
                </h2>
                <span :class="['badge', user?.role === 'admin' ? 'badge-primary' : 'badge-gray']">
                  {{ user?.role === 'admin' ? t('profile.administrator') : t('profile.user') }}
                </span>
                <span
                  :class="['badge', user?.status === 'active' ? 'badge-success' : 'badge-danger']"
                >
                  {{
                    user?.status === 'active'
                      ? t('common.active')
                      : t('common.disabled')
                  }}
                </span>
              </div>

              <div class="space-y-1">
                <p class="truncate text-sm text-gray-600 dark:text-gray-300">
                  {{ primaryEmailDisplay }}
                </p>
              </div>
            </div>

          </div>
        </div>
      </div>
    </section>

    <div class="space-y-6">
      <div data-testid="profile-main-column" class="space-y-6">
        <section
          data-testid="profile-basics-panel"
          class="card border border-gray-100 bg-white/90 p-6 dark:border-dark-700 dark:bg-dark-900/50"
        >
          <div class="mb-5 flex items-start justify-between gap-4">
            <div>
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('profile.basicsTitle') }}
              </h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('profile.basicsDescription') }}
              </p>
            </div>
          </div>

          <div class="rounded-3xl border border-gray-100 bg-gray-50/80 p-5 dark:border-dark-700 dark:bg-dark-900/30">
            <ProfileEditForm
              :initial-username="user?.username || ''"
              embedded
            />
          </div>
        </section>

      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ProfileEditForm from '@/components/user/profile/ProfileEditForm.vue'
import type { User } from '@/types'

const props = defineProps<{
  user: User | null
}>()

const { t } = useI18n()

const displayName = computed(() => props.user?.username?.trim() || props.user?.email?.trim() || t('profile.user'))
const primaryEmailDisplay = computed(() => props.user?.email?.trim() || '')
</script>
