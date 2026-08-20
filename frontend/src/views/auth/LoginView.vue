<template>
  <AuthLayout>
    <div class="space-y-6">
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">Sub2 Personal</h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">使用本机私有账号登录</p>
      </div>

      <form class="space-y-5" @submit.prevent="handleLogin">
        <div>
          <label for="email" class="input-label">邮箱</label>
          <input
            id="email"
            v-model.trim="email"
            type="email"
            required
            autofocus
            autocomplete="email"
            class="input"
            :disabled="busy"
            placeholder="you@example.com"
          />
        </div>

        <div>
          <label for="password" class="input-label">密码</label>
          <div class="relative">
            <input
              id="password"
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              required
              autocomplete="current-password"
              class="input pr-12"
              :disabled="busy"
              placeholder="输入密码"
            />
            <button
              type="button"
              class="absolute inset-y-0 right-0 px-3 text-sm text-gray-500 dark:text-dark-400"
              :disabled="busy"
              @click="showPassword = !showPassword"
            >
              {{ showPassword ? '隐藏' : '显示' }}
            </button>
          </div>
        </div>

        <button type="submit" class="btn btn-primary w-full" :disabled="busy">
          {{ loginLoading ? '登录中…' : '登录' }}
        </button>

        <template v-if="passkeyAvailable">
          <div class="flex items-center gap-3">
            <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
            <span class="text-xs text-gray-500 dark:text-dark-400">或</span>
            <div class="h-px flex-1 bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <button type="button" class="btn btn-secondary w-full" :disabled="busy" @click="handlePasskeyLogin">
            {{ passkeyLoading ? '正在验证…' : '使用 Passkey 登录' }}
          </button>
        </template>
      </form>

      <p class="text-center text-xs leading-5 text-gray-500 dark:text-dark-400">
        Personal Edition 不提供公开注册、找回密码或第三方站点登录。成员账号由管理员创建。
      </p>
    </div>
  </AuthLayout>

  <TotpLoginModal
    v-if="show2FAModal"
    ref="totpModalRef"
    :temp-token="totpTempToken"
    :user-email-masked="totpUserEmailMasked"
    @verify="handle2FAVerify"
    @cancel="handle2FACancel"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { AuthLayout } from '@/components/layout'
import TotpLoginModal from '@/components/auth/TotpLoginModal.vue'
import { useAppStore, useAuthStore } from '@/stores'
import { isTotp2FARequired } from '@/api'
import type { TotpLoginResponse } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'

const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

const email = ref('')
const password = ref('')
const showPassword = ref(false)
const loginLoading = ref(false)
const passkeyLoading = ref(false)
const show2FAModal = ref(false)
const totpTempToken = ref('')
const totpUserEmailMasked = ref('')
const totpModalRef = ref<InstanceType<typeof TotpLoginModal> | null>(null)

const busy = computed(() => loginLoading.value || passkeyLoading.value)
const passkeyAvailable = computed(
  () => typeof window !== 'undefined' && typeof window.PublicKeyCredential !== 'undefined',
)

function redirectAfterLogin() {
  const requested = router.currentRoute.value.query.redirect
  const target = typeof requested === 'string' && requested.startsWith('/')
    ? requested
    : authStore.isAdmin
      ? '/admin/accounts'
      : '/keys'
  return router.push(target)
}

async function handleLogin() {
  if (!email.value || !password.value) return
  loginLoading.value = true
  try {
    const response = await authStore.login({ email: email.value, password: password.value })
    if (isTotp2FARequired(response)) {
      const pending = response as TotpLoginResponse
      totpTempToken.value = pending.temp_token || ''
      totpUserEmailMasked.value = pending.user_email_masked || ''
      show2FAModal.value = true
      return
    }
    appStore.showSuccess('登录成功')
    await redirectAfterLogin()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, '登录失败'))
  } finally {
    loginLoading.value = false
  }
}

async function handlePasskeyLogin() {
  passkeyLoading.value = true
  try {
    await authStore.loginWithPasskey()
    appStore.showSuccess('登录成功')
    await redirectAfterLogin()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, 'Passkey 登录失败'))
  } finally {
    passkeyLoading.value = false
  }
}

async function handle2FAVerify(code: string) {
  totpModalRef.value?.setVerifying(true)
  try {
    await authStore.login2FA(totpTempToken.value, code)
    show2FAModal.value = false
    appStore.showSuccess('登录成功')
    await redirectAfterLogin()
  } catch (error: unknown) {
    totpModalRef.value?.setError(extractApiErrorMessage(error, '两步验证失败'))
  } finally {
    totpModalRef.value?.setVerifying(false)
  }
}

function handle2FACancel() {
  show2FAModal.value = false
  totpTempToken.value = ''
  totpUserEmailMasked.value = ''
}
</script>
