<template>
  <aside
    class="sidebar"
    :class="[
      sidebarCollapsed ? 'w-[72px]' : 'w-64',
      { '-translate-x-full lg:translate-x-0': !mobileOpen },
    ]"
  >
    <div class="sidebar-header" :class="{ 'sidebar-header-collapsed': sidebarCollapsed }">
      <router-link
        :to="homePath"
        class="sidebar-logo flex h-9 w-9 items-center justify-center overflow-hidden rounded-xl shadow-glow"
        @click="closeMobile"
      >
        <img v-if="settingsLoaded" :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
      </router-link>
      <div class="sidebar-brand" :class="{ 'sidebar-brand-collapsed': sidebarCollapsed }">
        <router-link :to="homePath" class="sidebar-brand-title text-lg font-bold text-gray-900 dark:text-white" @click="closeMobile">
          {{ siteName }}
        </router-link>
        <span v-if="siteVersion" class="text-xs text-gray-400 dark:text-gray-500">v{{ siteVersion }}</span>
      </div>
    </div>

    <nav class="sidebar-nav scrollbar-hide">
      <div v-if="isAdmin" class="sidebar-section">
        <div v-if="!sidebarCollapsed" class="sidebar-section-title">Personal Admin</div>
        <router-link
          v-for="item in adminNavItems"
          :key="item.path"
          :to="item.path"
          class="sidebar-link mb-1"
          :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
          :title="sidebarCollapsed ? item.label : undefined"
          @click="closeMobile"
        >
          <span class="flex h-5 w-5 flex-shrink-0 items-center justify-center text-xs">{{ item.mark }}</span>
          <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }">{{ item.label }}</span>
        </router-link>
      </div>

      <div class="sidebar-section">
        <div v-if="isAdmin && !sidebarCollapsed" class="sidebar-section-title">我的访问</div>
        <router-link
          v-for="item in personalNavItems"
          :key="item.path"
          :to="item.path"
          class="sidebar-link mb-1"
          :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
          :title="sidebarCollapsed ? item.label : undefined"
          @click="closeMobile"
        >
          <span class="flex h-5 w-5 flex-shrink-0 items-center justify-center text-xs">{{ item.mark }}</span>
          <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }">{{ item.label }}</span>
        </router-link>
      </div>
    </nav>

    <div class="mt-auto border-t border-gray-100 p-3 dark:border-dark-800">
      <button
        type="button"
        class="sidebar-link mb-2 w-full"
        :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
        @click="toggleTheme"
      >
        <span class="flex h-5 w-5 items-center justify-center">{{ isDark ? '☀' : '☾' }}</span>
        <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }">{{ isDark ? '浅色模式' : '深色模式' }}</span>
      </button>
      <button
        type="button"
        class="sidebar-link w-full"
        :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
        @click="appStore.toggleSidebar()"
      >
        <span class="flex h-5 w-5 items-center justify-center">{{ sidebarCollapsed ? '›' : '‹' }}</span>
        <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }">收起侧栏</span>
      </button>
    </div>
  </aside>

  <transition name="fade">
    <div v-if="mobileOpen" class="fixed inset-0 z-30 bg-black/50 lg:hidden" @click="closeMobile"></div>
  </transition>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore, useAuthStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

interface NavItem {
  path: string
  label: string
  mark: string
}

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const isDark = ref(document.documentElement.classList.contains('dark'))

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)
const isAdmin = computed(() => authStore.isAdmin)
const homePath = computed(() => (isAdmin.value ? '/admin/accounts' : '/keys'))
const siteName = computed(() => appStore.siteName || 'Sub2 Personal')
const siteVersion = computed(() => appStore.siteVersion)
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const adminNavItems: NavItem[] = [
  { path: '/admin/accounts', label: '账号池', mark: 'A' },
  { path: '/admin/users', label: '私人成员', mark: 'M' },
  { path: '/admin/groups', label: '分组管理', mark: 'G' },
  { path: '/admin/proxies', label: '代理管理', mark: 'P' },
  { path: '/admin/settings', label: '设置', mark: 'S' },
  { path: '/admin/audit-logs', label: '审计日志', mark: 'L' },
]

const personalNavItems: NavItem[] = [
  { path: '/keys', label: 'API 密钥', mark: 'K' },
  { path: '/usage', label: '使用记录', mark: 'U' },
  { path: '/profile', label: '个人资料', mark: 'I' },
]

function isActive(path: string) {
  return route.path === path || route.path.startsWith(`${path}/`)
}

function closeMobile() {
  appStore.setMobileOpen(false)
}

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
</script>
