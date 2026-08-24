<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50 p-4 dark:bg-dark-900">
    <div class="w-full max-w-lg rounded-2xl bg-white p-8 shadow-xl dark:bg-dark-800">
      <div class="mb-8 text-center">
        <div
          class="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-500 text-xl font-bold text-white"
        >
          S2
        </div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">Sub2 Personal Edition</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          私人 AI 账号池 · Windows 本地版
        </p>
      </div>

      <div v-if="completed" class="space-y-5 text-center">
        <div class="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-green-100 text-green-600">
          ✓
        </div>
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">初始化完成</h2>
          <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-dark-400">
            <template v-if="waitingForGateway">
              本地 SQLite 数据库、加密密钥和管理员账号已经创建，正在启动私人网关并进入登录页...
            </template>
            <template v-else>
              本地服务启动时间超过预期。数据已经保存，可以刷新页面；如果仍无法进入登录页，再重新打开程序即可。
            </template>
          </p>
        </div>
        <button v-if="!waitingForGateway" class="btn btn-primary w-full" type="button" @click="goToLogin">
          进入登录页
        </button>
      </div>

      <form v-else class="space-y-5" @submit.prevent="submit">
        <div class="rounded-xl bg-gray-50 p-4 text-sm leading-6 text-gray-600 dark:bg-dark-700 dark:text-dark-300">
          不需要配置 PostgreSQL、Redis、Docker 或 WSL。Personal Edition 会自动使用本地 SQLite 和内置调度缓存。
        </div>

        <div>
          <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-dark-200">管理员邮箱</label>
          <input
            v-model.trim="email"
            class="input w-full"
            type="email"
            autocomplete="username"
            placeholder="owner@example.com"
            required
          />
        </div>

        <div>
          <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-dark-200">管理员密码</label>
          <input
            v-model="password"
            class="input w-full"
            type="password"
            autocomplete="new-password"
            placeholder="至少 8 位"
            minlength="8"
            maxlength="128"
            required
          />
        </div>

        <div>
          <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-dark-200">确认密码</label>
          <input
            v-model="confirmPassword"
            class="input w-full"
            type="password"
            autocomplete="new-password"
            placeholder="再次输入密码"
            minlength="8"
            maxlength="128"
            required
          />
        </div>

        <div v-if="errorMessage" class="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-600 dark:bg-red-950/30 dark:text-red-300">
          {{ errorMessage }}
        </div>

        <button class="btn btn-primary w-full" type="submit" :disabled="submitting">
          {{ submitting ? '正在初始化...' : '创建管理员并开始使用' }}
        </button>

        <p class="text-center text-xs leading-5 text-gray-400 dark:text-dark-500">
          默认仅监听本机 127.0.0.1。之后可以在管理界面添加 GPT / Gemini 账号。
        </p>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { installPersonal } from '@/api/setup'

const email = ref('')
const password = ref('')
const confirmPassword = ref('')
const submitting = ref(false)
const completed = ref(false)
const waitingForGateway = ref(false)
const errorMessage = ref('')

const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms))

function goToLogin() {
  window.location.replace('/login')
}

async function waitForGateway() {
  waitingForGateway.value = true
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    try {
      const response = await fetch('/api/v1/settings/public', {
        cache: 'no-store',
        credentials: 'same-origin'
      })
      if (response.ok) {
        goToLogin()
        return
      }
    } catch {
      // Expected while the setup listener releases the port and the full
      // application starts on the same address.
    }
    await sleep(500)
  }
  waitingForGateway.value = false
}

async function submit() {
  errorMessage.value = ''
  if (!email.value.includes('@')) {
    errorMessage.value = '请输入有效的管理员邮箱。'
    return
  }
  if (password.value.length < 8 || password.value.length > 128) {
    errorMessage.value = '密码长度需要在 8 到 128 位之间。'
    return
  }
  if (password.value !== confirmPassword.value) {
    errorMessage.value = '两次输入的密码不一致。'
    return
  }

  submitting.value = true
  try {
    await installPersonal({
      admin: {
        email: email.value,
        password: password.value
      }
    })
    completed.value = true
    void waitForGateway()
  } catch (error: unknown) {
    errorMessage.value = error instanceof Error ? error.message : '初始化失败，请重试。'
  } finally {
    submitting.value = false
  }
}
</script>
