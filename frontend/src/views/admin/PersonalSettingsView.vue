<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">Personal Edition 设置</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
          仅保留私人 AI 网关需要的本地运行与账号池入口。商业支付、订阅、营销和 SaaS 运营设置已从 Personal Edition 移除。
        </p>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <section class="card p-5">
          <h2 class="font-semibold text-gray-900 dark:text-white">本地运行</h2>
          <dl class="mt-4 space-y-3 text-sm">
            <div class="flex items-center justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">默认监听</dt>
              <dd class="font-mono text-gray-900 dark:text-gray-100">127.0.0.1</dd>
            </div>
            <div class="flex items-center justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">数据存储</dt>
              <dd class="text-gray-900 dark:text-gray-100">SQLite</dd>
            </div>
            <div class="flex items-center justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">缓存 / 调度</dt>
              <dd class="text-gray-900 dark:text-gray-100">进程内嵌</dd>
            </div>
            <div class="flex items-center justify-between gap-4">
              <dt class="text-gray-500 dark:text-gray-400">OAuth Token 刷新</dt>
              <dd class="text-gray-900 dark:text-gray-100">自动</dd>
            </div>
          </dl>
        </section>

        <section class="card p-5">
          <h2 class="font-semibold text-gray-900 dark:text-white">Provider 架构</h2>
          <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-gray-400">
            V1 优先验证 GPT / OpenAI 与 Gemini。底层 Account Pool、Scheduler、Health、Quota 与 Failover 保持通用，后续可继续接入 Claude 和其他 Provider。
          </p>
          <RouterLink to="/admin/accounts" class="btn btn-primary btn-sm mt-4 inline-flex">
            管理 AI 账号池
          </RouterLink>
        </section>
      </div>

      <section class="card p-5">
        <h2 class="font-semibold text-gray-900 dark:text-white">管理入口</h2>
        <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <RouterLink to="/admin/accounts" class="btn btn-secondary justify-center">AI 账号池</RouterLink>
          <RouterLink to="/admin/users" class="btn btn-secondary justify-center">私有成员</RouterLink>
          <RouterLink to="/admin/groups" class="btn btn-secondary justify-center">分组与权限</RouterLink>
          <RouterLink to="/admin/proxies" class="btn btn-secondary justify-center">代理设置</RouterLink>
          <RouterLink to="/admin/audit-logs" class="btn btn-secondary justify-center">审计日志</RouterLink>
          <RouterLink to="/keys" class="btn btn-secondary justify-center">API Keys</RouterLink>
        </div>
      </section>

      <section class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        Personal Edition 不提供公开注册、充值、商业订阅、优惠码、返利、模型商城或 SaaS 运营后台。需要远程给固定朋友使用时，应通过私有成员/API Key 与受信任网络方式开放，而不是公开暴露管理端。
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
</script>
