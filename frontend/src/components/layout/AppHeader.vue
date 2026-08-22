<template>
  <header class="glass sticky top-0 z-30 border-b border-gray-200/50 dark:border-dark-700/50">
    <div class="flex h-16 items-center justify-between gap-3 px-3 sm:px-4 md:px-6">
      <div class="flex min-w-0 items-center gap-3">
        <button
          type="button"
          class="btn-ghost btn-icon lg:hidden"
          aria-label="Toggle menu"
          @click="appStore.toggleMobileSidebar()"
        >
          <span class="text-xl leading-none">☰</span>
        </button>
        <div class="min-w-0">
          <h1 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ pageTitle }}</h1>
          <p class="hidden text-xs text-gray-500 dark:text-dark-400 sm:block">Sub2 Personal Edition</p>
        </div>
      </div>

      <div class="flex items-center gap-2">
        <LocaleSwitcher />

        <div v-if="user" ref="dropdownRef" class="relative">
          <button
            type="button"
            class="flex items-center gap-2 rounded-xl p-1.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-800"
            @click="dropdownOpen = !dropdownOpen"
          >
            <div class="flex h-8 w-8 items-center justify-center rounded-xl bg-primary-600 text-sm font-semibold text-white">
              {{ userInitials }}
            </div>
            <div class="hidden text-left md:block">
              <div class="max-w-40 truncate text-sm font-medium text-gray-900 dark:text-white">{{ displayName }}</div>
              <div class="text-xs text-gray-500 dark:text-dark-400">{{ authStore.isAdmin ? 'Owner' : 'Member' }}</div>
            </div>
          </button>

          <transition name="dropdown">
            <div v-if="dropdownOpen" class="dropdown right-0 mt-2 w-52">
              <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ displayName }}</div>
                <div class="truncate text-xs text-gray-500 dark:text-dark-400">{{ user.email }}</div>
              </div>
              <div class="py-1">
                <router-link to="/profile" class="dropdown-item" @click="closeDropdown">个人资料</router-link>
                <router-link to="/keys" class="dropdown-item" @click="closeDropdown">API 密钥</router-link>
                <button
                  type="button"
                  class="dropdown-item w-full text-red-600 dark:text-red-400"
                  @click="handleLogout"
                >
                  退出登录
                </button>
              </div>
            </div>
          </transition>
        </div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore, useAuthStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'

const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()

const dropdownOpen = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)
const user = computed(() => authStore.user)
const pageTitle = computed(() => String(route.meta.title ?? 'Sub2 Personal'))
const displayName = computed(() => user.value?.username || user.value?.email?.split('@')[0] || 'User')
const userInitials = computed(() => displayName.value.slice(0, 2).toUpperCase())

function closeDropdown() {
  dropdownOpen.value = false
}

async function handleLogout() {
  closeDropdown()
  try {
    await authStore.logout()
  } finally {
    await router.push('/login')
  }
}

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) closeDropdown()
}

onMounted(() => document.addEventListener('click', handleClickOutside))
onBeforeUnmount(() => document.removeEventListener('click', handleClickOutside))
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.15s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: translateY(-4px) scale(0.98);
}
</style>
