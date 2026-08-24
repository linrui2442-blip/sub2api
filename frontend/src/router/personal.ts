import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getSetupStatus } from '@/api/setup'

const routes: RouteRecordRaw[] = [
  {
    path: '/setup',
    name: 'PersonalSetup',
    component: () => import('@/views/setup/SetupWizardView.vue'),
    meta: { requiresAuth: false, title: 'Setup' }
  },
  {
    path: '/login',
    name: 'PersonalLogin',
    component: () => import('@/views/auth/LoginView.vue'),
    meta: { requiresAuth: false, title: 'Login' }
  },
  {
    path: '/key-usage',
    name: 'PersonalKeyUsage',
    component: () => import('@/views/KeyUsageView.vue'),
    meta: { requiresAuth: false, title: 'Key Usage' }
  },
  {
    path: '/',
    redirect: '/login'
  },
  {
    path: '/dashboard',
    redirect: '/keys'
  },
  {
    path: '/keys',
    name: 'PersonalKeys',
    component: () => import('@/views/user/KeysView.vue'),
    meta: { requiresAuth: true, requiresAdmin: false, title: 'API Keys' }
  },
  {
    path: '/usage',
    name: 'PersonalUsage',
    component: () => import('@/views/user/UsageView.vue'),
    meta: { requiresAuth: true, requiresAdmin: false, title: 'Usage Records' }
  },
  {
    path: '/profile',
    name: 'PersonalProfile',
    component: () => import('@/views/user/ProfileView.vue'),
    meta: { requiresAuth: true, requiresAdmin: false, title: 'Profile' }
  },
  {
    path: '/admin',
    redirect: '/admin/accounts'
  },
  {
    path: '/admin/dashboard',
    redirect: '/admin/accounts'
  },
  {
    path: '/admin/accounts',
    name: 'PersonalAdminAccounts',
    component: () => import('@/views/admin/AccountsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Account Management' }
  },
  {
    path: '/admin/users',
    name: 'PersonalAdminUsers',
    component: () => import('@/views/admin/UsersView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Private Members' }
  },
  {
    path: '/admin/groups',
    name: 'PersonalAdminGroups',
    component: () => import('@/views/admin/GroupsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Group Management' }
  },
  {
    path: '/admin/proxies',
    name: 'PersonalAdminProxies',
    component: () => import('@/views/admin/ProxiesView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Proxy Management' }
  },
  {
    path: '/admin/settings',
    name: 'PersonalAdminSettings',
    component: () => import('@/views/admin/PersonalSettingsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Settings' }
  },
  {
    path: '/admin/audit-logs',
    name: 'PersonalAdminAuditLogs',
    component: () => import('@/views/admin/AuditLogView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, title: 'Audit Logs' }
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'PersonalNotFound',
    component: () => import('@/views/NotFoundView.vue'),
    meta: { title: '404 Not Found' }
  }
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    return savedPosition ?? { top: 0 }
  }
})

let authInitialized = false

router.beforeEach(async (to) => {
  const authStore = useAuthStore()
  if (!authInitialized) {
    authStore.checkAuth()
    authInitialized = true
  }

  document.title = `${String(to.meta.title ?? 'Sub2 Personal')} - Sub2 Personal`

  if (to.path === '/setup') {
    try {
      const status = await getSetupStatus()
      if (!status.needs_setup) {
        return authStore.isAuthenticated
          ? authStore.isAdmin
            ? '/admin/accounts'
            : '/keys'
          : '/login'
      }
    } catch {
      // Keep first-run setup reachable if the setup status endpoint is still starting.
    }
  }

  if (to.meta.requiresAuth === false) {
    if (to.path === '/login' && authStore.isAuthenticated) {
      return authStore.isAdmin ? '/admin/accounts' : '/keys'
    }
    return true
  }

  if (!authStore.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }

  if (to.meta.requiresAdmin === true && !authStore.isAdmin) {
    return '/keys'
  }

  return true
})

export default router
