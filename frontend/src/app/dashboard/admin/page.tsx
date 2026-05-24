'use client'

import { useEffect, useState } from 'react'
import {
  Building2, Users, Plus, Pencil, Trash2, Power, KeyRound,
  Search, X, Shield, ChevronDown, Link2, Lock, CheckSquare, Square, Bot,
  Plug, RefreshCw, AlertTriangle, HardDrive, Database, CheckCircle2,
  Activity, Eye, Send, Clock
} from 'lucide-react'

interface Account {
  id: string
  name: string
  slug: string
  plan: string
  max_devices: number
  max_users_override?: number | null
  max_users_effective?: number
  storage_limit_bytes: number
  is_active: boolean
  mcp_enabled: boolean
  kommo_enabled?: boolean
  subscription_status?: string
  trial_ends_at?: string | null
  current_period_end?: string | null
  grace_ends_at?: string | null
  user_count: number
  device_count: number
  chat_count: number
  created_at: string
}

interface Plan {
  code: string
  name: string
  description: string
  trial_days: number
  is_public: boolean
  sort_order: number
  entitlements?: Record<string, unknown>
}

interface User {
  id: string
  account_id: string
  username: string
  email: string
  display_name: string
  role: string
  is_admin: boolean
  is_super_admin: boolean
  is_active: boolean
  account_name: string
  accounts?: UserAccountAssignment[]
  created_at: string
}

interface UserAccountAssignment {
  account_id: string
  account_name: string
  account_slug: string
  role: string
  role_id?: string
  role_name?: string
  permissions?: string[]
  is_default: boolean
}

interface Role {
  id: string
  name: string
  description: string
  is_system: boolean
  permissions: string[]
  created_at: string
}

interface IntegrationAccount {
  account_id: string
  account_name: string
  account_slug: string
  enabled: boolean
}

interface IntegrationInstance {
  id: string
  provider: string
  scope: string
  name: string
  status: string
  is_active: boolean
  subdomain: string
  client_id: string
  redirect_uri: string
  has_client_secret: boolean
  has_access_token: boolean
  has_refresh_token: boolean
  has_webhook_secret: boolean
  accounts: IntegrationAccount[]
  last_sync_at?: string
  created_at: string
}

interface IntegrationMonitorEntry {
  id: number
  time: string
  source: string
  message: string
  level: string
  account_id?: string
  account_name?: string
  entity_type?: string
  kommo_entity_id?: number
  operation?: string
  status?: string
  direction?: string
  duration_ms?: number
  request_count?: number
  batch_size?: number
  details?: Record<string, unknown>
}

interface IntegrationMonitorData {
  entries: IntegrationMonitorEntry[]
  stats: Record<string, { count?: number; last_at?: string; last_msg?: string }>
}

interface IntegrationOutboxTotals {
  total: number
  pending: number
  processing: number
  errored: number
  retried: number
}

interface IntegrationOutboxItem extends IntegrationOutboxTotals {
  operation: string
  account_id: string
  account_name: string
  oldest_at: string
  last_error: string
}

interface IntegrationOutboxData {
  items: IntegrationOutboxItem[]
  totals: IntegrationOutboxTotals
}

interface IntegrationHealth {
  runtime_running: boolean
  webhook_configured: boolean
  webhook_url: string
  public_url_configured: boolean
  assigned_count: number
  outbox?: IntegrationOutboxTotals
  worker?: { running?: boolean; active_accounts?: number; connected_count?: number; last_check?: string }
  events_poller?: { last_poll_at?: string; seconds_since_last_poll?: number; last_poll_events_found?: number; last_poll_leads_synced?: number }
}

interface NewUserAssignment {
  account_id: string
  role: string
  role_id: string
  is_default: boolean
}

const ALL_MODULES = [
  { key: 'chats', label: 'Chats', color: 'emerald' },
  { key: 'contacts', label: 'Contactos', color: 'blue' },
  { key: 'leads', label: 'Leads', color: 'violet' },
  { key: 'programs', label: 'Programas', color: 'orange' },
  { key: 'automations', label: 'Automatizaciones', color: 'emerald' },
  { key: 'bots', label: 'Bots', color: 'sky' },
  { key: 'devices', label: 'Dispositivos', color: 'cyan' },
  { key: 'events', label: 'Eventos', color: 'pink' },
  { key: 'broadcasts', label: 'Difusión', color: 'yellow' },
  { key: 'tags', label: 'Etiquetas', color: 'teal' },
  { key: 'settings', label: 'Configuración', color: 'slate' },
  { key: 'integrations', label: 'Integraciones', color: 'indigo' },
  { key: 'surveys', label: 'Encuestas', color: 'amber' },
  { key: 'dynamics', label: 'Dinámicas', color: 'rose' },
  { key: 'tasks', label: 'Tareas', color: 'lime' },
  { key: 'documents', label: 'Plantillas', color: 'purple' },
]

function formatBytes(bytes?: number) {
  const value = bytes || 0
  if (value <= 0) return 'Ilimitado'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let idx = 0
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024
    idx++
  }
  return `${size >= 10 || idx === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[idx]}`
}

function gbToBytes(value: number) {
  return Math.max(0, Math.round(value * 1024 * 1024 * 1024))
}

function bytesToGb(value?: number) {
  return value && value > 0 ? Math.round((value / 1024 / 1024 / 1024) * 10) / 10 : 0
}

type Tab = 'accounts' | 'users' | 'roles' | 'integrations'

export default function AdminPage() {
  const [tab, setTab] = useState<Tab>('accounts')
  const [accounts, setAccounts] = useState<Account[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [plans, setPlans] = useState<Plan[]>([])
  const [integrations, setIntegrations] = useState<IntegrationInstance[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [filterAccountId, setFilterAccountId] = useState('')

  // Modals
  const [showAccountModal, setShowAccountModal] = useState(false)
  const [editingAccount, setEditingAccount] = useState<Account | null>(null)
  const [showUserModal, setShowUserModal] = useState(false)
  const [editingUser, setEditingUser] = useState<User | null>(null)
  const [showPasswordModal, setShowPasswordModal] = useState(false)
  const [passwordUserId, setPasswordUserId] = useState('')
  const [newPassword, setNewPassword] = useState('')

  const [showPurgeModal, setShowPurgeModal] = useState(false)
  const [purgeAccount, setPurgeAccount] = useState<Account | null>(null)
  const [purgeSummary, setPurgeSummary] = useState<Record<string, unknown> | null>(null)
  const [purgeConfirmation, setPurgeConfirmation] = useState('')
  const [purgeDeleteFiles, setPurgeDeleteFiles] = useState(true)

  // Account assignments modal
  const [showAssignModal, setShowAssignModal] = useState(false)
  const [assignUserId, setAssignUserId] = useState('')
  const [assignUserName, setAssignUserName] = useState('')
  const [userAssignments, setUserAssignments] = useState<UserAccountAssignment[]>([])
  const [assignAccountId, setAssignAccountId] = useState('')
  const [assignRole, setAssignRole] = useState('agent')
  const [assignRoleId, setAssignRoleId] = useState('')

  // Roles
  const [roles, setRoles] = useState<Role[]>([])
  const [showRoleModal, setShowRoleModal] = useState(false)
  const [editingRole, setEditingRole] = useState<Role | null>(null)
  const [roleForm, setRoleForm] = useState({ name: '', description: '', permissions: [] as string[] })

  const [showIntegrationModal, setShowIntegrationModal] = useState(false)
  const [editingIntegration, setEditingIntegration] = useState<IntegrationInstance | null>(null)
  const [integrationForm, setIntegrationForm] = useState({
    name: '', subdomain: '', client_id: '', client_secret: '', access_token: '', refresh_token: '', redirect_uri: '', webhook_secret: '', is_active: true
  })
  const [selectedIntegrationAccounts, setSelectedIntegrationAccounts] = useState<string[]>([])
  const [showIntegrationMonitor, setShowIntegrationMonitor] = useState(false)
  const [monitorIntegration, setMonitorIntegration] = useState<IntegrationInstance | null>(null)
  const [integrationMonitor, setIntegrationMonitor] = useState<IntegrationMonitorData | null>(null)
  const [integrationHealth, setIntegrationHealth] = useState<IntegrationHealth | null>(null)
  const [integrationOutbox, setIntegrationOutbox] = useState<IntegrationOutboxData | null>(null)
  const [monitorLoading, setMonitorLoading] = useState(false)

  // Account form
  const [accountForm, setAccountForm] = useState({
    name: '', slug: '', plan: 'basic', max_devices: 5, max_users_override: '', storage_limit_gb: 0, mcp_enabled: false,
    subscription_status: 'active', trial_ends_at: '', current_period_end: ''
  })

  // User form
  const [userForm, setUserForm] = useState({
    account_id: '', username: '', email: '', password: '', display_name: '', role: 'agent'
  })
  const [userFormAssignments, setUserFormAssignments] = useState<NewUserAssignment[]>([])

  const token = typeof window !== 'undefined' ? localStorage.getItem('token') : null

  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  }

  async function fetchAccounts() {
    try {
      const res = await fetch('/api/admin/accounts', { headers })
      const data = await res.json()
      if (data.success) setAccounts(data.accounts || [])
    } catch (e) {
      console.error('Failed to fetch accounts:', e)
    }
  }

  async function fetchUsers() {
    try {
      const url = filterAccountId
        ? `/api/admin/users?account_id=${filterAccountId}`
        : '/api/admin/users'
      const res = await fetch(url, { headers })
      const data = await res.json()
      if (data.success) setUsers(data.users || [])
    } catch (e) {
      console.error('Failed to fetch users:', e)
    }
  }

  async function fetchPlans() {
    try {
      const res = await fetch('/api/admin/plans', { headers })
      const data = await res.json()
      if (data.success) setPlans(data.plans || [])
    } catch (e) {
      console.error('Failed to fetch plans:', e)
    }
  }

  async function fetchRoles() {
    try {
      const res = await fetch('/api/admin/roles', { headers })
      const data = await res.json()
      if (data.success) setRoles(data.roles || [])
    } catch (e) {
      console.error('Failed to fetch roles:', e)
    }
  }

  async function fetchIntegrations() {
    try {
      const res = await fetch('/api/admin/integrations?provider=kommo', { headers })
      const data = await res.json()
      if (data.success) setIntegrations(data.integrations || [])
    } catch (e) {
      console.error('Failed to fetch integrations:', e)
    }
  }

  useEffect(() => {
    setLoading(true)
    Promise.all([fetchAccounts(), fetchUsers(), fetchPlans(), fetchRoles(), fetchIntegrations()]).finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    fetchUsers()
  }, [filterAccountId])

  // Close modals on Escape (topmost first)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      if (showIntegrationMonitor) { setShowIntegrationMonitor(false); return }
      if (showPasswordModal) { setShowPasswordModal(false); return }
      if (showPurgeModal) { setShowPurgeModal(false); return }
      if (showIntegrationModal) { setShowIntegrationModal(false); return }
      if (showRoleModal) { setShowRoleModal(false); return }
      if (showAssignModal) { setShowAssignModal(false); return }
      if (showUserModal) { setShowUserModal(false); return }
      if (showAccountModal) { setShowAccountModal(false); return }
    }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [showIntegrationMonitor, showPasswordModal, showPurgeModal, showIntegrationModal, showRoleModal, showAssignModal, showUserModal, showAccountModal])

  // Account CRUD
  function openCreateAccount() {
    setEditingAccount(null)
    setAccountForm({ name: '', slug: '', plan: 'basic', max_devices: 5, max_users_override: '', storage_limit_gb: 0, mcp_enabled: false, subscription_status: 'active', trial_ends_at: '', current_period_end: '' })
    setShowAccountModal(true)
  }

  function openEditAccount(a: Account) {
    setEditingAccount(a)
    setAccountForm({
      name: a.name,
      slug: a.slug,
      plan: a.plan,
      max_devices: a.max_devices,
      max_users_override: a.max_users_override === null || a.max_users_override === undefined ? '' : String(a.max_users_override),
      storage_limit_gb: bytesToGb(a.storage_limit_bytes),
      mcp_enabled: a.mcp_enabled,
      subscription_status: a.subscription_status || 'active',
      trial_ends_at: dateInputValue(a.trial_ends_at),
      current_period_end: dateInputValue(a.current_period_end),
    })
    setShowAccountModal(true)
  }

  async function saveAccount() {
    const method = editingAccount ? 'PUT' : 'POST'
    const url = editingAccount
      ? `/api/admin/accounts/${editingAccount.id}`
      : '/api/admin/accounts'

    const accountPayload = {
      name: accountForm.name,
      slug: accountForm.slug,
      plan: accountForm.plan,
      max_devices: accountForm.max_devices,
      max_users_override: accountForm.max_users_override === '' ? null : Math.max(0, parseInt(accountForm.max_users_override, 10) || 0),
      storage_limit_bytes: gbToBytes(accountForm.storage_limit_gb),
      mcp_enabled: accountForm.mcp_enabled,
    }

    const res = await fetch(url, { method, headers, body: JSON.stringify(accountPayload) })
    const data = await res.json()
    if (data.success) {
      const accountId = editingAccount?.id || data.account?.id
      if (accountId) {
        const subRes = await fetch(`/api/admin/accounts/${accountId}/subscription`, {
          method: 'PUT',
          headers,
          body: JSON.stringify({
            plan_code: accountForm.plan,
            status: accountForm.subscription_status,
            trial_ends_at: accountForm.trial_ends_at,
            current_period_end: accountForm.current_period_end,
          }),
        })
        const subData = await subRes.json()
        if (!subData.success) {
          alert(subData.error || 'La cuenta se guardó, pero no se pudo actualizar la suscripción')
          return
        }
      }
      setShowAccountModal(false)
      fetchAccounts()
    } else {
      alert(data.error || 'Error al guardar')
    }
  }

  async function toggleAccount(id: string) {
    await fetch(`/api/admin/accounts/${id}/toggle`, { method: 'PATCH', headers })
    fetchAccounts()
  }

  async function deleteAccount(id: string) {
    const account = accounts.find(a => a.id === id)
    if (!account) return
    if (account.device_count > 0 || account.chat_count > 0 || account.user_count > 0) {
      await openPurgeAccount(account)
      return
    }
    if (!confirm(`¿Eliminar la cuenta "${account.name}"? Esta acción no se puede deshacer.`)) return
    const res = await fetch(`/api/admin/accounts/${id}`, { method: 'DELETE', headers })
    const data = await res.json()
    if (!data.success) { alert(data.error || 'Error al eliminar'); return }
    fetchAccounts()
    fetchUsers()
  }

  async function openPurgeAccount(account: Account) {
    setPurgeAccount(account)
    setPurgeSummary(null)
    setPurgeConfirmation('')
    setPurgeDeleteFiles(true)
    setShowPurgeModal(true)
    const res = await fetch(`/api/admin/accounts/${account.id}/purge-preview`, { headers })
    const data = await res.json()
    if (data.success) {
      setPurgeSummary(data.summary)
    } else {
      alert(data.error || 'Error al preparar la eliminación')
    }
  }

  async function purgeAccountNow() {
    if (!purgeAccount) return
    const res = await fetch(`/api/admin/accounts/${purgeAccount.id}/purge`, {
      method: 'DELETE', headers,
      body: JSON.stringify({ confirmation: purgeConfirmation, delete_files: purgeDeleteFiles })
    })
    const data = await res.json()
    if (!data.success) {
      alert(data.error || 'Error al purgar cuenta')
      return
    }
    setShowPurgeModal(false)
    setPurgeAccount(null)
    await Promise.all([fetchAccounts(), fetchUsers(), fetchIntegrations()])
  }

  // User CRUD
  function openCreateUser() {
    setEditingUser(null)
    const initialAccount = filterAccountId || accounts.find(a => a.is_active)?.id || ''
    setUserForm({ account_id: initialAccount, username: '', email: '', password: '', display_name: '', role: 'agent' })
    setUserFormAssignments(initialAccount ? [{ account_id: initialAccount, role: 'agent', role_id: '', is_default: true }] : [])
    setShowUserModal(true)
  }

  function openEditUser(u: User) {
    setEditingUser(u)
    setUserForm({ account_id: u.account_id, username: u.username, email: u.email, password: '', display_name: u.display_name, role: u.role })
    setUserFormAssignments([])
    setShowUserModal(true)
  }

  function addUserFormAssignment() {
    const nextAccount = accounts.find(a => a.is_active && !userFormAssignments.some(ua => ua.account_id === a.id))
    if (!nextAccount) return
    setUserFormAssignments(prev => [...prev, { account_id: nextAccount.id, role: 'agent', role_id: '', is_default: prev.length === 0 }])
  }

  function updateUserFormAssignment(index: number, patch: Partial<NewUserAssignment>) {
    setUserFormAssignments(prev => prev.map((item, i) => {
      if (i !== index) return item
      const next = { ...item, ...patch }
      if (patch.role && patch.role !== 'agent') next.role_id = ''
      return next
    }).map((item, i) => patch.is_default && i !== index ? { ...item, is_default: false } : item))
  }

  function removeUserFormAssignment(index: number) {
    setUserFormAssignments(prev => {
      const next = prev.filter((_, i) => i !== index)
      if (next.length > 0 && !next.some(item => item.is_default)) next[0] = { ...next[0], is_default: true }
      return next
    })
  }

  async function saveUser() {
    if (editingUser) {
      const res = await fetch(`/api/admin/users/${editingUser.id}`, {
        method: 'PUT', headers,
        body: JSON.stringify({ username: userForm.username, email: userForm.email, display_name: userForm.display_name, role: userForm.role })
      })
      const data = await res.json()
      if (data.success) {
        setShowUserModal(false)
        fetchUsers()
      } else {
        alert(data.error || 'Error al guardar')
      }
    } else {
      const validAssignments = userFormAssignments.filter(item => item.account_id)
      const res = await fetch('/api/admin/users', {
        method: 'POST', headers, body: JSON.stringify({ ...userForm, accounts: validAssignments })
      })
      const data = await res.json()
      if (data.success) {
        setShowUserModal(false)
        fetchUsers()
      } else {
        alert(data.error || 'Error al crear usuario')
      }
    }
  }

  async function toggleUser(id: string) {
    await fetch(`/api/admin/users/${id}/toggle`, { method: 'PATCH', headers })
    fetchUsers()
  }

  async function deleteUser(id: string) {
    const user = users.find(u => u.id === id)
    if (!user) return
    if (user.is_super_admin) {
      alert('No se puede eliminar un super administrador')
      return
    }
    if (!confirm(`¿Eliminar al usuario "${user.display_name || user.username}"? Esta acción no se puede deshacer.`)) return
    const res = await fetch(`/api/admin/users/${id}`, { method: 'DELETE', headers })
    const data = await res.json()
    if (!data.success) { alert(data.error || 'Error al eliminar'); return }
    fetchUsers()
  }

  async function resetPassword() {
    if (!newPassword) return
    const res = await fetch(`/api/admin/users/${passwordUserId}/password`, {
      method: 'PATCH', headers, body: JSON.stringify({ password: newPassword })
    })
    const data = await res.json()
    if (data.success) {
      setShowPasswordModal(false)
      setNewPassword('')
      alert('Contraseña actualizada')
    } else {
      alert(data.error || 'Error')
    }
  }

  // Account assignments
  async function openAssignModal(u: User) {
    setAssignUserId(u.id)
    setAssignUserName(u.display_name || u.username)
    setAssignAccountId('')
    setAssignRole('agent')
    setAssignRoleId('')
    setShowAssignModal(true)
    await fetchUserAssignments(u.id)
  }

  async function fetchUserAssignments(userId: string) {
    try {
      const res = await fetch(`/api/admin/users/${userId}/accounts`, { headers })
      const data = await res.json()
      if (data.success) setUserAssignments(data.accounts || [])
    } catch (e) {
      console.error('Failed to fetch user accounts:', e)
    }
  }

  async function assignAccount() {
    if (!assignAccountId) return
    const body: Record<string, unknown> = { account_id: assignAccountId, role: assignRole }
    if (assignRoleId) body.role_id = assignRoleId
    const res = await fetch(`/api/admin/users/${assignUserId}/accounts`, {
      method: 'POST', headers,
      body: JSON.stringify(body)
    })
    const data = await res.json()
    if (data.success) {
      setAssignAccountId('')
      setAssignRole('agent')
      setAssignRoleId('')
      await fetchUserAssignments(assignUserId)
    } else {
      alert(data.error || 'Error al asignar')
    }
  }

  async function removeAssignment(accountId: string) {
    if (!confirm('¿Quitar esta cuenta del usuario?')) return
    const res = await fetch(`/api/admin/users/${assignUserId}/accounts/${accountId}`, {
      method: 'DELETE', headers
    })
    const data = await res.json()
    if (data.success) {
      await fetchUserAssignments(assignUserId)
    } else {
      alert(data.error || 'Error al quitar')
    }
  }

  const filteredAccounts = accounts.filter(a =>
    a.name.toLowerCase().includes(search.toLowerCase()) ||
    a.plan.toLowerCase().includes(search.toLowerCase())
  )

  const filteredUsers = users.filter(u =>
    u.username.toLowerCase().includes(search.toLowerCase()) ||
    u.email.toLowerCase().includes(search.toLowerCase()) ||
    (u.display_name || '').toLowerCase().includes(search.toLowerCase())
  )

  const filteredRoles = roles.filter(r =>
    r.name.toLowerCase().includes(search.toLowerCase()) ||
    r.description.toLowerCase().includes(search.toLowerCase())
  )

  const filteredIntegrations = integrations.filter(i =>
    i.name.toLowerCase().includes(search.toLowerCase()) ||
    i.provider.toLowerCase().includes(search.toLowerCase()) ||
    (i.subdomain || '').toLowerCase().includes(search.toLowerCase())
  )

  // Role CRUD
  function openCreateRole() {
    setEditingRole(null)
    setRoleForm({ name: '', description: '', permissions: [] })
    setShowRoleModal(true)
  }

  function openEditRole(r: Role) {
    setEditingRole(r)
    setRoleForm({ name: r.name, description: r.description, permissions: [...r.permissions] })
    setShowRoleModal(true)
  }

  function toggleModulePermission(module: string) {
    setRoleForm(f => ({
      ...f,
      permissions: f.permissions.includes(module)
        ? f.permissions.filter(p => p !== module)
        : [...f.permissions, module]
    }))
  }

  async function saveRole() {
    if (!roleForm.name.trim()) { alert('El nombre del rol es requerido'); return }
    const method = editingRole ? 'PUT' : 'POST'
    const url = editingRole ? `/api/admin/roles/${editingRole.id}` : '/api/admin/roles'
    const res = await fetch(url, { method, headers, body: JSON.stringify(roleForm) })
    const data = await res.json()
    if (data.success) {
      setShowRoleModal(false)
      fetchRoles()
    } else {
      alert(data.error || 'Error al guardar rol')
    }
  }

  async function deleteRole(id: string) {
    const role = roles.find(r => r.id === id)
    if (!role) return
    if (role.is_system) { alert('Los roles del sistema no pueden eliminarse'); return }
    if (!confirm(`¿Eliminar el rol "${role.name}"? Los usuarios asignados a este rol perderán sus permisos.`)) return
    const res = await fetch(`/api/admin/roles/${id}`, { method: 'DELETE', headers })
    const data = await res.json()
    if (!data.success) { alert(data.error || 'Error al eliminar'); return }
    fetchRoles()
  }

  function openCreateIntegration() {
    setEditingIntegration(null)
    setIntegrationForm({ name: '', subdomain: '', client_id: '', client_secret: '', access_token: '', refresh_token: '', redirect_uri: '', webhook_secret: '', is_active: true })
    setSelectedIntegrationAccounts([])
    setShowIntegrationModal(true)
  }

  function openEditIntegration(instance: IntegrationInstance) {
    setEditingIntegration(instance)
    setIntegrationForm({
      name: instance.name,
      subdomain: instance.subdomain,
      client_id: instance.client_id || '',
      client_secret: '',
      access_token: '',
      refresh_token: '',
      redirect_uri: instance.redirect_uri || '',
      webhook_secret: '',
      is_active: instance.is_active,
    })
    setSelectedIntegrationAccounts(instance.accounts?.filter(a => a.enabled).map(a => a.account_id) || [])
    setShowIntegrationModal(true)
  }

  function toggleSelectedIntegrationAccount(accountId: string) {
    setSelectedIntegrationAccounts(prev => prev.includes(accountId)
      ? prev.filter(id => id !== accountId)
      : [...prev, accountId]
    )
  }

  async function syncIntegrationAccounts(instance: IntegrationInstance, desired: string[]) {
    const current = new Set((instance.accounts || []).filter(a => a.enabled).map(a => a.account_id))
    const desiredSet = new Set(desired)
    await Promise.all([
      ...desired.filter(id => !current.has(id)).map(accountId => fetch(`/api/admin/integrations/${instance.id}/accounts`, {
        method: 'POST', headers, body: JSON.stringify({ account_id: accountId, enabled: true })
      })),
      ...(instance.accounts || []).filter(a => current.has(a.account_id) && !desiredSet.has(a.account_id)).map(a => fetch(`/api/admin/integrations/${instance.id}/accounts/${a.account_id}`, {
        method: 'DELETE', headers
      })),
    ])
  }

  async function saveIntegration() {
    if (!integrationForm.name.trim()) { alert('El nombre de la integración es requerido'); return }
    const method = editingIntegration ? 'PUT' : 'POST'
    const url = editingIntegration ? `/api/admin/integrations/${editingIntegration.id}` : '/api/admin/integrations'
    const payload = {
      provider: 'kommo', scope: 'multi_account', status: 'active', ...integrationForm,
      accounts: editingIntegration ? [] : selectedIntegrationAccounts,
    }
    const res = await fetch(url, { method, headers, body: JSON.stringify(payload) })
    const data = await res.json()
    if (!data.success) { alert(data.error || 'Error al guardar integración'); return }
    if (editingIntegration) {
      await syncIntegrationAccounts(editingIntegration, selectedIntegrationAccounts)
      await fetch(`/api/admin/integrations/${editingIntegration.id}/reload`, { method: 'POST', headers })
    }
    setShowIntegrationModal(false)
    await Promise.all([fetchIntegrations(), fetchAccounts()])
  }

  async function deleteIntegration(id: string) {
    const instance = integrations.find(i => i.id === id)
    if (!instance) return
    if (!confirm(`¿Eliminar la integración "${instance.name}"? Las cuentas asignadas dejarán de sincronizar Kommo.`)) return
    const res = await fetch(`/api/admin/integrations/${id}`, { method: 'DELETE', headers })
    const data = await res.json()
    if (!data.success) { alert(data.error || 'Error al eliminar integración'); return }
    await Promise.all([fetchIntegrations(), fetchAccounts()])
  }

  async function reloadIntegrations() {
    const first = integrations[0]
    if (!first) return fetchIntegrations()
    await fetch(`/api/admin/integrations/${first.id}/reload`, { method: 'POST', headers })
    fetchIntegrations()
  }

  async function refreshIntegrationDiagnostics(instance: IntegrationInstance | null = monitorIntegration) {
    if (!instance) return
    setMonitorLoading(true)
    try {
      const [monitorRes, healthRes, outboxRes] = await Promise.all([
        fetch(`/api/admin/integrations/${instance.id}/monitor`, { headers }),
        fetch(`/api/admin/integrations/${instance.id}/health`, { headers }),
        fetch(`/api/admin/integrations/${instance.id}/outbox`, { headers }),
      ])
      const [monitorData, healthData, outboxData] = await Promise.all([
        monitorRes.json(), healthRes.json(), outboxRes.json()
      ])
      if (monitorData.success) setIntegrationMonitor(monitorData.monitor || null)
      if (healthData.success) setIntegrationHealth(healthData.health || null)
      if (outboxData.success) setIntegrationOutbox(outboxData.outbox || null)
    } catch (e) {
      console.error('Failed to fetch integration diagnostics:', e)
    } finally {
      setMonitorLoading(false)
    }
  }

  function openIntegrationDiagnostics(instance: IntegrationInstance) {
    setMonitorIntegration(instance)
    setIntegrationMonitor(null)
    setIntegrationHealth(null)
    setIntegrationOutbox(null)
    setShowIntegrationMonitor(true)
    refreshIntegrationDiagnostics(instance)
  }

  async function forceIntegrationPoll() {
    if (!monitorIntegration) return
    setMonitorLoading(true)
    try {
      const res = await fetch(`/api/admin/integrations/${monitorIntegration.id}/poll`, { method: 'POST', headers })
      const data = await res.json()
      if (!data.success) {
        alert(data.error || 'No se pudo forzar el pull')
      }
      await refreshIntegrationDiagnostics(monitorIntegration)
    } catch (e) {
      console.error('Failed to force integration poll:', e)
    } finally {
      setMonitorLoading(false)
    }
  }

  const planColors: Record<string, string> = {
    free: 'bg-slate-100 text-slate-600',
    trial: 'bg-amber-100 text-amber-700',
    basic: 'bg-gray-100 text-gray-700',
    starter: 'bg-emerald-100 text-emerald-700',
    pro: 'bg-blue-100 text-blue-700',
    business: 'bg-cyan-100 text-cyan-700',
    enterprise: 'bg-purple-100 text-purple-700',
    internal: 'bg-slate-800 text-white',
  }

  const subscriptionColors: Record<string, string> = {
    trialing: 'bg-amber-100 text-amber-700',
    active: 'bg-emerald-100 text-emerald-700',
    past_due: 'bg-orange-100 text-orange-700',
    grace: 'bg-yellow-100 text-yellow-700',
    suspended: 'bg-red-100 text-red-700',
    canceled: 'bg-slate-100 text-slate-500',
    incomplete: 'bg-gray-100 text-gray-600',
  }

  const subscriptionLabels: Record<string, string> = {
    trialing: 'Prueba',
    active: 'Activa',
    past_due: 'Pendiente',
    grace: 'Gracia',
    suspended: 'Suspendida',
    canceled: 'Cancelada',
    incomplete: 'Incompleta',
  }

  const fallbackPlans: Plan[] = [
    { code: 'basic', name: 'Basic', description: '', trial_days: 0, is_public: true, sort_order: 20 },
    { code: 'pro', name: 'Pro', description: '', trial_days: 14, is_public: true, sort_order: 40 },
    { code: 'enterprise', name: 'Enterprise', description: '', trial_days: 0, is_public: true, sort_order: 60 },
  ]

  const planOptions = plans.length > 0 ? plans : fallbackPlans

  function planMaxUsers(planCode: string) {
    const plan = planOptions.find(p => p.code === planCode)
    const raw = plan?.entitlements?.max_users
    if (typeof raw === 'number') return raw
    if (typeof raw === 'string') {
      const parsed = parseInt(raw, 10)
      return Number.isFinite(parsed) ? parsed : null
    }
    return null
  }

  function userLimitLabel(account: Account) {
    const effective = account.max_users_effective ?? planMaxUsers(account.plan)
    if (effective === 0) return `${account.user_count}/Sin límite`
    if (effective && effective > 0) return `${account.user_count}/${effective}`
    return String(account.user_count)
  }

  function dateInputValue(value?: string | null) {
    if (!value) return ''
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) return ''
    return parsed.toISOString().slice(0, 10)
  }

  function daysUntil(value?: string | null) {
    if (!value) return null
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) return null
    const diff = parsed.getTime() - Date.now()
    return Math.max(0, Math.ceil(diff / 86400000))
  }

  function subscriptionDateFor(account: Account) {
    if (account.subscription_status === 'trialing') return account.trial_ends_at
    if (account.subscription_status === 'grace') return account.grace_ends_at
    return account.current_period_end
  }

  const roleLabels: Record<string, string> = {
    super_admin: 'Super Admin',
    admin: 'Admin',
    agent: 'Agente',
  }

  const roleColors: Record<string, string> = {
    super_admin: 'bg-red-100 text-red-700',
    admin: 'bg-blue-100 text-blue-700',
    agent: 'bg-gray-100 text-gray-700',
  }

  function roleDisplay(role: string, roleId?: string, roleName?: string) {
    if (roleName) return roleName
    if (roleId) return roles.find(r => r.id === roleId)?.name || 'Rol manual'
    return roleLabels[role] || role
  }

  function sourceLabel(source: string) {
    const labels: Record<string, string> = {
      webhook: 'Webhook',
      events_poller: 'Pull',
      push: 'Push',
      reconcile: 'Reconcile',
    }
    return labels[source] || source
  }

  function sourceClass(source: string) {
    const classes: Record<string, string> = {
      webhook: 'bg-cyan-100 text-cyan-700',
      events_poller: 'bg-emerald-100 text-emerald-700',
      push: 'bg-indigo-100 text-indigo-700',
      reconcile: 'bg-amber-100 text-amber-700',
    }
    return classes[source] || 'bg-slate-100 text-slate-700'
  }

  function statusClass(level?: string, status?: string) {
    if (level === 'error' || status === 'error' || status === 'failed') return 'bg-red-100 text-red-700'
    if (status === 'skipped' || status === 'no_changes') return 'bg-slate-100 text-slate-600'
    if (status === 'pushed' || status === 'updated' || status === 'fixed') return 'bg-emerald-100 text-emerald-700'
    return 'bg-slate-100 text-slate-700'
  }

  function formatDateTime(value?: string) {
    if (!value) return 'Sin datos'
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    return date.toLocaleString('es-PE', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })
  }

  function detailAccounts(details?: Record<string, unknown>) {
    const affected = details?.affected_accounts
    const accountsDetail = details?.accounts
    const source = Array.isArray(affected) ? affected : Array.isArray(accountsDetail) ? accountsDetail : []
    return source.slice(0, 8).map(item => {
      if (typeof item === 'string') return item
      if (item && typeof item === 'object') {
        const data = item as Record<string, unknown>
        return String(data.name || data.id || '')
      }
      return ''
    }).filter(Boolean)
  }

  const purgeTables = purgeSummary?.tables as Record<string, number | null> | undefined
  const purgeStorageObjects = Number(purgeSummary?.storage_objects || 0)
  const purgeRecordCount = purgeTables
    ? Object.values(purgeTables).reduce<number>((sum, value) => sum + (typeof value === 'number' ? value : 0), 0)
    : 0
  const monitorEntries = integrationMonitor?.entries || []
  const monitorStats = integrationMonitor?.stats || {}
  const outboxTotals = integrationOutbox?.totals || integrationHealth?.outbox || { total: 0, pending: 0, processing: 0, errored: 0, retried: 0 }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full bg-slate-900">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-emerald-500" />
      </div>
    )
  }

  return (
    <div className="h-full flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 flex items-center gap-2">
            <Shield className="w-7 h-7 text-green-600" />
            Administración
          </h1>
          <p className="text-gray-500 text-sm mt-1">Gestión de cuentas y usuarios de la plataforma</p>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 bg-gray-100 p-1 rounded-lg w-fit mb-4">
        <button
          onClick={() => { setTab('accounts'); setSearch('') }}
          className={`flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors ${
            tab === 'accounts' ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-700'
          }`}
        >
          <Building2 className="w-4 h-4" /> Cuentas
          <span className="ml-1 bg-gray-200 text-gray-600 rounded-full px-2 py-0.5 text-xs">{accounts.length}</span>
        </button>
        <button
          onClick={() => { setTab('users'); setSearch('') }}
          className={`flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors ${
            tab === 'users' ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-700'
          }`}
        >
          <Users className="w-4 h-4" /> Usuarios
          <span className="ml-1 bg-gray-200 text-gray-600 rounded-full px-2 py-0.5 text-xs">{users.length}</span>
        </button>
        <button
          onClick={() => { setTab('roles'); setSearch('') }}
          className={`flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors ${
            tab === 'roles' ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-700'
          }`}
        >
          <Lock className="w-4 h-4" /> Roles
          <span className="ml-1 bg-gray-200 text-gray-600 rounded-full px-2 py-0.5 text-xs">{roles.length}</span>
        </button>
        <button
          onClick={() => { setTab('integrations'); setSearch('') }}
          className={`flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors ${
            tab === 'integrations' ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-700'
          }`}
        >
          <Plug className="w-4 h-4" /> Integraciones
          <span className="ml-1 bg-gray-200 text-gray-600 rounded-full px-2 py-0.5 text-xs">{integrations.length}</span>
        </button>
      </div>

      {/* Search & Actions */}
      <div className="flex flex-col sm:flex-row gap-3 mb-4">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
          <input
            type="text"
            placeholder={tab === 'accounts' ? 'Buscar cuentas...' : tab === 'users' ? 'Buscar usuarios...' : tab === 'roles' ? 'Buscar roles...' : 'Buscar integraciones...'}
            value={search}
            onChange={e => setSearch(e.target.value)}
            className="w-full pl-10 pr-4 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500 focus:border-transparent"
          />
          {search && (
            <button onClick={() => setSearch('')} className="absolute right-3 top-1/2 -translate-y-1/2">
              <X className="w-4 h-4 text-gray-400" />
            </button>
          )}
        </div>

        {tab === 'users' && (
          <select
            value={filterAccountId}
            onChange={e => setFilterAccountId(e.target.value)}
            className="px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
          >
            <option value="">Todas las cuentas</option>
            {accounts.map(a => (
              <option key={a.id} value={a.id}>{a.name}</option>
            ))}
          </select>
        )}

        {tab === 'integrations' ? (
          <div className="flex items-center gap-2">
            <button
              onClick={reloadIntegrations}
              className="flex items-center gap-2 px-3 py-2 bg-slate-700 text-slate-100 rounded-lg hover:bg-slate-600 transition-colors text-sm font-medium whitespace-nowrap"
            >
              <RefreshCw className="w-4 h-4" /> Recargar
            </button>
            <button
              onClick={openCreateIntegration}
              className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 transition-colors text-sm font-medium whitespace-nowrap"
            >
              <Plus className="w-4 h-4" /> Nueva Integración
            </button>
          </div>
        ) : tab !== 'roles' ? (
          <button
            onClick={tab === 'accounts' ? openCreateAccount : openCreateUser}
            className="flex items-center gap-2 px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 transition-colors text-sm font-medium whitespace-nowrap"
          >
            <Plus className="w-4 h-4" />
            {tab === 'accounts' ? 'Nueva Cuenta' : 'Nuevo Usuario'}
          </button>
        ) : (
          <button
            onClick={openCreateRole}
            className="flex items-center gap-2 px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 transition-colors text-sm font-medium whitespace-nowrap"
          >
            <Plus className="w-4 h-4" /> Nuevo Rol
          </button>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto bg-white rounded-xl border border-gray-200">
        {tab === 'accounts' ? (
          <table className="w-full text-sm">
            <thead className="bg-gray-50 sticky top-0">
              <tr>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Cuenta</th>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Plan</th>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Suscripción</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Usuarios</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Dispositivos</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Chats</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Espacio</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Estado</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">MCP</th>
                <th className="text-right px-4 py-3 font-medium text-gray-500">Acciones</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {filteredAccounts.length === 0 ? (
                <tr><td colSpan={10} className="px-4 py-8 text-center text-gray-400">No se encontraron cuentas</td></tr>
              ) : filteredAccounts.map(a => (
                <tr key={a.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <div className="font-medium text-gray-900">{a.name}</div>
                    {a.slug && <div className="text-xs text-gray-400">{a.slug}</div>}
                  </td>
                  <td className="px-4 py-3">
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${planColors[a.plan] || 'bg-gray-100 text-gray-700'}`}>
                      {a.plan}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-col gap-1">
                      <span className={`inline-flex w-fit px-2 py-0.5 rounded-full text-xs font-medium ${subscriptionColors[a.subscription_status || 'active'] || 'bg-gray-100 text-gray-700'}`}>
                        {subscriptionLabels[a.subscription_status || 'active'] || a.subscription_status || 'Activa'}
                      </span>
                      {subscriptionDateFor(a) && (
                        <span className="text-[11px] text-gray-400">
                          {daysUntil(subscriptionDateFor(a))} días restantes
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-center text-gray-600">
                    <span title={a.max_users_override === null || a.max_users_override === undefined ? 'Límite según plan' : 'Límite personalizado'}>
                      {userLimitLabel(a)}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-center text-gray-600">{a.device_count}/{a.max_devices}</td>
                  <td className="px-4 py-3 text-center text-gray-600">{a.chat_count}</td>
                  <td className="px-4 py-3 text-center text-gray-600">{formatBytes(a.storage_limit_bytes)}</td>
                  <td className="px-4 py-3 text-center">
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${a.is_active ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>
                      {a.is_active ? 'Activa' : 'Inactiva'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-center">
                    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${a.mcp_enabled ? 'bg-violet-100 text-violet-700' : 'bg-gray-100 text-gray-400'}`}>
                      <Bot className="w-3 h-3" />
                      {a.mcp_enabled ? 'Sí' : 'No'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button onClick={() => openEditAccount(a)} className="p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded" title="Editar">
                        <Pencil className="w-4 h-4" />
                      </button>
                      <button onClick={() => toggleAccount(a.id)} className="p-1.5 text-gray-400 hover:text-yellow-600 hover:bg-yellow-50 rounded" title={a.is_active ? 'Desactivar' : 'Activar'}>
                        <Power className="w-4 h-4" />
                      </button>
                      <button onClick={() => deleteAccount(a.id)} className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded" title="Eliminar">
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : tab === 'users' ? (
          <table className="w-full text-sm">
            <thead className="bg-gray-50 sticky top-0">
              <tr>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Usuario</th>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Email</th>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Cuenta</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Rol</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Estado</th>
                <th className="text-right px-4 py-3 font-medium text-gray-500">Acciones</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {filteredUsers.length === 0 ? (
                <tr><td colSpan={6} className="px-4 py-8 text-center text-gray-400">No se encontraron usuarios</td></tr>
              ) : filteredUsers.map(u => (
                <tr key={u.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <div className="font-medium text-gray-900">{u.display_name || u.username}</div>
                    <div className="text-xs text-gray-400">@{u.username}</div>
                  </td>
                  <td className="px-4 py-3 text-gray-600">{u.email}</td>
                  <td className="px-4 py-3 text-gray-600 max-w-[280px]">
                    <div className="flex flex-wrap gap-1">
                      {(u.accounts || []).length > 0 ? (u.accounts || []).map(ua => (
                        <span key={ua.account_id} className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-slate-100 text-slate-700" title={ua.account_name}>
                          {ua.account_name}
                          <span className="text-slate-400">·</span>
                          {roleDisplay(ua.role, ua.role_id, ua.role_name)}
                        </span>
                      )) : (
                        <span>{u.account_name}</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-center">
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${roleColors[u.role] || 'bg-gray-100 text-gray-700'}`}>
                      {roleDisplay(u.role)}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-center">
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${u.is_active ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>
                      {u.is_active ? 'Activo' : 'Inactivo'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button onClick={() => openEditUser(u)} className="p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded" title="Editar">
                        <Pencil className="w-4 h-4" />
                      </button>
                      <button onClick={() => openAssignModal(u)} className="p-1.5 text-gray-400 hover:text-green-600 hover:bg-green-50 rounded" title="Gestionar cuentas">
                        <Link2 className="w-4 h-4" />
                      </button>
                      <button onClick={() => { setPasswordUserId(u.id); setNewPassword(''); setShowPasswordModal(true) }} className="p-1.5 text-gray-400 hover:text-purple-600 hover:bg-purple-50 rounded" title="Cambiar contraseña">
                        <KeyRound className="w-4 h-4" />
                      </button>
                      <button onClick={() => toggleUser(u.id)} className="p-1.5 text-gray-400 hover:text-yellow-600 hover:bg-yellow-50 rounded" title={u.is_active ? 'Desactivar' : 'Activar'}>
                        <Power className="w-4 h-4" />
                      </button>
                      {!u.is_super_admin && (
                        <button onClick={() => deleteUser(u.id)} className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded" title="Eliminar">
                          <Trash2 className="w-4 h-4" />
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : tab === 'roles' ? (
          /* Roles Table */
          <table className="w-full text-sm">
            <thead className="bg-gray-50 sticky top-0">
              <tr>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Rol</th>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Permisos</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Tipo</th>
                <th className="text-right px-4 py-3 font-medium text-gray-500">Acciones</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {filteredRoles.length === 0 ? (
                <tr><td colSpan={4} className="px-4 py-8 text-center text-gray-400">No se encontraron roles</td></tr>
              ) : filteredRoles.map(r => (
                <tr key={r.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <div className="font-medium text-gray-900">{r.name}</div>
                    {r.description && <div className="text-xs text-gray-400 mt-0.5">{r.description}</div>}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {r.permissions.length === 0 ? (
                        <span className="text-xs text-gray-400 italic">Sin permisos</span>
                      ) : r.permissions.map(p => {
                        const mod = ALL_MODULES.find(m => m.key === p)
                        return (
                          <span key={p} className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-emerald-100 text-emerald-700">
                            {mod?.label || p}
                          </span>
                        )
                      })}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-center">
                    {r.is_system ? (
                      <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-700">Sistema</span>
                    ) : (
                      <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-600">Custom</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button onClick={() => openEditRole(r)} className="p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded" title="Editar">
                        <Pencil className="w-4 h-4" />
                      </button>
                      {!r.is_system && (
                        <button onClick={() => deleteRole(r.id)} className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded" title="Eliminar">
                          <Trash2 className="w-4 h-4" />
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <table className="w-full text-sm">
            <thead className="bg-gray-50 sticky top-0">
              <tr>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Integración</th>
                <th className="text-left px-4 py-3 font-medium text-gray-500">Licencia</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Cuentas</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Credenciales</th>
                <th className="text-center px-4 py-3 font-medium text-gray-500">Estado</th>
                <th className="text-right px-4 py-3 font-medium text-gray-500">Acciones</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {filteredIntegrations.length === 0 ? (
                <tr><td colSpan={6} className="px-4 py-8 text-center text-gray-400">No se encontraron integraciones</td></tr>
              ) : filteredIntegrations.map(instance => (
                <tr key={instance.id} className="hover:bg-gray-50">
                  <td className="px-4 py-3">
                    <div className="font-medium text-gray-900 flex items-center gap-2">
                      <Plug className="w-4 h-4 text-emerald-600" />
                      {instance.name}
                    </div>
                    <div className="text-xs text-gray-400">{instance.provider} · {instance.scope}</div>
                  </td>
                  <td className="px-4 py-3 text-gray-600">
                    {instance.subdomain ? `${instance.subdomain}.kommo.com` : 'Sin subdominio'}
                  </td>
                  <td className="px-4 py-3 text-center">
                    <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-emerald-100 text-emerald-700">
                      {(instance.accounts || []).filter(a => a.enabled).length} cuentas
                    </span>
                  </td>
                  <td className="px-4 py-3 text-center">
                    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${instance.has_access_token ? 'bg-emerald-100 text-emerald-700' : 'bg-red-100 text-red-700'}`}>
                      {instance.has_access_token ? <CheckCircle2 className="w-3 h-3" /> : <AlertTriangle className="w-3 h-3" />}
                      Token
                    </span>
                  </td>
                  <td className="px-4 py-3 text-center">
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${instance.is_active ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}`}>
                      {instance.is_active ? 'Activa' : 'Inactiva'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button onClick={() => openIntegrationDiagnostics(instance)} className="p-1.5 text-gray-400 hover:text-emerald-600 hover:bg-emerald-50 rounded" title="Monitor">
                        <Activity className="w-4 h-4" />
                      </button>
                      <button onClick={() => openEditIntegration(instance)} className="p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded" title="Editar">
                        <Pencil className="w-4 h-4" />
                      </button>
                      <button onClick={() => deleteIntegration(instance.id)} className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded" title="Eliminar">
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Integration Monitor Modal */}
      {showIntegrationMonitor && monitorIntegration && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-6xl max-h-[90vh] flex flex-col">
            <div className="p-5 border-b border-gray-200 flex items-start justify-between gap-4">
              <div>
                <div className="flex items-center gap-2 text-xs font-medium text-emerald-700 mb-1">
                  <Activity className="w-4 h-4" /> Monitor Kommo
                </div>
                <h2 className="text-lg font-semibold text-gray-900">{monitorIntegration.name}</h2>
                <p className="text-sm text-gray-500 mt-1">
                  {monitorIntegration.subdomain ? `${monitorIntegration.subdomain}.kommo.com` : 'Sin subdominio'} · {integrationHealth?.assigned_count ?? monitorIntegration.accounts?.filter(a => a.enabled).length ?? 0} cuentas asignadas
                </p>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => refreshIntegrationDiagnostics()}
                  disabled={monitorLoading}
                  className="flex items-center gap-2 px-3 py-2 text-sm font-medium text-slate-700 bg-slate-100 rounded-lg hover:bg-slate-200 disabled:opacity-60"
                >
                  <RefreshCw className={`w-4 h-4 ${monitorLoading ? 'animate-spin' : ''}`} /> Actualizar
                </button>
                <button
                  onClick={forceIntegrationPoll}
                  disabled={monitorLoading || !integrationHealth?.runtime_running}
                  className="flex items-center gap-2 px-3 py-2 text-sm font-medium text-white bg-emerald-600 rounded-lg hover:bg-emerald-700 disabled:opacity-50"
                >
                  <Send className="w-4 h-4" /> Forzar pull
                </button>
                <button onClick={() => setShowIntegrationMonitor(false)} className="p-2 text-gray-400 hover:text-gray-700 hover:bg-gray-100 rounded-lg">
                  <X className="w-5 h-5" />
                </button>
              </div>
            </div>

            <div className="p-5 overflow-auto space-y-5">
              <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
                <div className="rounded-lg border border-gray-200 p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-medium text-gray-500">Runtime</span>
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${integrationHealth?.runtime_running ? 'bg-emerald-100 text-emerald-700' : 'bg-red-100 text-red-700'}`}>
                      {integrationHealth?.runtime_running ? 'Activo' : 'Inactivo'}
                    </span>
                  </div>
                  <div className="text-2xl font-semibold text-gray-900">{integrationHealth?.worker?.active_accounts ?? 0}</div>
                  <div className="text-xs text-gray-400 mt-1">cuentas en sync</div>
                </div>
                <div className="rounded-lg border border-gray-200 p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-medium text-gray-500">Webhook</span>
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${integrationHealth?.webhook_configured ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'}`}>
                      {integrationHealth?.webhook_configured ? 'Configurado' : 'Pendiente'}
                    </span>
                  </div>
                  <div className="text-xs text-gray-600 truncate" title={integrationHealth?.webhook_url || ''}>{integrationHealth?.webhook_url || 'Sin URL'}</div>
                  <div className="text-xs text-gray-400 mt-2">Public URL: {integrationHealth?.public_url_configured ? 'sí' : 'no'}</div>
                </div>
                <div className="rounded-lg border border-gray-200 p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-medium text-gray-500">Outbox</span>
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${outboxTotals.errored > 0 ? 'bg-red-100 text-red-700' : outboxTotals.pending > 0 ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'}`}>
                      {outboxTotals.total} items
                    </span>
                  </div>
                  <div className="grid grid-cols-3 gap-2 text-center text-xs">
                    <div><div className="text-lg font-semibold text-amber-600">{outboxTotals.pending}</div><div className="text-gray-400">pend.</div></div>
                    <div><div className="text-lg font-semibold text-blue-600">{outboxTotals.processing}</div><div className="text-gray-400">proc.</div></div>
                    <div><div className="text-lg font-semibold text-red-600">{outboxTotals.errored}</div><div className="text-gray-400">err.</div></div>
                  </div>
                </div>
                <div className="rounded-lg border border-gray-200 p-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-medium text-gray-500">Pull</span>
                    <Clock className="w-4 h-4 text-gray-400" />
                  </div>
                  <div className="text-sm font-medium text-gray-900">{formatDateTime(integrationHealth?.events_poller?.last_poll_at)}</div>
                  <div className="text-xs text-gray-400 mt-1">
                    {integrationHealth?.events_poller?.last_poll_events_found ?? 0} eventos · {integrationHealth?.events_poller?.last_poll_leads_synced ?? 0} leads
                  </div>
                </div>
              </div>

              <div className="grid grid-cols-1 xl:grid-cols-3 gap-5">
                <div className="xl:col-span-2 rounded-lg border border-gray-200 overflow-hidden">
                  <div className="px-4 py-3 bg-gray-50 border-b border-gray-200 flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-gray-900">Timeline</h3>
                    <span className="text-xs text-gray-400">{monitorEntries.length} eventos recientes</span>
                  </div>
                  <div className="divide-y divide-gray-100 max-h-[420px] overflow-auto">
                    {monitorLoading && monitorEntries.length === 0 ? (
                      <div className="p-8 text-center text-sm text-gray-400">Cargando monitor...</div>
                    ) : monitorEntries.length === 0 ? (
                      <div className="p-8 text-center text-sm text-gray-400">Sin eventos recientes</div>
                    ) : monitorEntries.map(entry => {
                      const accountsDetail = detailAccounts(entry.details)
                      return (
                        <div key={entry.id} className="p-4 hover:bg-gray-50">
                          <div className="flex flex-wrap items-center gap-2 mb-2">
                            <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${sourceClass(entry.source)}`}>{sourceLabel(entry.source)}</span>
                            <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${statusClass(entry.level, entry.status)}`}>{entry.status || entry.level}</span>
                            {entry.operation && <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-slate-100 text-slate-600">{entry.operation}</span>}
                            {entry.account_name && <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-emerald-50 text-emerald-700">{entry.account_name}</span>}
                            <span className="ml-auto text-xs text-gray-400">{formatDateTime(entry.time)}</span>
                          </div>
                          <p className="text-sm text-gray-800">{entry.message}</p>
                          <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-gray-400">
                            {entry.direction && <span>{entry.direction}</span>}
                            {entry.entity_type && <span>{entry.entity_type}{entry.kommo_entity_id ? ` #${entry.kommo_entity_id}` : ''}</span>}
                            {entry.batch_size ? <span>{entry.batch_size} items</span> : null}
                            {entry.request_count ? <span>{entry.request_count} req.</span> : null}
                            {entry.duration_ms ? <span>{entry.duration_ms} ms</span> : null}
                          </div>
                          {accountsDetail.length > 0 && (
                            <div className="mt-2 flex flex-wrap gap-1">
                              {accountsDetail.map(accountName => (
                                <span key={accountName} className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-slate-100 text-slate-600">{accountName}</span>
                              ))}
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                </div>

                <div className="space-y-5">
                  <div className="rounded-lg border border-gray-200 overflow-hidden">
                    <div className="px-4 py-3 bg-gray-50 border-b border-gray-200">
                      <h3 className="text-sm font-semibold text-gray-900">Canales</h3>
                    </div>
                    <div className="divide-y divide-gray-100">
                      {['webhook', 'events_poller', 'push', 'reconcile'].map(source => {
                        const stat = monitorStats[source]
                        return (
                          <div key={source} className="p-4">
                            <div className="flex items-center justify-between gap-2">
                              <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${sourceClass(source)}`}>{sourceLabel(source)}</span>
                              <span className="text-sm font-semibold text-gray-900">{stat?.count || 0}</span>
                            </div>
                            <div className="text-xs text-gray-400 mt-2">{formatDateTime(stat?.last_at)}</div>
                            {stat?.last_msg && <p className="text-xs text-gray-500 mt-1 line-clamp-2">{stat.last_msg}</p>}
                          </div>
                        )
                      })}
                    </div>
                  </div>

                  <div className="rounded-lg border border-gray-200 overflow-hidden">
                    <div className="px-4 py-3 bg-gray-50 border-b border-gray-200 flex items-center gap-2">
                      <Eye className="w-4 h-4 text-gray-400" />
                      <h3 className="text-sm font-semibold text-gray-900">Outbox por operación</h3>
                    </div>
                    <div className="divide-y divide-gray-100 max-h-64 overflow-auto">
                      {(integrationOutbox?.items || []).length === 0 ? (
                        <div className="p-5 text-sm text-gray-400 text-center">Sin cola pendiente</div>
                      ) : (integrationOutbox?.items || []).map(item => (
                        <div key={`${item.operation}-${item.account_id}`} className="p-3 text-sm">
                          <div className="flex items-center justify-between gap-2">
                            <span className="font-medium text-gray-900">{item.operation}</span>
                            <span className="text-xs text-gray-400">{item.total}</span>
                          </div>
                          <div className="text-xs text-gray-500 mt-1">{item.account_name || 'Sin cuenta'}</div>
                          <div className="flex items-center gap-2 mt-2 text-xs">
                            <span className="text-amber-600">{item.pending} pend.</span>
                            <span className="text-blue-600">{item.processing} proc.</span>
                            <span className="text-red-600">{item.errored} err.</span>
                          </div>
                          {item.last_error && <div className="mt-2 text-xs text-red-600 line-clamp-2">{item.last_error}</div>}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Role Modal */}
      {showRoleModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-lg">
            <div className="p-6 border-b border-gray-200">
              <h2 className="text-lg font-semibold text-gray-900">
                {editingRole ? 'Editar Rol' : 'Nuevo Rol'}
              </h2>
              <p className="text-sm text-gray-500 mt-1">Define qué módulos pueden acceder los usuarios con este rol</p>
            </div>
            <div className="p-6 space-y-5">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Nombre del rol</label>
                <input
                  type="text"
                  value={roleForm.name}
                  onChange={e => setRoleForm(f => ({ ...f, name: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  placeholder="Ej: Vendedor, Soporte, Supervisor..."
                  disabled={editingRole?.is_system}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Descripción (opcional)</label>
                <input
                  type="text"
                  value={roleForm.description}
                  onChange={e => setRoleForm(f => ({ ...f, description: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  placeholder="Breve descripción del rol..."
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-3">
                  Módulos accesibles
                  <span className="ml-2 text-xs font-normal text-gray-400">
                    ({roleForm.permissions.length} de {ALL_MODULES.length} seleccionados)
                  </span>
                </label>
                <div className="grid grid-cols-2 gap-2">
                  {ALL_MODULES.map(mod => {
                    const active = roleForm.permissions.includes(mod.key)
                    return (
                      <button
                        key={mod.key}
                        type="button"
                        onClick={() => toggleModulePermission(mod.key)}
                        className={`flex items-center gap-2.5 px-3 py-2.5 rounded-lg border text-sm font-medium transition-all ${
                          active
                            ? 'border-emerald-500 bg-emerald-50 text-emerald-700'
                            : 'border-gray-200 bg-white text-gray-500 hover:border-gray-300 hover:bg-gray-50'
                        }`}
                      >
                        {active
                          ? <CheckSquare className="w-4 h-4 shrink-0 text-emerald-500" />
                          : <Square className="w-4 h-4 shrink-0 text-gray-300" />
                        }
                        {mod.label}
                      </button>
                    )
                  })}
                </div>
                <button
                  type="button"
                  onClick={() => setRoleForm(f => ({
                    ...f,
                    permissions: f.permissions.length === ALL_MODULES.length ? [] : ALL_MODULES.map(m => m.key)
                  }))}
                  className="mt-3 text-xs text-emerald-600 hover:underline"
                >
                  {roleForm.permissions.length === ALL_MODULES.length ? 'Quitar todos' : 'Seleccionar todos'}
                </button>
              </div>
            </div>
            <div className="p-6 border-t border-gray-200 flex justify-end gap-3">
              <button onClick={() => setShowRoleModal(false)} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">
                Cancelar
              </button>
              <button onClick={saveRole} className="px-4 py-2 text-sm bg-green-600 text-white rounded-lg hover:bg-green-700">
                {editingRole ? 'Guardar' : 'Crear Rol'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Account Modal */}
      {showAccountModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-2xl">
            <div className="p-6 border-b border-gray-200">
              <h2 className="text-lg font-semibold text-gray-900">
                {editingAccount ? 'Editar Cuenta' : 'Nueva Cuenta'}
              </h2>
            </div>
            <div className="p-6 space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Nombre</label>
                <input
                  type="text"
                  value={accountForm.name}
                  onChange={e => setAccountForm(f => ({ ...f, name: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  placeholder="Nombre de la cuenta"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Slug (opcional)</label>
                <input
                  type="text"
                  value={accountForm.slug}
                  onChange={e => setAccountForm(f => ({ ...f, slug: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  placeholder="mi-cuenta"
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Plan</label>
                  <select
                    value={accountForm.plan}
                    onChange={e => setAccountForm(f => ({ ...f, plan: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  >
                    {planOptions.map(plan => (
                      <option key={plan.code} value={plan.code}>{plan.name}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Max Dispositivos</label>
                  <input
                    type="number"
                    value={accountForm.max_devices}
                    onChange={e => setAccountForm(f => ({ ...f, max_devices: parseInt(e.target.value) || 1 }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                    min={1}
                  />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Límite de usuarios</label>
                <input
                  type="number"
                  value={accountForm.max_users_override}
                  onChange={e => setAccountForm(f => ({ ...f, max_users_override: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  min={0}
                  placeholder={`Según plan (${planMaxUsers(accountForm.plan) ?? 'sin límite'})`}
                />
                <p className="mt-1 text-xs text-gray-400">Vacío usa el plan. 0 deja la cuenta sin límite.</p>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Límite de almacenamiento (GB)</label>
                <input
                  type="number"
                  value={accountForm.storage_limit_gb}
                  onChange={e => setAccountForm(f => ({ ...f, storage_limit_gb: Math.max(0, parseFloat(e.target.value) || 0) }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  min={0}
                  step={0.5}
                />
                <p className="mt-1 text-xs text-gray-400">Usa 0 para dejar la cuenta sin límite.</p>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Estado</label>
                  <select
                    value={accountForm.subscription_status}
                    onChange={e => setAccountForm(f => ({ ...f, subscription_status: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  >
                    <option value="active">Activa</option>
                    <option value="trialing">Prueba</option>
                    <option value="grace">Gracia</option>
                    <option value="past_due">Pendiente</option>
                    <option value="suspended">Suspendida</option>
                    <option value="canceled">Cancelada</option>
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Prueba hasta</label>
                  <input
                    type="date"
                    value={accountForm.trial_ends_at}
                    onChange={e => setAccountForm(f => ({ ...f, trial_ends_at: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Periodo hasta</label>
                  <input
                    type="date"
                    value={accountForm.current_period_end}
                    onChange={e => setAccountForm(f => ({ ...f, current_period_end: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  />
                </div>
              </div>
              <div className="flex items-center gap-3 pt-2">
                <button
                  type="button"
                  onClick={() => setAccountForm(f => ({ ...f, mcp_enabled: !f.mcp_enabled }))}
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${accountForm.mcp_enabled ? 'bg-violet-600' : 'bg-gray-300'}`}
                >
                  <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${accountForm.mcp_enabled ? 'translate-x-4' : 'translate-x-1'}`} />
                </button>
                <div>
                  <span className="text-sm font-medium text-gray-700">Acceso MCP</span>
                  <p className="text-xs text-gray-400">Permitir conexión desde ChatGPT u otros clientes MCP</p>
                </div>
              </div>
            </div>
            <div className="p-6 border-t border-gray-200 flex justify-end gap-3">
              <button onClick={() => setShowAccountModal(false)} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">
                Cancelar
              </button>
              <button onClick={saveAccount} className="px-4 py-2 text-sm bg-green-600 text-white rounded-lg hover:bg-green-700">
                {editingAccount ? 'Guardar' : 'Crear'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* User Modal */}
      {showUserModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-4xl max-h-[90vh] overflow-y-auto">
            <div className="p-6 border-b border-gray-200">
              <h2 className="text-lg font-semibold text-gray-900">
                {editingUser ? 'Editar Usuario' : 'Nuevo Usuario'}
              </h2>
            </div>
            <div className="p-6 space-y-4">
              {!editingUser && (
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <label className="block text-sm font-medium text-gray-700">Cuentas y roles</label>
                    <button type="button" onClick={addUserFormAssignment} className="text-xs text-emerald-600 hover:underline disabled:text-gray-300" disabled={userFormAssignments.length >= accounts.filter(a => a.is_active).length}>
                      Agregar cuenta
                    </button>
                  </div>
                  <div className="space-y-2">
                    {userFormAssignments.map((assignment, index) => (
                      <div key={`${assignment.account_id}-${index}`} className="grid grid-cols-[1fr_120px_1fr_auto_auto] gap-2 items-center bg-gray-50 border border-gray-200 rounded-lg p-2">
                        <select
                          value={assignment.account_id}
                          onChange={e => updateUserFormAssignment(index, { account_id: e.target.value })}
                          className="min-w-0 px-2 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                        >
                          <option value="">Cuenta...</option>
                          {accounts.filter(a => a.is_active && (a.id === assignment.account_id || !userFormAssignments.some(ua => ua.account_id === a.id))).map(a => (
                            <option key={a.id} value={a.id}>{a.name}</option>
                          ))}
                        </select>
                        <select
                          value={assignment.role}
                          onChange={e => updateUserFormAssignment(index, { role: e.target.value })}
                          className="px-2 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                        >
                          <option value="agent">Agente</option>
                          <option value="admin">Admin</option>
                          <option value="super_admin">Super Admin</option>
                        </select>
                        <select
                          value={assignment.role_id}
                          onChange={e => updateUserFormAssignment(index, { role_id: e.target.value })}
                          disabled={assignment.role !== 'agent'}
                          className="min-w-0 px-2 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500 disabled:bg-gray-100 disabled:text-gray-400"
                        >
                          <option value="">Rol manual...</option>
                          {roles.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
                        </select>
                        <button type="button" onClick={() => updateUserFormAssignment(index, { is_default: true })} className={`px-2 py-2 rounded-lg text-xs font-medium ${assignment.is_default ? 'bg-emerald-100 text-emerald-700' : 'bg-white text-gray-500 border border-gray-200'}`}>
                          Principal
                        </button>
                        <button type="button" onClick={() => removeUserFormAssignment(index)} className="p-2 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded-lg" disabled={userFormAssignments.length === 1}>
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    ))}
                    {userFormAssignments.length === 0 && (
                      <button type="button" onClick={addUserFormAssignment} className="w-full px-3 py-3 border border-dashed border-gray-300 rounded-lg text-sm text-gray-500 hover:border-emerald-400 hover:text-emerald-600">
                        Agregar primera cuenta
                      </button>
                    )}
                  </div>
                </div>
              )}
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Username</label>
                  <input
                    type="text"
                    value={userForm.username}
                    onChange={e => setUserForm(f => ({ ...f, username: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Nombre</label>
                  <input
                    type="text"
                    value={userForm.display_name}
                    onChange={e => setUserForm(f => ({ ...f, display_name: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Email opcional</label>
                <input
                  type="email"
                  value={userForm.email}
                  onChange={e => setUserForm(f => ({ ...f, email: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                />
              </div>
              {!editingUser && (
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Contraseña</label>
                  <input
                    type="password"
                    value={userForm.password}
                    onChange={e => setUserForm(f => ({ ...f, password: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  />
                </div>
              )}
              {editingUser && <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Rol</label>
                <select
                  value={userForm.role}
                  onChange={e => setUserForm(f => ({ ...f, role: e.target.value }))}
                  className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                >
                  <option value="agent">Agente</option>
                  <option value="admin">Admin</option>
                  <option value="super_admin">Super Admin</option>
                </select>
              </div>}
            </div>
            <div className="p-6 border-t border-gray-200 flex justify-end gap-3">
              <button onClick={() => setShowUserModal(false)} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">
                Cancelar
              </button>
              <button onClick={saveUser} className="px-4 py-2 text-sm bg-green-600 text-white rounded-lg hover:bg-green-700">
                {editingUser ? 'Guardar' : 'Crear'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Password Modal */}
      {showPasswordModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-sm">
            <div className="p-6 border-b border-gray-200">
              <h2 className="text-lg font-semibold text-gray-900">Cambiar Contraseña</h2>
            </div>
            <div className="p-6">
              <label className="block text-sm font-medium text-gray-700 mb-1">Nueva Contraseña</label>
              <input
                type="password"
                value={newPassword}
                onChange={e => setNewPassword(e.target.value)}
                className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                placeholder="Ingrese nueva contraseña"
              />
            </div>
            <div className="p-6 border-t border-gray-200 flex justify-end gap-3">
              <button onClick={() => setShowPasswordModal(false)} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">
                Cancelar
              </button>
              <button onClick={resetPassword} className="px-4 py-2 text-sm bg-green-600 text-white rounded-lg hover:bg-green-700">
                Cambiar
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Account Assignments Modal */}
      {showAssignModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-lg">
            <div className="p-6 border-b border-gray-200">
              <h2 className="text-lg font-semibold text-gray-900">
                Cuentas de {assignUserName}
              </h2>
              <p className="text-sm text-gray-500 mt-1">Gestiona las cuentas asignadas a este usuario</p>
            </div>
            <div className="p-6 space-y-4">
              {/* Current assignments */}
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-2">Cuentas asignadas</label>
                {userAssignments.length === 0 ? (
                  <p className="text-sm text-gray-400 py-2">Sin cuentas asignadas</p>
                ) : (
                  <div className="space-y-2">
                    {userAssignments.map(ua => (
                      <div key={ua.account_id} className="flex items-center justify-between px-3 py-2 bg-gray-50 rounded-lg">
                        <div className="flex items-center gap-2 flex-wrap">
                          <Building2 className="w-4 h-4 text-gray-400 shrink-0" />
                          <span className="text-sm font-medium text-gray-900">{ua.account_name}</span>
                          <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${roleColors[ua.role] || 'bg-gray-100 text-gray-700'}`}>
                            {roleDisplay(ua.role, ua.role_id, ua.role_name)}
                          </span>
                          {ua.is_default && (
                            <span className="inline-flex px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-700">
                              Principal
                            </span>
                          )}
                        </div>
                        <button
                          onClick={() => removeAssignment(ua.account_id)}
                          className="p-1 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded"
                          title="Quitar cuenta"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* Add new assignment */}
              <div className="border-t border-gray-200 pt-4">
                <label className="block text-sm font-medium text-gray-700 mb-2">Agregar cuenta</label>
                <div className="flex gap-2 flex-wrap">
                  <select
                    value={assignAccountId}
                    onChange={e => setAssignAccountId(e.target.value)}
                    className="flex-1 min-w-[140px] px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  >
                    <option value="">Seleccionar cuenta...</option>
                    {accounts.filter(a => a.is_active && !userAssignments.some(ua => ua.account_id === a.id)).map(a => (
                      <option key={a.id} value={a.id}>{a.name}</option>
                    ))}
                  </select>
                  <select
                    value={assignRole}
                    onChange={e => setAssignRole(e.target.value)}
                    className="px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                  >
                    <option value="agent">Agente</option>
                    <option value="admin">Admin</option>
                    <option value="super_admin">Super Admin</option>
                  </select>
                  {assignRole === 'agent' && roles.length > 0 && (
                    <select
                      value={assignRoleId}
                      onChange={e => setAssignRoleId(e.target.value)}
                      className="flex-1 min-w-[140px] px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-green-500"
                    >
                      <option value="">Sin rol de permisos</option>
                      {roles.map(r => (
                        <option key={r.id} value={r.id}>{r.name}</option>
                      ))}
                    </select>
                  )}
                  <button
                    onClick={assignAccount}
                    disabled={!assignAccountId}
                    className="px-3 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                  >
                    <Plus className="w-4 h-4" />
                  </button>
                </div>
                {assignRole === 'agent' && assignRoleId && (
                  <p className="text-xs text-emerald-600 mt-1.5">
                    ✓ Permisos del rol: {roles.find(r => r.id === assignRoleId)?.permissions.map(p => ALL_MODULES.find(m => m.key === p)?.label || p).join(', ')}
                  </p>
                )}
              </div>
            </div>
            <div className="p-6 border-t border-gray-200 flex justify-end">
              <button onClick={() => setShowAssignModal(false)} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">
                Cerrar
              </button>
            </div>
          </div>
        </div>
      )}

      {showIntegrationModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-3xl max-h-[90vh] overflow-y-auto">
            <div className="p-6 border-b border-gray-200 flex items-center justify-between">
              <div>
                <h2 className="text-lg font-semibold text-gray-900">{editingIntegration ? 'Editar integración Kommo' : 'Nueva integración Kommo'}</h2>
                <p className="text-sm text-gray-500 mt-1">Una licencia Kommo puede alimentar varias cuentas de Clarin.</p>
              </div>
              <button onClick={() => setShowIntegrationModal(false)} className="p-1.5 text-gray-400 hover:text-gray-700 hover:bg-gray-100 rounded-lg">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="p-6 space-y-5">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Nombre</label>
                  <input value={integrationForm.name} onChange={e => setIntegrationForm(f => ({ ...f, name: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" placeholder="Kommo Comercial" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Subdominio</label>
                  <input value={integrationForm.subdomain} onChange={e => setIntegrationForm(f => ({ ...f, subdomain: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" placeholder="miempresa" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Client ID</label>
                  <input value={integrationForm.client_id} onChange={e => setIntegrationForm(f => ({ ...f, client_id: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Redirect URI</label>
                  <input value={integrationForm.redirect_uri} onChange={e => setIntegrationForm(f => ({ ...f, redirect_uri: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1 flex items-center gap-2">
                    Client Secret
                    {editingIntegration && (
                      editingIntegration.has_client_secret
                        ? <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-600 bg-emerald-50 border border-emerald-200 rounded-full px-2 py-0.5"><svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" /></svg>Guardado</span>
                        : <span className="text-xs text-gray-400">(vacío)</span>
                    )}
                    {editingIntegration && <span className="ml-auto text-xs text-gray-400 font-normal">dejar vacío para conservar</span>}
                  </label>
                  <input type="password" value={integrationForm.client_secret} onChange={e => setIntegrationForm(f => ({ ...f, client_secret: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" placeholder={editingIntegration?.has_client_secret ? '••••••••' : ''} />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1 flex items-center gap-2">
                    Access Token
                    {editingIntegration && (
                      editingIntegration.has_access_token
                        ? <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-600 bg-emerald-50 border border-emerald-200 rounded-full px-2 py-0.5"><svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" /></svg>Guardado</span>
                        : <span className="text-xs text-gray-400">(vacío)</span>
                    )}
                    {editingIntegration && <span className="ml-auto text-xs text-gray-400 font-normal">dejar vacío para conservar</span>}
                  </label>
                  <input type="password" value={integrationForm.access_token} onChange={e => setIntegrationForm(f => ({ ...f, access_token: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" placeholder={editingIntegration?.has_access_token ? '••••••••' : ''} />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1 flex items-center gap-2">
                    Refresh Token
                    {editingIntegration && (
                      editingIntegration.has_refresh_token
                        ? <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-600 bg-emerald-50 border border-emerald-200 rounded-full px-2 py-0.5"><svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" /></svg>Guardado</span>
                        : <span className="text-xs text-gray-400">(opcional)</span>
                    )}
                  </label>
                  <input type="password" value={integrationForm.refresh_token} onChange={e => setIntegrationForm(f => ({ ...f, refresh_token: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" placeholder={editingIntegration?.has_refresh_token ? '••••••••' : ''} />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1 flex items-center gap-2">
                    Webhook Secret
                    {editingIntegration && (
                      editingIntegration.has_webhook_secret
                        ? <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-600 bg-emerald-50 border border-emerald-200 rounded-full px-2 py-0.5"><svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" /></svg>Guardado</span>
                        : <span className="text-xs text-gray-400">(vacío)</span>
                    )}
                    {editingIntegration && <span className="ml-auto text-xs text-gray-400 font-normal">dejar vacío para conservar</span>}
                  </label>
                  <input type="password" value={integrationForm.webhook_secret} onChange={e => setIntegrationForm(f => ({ ...f, webhook_secret: e.target.value }))} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500" placeholder={editingIntegration?.has_webhook_secret ? '••••••••' : ''} />
                </div>
              </div>
              <div className="flex items-center gap-3">
                <button type="button" onClick={() => setIntegrationForm(f => ({ ...f, is_active: !f.is_active }))} className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${integrationForm.is_active ? 'bg-emerald-600' : 'bg-gray-300'}`}>
                  <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${integrationForm.is_active ? 'translate-x-4' : 'translate-x-1'}`} />
                </button>
                <span className="text-sm font-medium text-gray-700">Integración activa</span>
              </div>
              <div className="border-t border-gray-200 pt-5">
                <div className="flex items-center justify-between mb-3">
                  <label className="block text-sm font-medium text-gray-700">Cuentas conectadas</label>
                  <span className="text-xs text-gray-400">{selectedIntegrationAccounts.length} seleccionadas</span>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 max-h-60 overflow-y-auto pr-1">
                  {accounts.map(account => {
                    const active = selectedIntegrationAccounts.includes(account.id)
                    return (
                      <button key={account.id} type="button" onClick={() => toggleSelectedIntegrationAccount(account.id)} className={`flex items-center justify-between gap-3 px-3 py-2.5 rounded-lg border text-left transition-all ${active ? 'border-emerald-500 bg-emerald-50 text-emerald-800' : 'border-gray-200 bg-white text-gray-600 hover:bg-gray-50'}`}>
                        <span className="text-sm font-medium truncate">{account.name}</span>
                        {active ? <CheckSquare className="w-4 h-4 text-emerald-600 shrink-0" /> : <Square className="w-4 h-4 text-gray-300 shrink-0" />}
                      </button>
                    )
                  })}
                </div>
              </div>
            </div>
            <div className="p-6 border-t border-gray-200 flex justify-end gap-3">
              <button onClick={() => setShowIntegrationModal(false)} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">Cancelar</button>
              <button onClick={saveIntegration} className="px-4 py-2 text-sm bg-emerald-600 text-white rounded-lg hover:bg-emerald-700">Guardar integración</button>
            </div>
          </div>
        </div>
      )}

      {showPurgeModal && purgeAccount && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-xl shadow-xl w-full max-w-xl">
            <div className="p-6 border-b border-gray-200 flex items-start gap-3">
              <div className="w-10 h-10 rounded-lg bg-red-100 flex items-center justify-center shrink-0">
                <AlertTriangle className="w-5 h-5 text-red-600" />
              </div>
              <div>
                <h2 className="text-lg font-semibold text-gray-900">Purgar cuenta</h2>
                <p className="text-sm text-gray-500 mt-1">Esta acción elimina la cuenta, sus datos relacionados y los archivos almacenados bajo su prefijo.</p>
              </div>
            </div>
            <div className="p-6 space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div className="bg-gray-50 border border-gray-200 rounded-lg p-3">
                  <div className="flex items-center gap-2 text-xs text-gray-500 mb-1"><Database className="w-4 h-4" /> Registros</div>
                  <div className="text-2xl font-semibold text-gray-900">{purgeTables ? purgeRecordCount : '...'}</div>
                </div>
                <div className="bg-gray-50 border border-gray-200 rounded-lg p-3">
                  <div className="flex items-center gap-2 text-xs text-gray-500 mb-1"><HardDrive className="w-4 h-4" /> Archivos</div>
                  <div className="text-2xl font-semibold text-gray-900">{purgeSummary ? purgeStorageObjects : '...'}</div>
                </div>
              </div>
              {purgeTables && (
                <div className="max-h-36 overflow-y-auto border border-gray-200 rounded-lg divide-y divide-gray-100">
                  {Object.entries(purgeTables).filter(([, value]) => typeof value === 'number' && value > 0).map(([table, value]) => (
                    <div key={table} className="flex items-center justify-between px-3 py-2 text-sm">
                      <span className="text-gray-600">{table}</span>
                      <span className="font-medium text-gray-900">{value}</span>
                    </div>
                  ))}
                </div>
              )}
              <label className="flex items-center gap-3 text-sm text-gray-700">
                <input type="checkbox" checked={purgeDeleteFiles} onChange={e => setPurgeDeleteFiles(e.target.checked)} className="rounded border-gray-300 text-emerald-600 focus:ring-emerald-500" />
                Eliminar archivos de MinIO para esta cuenta
              </label>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Escribe el nombre de la cuenta para confirmar</label>
                <input value={purgeConfirmation} onChange={e => setPurgeConfirmation(e.target.value)} className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm focus:ring-2 focus:ring-red-500" placeholder={purgeAccount.name} />
              </div>
            </div>
            <div className="p-6 border-t border-gray-200 flex justify-end gap-3">
              <button onClick={() => setShowPurgeModal(false)} className="px-4 py-2 text-sm text-gray-600 hover:bg-gray-100 rounded-lg">Cancelar</button>
              <button onClick={purgeAccountNow} disabled={purgeConfirmation !== purgeAccount.name} className="px-4 py-2 text-sm bg-red-600 text-white rounded-lg hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed">Purgar cuenta</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
