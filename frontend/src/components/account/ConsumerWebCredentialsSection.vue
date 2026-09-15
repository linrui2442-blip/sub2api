<template>
  <section
    v-if="eligible"
    class="border-t border-gray-200 pt-4 dark:border-dark-600"
    data-testid="consumer-web-credentials-section"
  >
    <div class="mb-3">
      <label class="input-label mb-0">{{ t('admin.accounts.consumerWebCredentials.title') }}</label>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.consumerWebCredentials.encryptedHint') }}
      </p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.consumerWebCredentials.blankPreserves') }}
      </p>
    </div>

    <div class="mb-3 grid gap-2 text-xs text-gray-600 dark:text-gray-300 sm:grid-cols-2">
      <span data-testid="consumer-web-psid-status">
        __Secure-1PSID:
        {{ t(hasPSID ? 'admin.accounts.consumerWebCredentials.configured' : 'admin.accounts.consumerWebCredentials.notConfigured') }}
      </span>
      <span data-testid="consumer-web-psidts-status">
        __Secure-1PSIDTS:
        {{ t(hasPSIDTS ? 'admin.accounts.consumerWebCredentials.configured' : 'admin.accounts.consumerWebCredentials.notConfigured') }}
      </span>
    </div>

    <p
      v-if="!hasPSID && !hasPSIDTS"
      class="mb-3 text-xs text-amber-700 dark:text-amber-400"
      data-testid="consumer-web-first-setup-hint"
    >
      {{ t('admin.accounts.consumerWebCredentials.firstSetupHint') }}
    </p>

    <div class="space-y-3">
      <div>
        <label class="input-label" for="consumer-web-psid">__Secure-1PSID</label>
        <input
          id="consumer-web-psid"
          v-model="secure1PSID"
          type="password"
          autocomplete="off"
          class="input font-mono"
          data-testid="consumer-web-psid-input"
          data-1p-ignore
          data-lpignore="true"
          data-bwignore="true"
        />
      </div>
      <div>
        <label class="input-label" for="consumer-web-psidts">__Secure-1PSIDTS</label>
        <input
          id="consumer-web-psidts"
          v-model="secure1PSIDTS"
          type="password"
          autocomplete="off"
          class="input font-mono"
          data-testid="consumer-web-psidts-input"
          data-1p-ignore
          data-lpignore="true"
          data-bwignore="true"
        />
      </div>
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="submitting || !hasInput"
        data-testid="consumer-web-save"
        @click="submit"
      >
        {{ submitting ? t('common.saving') : t('admin.accounts.consumerWebCredentials.save') }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Account, ConsumerWebCredentialStatus, ConsumerWebCredentialsPatch } from '@/types'

const props = defineProps<{ account: Account }>()

const { t } = useI18n()
const appStore = useAppStore()
const secure1PSID = ref('')
const secure1PSIDTS = ref('')
const submitting = ref(false)
const hasPSID = ref(false)
const hasPSIDTS = ref(false)

const eligible = computed(
  () => props.account.platform === 'antigravity' && props.account.type === 'oauth'
)
const hasInput = computed(
  () => secure1PSID.value.trim() !== '' || secure1PSIDTS.value.trim() !== ''
)

function syncStatus(account: Account) {
  hasPSID.value = Boolean(account.credentials_status?.has_secure_1psid)
  hasPSIDTS.value = Boolean(account.credentials_status?.has_secure_1psidts)
  secure1PSID.value = ''
  secure1PSIDTS.value = ''
}

watch(() => props.account, syncStatus, { immediate: true })

function applyStatus(status: ConsumerWebCredentialStatus) {
  hasPSID.value = status.has_secure_1psid
  hasPSIDTS.value = status.has_secure_1psidts
}

async function submit() {
  const psid = secure1PSID.value.trim()
  const psidts = secure1PSIDTS.value.trim()
  if (!psid && !psidts) return

  const patch: ConsumerWebCredentialsPatch = {}
  if (psid) patch.secure_1psid = psid
  if (psidts) patch.secure_1psidts = psidts

  submitting.value = true
  try {
    const status = await adminAPI.accounts.updateConsumerWebCredentials(props.account.id, patch)
    applyStatus(status)
    appStore.showSuccess(t('admin.accounts.consumerWebCredentials.saved'))
  } catch {
    appStore.showError(t('admin.accounts.consumerWebCredentials.saveFailed'))
  } finally {
    secure1PSID.value = ''
    secure1PSIDTS.value = ''
    submitting.value = false
  }
}
</script>
