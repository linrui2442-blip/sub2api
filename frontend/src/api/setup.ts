/**
 * Setup API endpoints
 */
import axios from 'axios'
import { buildGatewayUrl } from './url'

// Create a separate client for setup endpoints (not under /api/v1)
const setupClient = axios.create({
  baseURL: buildGatewayUrl('/').replace(/\/+$/, ''),
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

export interface SetupStatus {
  needs_setup: boolean
  step: string
  personal?: boolean
}

export interface AdminConfig {
  email: string
  password: string
}

export interface PersonalInstallRequest {
  admin: AdminConfig
}

export interface InstallResponse {
  message: string
  restart: boolean
}

/**
 * Get setup status
 */
export async function getSetupStatus(): Promise<SetupStatus> {
  const response = await setupClient.get('/setup/status')
  return response.data.data
}

/**
 * Initialize Personal Edition. SQLite and the in-process cache/scheduler are
 * automatic; only the local owner account is provided by the user.
 */
export async function installPersonal(config: PersonalInstallRequest): Promise<InstallResponse> {
  const response = await setupClient.post('/setup/personal/install', config)
  return response.data.data
}
