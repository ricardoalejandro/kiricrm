// API helper for Clarin frontend

const API_BASE = process.env.NEXT_PUBLIC_API_URL || ''

// ─── Token Refresh ────────────────────────────────────────────────────────────
// Transparently refreshes the JWT when it expires (401), using the httpOnly
// refresh-token cookie. Only one refresh request runs at a time.

let _refreshPromise: Promise<boolean> | null = null

const IDLE_TIMEOUT_MS = 30 * 60 * 1000 // 30 minutes
const HEARTBEAT_INTERVAL_MS = 5 * 60 * 1000
const ACTIVITY_THROTTLE_MS = 15 * 1000
const LAST_ACTIVITY_KEY = 'clarin:last_activity_at'
const LOGOUT_EVENT_KEY = 'clarin:logout_at'

export function clearAuthState() {
  if (typeof window === 'undefined') return
  localStorage.removeItem('token')
  localStorage.removeItem('kommo_enabled')
  localStorage.removeItem(LAST_ACTIVITY_KEY)
}

export function markAuthActivity(force = false) {
  if (typeof window === 'undefined') return
  if (!localStorage.getItem('token')) return
  const now = Date.now()
  const previous = Number(localStorage.getItem(LAST_ACTIVITY_KEY) || '0')
  if (!force && previous && now - previous < ACTIVITY_THROTTLE_MS) return
  localStorage.setItem(LAST_ACTIVITY_KEY, String(now))
}

export function isAuthIdleExpired() {
  if (typeof window === 'undefined') return false
  const lastActivity = Number(localStorage.getItem(LAST_ACTIVITY_KEY) || '0')
  return lastActivity > 0 && Date.now() - lastActivity >= IDLE_TIMEOUT_MS
}

export async function logoutFromBrowser(reason: 'manual' | 'idle' | 'expired' = 'manual') {
  if (typeof window === 'undefined') return
  const token = localStorage.getItem('token')
  clearIdleTimeout()
  try {
    await fetch(`${API_BASE}/api/auth/logout`, {
      method: 'POST',
      credentials: 'include',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
  } catch {
    // The local state still needs to be cleared even if the network is gone.
  }
  clearAuthState()
  localStorage.setItem(LOGOUT_EVENT_KEY, `${Date.now()}:${reason}`)
  window.location.href = '/login'
}

export async function tryRefreshToken(): Promise<boolean> {
  if (isAuthIdleExpired()) {
    await logoutFromBrowser('idle')
    return false
  }
  // Deduplicate concurrent refresh attempts
  if (_refreshPromise) return _refreshPromise

  _refreshPromise = (async () => {
    try {
      const res = await fetch(`${API_BASE}/api/auth/refresh`, {
        method: 'POST',
        credentials: 'include', // sends httpOnly refresh-token cookie
      })
      if (!res.ok) {
        clearAuthState()
        return false
      }
      const data = await res.json()
      if (data.success && data.token) {
        localStorage.setItem('token', data.token)
        markAuthActivity(true)
        return true
      }
      clearAuthState()
      return false
    } catch {
      return false
    } finally {
      _refreshPromise = null
    }
  })()

  return _refreshPromise
}

let _idleTimer: ReturnType<typeof setTimeout> | null = null
let _heartbeatTimer: ReturnType<typeof setInterval> | null = null
let _idleInitialized = false
let _storageListener: ((event: StorageEvent) => void) | null = null
const _activityEvents = ['mousemove', 'keydown', 'click', 'touchstart', 'scroll', 'visibilitychange']

function getRemainingIdleMs() {
  const lastActivity = Number(localStorage.getItem(LAST_ACTIVITY_KEY) || '0')
  if (!lastActivity) return IDLE_TIMEOUT_MS
  return Math.max(0, IDLE_TIMEOUT_MS - (Date.now() - lastActivity))
}

function scheduleIdleCheck() {
  if (_idleTimer) clearTimeout(_idleTimer)
  const remaining = getRemainingIdleMs()
  _idleTimer = setTimeout(() => {
    if (isAuthIdleExpired()) {
      void logoutFromBrowser('idle')
      return
    }
    scheduleIdleCheck()
  }, Math.max(remaining, 1000))
}

function handleUserActivity() {
  markAuthActivity()
  scheduleIdleCheck()
}

async function sendActivityHeartbeat() {
  if (typeof window === 'undefined') return
  if (!localStorage.getItem('token')) return
  if (isAuthIdleExpired()) {
    await logoutFromBrowser('idle')
    return
  }
  const token = localStorage.getItem('token')
  try {
    const res = await fetch(`${API_BASE}/api/auth/activity`, {
      method: 'POST',
      credentials: 'include',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    if (res.status === 401) {
      const refreshed = await tryRefreshToken()
      if (!refreshed) await logoutFromBrowser('expired')
    }
  } catch {
    // Avoid logging out on transient network hiccups; the next API call will validate.
  }
}

export function initIdleTimeout() {
  if (typeof window === 'undefined' || _idleInitialized) return
  _idleInitialized = true
  if (!localStorage.getItem(LAST_ACTIVITY_KEY)) markAuthActivity(true)

  _activityEvents.forEach(evt => window.addEventListener(evt, handleUserActivity, { passive: true }))
  _storageListener = (event: StorageEvent) => {
    if (event.key === LOGOUT_EVENT_KEY) {
      clearAuthState()
      window.location.href = '/login'
      return
    }
    if (event.key === LAST_ACTIVITY_KEY) scheduleIdleCheck()
  }
  window.addEventListener('storage', _storageListener)
  scheduleIdleCheck()
  _heartbeatTimer = setInterval(() => {
    void sendActivityHeartbeat()
  }, HEARTBEAT_INTERVAL_MS)
}

export function clearIdleTimeout() {
  if (_idleTimer) {
    clearTimeout(_idleTimer)
    _idleTimer = null
  }
  if (_heartbeatTimer) {
    clearInterval(_heartbeatTimer)
    _heartbeatTimer = null
  }
  if (typeof window !== 'undefined') {
    _activityEvents.forEach(evt => window.removeEventListener(evt, handleUserActivity))
    if (_storageListener) window.removeEventListener('storage', _storageListener)
  }
  _storageListener = null
  _idleInitialized = false
}

// ─── Version Detection ────────────────────────────────────────────────────────
// Tracks server version from X-Clarin-Version header and notifies listeners
let _latestServerVersion: string | null = null
const _versionListeners = new Set<(version: string) => void>()

export function getLatestServerVersion(): string | null {
  return _latestServerVersion
}

export function onServerVersionChange(cb: (version: string) => void): () => void {
  _versionListeners.add(cb)
  return () => _versionListeners.delete(cb)
}

function checkVersionHeader(res: Response) {
  const serverVersion = res.headers.get('x-clarin-version')
  if (serverVersion && serverVersion !== _latestServerVersion) {
    _latestServerVersion = serverVersion
    _versionListeners.forEach(cb => {
      try { cb(serverVersion) } catch (e) { console.error('Version listener error:', e) }
    })
  }
}

interface FetchOptions extends RequestInit {
  skipAuth?: boolean
}

export async function api<T>(
  endpoint: string,
  options: FetchOptions = {}
): Promise<{ success: boolean; data?: T; error?: string }> {
  const { skipAuth = false, ...fetchOptions } = options

  if (!skipAuth && isAuthIdleExpired()) {
    await logoutFromBrowser('idle')
    return { success: false, error: 'Sesión expirada por inactividad' }
  }

  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...fetchOptions.headers,
  }

  // Add auth token if available and not skipped
  if (!skipAuth && typeof window !== 'undefined') {
    const token = localStorage.getItem('token')
    if (token) {
      (headers as Record<string, string>)['Authorization'] = `Bearer ${token}`
    }
  }

  try {
    const res = await fetch(`${API_BASE}${endpoint}`, {
      ...fetchOptions,
      headers,
    })

    // Check for server version changes
    checkVersionHeader(res)

    // Handle empty responses (204 No Content, etc.)
    if (res.status === 204 || res.headers.get('content-length') === '0') {
      return { success: true, data: undefined as unknown as T }
    }

    let data: any
    try {
      data = await res.json()
    } catch {
      // Response body is not JSON (empty or non-JSON)
      if (res.ok) return { success: true, data: undefined as unknown as T }
      return { success: false, error: `Error ${res.status}` }
    }

    if (!res.ok) {
      // Handle 401 - try to refresh token before giving up
      if (res.status === 401 && typeof window !== 'undefined' && !skipAuth) {
        const refreshed = await tryRefreshToken()
        if (refreshed) {
          // Retry the original request with the new token
          const newToken = localStorage.getItem('token')
          if (newToken) {
            (headers as Record<string, string>)['Authorization'] = `Bearer ${newToken}`
          }
          const retryRes = await fetch(`${API_BASE}${endpoint}`, {
            ...fetchOptions,
            headers,
          })
          if (retryRes.ok) {
            const retryData = await retryRes.json().catch(() => undefined)
            markAuthActivity()
            return { success: true, data: retryData as T }
          }
        }
        // Refresh failed — session truly expired
        await logoutFromBrowser('expired')
        return { success: false, error: 'Sesión expirada' }
      }
      return { success: false, error: data?.error || `Error ${res.status}` }
    }

    if (!skipAuth) markAuthActivity()
    return { success: true, data: data as T }
  } catch (err) {
    console.error('API Error:', err)
    return { success: false, error: 'Error de conexión' }
  }
}

// Convenience methods
export const apiGet = <T>(endpoint: string) => api<T>(endpoint, { method: 'GET' })

export const apiPost = <T>(endpoint: string, body: unknown) =>
  api<T>(endpoint, {
    method: 'POST',
    body: JSON.stringify(body),
  })

export const apiPut = <T>(endpoint: string, body: unknown) =>
  api<T>(endpoint, {
    method: 'PUT',
    body: JSON.stringify(body),
  })

export const apiDelete = <T>(endpoint: string) =>
  api<T>(endpoint, { method: 'DELETE' })

export async function apiUpload<T = any>(endpoint: string, formData: FormData): Promise<{ success: boolean; data?: T; error?: string }> {
  const getToken = () => typeof window !== 'undefined' ? localStorage.getItem('token') : null
  if (isAuthIdleExpired()) {
    await logoutFromBrowser('idle')
    return { success: false, error: 'Sesión expirada por inactividad' }
  }

  const doFetch = async (token: string | null) => {
    return fetch(`${API_BASE}${endpoint}`, {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: formData,
    })
  }

  try {
    let res = await doFetch(getToken())
    if (res.status === 401 && typeof window !== 'undefined') {
      const refreshed = await tryRefreshToken()
      if (refreshed) {
        res = await doFetch(getToken())
      } else {
        await logoutFromBrowser('expired')
        return { success: false, error: 'Sesión expirada' }
      }
    }
    const data = await res.json().catch(() => undefined)
    if (!res.ok) return { success: false, error: (data as any)?.error || `Error ${res.status}` }
    markAuthActivity()
    return { success: true, data: data as T }
  } catch (err) {
    console.error('Upload Error:', err)
    return { success: false, error: 'Error de conexión' }
  }
}

// WebSocket helper with auto-reconnect and exponential backoff
export function createWebSocket(
  onMessage: (data: unknown) => void,
  onConnect?: (send: (data: string) => void) => void
) {
  if (typeof window === 'undefined') return null

  const token = localStorage.getItem('token')
  if (!token) return null

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const wsUrl = `${protocol}//${window.location.host}/ws?token=${token}`

  let ws: WebSocket | null = null
  let reconnectAttempts = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let intentionallyClosed = false

  function connect() {
    ws = new WebSocket(wsUrl)

    ws.onopen = () => {
      console.log('WebSocket connected')
      reconnectAttempts = 0
      if (onConnect) {
        onConnect((data: string) => {
          if (ws && ws.readyState === WebSocket.OPEN) ws.send(data)
        })
      }
    }

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        onMessage(data)
      } catch (err) {
        console.error('WebSocket parse error:', err)
      }
    }

    ws.onerror = () => {
      // Error is logged by the browser natively
    }

    ws.onclose = () => {
      if (intentionallyClosed) return
      const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), 30000)
      reconnectAttempts++
      console.log(`WebSocket reconnecting in ${delay / 1000}s...`)
      reconnectTimer = setTimeout(connect, delay)
    }
  }

  connect()

  // Return proxy that delegates to current ws instance
  return {
    close() {
      intentionallyClosed = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      if (ws) ws.close()
    },
    get readyState() {
      return ws ? ws.readyState : WebSocket.CLOSED
    },
    send(data: string) {
      if (ws && ws.readyState === WebSocket.OPEN) ws.send(data)
    },
  }
}

// ─── Shared WebSocket Singleton ───────────────────────────────────────────────
// A single WS connection shared across all components. Components subscribe to
// events via callbacks and unsubscribe on unmount. This prevents the
// "WebSocket closed before the connection is established" error caused by
// multiple components each opening/closing their own WS rapidly during navigation.

type WSListener = (data: unknown) => void
type WSConnectListener = (send: (data: string) => void) => void

let _sharedWS: WebSocket | null = null
let _sharedReconnectTimer: ReturnType<typeof setTimeout> | null = null
let _sharedReconnectAttempts = 0
let _sharedIntentionallyClosed = false
let _sharedRefCount = 0
const _sharedListeners = new Set<WSListener>()
const _sharedConnectListeners = new Set<WSConnectListener>()

function _sharedSend(data: string) {
  if (_sharedWS && _sharedWS.readyState === WebSocket.OPEN) {
    _sharedWS.send(data)
  }
}

function _sharedConnect() {
  if (typeof window === 'undefined') return

  const token = localStorage.getItem('token')
  if (!token) return

  // Don't create a new connection if one is already open/connecting
  if (_sharedWS && (_sharedWS.readyState === WebSocket.OPEN || _sharedWS.readyState === WebSocket.CONNECTING)) {
    return
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const wsUrl = `${protocol}//${window.location.host}/ws?token=${token}`

  _sharedWS = new WebSocket(wsUrl)

  _sharedWS.onopen = () => {
    console.log('WebSocket connected')
    _sharedReconnectAttempts = 0
    _sharedConnectListeners.forEach(cb => {
      try { cb(_sharedSend) } catch (e) { console.error('WS connect listener error:', e) }
    })
  }

  _sharedWS.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data)
      _sharedListeners.forEach(cb => {
        try { cb(data) } catch (e) { console.error('WS listener error:', e) }
      })
    } catch (err) {
      console.error('WebSocket parse error:', err)
    }
  }

  _sharedWS.onerror = () => {
    // Logged natively by the browser
  }

  _sharedWS.onclose = () => {
    _sharedWS = null
    if (_sharedIntentionallyClosed || _sharedRefCount <= 0) return
    const delay = Math.min(1000 * Math.pow(2, _sharedReconnectAttempts), 30000)
    _sharedReconnectAttempts++
    console.log(`WebSocket reconnecting in ${delay / 1000}s...`)
    _sharedReconnectTimer = setTimeout(_sharedConnect, delay)
  }
}

/**
 * Subscribe to the shared WebSocket. Returns an unsubscribe function.
 * The WS connection is opened on first subscribe, closed when all unsubscribe.
 */
export function subscribeWebSocket(
  onMessage: WSListener,
  onConnect?: WSConnectListener
): () => void {
  _sharedListeners.add(onMessage)
  if (onConnect) _sharedConnectListeners.add(onConnect)

  _sharedRefCount++
  _sharedIntentionallyClosed = false

  // Ensure connection is alive
  _sharedConnect()

  // If already connected, fire onConnect immediately
  if (onConnect && _sharedWS && _sharedWS.readyState === WebSocket.OPEN) {
    try { onConnect(_sharedSend) } catch (e) { console.error('WS connect listener error:', e) }
  }

  // Return unsubscribe function
  return () => {
    _sharedListeners.delete(onMessage)
    if (onConnect) _sharedConnectListeners.delete(onConnect)
    _sharedRefCount--

    if (_sharedRefCount <= 0) {
      _sharedRefCount = 0
      _sharedIntentionallyClosed = true
      if (_sharedReconnectTimer) {
        clearTimeout(_sharedReconnectTimer)
        _sharedReconnectTimer = null
      }
      if (_sharedWS) {
        _sharedWS.close()
        _sharedWS = null
      }
    }
  }
}

/** Send a message through the shared WebSocket */
export function sendSharedWS(data: string) {
  _sharedSend(data)
}

// Type definitions for API responses
export interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: string
}

export interface Device {
  id: string
  name: string
  phone_number: string
  status: string
  last_seen: string
  created_at: string
}

export interface Chat {
  id: string
  jid: string
  name: string
  last_message: string
  last_message_at: string
  unread_count: number
}

export interface Message {
  id: string
  message_id: string
  from_jid: string
  from_name: string
  body: string
  message_type: string
  media_deleted?: boolean
  is_from_me: boolean
  is_read: boolean
  status: string
  timestamp: string
}

export interface Tag {
  id: string
  account_id: string
  name: string
  color: string
  created_at: string
}

export interface Lead {
  id: string
  jid: string
  contact_id: string | null
  name: string
  last_name: string | null
  short_name: string | null
  phone: string
  email: string
  company: string | null
  age: number | null
  dni: string | null
  birth_date: string | null
  status: string
  notes: string
  tags: string[]
  structured_tags: Tag[] | null
  assigned_to: string
  created_at: string
  updated_at: string
}

export interface Contact {
  id: string
  account_id: string
  device_id: string | null
  jid: string
  phone: string | null
  name: string | null
  custom_name: string | null
  push_name: string | null
  avatar_url: string | null
  email: string | null
  company: string | null
  tags: string[] | null
  structured_tags: Tag[] | null
  notes: string | null
  source: string | null
  is_group: boolean
  created_at: string
  updated_at: string
}

export interface User {
  id: string
  email: string
  name: string
  role: string
}

export interface Account {
  id: string
  name: string
  slug: string
  plan: string
  created_at: string
}

export interface QuickReply {
  id: string
  account_id: string
  shortcut: string
  title: string
  body: string
  created_at: string
  updated_at: string
}
