import type { ApiKey } from '@/types'
export type ApiKeyMode = 'UNIFIED' | 'GROUP_PINNED'
export function getApiKeyMode(key: Pick<ApiKey, 'group_id'>, personalEdition: boolean): ApiKeyMode | null {
  if (key.group_id !== null) return 'GROUP_PINNED'
  return personalEdition ? 'UNIFIED' : null
}
