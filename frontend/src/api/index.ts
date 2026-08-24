/**
 * Personal Edition API surface.
 * Only modules used by the private gateway UI are exported here.
 */

export { apiClient } from './client'

export { authAPI, isTotp2FARequired, type LoginResponse } from './auth'

export { keysAPI } from './keys'
export { usageAPI } from './usage'
export { userAPI } from './user'
export { userGroupsAPI } from './groups'
export { totpAPI } from './totp'
export { passkeyAPI, type PasskeyCredentialSummary } from './passkey'

export { adminAPI } from './admin'

export { default } from './client'
