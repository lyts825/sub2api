<template>
  <BaseDialog :show="show" :title="t('admin.users.tokenQuota.title')" @close="$emit('close')">
    <div v-if="user" class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-400">
        {{ t('admin.users.tokenQuota.subtitle', { email: user.email }) }}
      </p>
      <div v-if="loading" class="py-6 text-center text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="!loaded" class="text-sm text-red-500">{{ t('admin.users.tokenQuota.loadFailed') }}</div>
      <template v-else>
        <div>
          <label for="user-token-quota" class="input-label">{{ t('admin.users.tokenQuota.limit') }}</label>
          <input
            id="user-token-quota"
            v-model.number="quota"
            type="number"
            min="0"
            step="1"
            :max="Number.MAX_SAFE_INTEGER"
            class="input"
            :disabled="submitting"
            :placeholder="t('keys.tokenQuotaPlaceholder')"
          />
          <p class="input-hint">{{ t('admin.users.tokenQuota.hint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('keys.tokenQuotaUsed') }}</label>
          <div class="flex items-center gap-3">
            <span class="flex-1 rounded-lg bg-gray-100 px-3 py-2 font-medium dark:bg-dark-700">{{ used.toLocaleString() }}</span>
            <button type="button" class="btn btn-secondary" :disabled="submitting" @click="showResetConfirm = true">
              {{ t('keys.reset') }}
            </button>
          </div>
        </div>
      </template>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="loading || !loaded || submitting" @click="saveQuota">
          {{ submitting ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
  <ConfirmDialog
    :show="showResetConfirm"
    :title="t('admin.users.tokenQuota.resetTitle')"
    :message="t('admin.users.tokenQuota.resetConfirm', { email: user?.email, used: used.toLocaleString() })"
    :confirm-text="t('keys.reset')"
    danger
    @confirm="resetUsage"
    @cancel="showResetConfirm = false"
  />
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()
const quota = ref<number | string>(0)
const used = ref(0)
const loading = ref(false)
const loaded = ref(false)
const submitting = ref(false)
const showResetConfirm = ref(false)
let generation = 0

watch(() => [props.show, props.user?.id] as const, async ([show, userId]) => {
  const requestGeneration = ++generation
  showResetConfirm.value = false
  loaded.value = false
  loading.value = false
  submitting.value = false
  if (!show || userId == null) return
  loading.value = true
  try {
    const data = await adminAPI.users.getTokenQuota(userId)
    if (requestGeneration !== generation) return
    quota.value = data.quota
    used.value = data.used
    loaded.value = true
  } catch {
    if (requestGeneration === generation) appStore.showError(t('admin.users.tokenQuota.loadFailed'))
  } finally {
    if (requestGeneration === generation) loading.value = false
  }
}, { immediate: true })

async function saveQuota() {
  if (!props.user || !loaded.value || submitting.value) return
  const limit = Number(quota.value)
  if (!Number.isSafeInteger(limit) || limit < 0) {
    appStore.showError(t('keys.tokenQuotaInvalid'))
    return
  }
  const requestGeneration = generation
  submitting.value = true
  try {
    await adminAPI.users.updateTokenQuota(props.user.id, { quota: limit })
    if (requestGeneration !== generation) return
    appStore.showSuccess(t('admin.users.tokenQuota.saved'))
    emit('success')
    emit('close')
  } catch {
    if (requestGeneration === generation) appStore.showError(t('admin.users.tokenQuota.saveFailed'))
  } finally {
    if (requestGeneration === generation) submitting.value = false
  }
}

async function resetUsage() {
  showResetConfirm.value = false
  if (!props.user || !loaded.value || submitting.value) return
  const requestGeneration = generation
  submitting.value = true
  try {
    const data = await adminAPI.users.updateTokenQuota(props.user.id, { reset_used: true })
    if (requestGeneration !== generation) return
    used.value = data.used
    appStore.showSuccess(t('admin.users.tokenQuota.resetSuccess'))
    emit('success')
  } catch {
    if (requestGeneration === generation) appStore.showError(t('admin.users.tokenQuota.resetFailed'))
  } finally {
    if (requestGeneration === generation) submitting.value = false
  }
}
</script>
