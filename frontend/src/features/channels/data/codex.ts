import { apiRequest } from '@/lib/api-client'
import { ProxyConfig } from '../hooks/use-oauth-flow'

const OFFICIAL_CODEX_BASE_URLS = new Set([
  'https://chatgpt.com/backend-api/codex',
  'wss://chatgpt.com/backend-api/codex',
])

function normalizeCodexBaseURL(baseURL: string): string {
  return baseURL.trim().toLowerCase().replace(/[#/]+$/, '')
}

/**
 * Official OAuth and imported auth.json channels are stored as normalized
 * OAuth JSON in credentials.apiKey. Requiring the official Codex endpoint as
 * well prevents third-party channels that happen to use JSON credentials from
 * being presented as ChatGPT subscription quota channels.
 */
export function isOfficialCodexQuotaChannel(channel: {
  type: string
  baseURL: string
  credentials?: { apiKey?: string | null } | null
}): boolean {
  if (channel.type !== 'codex' || !OFFICIAL_CODEX_BASE_URLS.has(normalizeCodexBaseURL(channel.baseURL))) {
    return false
  }

  try {
    const credentials = JSON.parse(channel.credentials?.apiKey || '')
    const accessToken = credentials?.access_token ?? credentials?.tokens?.access_token
    const refreshToken = credentials?.refresh_token ?? credentials?.tokens?.refresh_token
    return typeof accessToken === 'string' && accessToken.length > 0 && typeof refreshToken === 'string' && refreshToken.length > 0
  } catch {
    return false
  }
}

export async function codexOAuthStart(headers?: Record<string, string>): Promise<{ session_id: string; auth_url: string }> {
  return apiRequest('/admin/codex/oauth/start', {
    method: 'POST',
    body: {},
    headers,
    requireAuth: true,
  })
}

export async function codexOAuthExchange(
  input: {
    session_id: string
    callback_url: string
    proxy?: ProxyConfig
  },
  headers?: Record<string, string>
): Promise<{ credentials: string }> {
  return apiRequest('/admin/codex/oauth/exchange', {
    method: 'POST',
    body: input,
    headers,
    requireAuth: true,
  })
}

export async function codexDecodeAuthJSON(
  input: {
    auth_json: string
  },
  headers?: Record<string, string>
): Promise<{ credentials: string }> {
  return apiRequest('/admin/codex/auth/decode', {
    method: 'POST',
    body: input,
    headers,
    requireAuth: true,
  })
}
