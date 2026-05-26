'use client'

import { useEffect, useState, useCallback } from 'react'
import { useSearchParams, useRouter } from 'next/navigation'
import { User, Building, Bell, Shield, LogOut, Save, Loader2, Volume2, VolumeX, BellRing, BellOff, Eye, EyeOff, Play, Zap, Plus, Pencil, Trash2, X, Link2, RefreshCw, CheckCircle2, XCircle, Power, Activity, Inbox, Paperclip, Image, Video, File, ChevronDown, ChevronRight, GripVertical, Smartphone, Wifi, WifiOff, Signal, QrCode, Edit, Key, Copy, ExternalLink, Settings, ArrowLeft, Users, Globe, Hash, Calendar, ToggleLeft, Mail, Phone, Link, DollarSign, Type, Tag, List, AlertCircle, HardDrive } from 'lucide-react'
import { logoutFromBrowser, subscribeWebSocket } from '@/lib/api'
import WhatsAppAPISettingsPanel from '@/components/WhatsAppAPISettingsPanel'
import { CustomFieldDefinition, CustomFieldType, CustomFieldOption, CustomFieldConfig } from '@/types/custom-field'
import { DndContext, closestCenter, KeyboardSensor, PointerSensor, useSensor, useSensors, DragEndEvent } from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy, arrayMove } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import {
  getNotificationSettings,
  saveNotificationSettings,
  playNotificationSound,
  requestNotificationPermission,
  SOUND_OPTIONS,
  type NotificationSettings,
} from '@/lib/notificationSounds'
import { useNotifications } from '@/components/NotificationProvider'

interface Account {
  id: string
  name: string
  slug: string
  plan: string
  subscription_status?: string
  trial_ends_at?: string | null
  current_period_end?: string | null
  grace_ends_at?: string | null
  created_at: string
  storage_limit_bytes?: number
}

interface StorageUsage {
  limit_bytes: number
  used_bytes: number
  available_bytes: number
  object_count: number
  percent_used: number
  can_manage: boolean
  by_type: Record<string, number>
}

interface StorageFile {
  object_key: string
  media_url: string
  media_type: string
  filename: string
  size_bytes: number
  last_used_at: string
  references_count: number
}

const subscriptionLabels: Record<string, string> = {
  trialing: 'Prueba',
  active: 'Activa',
  past_due: 'Pago pendiente',
  grace: 'Periodo de gracia',
  suspended: 'Suspendida',
  canceled: 'Cancelada',
  incomplete: 'Incompleta',
}

const subscriptionColors: Record<string, string> = {
  trialing: 'bg-amber-100 text-amber-700',
  active: 'bg-emerald-100 text-emerald-700',
  past_due: 'bg-orange-100 text-orange-700',
  grace: 'bg-yellow-100 text-yellow-700',
  suspended: 'bg-red-100 text-red-700',
  canceled: 'bg-slate-100 text-slate-500',
  incomplete: 'bg-slate-100 text-slate-600',
}

function subscriptionDeadline(account?: Account | null) {
  if (!account) return null
  if (account.subscription_status === 'trialing') return account.trial_ends_at
  if (account.subscription_status === 'grace') return account.grace_ends_at
  return account.current_period_end
}

function daysUntil(value?: string | null) {
  if (!value) return null
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return null
  return Math.max(0, Math.ceil((parsed.getTime() - Date.now()) / 86400000))
}

function formatBytes(bytes?: number) {
  const value = bytes || 0
  if (value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let idx = 0
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024
    idx++
  }
  return `${size >= 10 || idx === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[idx]}`
}

interface UserProfile {
  id: string
  email: string
  name: string
  role: string
  account_id?: string
  is_super_admin?: boolean
  is_admin?: boolean
  permissions?: string[]
}

// ─── API Keys / MCP Panel ───────────────────────────────────────────────────
function APIKeysPanel() {
  const [apiKeys, setApiKeys] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [newKeyName, setNewKeyName] = useState('')
  const [revealedKey, setRevealedKey] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [deleting, setDeleting] = useState<string | null>(null)

  const fetchKeys = useCallback(async () => {
    try {
      const res = await fetch('/api/settings/api-keys')
      const data = await res.json()
      if (data.success) setApiKeys(data.api_keys || [])
    } catch (e) {}
    setLoading(false)
  }, [])

  useEffect(() => { fetchKeys() }, [fetchKeys])

  const handleCreate = async () => {
    if (!newKeyName.trim()) return
    setCreating(true)
    try {
      const res = await fetch('/api/settings/api-keys', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newKeyName.trim() }),
      })
      const data = await res.json()
      if (data.success) {
        setRevealedKey(data.key)
        setNewKeyName('')
        fetchKeys()
      }
    } catch (e) {}
    setCreating(false)
  }

  const handleDelete = async (id: string) => {
    if (!confirm('¿Revocar esta API Key? Los servicios que la usen dejarán de funcionar.')) return
    setDeleting(id)
    try {
      await fetch(`/api/settings/api-keys/${id}`, { method: 'DELETE' })
      fetchKeys()
    } catch (e) {}
    setDeleting(null)
  }

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="space-y-6">
      {/* MCP Info Banner */}
      <div className="bg-gradient-to-r from-emerald-50 to-teal-50 border border-emerald-200 rounded-xl p-4">
        <div className="flex items-start gap-3">
          <div className="w-8 h-8 bg-emerald-100 rounded-lg flex items-center justify-center flex-shrink-0 mt-0.5">
            <Key className="w-4 h-4 text-emerald-600" />
          </div>
          <div>
            <h3 className="text-sm font-semibold text-emerald-900">API Keys — MCP (Model Context Protocol)</h3>
            <p className="text-xs text-emerald-700 mt-1 leading-relaxed">
              Conecta tu CRM con ChatGPT, Claude, VS Code Copilot u otros asistentes de IA usando el protocolo MCP.
              Las API Keys permiten acceso de <span className="font-semibold">solo lectura</span> a tus datos (leads, eventos, contactos, bitácoras, conversaciones).
            </p>
            <div className="mt-3 flex items-center gap-2">
              <span className="text-[10px] font-mono bg-white/70 text-emerald-800 px-2 py-1 rounded border border-emerald-200">
                https://clarin.naperu.cloud/mcp/sse
              </span>
              <button
                onClick={() => handleCopy('https://clarin.naperu.cloud/mcp/sse')}
                className="p-1 text-emerald-600 hover:text-emerald-800 transition"
                title="Copiar URL"
              >
                <Copy className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Revealed Key Modal */}
      {revealedKey && (
        <div className="bg-amber-50 border border-amber-200 rounded-xl p-4">
          <div className="flex items-start gap-3">
            <div className="w-8 h-8 bg-amber-100 rounded-lg flex items-center justify-center flex-shrink-0 mt-0.5">
              <Eye className="w-4 h-4 text-amber-600" />
            </div>
            <div className="flex-1 min-w-0">
              <h4 className="text-sm font-semibold text-amber-900">¡API Key creada!</h4>
              <p className="text-xs text-amber-700 mt-0.5">Copia esta clave ahora. No podrás verla de nuevo.</p>
              <div className="mt-2 flex items-center gap-2">
                <code className="text-xs font-mono bg-white text-amber-900 px-3 py-1.5 rounded border border-amber-200 break-all flex-1">
                  {revealedKey}
                </code>
                <button
                  onClick={() => handleCopy(revealedKey)}
                  className={`flex items-center gap-1 px-3 py-1.5 rounded-lg text-xs font-medium transition ${
                    copied ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-200 text-amber-800 hover:bg-amber-300'
                  }`}
                >
                  {copied ? <><CheckCircle2 className="w-3.5 h-3.5" /> Copiado</> : <><Copy className="w-3.5 h-3.5" /> Copiar</>}
                </button>
              </div>
              <button onClick={() => setRevealedKey(null)} className="mt-2 text-xs text-amber-600 hover:text-amber-800 underline">
                Cerrar
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Create New Key */}
      <div>
        <h3 className="text-sm font-medium text-slate-900 mb-3">Crear nueva API Key</h3>
        <div className="flex items-center gap-2 max-w-md">
          <input
            value={newKeyName}
            onChange={e => setNewKeyName(e.target.value)}
            placeholder="Nombre (ej: ChatGPT MCP)"
            className="flex-1 px-3 py-2 border border-slate-200 rounded-xl text-sm focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900 placeholder:text-slate-400"
            onKeyDown={e => e.key === 'Enter' && handleCreate()}
          />
          <button
            onClick={handleCreate}
            disabled={creating || !newKeyName.trim()}
            className="inline-flex items-center gap-1.5 px-4 py-2 bg-emerald-600 text-white rounded-xl text-sm font-medium hover:bg-emerald-700 disabled:opacity-50 transition shadow-sm"
          >
            {creating ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
            Crear
          </button>
        </div>
      </div>

      {/* Keys List */}
      <div>
        <h3 className="text-sm font-medium text-slate-900 mb-3">API Keys activas</h3>
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="w-5 h-5 animate-spin text-slate-400" />
          </div>
        ) : apiKeys.length === 0 ? (
          <div className="text-center py-8 text-slate-400">
            <Key className="w-8 h-8 mx-auto mb-2 opacity-50" />
            <p className="text-sm">No hay API Keys creadas</p>
          </div>
        ) : (
          <div className="space-y-2">
            {apiKeys.map((key: any) => (
              <div key={key.id} className="flex items-center justify-between px-4 py-3 bg-slate-50 rounded-xl border border-slate-100">
                <div className="flex items-center gap-3 min-w-0">
                  <div className={`w-2 h-2 rounded-full ${key.is_active ? 'bg-emerald-500' : 'bg-slate-300'}`} />
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-slate-900 truncate">{key.name || 'API Key'}</p>
                    <div className="flex items-center gap-2 mt-0.5">
                      <code className="text-[11px] font-mono text-slate-400">{key.key_prefix}</code>
                      <span className="text-[10px] text-slate-400">·</span>
                      <span className="text-[10px] text-slate-400">
                        {key.last_used_at ? `Último uso: ${new Date(key.last_used_at).toLocaleDateString('es-PE')}` : 'Nunca usada'}
                      </span>
                      <span className="text-[10px] text-slate-400">·</span>
                      <span className="text-[10px] text-slate-400">
                        Creada: {new Date(key.created_at).toLocaleDateString('es-PE')}
                      </span>
                    </div>
                  </div>
                </div>
                <button
                  onClick={() => handleDelete(key.id)}
                  disabled={deleting === key.id}
                  className="p-1.5 text-slate-400 hover:text-red-500 transition rounded-lg hover:bg-red-50"
                  title="Revocar"
                >
                  {deleting === key.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Connection Instructions */}
      <div className="border-t border-slate-200 pt-6">
        <h3 className="text-sm font-medium text-slate-900 mb-3">Cómo conectar con ChatGPT</h3>
        <div className="space-y-4 text-xs text-slate-600 leading-relaxed">
          <div className="flex items-start gap-3 bg-slate-50 rounded-xl p-3">
            <span className="w-5 h-5 bg-slate-200 text-slate-600 rounded-full flex items-center justify-center text-[10px] font-bold flex-shrink-0 mt-0.5">1</span>
            <div>
              <p className="font-medium text-slate-800">Abre ChatGPT → Aplicaciones → Nueva aplicación</p>
              <p className="text-slate-500 mt-0.5">Crea una nueva aplicación MCP con los siguientes datos:</p>
            </div>
          </div>
          <div className="flex items-start gap-3 bg-slate-50 rounded-xl p-3">
            <span className="w-5 h-5 bg-slate-200 text-slate-600 rounded-full flex items-center justify-center text-[10px] font-bold flex-shrink-0 mt-0.5">2</span>
            <div>
              <p className="font-medium text-slate-800">Configura la conexión</p>
              <div className="text-slate-500 mt-1 space-y-1">
                <p>• <strong>Nombre:</strong> Clarin</p>
                <p>• <strong>URL del servidor MCP:</strong> <code className="bg-white px-1 py-0.5 rounded border border-slate-200">https://clarin.naperu.cloud/mcp/sse</code></p>
                <p>• <strong>Autenticación:</strong> OAuth</p>
                <p>• <strong>URL de autorización:</strong> <code className="bg-white px-1 py-0.5 rounded border border-slate-200">https://clarin.naperu.cloud/mcp/authorize</code></p>
                <p>• <strong>URL del token:</strong> <code className="bg-white px-1 py-0.5 rounded border border-slate-200">https://clarin.naperu.cloud/mcp/token</code></p>
                <p>• <strong>Client ID / Secret:</strong> Dejar vacío (opcional)</p>
              </div>
            </div>
          </div>
          <div className="flex items-start gap-3 bg-slate-50 rounded-xl p-3">
            <span className="w-5 h-5 bg-slate-200 text-slate-600 rounded-full flex items-center justify-center text-[10px] font-bold flex-shrink-0 mt-0.5">3</span>
            <div>
              <p className="font-medium text-slate-800">Inicia sesión y pregunta lo que quieras</p>
              <p className="text-slate-500 mt-0.5">Se abrirá una ventana para iniciar sesión en Clarin. Luego podrás preguntar: &quot;¿Cuántos eventos activos tengo?&quot;, &quot;Dame el resumen de la bitácora del 7 de marzo&quot;, &quot;¿Qué dice la conversación con Lucero?&quot;</p>
            </div>
          </div>
        </div>

        {/* Also available: manual API Key */}
        <div className="mt-4 bg-slate-50 rounded-xl p-3">
          <p className="text-[11px] text-slate-500">
            <strong>Conexión manual:</strong> También puedes usar una API Key (creada arriba) como Bearer token directamente. Útil para VS Code Copilot, Claude u otros clientes MCP que soporten Bearer auth.
          </p>
        </div>
      </div>
    </div>
  )
}

// ─── Field Type Config ──────────────────────────────────────────────────────
const FIELD_TYPE_OPTIONS: { value: CustomFieldType; label: string; icon: React.ElementType; description: string }[] = [
  { value: 'text', label: 'Texto', icon: Type, description: 'Texto libre' },
  { value: 'number', label: 'Número', icon: Hash, description: 'Valor numérico' },
  { value: 'date', label: 'Fecha', icon: Calendar, description: 'Selector de fecha' },
  { value: 'select', label: 'Selección', icon: List, description: 'Una opción de lista' },
  { value: 'multi_select', label: 'Selección múltiple', icon: List, description: 'Varias opciones' },
  { value: 'checkbox', label: 'Casilla', icon: ToggleLeft, description: 'Sí / No' },
  { value: 'email', label: 'Email', icon: Mail, description: 'Correo electrónico' },
  { value: 'phone', label: 'Teléfono', icon: Phone, description: 'Número de teléfono' },
  { value: 'url', label: 'URL', icon: Link, description: 'Enlace web' },
  { value: 'currency', label: 'Moneda', icon: DollarSign, description: 'Valor monetario' },
]

function getFieldTypeIcon(type: CustomFieldType) {
  return FIELD_TYPE_OPTIONS.find(o => o.value === type)?.icon || Type
}

function getFieldTypeLabel(type: CustomFieldType) {
  return FIELD_TYPE_OPTIONS.find(o => o.value === type)?.label || type
}

// ─── Sortable Field Item ────────────────────────────────────────────────────
function SortableFieldItem({ field, onEdit, onDelete }: { field: CustomFieldDefinition; onEdit: (f: CustomFieldDefinition) => void; onDelete: (f: CustomFieldDefinition) => void }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: field.id })
  const style = { transform: CSS.Transform.toString(transform), transition }
  const Icon = getFieldTypeIcon(field.field_type)

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`flex items-center gap-3 px-4 py-3 bg-white border border-slate-200 rounded-xl group transition-all ${isDragging ? 'shadow-lg opacity-80 z-10' : 'hover:border-slate-300'}`}
    >
      <button {...attributes} {...listeners} className="cursor-grab active:cursor-grabbing p-1 text-slate-300 hover:text-slate-500 touch-none">
        <GripVertical className="w-4 h-4" />
      </button>
      <div className="w-8 h-8 bg-emerald-50 rounded-lg flex items-center justify-center shrink-0">
        <Icon className="w-4 h-4 text-emerald-600" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-slate-900 truncate">{field.name}</span>
          {field.is_required && (
            <span className="px-1.5 py-0.5 bg-amber-50 text-amber-700 text-[10px] font-medium rounded-md">Requerido</span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <span className="text-[11px] text-slate-400">{getFieldTypeLabel(field.field_type)}</span>
          <span className="text-[10px] text-slate-300">·</span>
          <code className="text-[10px] text-slate-400 font-mono">{field.slug}</code>
          {field.field_type === 'select' && field.config?.options && (
            <>
              <span className="text-[10px] text-slate-300">·</span>
              <span className="text-[10px] text-slate-400">{field.config.options.length} opciones</span>
            </>
          )}
          {field.field_type === 'multi_select' && field.config?.options && (
            <>
              <span className="text-[10px] text-slate-300">·</span>
              <span className="text-[10px] text-slate-400">{field.config.options.length} opciones</span>
            </>
          )}
          {field.field_type === 'currency' && field.config?.symbol && (
            <>
              <span className="text-[10px] text-slate-300">·</span>
              <span className="text-[10px] text-slate-400">{field.config.symbol}</span>
            </>
          )}
        </div>
      </div>
      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
        <button
          onClick={() => onEdit(field)}
          className="p-1.5 text-slate-400 hover:text-emerald-600 hover:bg-emerald-50 rounded-lg transition"
          title="Editar"
        >
          <Pencil className="w-3.5 h-3.5" />
        </button>
        <button
          onClick={() => onDelete(field)}
          className="p-1.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-lg transition"
          title="Eliminar"
        >
          <Trash2 className="w-3.5 h-3.5" />
        </button>
      </div>
    </div>
  )
}

// ─── Custom Fields Panel ────────────────────────────────────────────────────
function CustomFieldsPanel() {
  const [fields, setFields] = useState<CustomFieldDefinition[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [showModal, setShowModal] = useState(false)
  const [editingField, setEditingField] = useState<CustomFieldDefinition | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<CustomFieldDefinition | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState('')

  // Form state
  const [formName, setFormName] = useState('')
  const [formType, setFormType] = useState<CustomFieldType>('text')
  const [formRequired, setFormRequired] = useState(false)
  const [formDefault, setFormDefault] = useState('')
  const [formOptions, setFormOptions] = useState<CustomFieldOption[]>([])
  const [formSymbol, setFormSymbol] = useState('S/.')
  const [formDecimals, setFormDecimals] = useState(2)
  const [formMin, setFormMin] = useState<string>('')
  const [formMax, setFormMax] = useState<string>('')
  const [formMaxLength, setFormMaxLength] = useState<string>('')
  const [formTextVariant, setFormTextVariant] = useState<'inline' | 'textarea' | 'rich'>('inline')
  const [newOptionLabel, setNewOptionLabel] = useState('')

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  )

  const fetchFields = useCallback(async () => {
    try {
      const token = localStorage.getItem('token')
      const res = await fetch('/api/custom-fields', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) setFields(data.fields || [])
    } catch {}
    setLoading(false)
  }, [])

  useEffect(() => { fetchFields() }, [fetchFields])

  // WebSocket listener for real-time updates
  useEffect(() => {
    const unsub = subscribeWebSocket((msg: any) => {
      if (msg.type === 'custom_field_def_update') {
        fetchFields()
      }
    })
    return unsub
  }, [fetchFields])

  const resetForm = () => {
    setFormName('')
    setFormType('text')
    setFormRequired(false)
    setFormDefault('')
    setFormOptions([])
    setFormSymbol('S/.')
    setFormDecimals(2)
    setFormMin('')
    setFormMax('')
    setFormMaxLength('')
    setFormTextVariant('inline')
    setNewOptionLabel('')
    setError('')
  }

  const openCreate = () => {
    setEditingField(null)
    resetForm()
    setShowModal(true)
  }

  const openEdit = (field: CustomFieldDefinition) => {
    setEditingField(field)
    setFormName(field.name)
    setFormType(field.field_type)
    setFormRequired(field.is_required)
    setFormDefault(field.default_value || '')
    setFormOptions(field.config?.options || [])
    setFormSymbol(field.config?.symbol || 'S/.')
    setFormDecimals(field.config?.decimals ?? 2)
    setFormMin(field.config?.min != null ? String(field.config.min) : '')
    setFormMax(field.config?.max != null ? String(field.config.max) : '')
    setFormMaxLength(field.config?.max_length != null ? String(field.config.max_length) : '')
    setFormTextVariant(field.config?.text_variant || 'inline')
    setNewOptionLabel('')
    setError('')
    setShowModal(true)
  }

  const buildConfig = (): CustomFieldConfig => {
    const config: CustomFieldConfig = {}
    const t = editingField ? editingField.field_type : formType
    if (t === 'select' || t === 'multi_select') {
      config.options = formOptions
    }
    if (t === 'currency') {
      config.symbol = formSymbol
      config.decimals = formDecimals
    }
    if (t === 'number' || t === 'currency') {
      if (formMin !== '') config.min = Number(formMin)
      if (formMax !== '') config.max = Number(formMax)
    }
    if (t === 'text') {
      if (formMaxLength !== '') config.max_length = Number(formMaxLength)
      if (formTextVariant !== 'inline') config.text_variant = formTextVariant
    }
    return config
  }

  const validate = (): string | null => {
    if (!formName.trim()) return 'El nombre es obligatorio'
    if (formName.trim().length > 255) return 'El nombre no puede exceder 255 caracteres'

    const t = editingField ? editingField.field_type : formType
    // Check duplicate name (case-insensitive)
    const dup = fields.find(f => f.name.toLowerCase() === formName.trim().toLowerCase() && f.id !== editingField?.id)
    if (dup) return 'Ya existe un campo con ese nombre'
    // Check limit
    if (!editingField && fields.length >= 50) return 'Máximo 50 campos personalizados por cuenta'
    // Check options for select types
    if ((t === 'select' || t === 'multi_select') && formOptions.length === 0) {
      return 'Agrega al menos una opción'
    }
    return null
  }

  const handleSave = async () => {
    const err = validate()
    if (err) { setError(err); return }
    setError('')
    setSaving(true)
    try {
      const token = localStorage.getItem('token')
      const body: any = {
        name: formName.trim(),
        is_required: formRequired,
        default_value: formDefault.trim() || null,
        config: buildConfig(),
      }
      if (!editingField) {
        body.field_type = formType
      }

      const url = editingField ? `/api/custom-fields/${editingField.id}` : '/api/custom-fields'
      const method = editingField ? 'PUT' : 'POST'
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify(body),
      })
      const data = await res.json()
      if (data.success) {
        setShowModal(false)
        resetForm()
        fetchFields()
      } else {
        setError(data.error || 'Error al guardar')
      }
    } catch {
      setError('Error de conexión')
    }
    setSaving(false)
  }

  const handleDelete = async () => {
    if (!deleteConfirm) return
    setDeleting(true)
    try {
      const token = localStorage.getItem('token')
      const res = await fetch(`/api/custom-fields/${deleteConfirm.id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        setDeleteConfirm(null)
        fetchFields()
      }
    } catch {}
    setDeleting(false)
  }

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event
    if (!over || active.id === over.id) return
    const oldIndex = fields.findIndex(f => f.id === active.id)
    const newIndex = fields.findIndex(f => f.id === over.id)
    if (oldIndex === -1 || newIndex === -1) return
    const reordered = arrayMove(fields, oldIndex, newIndex)
    setFields(reordered)
    // Persist order
    try {
      const token = localStorage.getItem('token')
      await fetch('/api/custom-fields/reorder', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ field_ids: reordered.map(f => f.id) }),
      })
    } catch {}
  }

  const addOption = () => {
    const label = newOptionLabel.trim()
    if (!label) return
    const value = label.toLowerCase().replace(/\s+/g, '_').replace(/[^a-z0-9_]/g, '')
    if (formOptions.some(o => o.value === value)) return
    setFormOptions([...formOptions, { label, value }])
    setNewOptionLabel('')
  }

  const removeOption = (idx: number) => {
    setFormOptions(formOptions.filter((_, i) => i !== idx))
  }

  const moveOption = (idx: number, dir: -1 | 1) => {
    const newIdx = idx + dir
    if (newIdx < 0 || newIdx >= formOptions.length) return
    const arr = [...formOptions];
    [arr[idx], arr[newIdx]] = [arr[newIdx], arr[idx]]
    setFormOptions(arr)
  }

  const activeType = editingField ? editingField.field_type : formType

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-slate-900">Campos Personalizados</h3>
          <p className="text-xs text-slate-500 mt-1">
            Define campos adicionales para almacenar información específica de tus contactos. Máximo 50 campos.
          </p>
        </div>
        <button
          onClick={openCreate}
          disabled={fields.length >= 50}
          className="inline-flex items-center gap-1.5 bg-emerald-600 text-white px-3 py-1.5 rounded-xl hover:bg-emerald-700 text-xs font-medium shadow-sm disabled:opacity-50 transition"
        >
          <Plus className="w-3.5 h-3.5" />
          Nuevo Campo
        </button>
      </div>

      {/* Field count */}
      {fields.length > 0 && (
        <div className="text-[11px] text-slate-400">
          {fields.length} de 50 campos usados
          <div className="mt-1 w-full h-1 bg-slate-100 rounded-full overflow-hidden">
            <div className="h-full bg-emerald-500 rounded-full transition-all" style={{ width: `${(fields.length / 50) * 100}%` }} />
          </div>
        </div>
      )}

      {/* Fields List */}
      {loading ? (
        <div className="space-y-2">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-16 bg-slate-50 rounded-xl animate-pulse" />
          ))}
        </div>
      ) : fields.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-12 text-center">
          <div className="w-14 h-14 bg-slate-50 rounded-2xl flex items-center justify-center mb-3">
            <Tag className="w-7 h-7 text-slate-300" />
          </div>
          <h4 className="text-sm font-medium text-slate-700 mb-1">Sin campos personalizados</h4>
          <p className="text-xs text-slate-400 mb-4 max-w-xs">
            Crea campos para almacenar información adicional como ciudad, nivel educativo, presupuesto, etc.
          </p>
          <button
            onClick={openCreate}
            className="inline-flex items-center gap-1.5 bg-emerald-600 text-white px-3 py-1.5 rounded-xl hover:bg-emerald-700 text-xs font-medium shadow-sm transition"
          >
            <Plus className="w-3.5 h-3.5" />
            Crear primer campo
          </button>
        </div>
      ) : (
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={fields.map(f => f.id)} strategy={verticalListSortingStrategy}>
            <div className="space-y-2">
              {fields.map(field => (
                <SortableFieldItem key={field.id} field={field} onEdit={openEdit} onDelete={setDeleteConfirm} />
              ))}
            </div>
          </SortableContext>
        </DndContext>
      )}

      {/* Create/Edit Modal */}
      {showModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-sm z-50 flex items-center justify-center p-4" onClick={() => { setShowModal(false); resetForm() }}>
          <div className="bg-white border border-slate-200 rounded-2xl shadow-2xl w-full max-w-lg max-h-[90vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
            {/* Modal Header */}
            <div className="flex items-center justify-between p-5 border-b border-slate-100">
              <h3 className="text-base font-semibold text-slate-900">
                {editingField ? 'Editar Campo' : 'Nuevo Campo Personalizado'}
              </h3>
              <button onClick={() => { setShowModal(false); resetForm() }} className="p-1 text-slate-400 hover:text-slate-600 rounded-lg hover:bg-slate-100 transition">
                <X className="w-5 h-5" />
              </button>
            </div>

            {/* Modal Body */}
            <div className="p-5 space-y-4">
              {error && (
                <div className="flex items-center gap-2 px-3 py-2 bg-red-50 border border-red-100 rounded-xl text-xs text-red-700">
                  <AlertCircle className="w-4 h-4 shrink-0" />
                  {error}
                </div>
              )}

              {/* Name */}
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">Nombre del campo *</label>
                <input
                  type="text"
                  value={formName}
                  onChange={e => setFormName(e.target.value)}
                  placeholder="Ej: Ciudad de Origen"
                  className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                  autoFocus
                />
              </div>

              {/* Type (disabled when editing) */}
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">
                  Tipo de campo {editingField && <span className="text-slate-400 font-normal">(no modificable)</span>}
                </label>
                {editingField ? (
                  <div className="flex items-center gap-2 px-3 py-2 bg-slate-50 border border-slate-200 rounded-xl">
                    {(() => { const Ic = getFieldTypeIcon(editingField.field_type); return <Ic className="w-4 h-4 text-emerald-600" /> })()}
                    <span className="text-sm text-slate-700">{getFieldTypeLabel(editingField.field_type)}</span>
                  </div>
                ) : (
                  <div className="grid grid-cols-2 sm:grid-cols-5 gap-1.5">
                    {FIELD_TYPE_OPTIONS.map(opt => {
                      const Ic = opt.icon
                      return (
                        <button
                          key={opt.value}
                          type="button"
                          onClick={() => setFormType(opt.value)}
                          className={`flex flex-col items-center gap-1 p-2 rounded-xl border text-xs transition ${
                            formType === opt.value
                              ? 'border-emerald-300 bg-emerald-50 text-emerald-700'
                              : 'border-slate-200 text-slate-500 hover:border-slate-300 hover:bg-slate-50'
                          }`}
                        >
                          <Ic className="w-4 h-4" />
                          <span className="font-medium truncate w-full text-center">{opt.label}</span>
                        </button>
                      )
                    })}
                  </div>
                )}
              </div>

              {/* Options for select/multi_select */}
              {(activeType === 'select' || activeType === 'multi_select') && (
                <div>
                  <label className="block text-xs font-medium text-slate-600 mb-1">
                    Opciones {formOptions.length > 0 && <span className="text-slate-400 font-normal">({formOptions.length})</span>}
                  </label>
                  {formOptions.length > 0 && (
                    <div className="space-y-1 mb-2">
                      {formOptions.map((opt, idx) => (
                        <div key={idx} className="flex items-center gap-2 px-3 py-1.5 bg-slate-50 rounded-lg border border-slate-100">
                          <GripVertical className="w-3 h-3 text-slate-300" />
                          <span className="text-sm text-slate-700 flex-1">{opt.label}</span>
                          <code className="text-[10px] text-slate-400 font-mono">{opt.value}</code>
                          <div className="flex items-center gap-0.5">
                            {idx > 0 && (
                              <button type="button" onClick={() => moveOption(idx, -1)} className="p-0.5 text-slate-400 hover:text-slate-600"><ChevronDown className="w-3 h-3 rotate-180" /></button>
                            )}
                            {idx < formOptions.length - 1 && (
                              <button type="button" onClick={() => moveOption(idx, 1)} className="p-0.5 text-slate-400 hover:text-slate-600"><ChevronDown className="w-3 h-3" /></button>
                            )}
                            <button type="button" onClick={() => removeOption(idx)} className="p-0.5 text-slate-400 hover:text-red-500"><X className="w-3 h-3" /></button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={newOptionLabel}
                      onChange={e => setNewOptionLabel(e.target.value)}
                      placeholder="Nueva opción"
                      className="flex-1 px-3 py-1.5 border border-slate-200 rounded-lg focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                      onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addOption() } }}
                    />
                    <button
                      type="button"
                      onClick={addOption}
                      disabled={!newOptionLabel.trim()}
                      className="px-3 py-1.5 bg-slate-100 text-slate-600 rounded-lg text-xs font-medium hover:bg-slate-200 disabled:opacity-50 transition"
                    >
                      Agregar
                    </button>
                  </div>
                </div>
              )}

              {/* Currency config */}
              {activeType === 'currency' && (
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Símbolo</label>
                    <input
                      type="text"
                      value={formSymbol}
                      onChange={e => setFormSymbol(e.target.value)}
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Decimales</label>
                    <input
                      type="number"
                      value={formDecimals}
                      onChange={e => setFormDecimals(Number(e.target.value))}
                      min={0}
                      max={4}
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900"
                    />
                  </div>
                </div>
              )}

              {/* Number/Currency min/max */}
              {(activeType === 'number' || activeType === 'currency') && (
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Mínimo <span className="text-slate-400 font-normal">(opcional)</span></label>
                    <input
                      type="number"
                      value={formMin}
                      onChange={e => setFormMin(e.target.value)}
                      placeholder="Sin límite"
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Máximo <span className="text-slate-400 font-normal">(opcional)</span></label>
                    <input
                      type="number"
                      value={formMax}
                      onChange={e => setFormMax(e.target.value)}
                      placeholder="Sin límite"
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                    />
                  </div>
                </div>
              )}

              {/* Text variant + max_length */}
              {activeType === 'text' && (
                <div className="space-y-3">
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1.5">Tipo de entrada</label>
                    <div className="grid grid-cols-3 gap-2">
                      {[
                        { value: 'inline',   label: 'Línea simple', desc: 'Texto corto' },
                        { value: 'textarea', label: 'Texto largo',  desc: 'Multi-línea' },
                        { value: 'rich',     label: 'Enriquecido',  desc: 'Con formato' },
                      ].map(opt => (
                        <button
                          key={opt.value}
                          type="button"
                          onClick={() => setFormTextVariant(opt.value as 'inline' | 'textarea' | 'rich')}
                          className={`px-2.5 py-2 rounded-xl border text-left transition ${
                            formTextVariant === opt.value
                              ? 'border-emerald-500 bg-emerald-50 ring-2 ring-emerald-500/20'
                              : 'border-slate-200 hover:border-slate-300 bg-white'
                          }`}
                        >
                          <div className={`text-xs font-semibold ${formTextVariant === opt.value ? 'text-emerald-700' : 'text-slate-700'}`}>{opt.label}</div>
                          <div className="text-[10px] text-slate-400 mt-0.5">{opt.desc}</div>
                        </button>
                      ))}
                    </div>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Longitud máxima <span className="text-slate-400 font-normal">(opcional)</span></label>
                    <input
                      type="number"
                      value={formMaxLength}
                      onChange={e => setFormMaxLength(e.target.value)}
                      placeholder="Sin límite"
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400 max-w-[200px]"
                    />
                    <p className="text-[10px] text-slate-400 mt-1">Al definirlo, se muestra un contador de caracteres durante la captura.</p>
                  </div>
                </div>
              )}

              {/* Required toggle */}
              <div className="flex items-center justify-between py-2">
                <div>
                  <span className="text-xs font-medium text-slate-600">Campo obligatorio</span>
                  <p className="text-[10px] text-slate-400 mt-0.5">Se mostrará un indicador visual si no tiene valor</p>
                </div>
                <button
                  type="button"
                  onClick={() => setFormRequired(!formRequired)}
                  className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${formRequired ? 'bg-emerald-600' : 'bg-slate-200'}`}
                >
                  <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform shadow-sm ${formRequired ? 'translate-x-4' : 'translate-x-0.5'}`} />
                </button>
              </div>

              {/* Default value */}
              {activeType !== 'checkbox' && activeType !== 'multi_select' && (
                <div>
                  <label className="block text-xs font-medium text-slate-600 mb-1">Valor por defecto <span className="text-slate-400 font-normal">(opcional)</span></label>
                  {activeType === 'select' ? (
                    <select
                      value={formDefault}
                      onChange={e => setFormDefault(e.target.value)}
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900"
                    >
                      <option value="">Sin valor por defecto</option>
                      {formOptions.map(opt => (
                        <option key={opt.value} value={opt.value}>{opt.label}</option>
                      ))}
                    </select>
                  ) : (
                    <input
                      type={activeType === 'number' || activeType === 'currency' ? 'number' : activeType === 'date' ? 'date' : 'text'}
                      value={formDefault}
                      onChange={e => setFormDefault(e.target.value)}
                      placeholder="Ninguno"
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400 max-w-sm"
                    />
                  )}
                </div>
              )}
            </div>

            {/* Modal Footer */}
            <div className="flex justify-end gap-2 px-5 py-4 border-t border-slate-100">
              <button
                onClick={() => { setShowModal(false); resetForm() }}
                className="px-4 py-2 text-sm text-slate-600 hover:text-slate-900 hover:bg-slate-100 rounded-xl transition"
              >
                Cancelar
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                className="inline-flex items-center gap-1.5 px-4 py-2 bg-emerald-600 text-white rounded-xl text-sm font-medium hover:bg-emerald-700 disabled:opacity-50 transition shadow-sm"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
                {editingField ? 'Guardar Cambios' : 'Crear Campo'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation */}
      {deleteConfirm && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-sm z-50 flex items-center justify-center p-4" onClick={() => setDeleteConfirm(null)}>
          <div className="bg-white border border-slate-200 rounded-2xl shadow-2xl w-full max-w-sm" onClick={e => e.stopPropagation()}>
            <div className="p-5 text-center">
              <div className="w-12 h-12 bg-red-50 rounded-full flex items-center justify-center mx-auto mb-3">
                <Trash2 className="w-6 h-6 text-red-500" />
              </div>
              <h4 className="text-base font-semibold text-slate-900 mb-1">Eliminar campo</h4>
              <p className="text-sm text-slate-500 mb-1">
                ¿Eliminar <strong>{deleteConfirm.name}</strong>?
              </p>
              <p className="text-xs text-red-500">
                Se eliminarán todos los valores asociados a este campo en todos los contactos.
              </p>
            </div>
            <div className="flex gap-2 px-5 py-4 border-t border-slate-100">
              <button
                onClick={() => setDeleteConfirm(null)}
                className="flex-1 px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 rounded-xl transition"
              >
                Cancelar
              </button>
              <button
                onClick={handleDelete}
                disabled={deleting}
                className="flex-1 inline-flex items-center justify-center gap-1.5 px-4 py-2 bg-red-600 text-white rounded-xl text-sm font-medium hover:bg-red-700 disabled:opacity-50 transition"
              >
                {deleting ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
                Eliminar
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default function SettingsPage() {
  const [account, setAccount] = useState<Account | null>(null)
  const [user, setUser] = useState<UserProfile | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [activeTab, setActiveTab] = useState('profile')
  const [formData, setFormData] = useState({
    userName: '',
    userEmail: '',
    accountName: '',
    currentPassword: '',
    newPassword: '',
    confirmPassword: '',
  })
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [notifSettings, setNotifSettings] = useState<NotificationSettings | null>(null)
  const [notifPermission, setNotifPermission] = useState<NotificationPermission>('default')
  const { refreshSettings: refreshProviderSettings } = useNotifications()
  const [quickReplies, setQuickReplies] = useState<{ id: string; shortcut: string; title: string; body: string; media_url: string; media_type: string; media_filename: string; attachments: { id?: string; media_url: string; media_type: string; media_filename: string; caption: string; position: number }[] }[]>([])
  const [editingQR, setEditingQR] = useState<{ id?: string; shortcut: string; title: string; body: string; media_url: string; media_type: string; media_filename: string; attachments: { id?: string; media_url: string; media_type: string; media_filename: string; caption: string; position: number }[] } | null>(null)
  const [savingQR, setSavingQR] = useState(false)
  const [uploadingQRMedia, setUploadingQRMedia] = useState(false)
  const [integrationView, setIntegrationView] = useState<'list' | 'google'>('list')
  const [incomingStageId, setIncomingStageId] = useState<string>('')
  const [pipelineStages, setPipelineStages] = useState<{ id: string; name: string; color: string; pipeline_name: string }[]>([])
  const [savingStage, setSavingStage] = useState(false)
  const [storageUsage, setStorageUsage] = useState<StorageUsage | null>(null)
  const [storageFiles, setStorageFiles] = useState<StorageFile[]>([])
  const [storageLoading, setStorageLoading] = useState(false)
  const [storageDeleting, setStorageDeleting] = useState<string | null>(null)
  const [storageType, setStorageType] = useState('')

  // Devices state
  type DeviceProvider = 'whatsapp_web' | 'whatsapp_cloud_api'
  interface DeviceItem {
    id: string; name: string; phone: string; jid: string; status: string; qr_code: string; last_seen_at: string; receive_messages: boolean
    provider?: DeviceProvider; waba_id?: string; phone_number_id?: string; api_display_phone?: string; api_webhook_status?: string; api_billing_status?: string; api_sending_enabled?: boolean; api_templates_enabled?: boolean; capabilities?: string[]
  }
  const [devDevices, setDevDevices] = useState<DeviceItem[]>([])
  const [devLoading, setDevLoading] = useState(true)
  const [devShowCreate, setDevShowCreate] = useState(false)
  const [devCreateProvider, setDevCreateProvider] = useState<DeviceProvider>('whatsapp_web')
  const [devNewName, setDevNewName] = useState('')
  const [devNewApiDisplayPhone, setDevNewApiDisplayPhone] = useState('')
  const [devNewApiPhoneNumberID, setDevNewApiPhoneNumberID] = useState('')
  const [devNewApiWabaID, setDevNewApiWabaID] = useState('')
  const [devCreating, setDevCreating] = useState(false)
  const [devSelected, setDevSelected] = useState<DeviceItem | null>(null)
  const [devEditing, setDevEditing] = useState<DeviceItem | null>(null)
  const [devEditName, setDevEditName] = useState('')
  const [devEditApiDisplayPhone, setDevEditApiDisplayPhone] = useState('')
  const [devEditApiPhoneNumberID, setDevEditApiPhoneNumberID] = useState('')
  const [devEditApiWabaID, setDevEditApiWabaID] = useState('')
  const [devSaving, setDevSaving] = useState(false)

  // Google Contacts state
  const [googleStatus, setGoogleStatus] = useState<{
    connected: boolean; email: string | null; sync_limit: number; sync_count: number; configured: boolean; token_valid?: boolean
  } | null>(null)
  const [googleLoading, setGoogleLoading] = useState(false)
  const [googleDisconnecting, setGoogleDisconnecting] = useState(false)

  // URL tab param
  const searchParams = useSearchParams()
  const router = useRouter()

  // Pipeline management state
  interface ManagedPipeline {
    id: string
    name: string
    description: string | null
    is_default: boolean
    stages: { id: string; name: string; color: string; position: number }[] | null
  }
  const [managedPipelines, setManagedPipelines] = useState<ManagedPipeline[]>([])
  const [loadingPipelines, setLoadingPipelines] = useState(false)
  const [expandedPipelineId, setExpandedPipelineId] = useState<string | null>(null)
  const [showNewPipelineForm, setShowNewPipelineForm] = useState(false)
  const [newPipelineName, setNewPipelineName] = useState('')
  const [savingPipeline, setSavingPipeline] = useState(false)
  const [editingPipelineId, setEditingPipelineId] = useState<string | null>(null)
  const [editPipelineName, setEditPipelineName] = useState('')
  const [newStageName, setNewStageName] = useState('')
  const [newStageColor, setNewStageColor] = useState('#6366f1')
  const [addingStageForPipeline, setAddingStageForPipeline] = useState<string | null>(null)
  const [savingNewStage, setSavingNewStage] = useState(false)

  const fetchSettings = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      // Fetch user info from /api/me (always works)
      const meRes = await fetch('/api/me', {
        headers: { Authorization: `Bearer ${token}` },
      })
      const meData = await meRes.json()
      if (meData.success && meData.user) {
        const u = meData.user
        setUser({
          id: u.id,
          email: u.email,
          name: u.display_name || u.username,
          role: u.role,
          account_id: u.account_id,
          is_super_admin: u.is_super_admin,
          is_admin: u.is_admin,
          permissions: u.permissions || [],
        })
        setFormData(prev => ({
          ...prev,
          userName: u.display_name || u.username || '',
          userEmail: u.email || '',
          accountName: u.account_name || '',
        }))
        // Build account info from /api/me response
        setAccount({
          id: u.account_id,
          name: u.account_name || '',
          slug: '',
          plan: '',
          created_at: '',
        })
      }
      // Try to fetch richer settings (may not exist)
      try {
        const res = await fetch('/api/settings', {
          headers: { Authorization: `Bearer ${token}` },
        })
        const data = await res.json()
        if (data.success) {
          if (data.account) {
            setAccount(data.account)
            setIncomingStageId(data.account.default_incoming_stage_id || '')
          }
          if (data.user) {
            setUser(prev => ({ ...prev!, ...data.user, account_id: prev?.account_id }))
            setFormData(prev => ({
              ...prev,
              userName: data.user?.name || prev.userName,
              userEmail: data.user?.email || prev.userEmail,
              accountName: data.account?.name || prev.accountName,
            }))
          }
        }
      } catch { /* optional endpoint */ }
    } catch (err) {
      console.error('Failed to fetch settings:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  // Read tab from URL param (for redirect from /dashboard/devices)
  useEffect(() => {
    const tab = searchParams.get('tab')
    if (tab === 'storage') {
      router.replace('/dashboard/storage')
      return
    }
    if (tab) setActiveTab(tab)
  }, [router, searchParams])

  const fetchQuickReplies = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/quick-replies', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) setQuickReplies(data.quick_replies || [])
    } catch {}
  }, [])

  useEffect(() => {
    fetchQuickReplies()
  }, [fetchQuickReplies])

  const fetchPipelineStages = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/pipelines', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success && data.pipelines) {
        const stages: { id: string; name: string; color: string; pipeline_name: string }[] = []
        for (const p of data.pipelines) {
          if (p.stages) {
            for (const st of p.stages) {
              stages.push({ id: st.id, name: st.name, color: st.color, pipeline_name: p.name })
            }
          }
        }
        setPipelineStages(stages)
      }
    } catch {}
  }, [])

  useEffect(() => {
    if (activeTab === 'account') {
      fetchPipelineStages()
      fetchManagedPipelines()
    }
  }, [activeTab, fetchPipelineStages])

  const fetchManagedPipelines = async () => {
    setLoadingPipelines(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/pipelines', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success && data.pipelines) {
        setManagedPipelines(data.pipelines)
      }
    } catch {} finally { setLoadingPipelines(false) }
  }

  const handleCreatePipeline = async () => {
    if (!newPipelineName.trim()) return
    setSavingPipeline(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/pipelines', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ name: newPipelineName.trim() }),
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', `Pipeline "${newPipelineName.trim()}" creado`)
        setNewPipelineName('')
        setShowNewPipelineForm(false)
        fetchManagedPipelines()
        fetchPipelineStages()
      } else {
        showMessage('error', data.error || 'Error al crear pipeline')
      }
    } catch { showMessage('error', 'Error de conexión') } finally { setSavingPipeline(false) }
  }

  const handleUpdatePipeline = async (id: string) => {
    if (!editPipelineName.trim()) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ name: editPipelineName.trim() }),
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', 'Pipeline actualizado')
        setEditingPipelineId(null)
        fetchManagedPipelines()
        fetchPipelineStages()
      } else {
        showMessage('error', data.error || 'Error al actualizar')
      }
    } catch { showMessage('error', 'Error de conexión') }
  }

  const handleDeletePipeline = async (id: string, name: string) => {
    if (!confirm(`¿Eliminar el pipeline "${name}" y todas sus etapas? Esta acción no se puede deshacer.`)) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', `Pipeline "${name}" eliminado`)
        fetchManagedPipelines()
        fetchPipelineStages()
      } else {
        showMessage('error', data.error || 'Error al eliminar')
      }
    } catch { showMessage('error', 'Error de conexión') }
  }

  const handleCreateStage = async (pipelineId: string) => {
    if (!newStageName.trim()) return
    setSavingNewStage(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${pipelineId}/stages`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ name: newStageName.trim(), color: newStageColor }),
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', `Etapa "${newStageName.trim()}" creada`)
        setNewStageName('')
        setNewStageColor('#6366f1')
        setAddingStageForPipeline(null)
        fetchManagedPipelines()
        fetchPipelineStages()
      } else {
        showMessage('error', data.error || 'Error al crear etapa')
      }
    } catch { showMessage('error', 'Error de conexión') } finally { setSavingNewStage(false) }
  }

  const handleDeleteStage = async (pipelineId: string, stageId: string, stageName: string) => {
    if (!confirm(`¿Eliminar la etapa "${stageName}"?`)) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${pipelineId}/stages/${stageId}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', `Etapa "${stageName}" eliminada`)
        fetchManagedPipelines()
        fetchPipelineStages()
      } else {
        showMessage('error', data.error || 'Error al eliminar etapa')
      }
    } catch { showMessage('error', 'Error de conexión') }
  }

  const handleSaveIncomingStage = async (stageId: string) => {
    setSavingStage(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/settings/incoming-stage', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ stage_id: stageId || null }),
      })
      const data = await res.json()
      if (data.success) {
        setIncomingStageId(stageId)
        showMessage('success', 'Etapa de leads entrantes actualizada')
      } else {
        showMessage('error', data.error || 'Error al actualizar')
      }
    } catch {
      showMessage('error', 'Error de conexión')
    } finally {
      setSavingStage(false)
    }
  }

  // --- Google Contacts ---
  const fetchGoogleStatus = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/google/status', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) setGoogleStatus(data)
    } catch {}
  }, [])

  useEffect(() => {
    if (activeTab === 'integrations') fetchGoogleStatus()
  }, [activeTab, fetchGoogleStatus])

  // Handle Google redirect callback
  useEffect(() => {
    const googleParam = searchParams.get('google')
    if (googleParam === 'connected') {
      fetchGoogleStatus()
      showMessage('success', 'Google Contacts conectado exitosamente')
      router.replace('/dashboard/settings?tab=integrations')
    } else if (googleParam === 'error') {
      const msg = searchParams.get('msg') || 'Error al conectar Google Contacts'
      showMessage('error', msg)
      router.replace('/dashboard/settings?tab=integrations')
    }
  }, [searchParams, fetchGoogleStatus, router])

  const handleGoogleConnect = async () => {
    setGoogleLoading(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/google/auth-url', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success && data.url) {
        window.location.href = data.url
      } else {
        showMessage('error', data.error || 'Error al obtener URL de autorización')
        setGoogleLoading(false)
      }
    } catch {
      showMessage('error', 'Error de conexión')
      setGoogleLoading(false)
    }
  }

  const handleGoogleDisconnect = async () => {
    if (!confirm('¿Desconectar Google Contacts? Se dejará de sincronizar todos los contactos.')) return
    setGoogleDisconnecting(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/google/disconnect', { method: 'DELETE', headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) {
        showMessage('success', 'Google Contacts desconectado')
        fetchGoogleStatus()
      } else {
        showMessage('error', data.error || 'Error al desconectar')
      }
    } catch {
      showMessage('error', 'Error de conexión')
    } finally {
      setGoogleDisconnecting(false)
    }
  }

  const handleQRMediaUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files
    if (!files || files.length === 0 || !editingQR) return
    const currentCount = editingQR.attachments.length
    const maxNew = 5 - currentCount
    if (maxNew <= 0) {
      showMessage('error', 'Máximo 5 adjuntos por respuesta rápida')
      e.target.value = ''
      return
    }
    setUploadingQRMedia(true)
    const token = localStorage.getItem('token')
    const newAttachments = [...editingQR.attachments]
    try {
      const { compressImageStandard } = await import('@/utils/imageCompression')
      for (let i = 0; i < Math.min(files.length, maxNew); i++) {
        let file = files[i]
        // Compress images
        if (file.type.startsWith('image/') && !file.type.includes('gif')) {
          try { file = await compressImageStandard(file) } catch {}
        }
        const formData = new FormData()
        formData.append('file', file)
        const res = await fetch('/api/media/upload', {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
          body: formData,
        })
        const data = await res.json()
        if (data.success && (data.proxy_url || data.public_url)) {
          let mediaType = 'document'
          if (file.type.startsWith('image/')) mediaType = 'image'
          else if (file.type.startsWith('video/')) mediaType = 'video'
          else if (file.type.startsWith('audio/')) mediaType = 'audio'
          newAttachments.push({
            media_url: data.proxy_url || data.public_url,
            media_type: mediaType,
            media_filename: files[i].name,
            caption: '',
            position: newAttachments.length,
          })
        } else {
          showMessage('error', data.error || `Error al subir ${files[i].name}`)
        }
      }
      setEditingQR({ ...editingQR, attachments: newAttachments })
    } catch {
      showMessage('error', 'Error al subir archivo(s)')
    } finally {
      setUploadingQRMedia(false)
      e.target.value = ''
    }
  }

  const handleSaveQuickReply = async () => {
    if (!editingQR || !editingQR.shortcut.trim() || (!editingQR.body.trim() && !editingQR.media_url && editingQR.attachments.length === 0)) return
    setSavingQR(true)
    const token = localStorage.getItem('token')
    try {
      const isEdit = !!editingQR.id
      const res = await fetch(isEdit ? `/api/quick-replies/${editingQR.id}` : '/api/quick-replies', {
        method: isEdit ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({
          shortcut: editingQR.shortcut.trim(),
          title: editingQR.title.trim(),
          body: editingQR.body.trim(),
          media_url: editingQR.media_url || '',
          media_type: editingQR.media_type || '',
          media_filename: editingQR.media_filename || '',
          attachments: editingQR.attachments.map((a, i) => ({
            media_url: a.media_url,
            media_type: a.media_type,
            media_filename: a.media_filename,
            caption: a.caption || '',
          })),
        }),
      })
      const data = await res.json()
      if (data.success) {
        setEditingQR(null)
        fetchQuickReplies()
        showMessage('success', isEdit ? 'Respuesta rápida actualizada' : 'Respuesta rápida creada')
      } else {
        showMessage('error', data.error || 'Error al guardar')
      }
    } catch {
      showMessage('error', 'Error al guardar respuesta rápida')
    } finally {
      setSavingQR(false)
    }
  }

  const handleDeleteQuickReply = async (id: string) => {
    if (!confirm('¿Eliminar esta respuesta rápida?')) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/quick-replies/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        fetchQuickReplies()
        showMessage('success', 'Respuesta rápida eliminada')
      } else {
        showMessage('error', data.error || 'Error al eliminar')
      }
    } catch {
      showMessage('error', 'Error al eliminar respuesta rápida')
    }
  }

  // Load notification settings once we have the account ID
  useEffect(() => {
    if (user?.account_id) {
      setNotifSettings(getNotificationSettings(user.account_id))
    }
    if ('Notification' in window) {
      setNotifPermission(Notification.permission)
    }
  }, [user?.account_id])

  const handleSaveNotifications = () => {
    if (!user?.account_id || !notifSettings) return
    saveNotificationSettings(user.account_id, notifSettings)
    refreshProviderSettings()
    showMessage('success', 'Preferencias de notificación guardadas')
  }

  const handleRequestPermission = async () => {
    const perm = await requestNotificationPermission()
    setNotifPermission(perm)
    if (perm === 'granted') {
      showMessage('success', 'Notificaciones del navegador activadas')
    }
  }

  const handlePreviewSound = () => {
    if (notifSettings) {
      playNotificationSound(notifSettings.sound_type, notifSettings.sound_volume)
    }
  }

  const fetchStorage = useCallback(async () => {
    setStorageLoading(true)
    const token = localStorage.getItem('token')
    try {
      const [usageRes, filesRes] = await Promise.all([
        fetch('/api/storage/usage', { headers: { Authorization: `Bearer ${token}` } }),
        fetch(`/api/storage/files?limit=80${storageType ? `&type=${storageType}` : ''}`, { headers: { Authorization: `Bearer ${token}` } }),
      ])
      const usageData = await usageRes.json()
      const filesData = await filesRes.json()
      if (usageData.success) setStorageUsage(usageData)
      if (filesData.success) setStorageFiles(filesData.files || [])
    } catch (err) {
      showMessage('error', 'No se pudo cargar el almacenamiento')
    } finally {
      setStorageLoading(false)
    }
  }, [storageType])

  useEffect(() => {
    if (activeTab === 'storage') fetchStorage()
  }, [activeTab, fetchStorage])

  const handleDeleteStorageFile = async (file: StorageFile) => {
    if (!confirm(`¿Eliminar "${file.filename}"? El archivo dejará de mostrarse en los chats.`)) return
    setStorageDeleting(file.object_key)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/storage/files', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ object_keys: [file.object_key], confirmation: 'DELETE_MEDIA' }),
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', `Archivo eliminado. Liberado: ${formatBytes(data.freed_bytes)}`)
        fetchStorage()
      } else {
        showMessage('error', data.error || 'No se pudo eliminar')
      }
    } catch (err) {
      showMessage('error', 'No se pudo eliminar')
    } finally {
      setStorageDeleting(null)
    }
  }

  const showMessage = (type: 'success' | 'error', text: string) => {
    setMessage({ type, text })
    setTimeout(() => setMessage(null), 3000)
  }

  const handleSaveProfile = async () => {
    setSaving(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/settings/profile', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name: formData.userName,
          email: formData.userEmail,
        }),
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', 'Perfil actualizado correctamente')
        fetchSettings()
      } else {
        showMessage('error', data.error || 'Error al actualizar perfil')
      }
    } catch (err) {
      showMessage('error', 'Error al actualizar perfil')
    } finally {
      setSaving(false)
    }
  }

  const handleSaveAccount = async () => {
    setSaving(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/settings/account', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name: formData.accountName,
        }),
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', 'Cuenta actualizada correctamente')
        fetchSettings()
      } else {
        showMessage('error', data.error || 'Error al actualizar cuenta')
      }
    } catch (err) {
      showMessage('error', 'Error al actualizar cuenta')
    } finally {
      setSaving(false)
    }
  }

  const handleChangePassword = async () => {
    if (formData.newPassword !== formData.confirmPassword) {
      showMessage('error', 'Las contraseñas no coinciden')
      return
    }
    if (formData.newPassword.length < 8) {
      showMessage('error', 'La contraseña debe tener al menos 8 caracteres')
      return
    }
    setSaving(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/settings/password', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          currentPassword: formData.currentPassword,
          newPassword: formData.newPassword,
        }),
      })
      const data = await res.json()
      if (data.success) {
        showMessage('success', 'Contraseña actualizada correctamente')
        setFormData(prev => ({
          ...prev,
          currentPassword: '',
          newPassword: '',
          confirmPassword: '',
        }))
      } else {
        showMessage('error', data.error || 'Error al cambiar contraseña')
      }
    } catch (err) {
      showMessage('error', 'Error al cambiar contraseña')
    } finally {
      setSaving(false)
    }
  }

  // ─── Device Functions ───
  const fetchDevicesForSettings = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/devices', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) setDevDevices(data.devices || [])
    } catch (err) { console.error('Failed to fetch devices:', err) }
    finally { setDevLoading(false) }
  }, [])

  useEffect(() => {
    if (activeTab === 'devices') {
      fetchDevicesForSettings()
      const interval = setInterval(fetchDevicesForSettings, 5000)
      return () => clearInterval(interval)
    }
  }, [activeTab, fetchDevicesForSettings])

  useEffect(() => {
    if (activeTab !== 'devices') return
    const unsubscribe = subscribeWebSocket((data: unknown) => {
      const msg = data as { event?: string; data?: { status?: string; device_id?: string } }
      if (msg.event === 'device_status') {
        if (msg.data?.status === 'connected' && devSelected?.id === msg.data?.device_id) setDevSelected(null)
        fetchDevicesForSettings()
      } else if (msg.event === 'qr_code') fetchDevicesForSettings()
    })
    return () => unsubscribe()
  }, [activeTab, fetchDevicesForSettings, devSelected])

  useEffect(() => {
    if (devSelected) {
      const upd = devDevices.find(d => d.id === devSelected.id)
      if (upd && upd.status === 'connected') setDevSelected(null)
      else if (upd && upd.qr_code !== devSelected.qr_code) setDevSelected(upd)
    }
  }, [devDevices, devSelected])

  const getDeviceProvider = (device?: DeviceItem | null): DeviceProvider => device?.provider || 'whatsapp_web'
  const isApiDevice = (device?: DeviceItem | null) => getDeviceProvider(device) === 'whatsapp_cloud_api'

  const resetDevCreateForm = () => {
    setDevCreateProvider('whatsapp_web')
    setDevNewName('')
    setDevNewApiDisplayPhone('')
    setDevNewApiPhoneNumberID('')
    setDevNewApiWabaID('')
  }

  const openDevEdit = (device: DeviceItem) => {
    setDevEditing(device)
    setDevEditName(device.name || '')
    setDevEditApiDisplayPhone(device.api_display_phone || device.phone || '')
    setDevEditApiPhoneNumberID(device.phone_number_id || '')
    setDevEditApiWabaID(device.waba_id || '')
  }

  const handleDevCreate = async () => {
    if (!devNewName.trim()) return
    setDevCreating(true)
    const token = localStorage.getItem('token')
    try {
      const payload = devCreateProvider === 'whatsapp_cloud_api'
        ? {
            name: devNewName.trim(),
            provider: devCreateProvider,
            api_display_phone: devNewApiDisplayPhone.trim() || undefined,
            phone_number_id: devNewApiPhoneNumberID.trim() || undefined,
            waba_id: devNewApiWabaID.trim() || undefined,
          }
        : { name: devNewName.trim(), provider: devCreateProvider }
      const res = await fetch('/api/devices', { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` }, body: JSON.stringify(payload) })
      const data = await res.json()
      if (data.success) {
        resetDevCreateForm()
        setDevShowCreate(false)
        fetchDevicesForSettings()
        if (devCreateProvider === 'whatsapp_web') await handleDevConnect(data.device.id)
        else showMessage('success', 'Canal API Oficial creado en modo configuración')
      } else {
        showMessage('error', data.error || 'Error al crear dispositivo')
      }
    } catch (err) { console.error('Failed to create device:', err) }
    finally { setDevCreating(false) }
  }

  const handleDevConnect = async (deviceId: string) => {
    const token = localStorage.getItem('token')
    try { await fetch(`/api/devices/${deviceId}/connect`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } }); fetchDevicesForSettings() }
    catch (err) { console.error('Failed to connect device:', err) }
  }

  const handleDevDisconnect = async (deviceId: string) => {
    const token = localStorage.getItem('token')
    try { await fetch(`/api/devices/${deviceId}/disconnect`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } }); fetchDevicesForSettings() }
    catch (err) { console.error('Failed to disconnect device:', err) }
  }

  const handleDevReset = async (deviceId: string) => {
    if (!confirm('¿Re-vincular este dispositivo? Se desconectará de WhatsApp y necesitarás escanear un nuevo código QR. Esto sincronizará todo el historial de mensajes.')) return
    const token = localStorage.getItem('token')
    try {
      await fetch(`/api/devices/${deviceId}/reset`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } })
      fetchDevicesForSettings()
      // Auto-reconnect to generate QR code
      setTimeout(async () => {
        await fetch(`/api/devices/${deviceId}/connect`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } })
        fetchDevicesForSettings()
        const dev = devDevices.find((d: any) => d.id === deviceId)
        if (dev) setDevSelected(dev)
      }, 1000)
    } catch (err) { console.error('Failed to reset device:', err) }
  }

  const handleDevDelete = async (deviceId: string) => {
    if (!confirm('¿Estás seguro de eliminar este dispositivo?')) return
    const token = localStorage.getItem('token')
    try { await fetch(`/api/devices/${deviceId}`, { method: 'DELETE', headers: { Authorization: `Bearer ${token}` } }); fetchDevicesForSettings(); if (devSelected?.id === deviceId) setDevSelected(null) }
    catch (err) { console.error('Failed to delete device:', err) }
  }

  const handleDevUpdate = async () => {
    if (!devEditing || !devEditName.trim()) return
    setDevSaving(true)
    const token = localStorage.getItem('token')
    try {
      const payload = isApiDevice(devEditing)
        ? {
            name: devEditName.trim(),
            api_display_phone: devEditApiDisplayPhone.trim(),
            phone_number_id: devEditApiPhoneNumberID.trim(),
            waba_id: devEditApiWabaID.trim(),
            api_sending_enabled: false,
            api_templates_enabled: false,
          }
        : { name: devEditName.trim() }
      const res = await fetch(`/api/devices/${devEditing.id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` }, body: JSON.stringify(payload) })
      const data = await res.json()
      if (data.success) { setDevEditing(null); fetchDevicesForSettings() } else alert(data.error || 'Error al actualizar')
    } catch (err) { console.error('Failed to update device:', err) }
    finally { setDevSaving(false) }
  }

  const handleToggleReceiveMessages = async (device: DeviceItem) => {
    const token = localStorage.getItem('token')
    const newValue = !device.receive_messages
    // Optimistic update
    setDevDevices(prev => prev.map(d => d.id === device.id ? { ...d, receive_messages: newValue } : d))
    try {
      const res = await fetch(`/api/devices/${device.id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` }, body: JSON.stringify({ receive_messages: newValue }) })
      const data = await res.json()
      if (!data.success) {
        // Revert on failure
        setDevDevices(prev => prev.map(d => d.id === device.id ? { ...d, receive_messages: !newValue } : d))
      }
    } catch {
      setDevDevices(prev => prev.map(d => d.id === device.id ? { ...d, receive_messages: !newValue } : d))
    }
  }

  const getDevStatusBadge = (status: string) => {
    switch (status) {
      case 'connected': return <span className="flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs bg-emerald-50 text-emerald-700"><Wifi className="w-3.5 h-3.5" /> Conectado</span>
      case 'connecting': return <span className="flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs bg-amber-50 text-amber-700"><Signal className="w-3.5 h-3.5 animate-pulse" /> Conectando</span>
      default: return <span className="flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs bg-slate-100 text-slate-500"><WifiOff className="w-3.5 h-3.5" /> Desconectado</span>
    }
  }

  const getDevProviderBadge = (device: DeviceItem) => {
    if (isApiDevice(device)) {
      return <span className="flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs bg-sky-50 text-sky-700"><Globe className="w-3.5 h-3.5" /> API Oficial</span>
    }
    return <span className="flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs bg-emerald-50 text-emerald-700"><QrCode className="w-3.5 h-3.5" /> QR</span>
  }

  const getApiGuardBadges = (device: DeviceItem) => {
    if (!isApiDevice(device)) return null
    return (
      <div className="flex flex-wrap gap-1.5">
        <span className="flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] bg-slate-100 text-slate-600"><Power className="w-3 h-3" /> Envio bloqueado</span>
        <span className="flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] bg-amber-50 text-amber-700"><DollarSign className="w-3 h-3" /> Pago no configurado</span>
      </div>
    )
  }

  const handleLogout = () => {
    void logoutFromBrowser('manual')
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-6 w-6 border-2 border-emerald-200 border-t-emerald-600" />
      </div>
    )
  }

  const tabs = [
    { id: 'profile', label: 'Perfil', icon: User },
    { id: 'account', label: 'Cuenta', icon: Building },
    ...((user?.is_super_admin || user?.is_admin || user?.permissions?.includes('integrations') || user?.permissions?.includes('*')) ? [{ id: 'integrations', label: 'Integraciones', icon: Link2 }] : []),
    ...((user?.is_super_admin || user?.is_admin || user?.permissions?.includes('devices') || user?.permissions?.includes('*')) ? [{ id: 'devices', label: 'Dispositivos', icon: Smartphone }] : []),
    ...((user?.is_super_admin || user?.is_admin || user?.permissions?.includes('integrations') || user?.permissions?.includes('*')) ? [{ id: 'whatsapp-api', label: 'WhatsApp API', icon: Globe }] : []),
    { id: 'quick-replies', label: 'Resp. Rápidas', icon: Zap },
    ...((user?.is_super_admin || user?.is_admin || user?.permissions?.includes('settings') || user?.permissions?.includes('*')) ? [{ id: 'custom-fields', label: 'Campos', icon: Tag }] : []),
    { id: 'notifications', label: 'Notificaciones', icon: Bell },
    ...((user?.is_super_admin || user?.is_admin) ? [{ id: 'api-keys', label: 'API / MCP', icon: Key }] : []),
    { id: 'security', label: 'Seguridad', icon: Shield },
  ]

  return (
    <div className="h-full flex flex-col min-h-0 gap-3">
      {/* Header */}
      <div className="flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2.5">
          <div className="w-9 h-9 bg-emerald-50 rounded-xl flex items-center justify-center">
            <Settings className="w-5 h-5 text-emerald-600" />
          </div>
          <div>
            <h1 className="text-lg font-semibold text-slate-900">Configuración</h1>
            <p className="text-xs text-slate-500">Administra tu perfil y preferencias</p>
          </div>
        </div>
      </div>

      {/* Message */}
      {message && (
        <div className={`px-3 py-2 rounded-xl text-xs shrink-0 ${message.type === 'success' ? 'bg-emerald-50 text-emerald-700 border border-emerald-100' : 'bg-red-50 text-red-700 border border-red-100'}`}>
          {message.text}
        </div>
      )}

      {/* Tabs */}
      <div className="bg-white rounded-xl border border-slate-200 overflow-hidden flex-1 min-h-0 flex flex-col">
        <div className="flex overflow-x-auto border-b border-slate-200 shrink-0">
          {tabs.map((tab) => {
            const Icon = tab.icon
            return (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`flex items-center gap-1.5 px-3 py-2.5 sm:px-5 sm:py-3 font-medium transition whitespace-nowrap text-xs sm:text-sm ${
                  activeTab === tab.id
                    ? 'text-emerald-600 border-b-2 border-emerald-600 bg-emerald-50/50'
                    : 'text-slate-500 hover:text-slate-900 hover:bg-slate-50'
                }`}
              >
                <Icon className="w-4 h-4" />
                {tab.label}
              </button>
            )
          })}
        </div>

        <div className="p-6 flex-1 overflow-y-auto">
          {/* Profile Tab */}
          {activeTab === 'profile' && (
            <div className="space-y-6">
              <div className="flex items-center gap-4">
                <div className="w-16 h-16 bg-emerald-50 rounded-full flex items-center justify-center">
                  <span className="text-emerald-700 text-xl font-semibold">
                    {(user?.name || user?.email || 'U').charAt(0).toUpperCase()}
                  </span>
                </div>
                <div>
                  <h2 className="text-lg font-semibold text-slate-900">{user?.name || 'Usuario'}</h2>
                  <p className="text-sm text-slate-500">{user?.email}</p>
                  <span className="inline-block mt-1 px-2 py-0.5 bg-emerald-50 text-emerald-700 text-xs font-medium rounded-full">
                    {user?.role || 'user'}
                  </span>
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="block text-xs font-medium text-slate-600 mb-1">Nombre</label>
                  <input
                    type="text"
                    value={formData.userName}
                    onChange={(e) => setFormData({ ...formData, userName: e.target.value })}
                    className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-slate-600 mb-1">Email</label>
                  <input
                    type="email"
                    value={formData.userEmail}
                    onChange={(e) => setFormData({ ...formData, userEmail: e.target.value })}
                    className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                  />
                </div>
              </div>

              <button
                onClick={handleSaveProfile}
                disabled={saving}
                className="inline-flex items-center gap-2 bg-emerald-600 text-white px-4 py-2 rounded-xl hover:bg-emerald-700 disabled:opacity-50 text-sm font-medium shadow-sm"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                Guardar Cambios
              </button>
            </div>
          )}

          {/* Account Tab */}
          {activeTab === 'account' && (
            <div className="space-y-6">
              <div className="bg-slate-50 p-4 rounded-xl">
                <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
                  <div>
                    <h3 className="text-sm font-medium text-slate-900">Información de la Cuenta</h3>
                    <div className="mt-2 space-y-1 text-xs text-slate-500">
                      <p>Plan: <span className="font-medium text-slate-900">{account?.plan || 'free'}</span></p>
                      <p>Slug: <span className="font-medium text-slate-900">{account?.slug || 'N/A'}</span></p>
                      <p>Creada: <span className="font-medium text-slate-900">
                        {account?.created_at ? new Date(account.created_at).toLocaleDateString('es') : 'N/A'}
                      </span></p>
                    </div>
                  </div>
                  <div className="sm:text-right shrink-0">
                    <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${subscriptionColors[account?.subscription_status || 'active'] || 'bg-slate-100 text-slate-600'}`}>
                      {subscriptionLabels[account?.subscription_status || 'active'] || account?.subscription_status || 'Activa'}
                    </span>
                    {subscriptionDeadline(account) && (
                      <p className="mt-1 text-[11px] text-slate-500">
                        {daysUntil(subscriptionDeadline(account))} días restantes
                      </p>
                    )}
                  </div>
                </div>
              </div>

              <button
                onClick={() => router.push('/dashboard/storage')}
                className="w-full flex items-center justify-between gap-4 rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-left hover:bg-emerald-100 transition-colors"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <div className="w-10 h-10 rounded-xl bg-white flex items-center justify-center shrink-0">
                    <HardDrive className="w-5 h-5 text-emerald-600" />
                  </div>
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-emerald-900">Almacenamiento de la cuenta</p>
                    <p className="text-xs text-emerald-700 truncate">Abre el explorador de archivos, uso y limpieza de multimedia.</p>
                  </div>
                </div>
                <ExternalLink className="w-4 h-4 text-emerald-700 shrink-0" />
              </button>

              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">Nombre de la Cuenta</label>
                <input
                  type="text"
                  value={formData.accountName}
                  onChange={(e) => setFormData({ ...formData, accountName: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                />
              </div>

              <button
                onClick={handleSaveAccount}
                disabled={saving}
                className="inline-flex items-center gap-2 bg-emerald-600 text-white px-4 py-2 rounded-xl hover:bg-emerald-700 disabled:opacity-50 text-sm font-medium shadow-sm"
              >
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                Guardar Cambios
              </button>

              {/* Default Incoming Stage Selector */}
              <div className="pt-6 border-t border-slate-200">
                <div className="flex items-center gap-2 mb-1">
                  <Inbox className="w-4 h-4 text-emerald-600" />
                  <h3 className="text-sm font-medium text-slate-900">Etapa de Leads Entrantes</h3>
                </div>
                <p className="text-xs text-slate-500 mb-3">
                  Los nuevos leads se asignarán por defecto a esta etapa cuando no se elija otra manualmente.
                </p>
                <div className="flex items-center gap-3">
                  <select
                    value={incomingStageId}
                    onChange={(e) => handleSaveIncomingStage(e.target.value)}
                    disabled={savingStage}
                    className="flex-1 max-w-md px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 bg-white disabled:opacity-50"
                  >
                    <option value="">Automático (Leads Entrantes)</option>
                    {pipelineStages.map((st) => (
                      <option key={st.id} value={st.id}>
                        {st.pipeline_name} → {st.name}
                      </option>
                    ))}
                  </select>
                  {savingStage && <Loader2 className="w-4 h-4 animate-spin text-emerald-600" />}
                </div>
                {incomingStageId && (
                  <p className="text-[10px] text-emerald-600 mt-1.5">
                    ✓ Configurado: {pipelineStages.find(s => s.id === incomingStageId)?.name || 'Etapa seleccionada'}
                  </p>
                )}
              </div>

              {/* Pipeline & Stage Management */}
              <div className="pt-6 border-t border-slate-200">
                <div className="flex items-center justify-between mb-1">
                  <div className="flex items-center gap-2">
                    <GripVertical className="w-4 h-4 text-emerald-600" />
                    <h3 className="text-sm font-medium text-slate-900">Pipelines y Etapas</h3>
                  </div>
                  <button
                    onClick={() => { setShowNewPipelineForm(true); setNewPipelineName('') }}
                    className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-emerald-600 text-white text-xs rounded-lg hover:bg-emerald-700 transition font-medium shadow-sm"
                  >
                    <Plus className="w-3.5 h-3.5" /> Nuevo Pipeline
                  </button>
                </div>
                <p className="text-xs text-slate-500 mb-4">
                  Administra los pipelines y etapas para organizar tus leads. Puedes crear pipelines manualmente sin necesidad de sincronizar con Kommo.
                </p>

                {/* New Pipeline Form */}
                {showNewPipelineForm && (
                  <div className="mb-4 p-4 bg-emerald-50 rounded-xl border border-emerald-200">
                    <p className="text-xs font-medium text-emerald-800 mb-2">Crear nuevo pipeline</p>
                    <div className="flex items-center gap-2">
                      <input
                        type="text"
                        value={newPipelineName}
                        onChange={(e) => setNewPipelineName(e.target.value)}
                        placeholder="Nombre del pipeline"
                        className="flex-1 px-3 py-2 border border-slate-200 rounded-lg focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400"
                        onKeyDown={(e) => e.key === 'Enter' && handleCreatePipeline()}
                        autoFocus
                      />
                      <button
                        onClick={handleCreatePipeline}
                        disabled={!newPipelineName.trim() || savingPipeline}
                        className="px-4 py-2 bg-emerald-600 text-white text-sm rounded-lg hover:bg-emerald-700 disabled:opacity-50 transition font-medium"
                      >
                        {savingPipeline ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Crear'}
                      </button>
                      <button
                        onClick={() => setShowNewPipelineForm(false)}
                        className="p-2 text-slate-400 hover:text-slate-600 rounded-lg hover:bg-slate-100 transition"
                      >
                        <X className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                )}

                {/* Pipeline List */}
                {loadingPipelines ? (
                  <div className="flex items-center justify-center py-6">
                    <Loader2 className="w-5 h-5 animate-spin text-emerald-600" />
                  </div>
                ) : managedPipelines.length === 0 ? (
                  <div className="text-center py-8 bg-slate-50 rounded-xl border border-dashed border-slate-200">
                    <GripVertical className="w-8 h-8 text-slate-300 mx-auto mb-2" />
                    <p className="text-sm text-slate-500">No hay pipelines creados</p>
                    <p className="text-xs text-slate-400 mt-1">Crea tu primer pipeline para organizar tus leads en etapas</p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    {managedPipelines.map((pipeline) => {
                      const isExpPipeline = expandedPipelineId === pipeline.id
                      const stages = pipeline.stages || []
                      return (
                        <div key={pipeline.id} className="border border-slate-200 rounded-xl overflow-hidden bg-white">
                          {/* Pipeline header */}
                          <div
                            className="flex items-center justify-between px-4 py-3 cursor-pointer hover:bg-slate-50 transition"
                            onClick={() => setExpandedPipelineId(isExpPipeline ? null : pipeline.id)}
                          >
                            <div className="flex items-center gap-3">
                              {isExpPipeline ? <ChevronDown className="w-4 h-4 text-slate-400" /> : <ChevronRight className="w-4 h-4 text-slate-400" />}
                              {editingPipelineId === pipeline.id ? (
                                <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                                  <input
                                    type="text"
                                    value={editPipelineName}
                                    onChange={(e) => setEditPipelineName(e.target.value)}
                                    className="px-2 py-1 border border-slate-200 rounded-lg text-sm text-slate-900 focus:ring-2 focus:ring-emerald-500"
                                    onKeyDown={(e) => { if (e.key === 'Enter') handleUpdatePipeline(pipeline.id); if (e.key === 'Escape') setEditingPipelineId(null) }}
                                    autoFocus
                                  />
                                  <button onClick={() => handleUpdatePipeline(pipeline.id)} className="p-1 text-emerald-600 hover:text-emerald-700"><Save className="w-3.5 h-3.5" /></button>
                                  <button onClick={() => setEditingPipelineId(null)} className="p-1 text-slate-400 hover:text-slate-600"><X className="w-3.5 h-3.5" /></button>
                                </div>
                              ) : (
                                <div>
                                  <span className="text-sm font-medium text-slate-900">{pipeline.name}</span>
                                  <span className="ml-2 text-[10px] text-slate-400">{stages.length} etapa{stages.length !== 1 ? 's' : ''}</span>
                                </div>
                              )}
                            </div>
                            {editingPipelineId !== pipeline.id && (
                              <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                                <button
                                  onClick={() => { setEditingPipelineId(pipeline.id); setEditPipelineName(pipeline.name) }}
                                  className="p-1.5 text-slate-400 hover:text-emerald-600 hover:bg-emerald-50 rounded-lg transition"
                                  title="Renombrar"
                                >
                                  <Pencil className="w-3.5 h-3.5" />
                                </button>
                                <button
                                  onClick={() => handleDeletePipeline(pipeline.id, pipeline.name)}
                                  className="p-1.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-lg transition"
                                  title="Eliminar pipeline"
                                >
                                  <Trash2 className="w-3.5 h-3.5" />
                                </button>
                              </div>
                            )}
                          </div>

                          {/* Stages */}
                          {isExpPipeline && (
                            <div className="border-t border-slate-100 bg-slate-50/50">
                              {stages.length === 0 ? (
                                <div className="px-4 py-4 text-center">
                                  <p className="text-xs text-slate-400">Sin etapas — agrega una para empezar</p>
                                </div>
                              ) : (
                                <div className="divide-y divide-slate-100">
                                  {stages.sort((a, b) => a.position - b.position).map((stage, idx) => (
                                    <div key={stage.id} className="flex items-center justify-between px-4 py-2.5 hover:bg-white transition group">
                                      <div className="flex items-center gap-3">
                                        <span className="text-[10px] text-slate-400 w-4 text-center">{idx + 1}</span>
                                        <div className="w-3 h-3 rounded-full shrink-0" style={{ backgroundColor: stage.color }} />
                                        <span className="text-sm text-slate-700">{stage.name}</span>
                                      </div>
                                      <button
                                        onClick={() => handleDeleteStage(pipeline.id, stage.id, stage.name)}
                                        className="p-1 text-slate-300 hover:text-red-500 opacity-0 group-hover:opacity-100 transition-opacity"
                                        title="Eliminar etapa"
                                      >
                                        <Trash2 className="w-3.5 h-3.5" />
                                      </button>
                                    </div>
                                  ))}
                                </div>
                              )}

                              {/* Add Stage Form */}
                              {addingStageForPipeline === pipeline.id ? (
                                <div className="px-4 py-3 border-t border-slate-100 bg-emerald-50/50">
                                  <div className="flex items-center gap-2">
                                    <input
                                      type="color"
                                      value={newStageColor}
                                      onChange={(e) => setNewStageColor(e.target.value)}
                                      className="w-8 h-8 p-0.5 border border-slate-200 rounded-lg cursor-pointer"
                                      title="Color de la etapa"
                                    />
                                    <input
                                      type="text"
                                      value={newStageName}
                                      onChange={(e) => setNewStageName(e.target.value)}
                                      placeholder="Nombre de la etapa"
                                      className="flex-1 px-3 py-1.5 border border-slate-200 rounded-lg text-sm text-slate-900 placeholder:text-slate-400 focus:ring-2 focus:ring-emerald-500"
                                      onKeyDown={(e) => e.key === 'Enter' && handleCreateStage(pipeline.id)}
                                      autoFocus
                                    />
                                    <button
                                      onClick={() => handleCreateStage(pipeline.id)}
                                      disabled={!newStageName.trim() || savingNewStage}
                                      className="px-3 py-1.5 bg-emerald-600 text-white text-xs rounded-lg hover:bg-emerald-700 disabled:opacity-50 transition font-medium"
                                    >
                                      {savingNewStage ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : 'Agregar'}
                                    </button>
                                    <button
                                      onClick={() => { setAddingStageForPipeline(null); setNewStageName(''); setNewStageColor('#6366f1') }}
                                      className="p-1.5 text-slate-400 hover:text-slate-600 rounded-lg hover:bg-slate-100 transition"
                                    >
                                      <X className="w-3.5 h-3.5" />
                                    </button>
                                  </div>
                                </div>
                              ) : (
                                <div className="px-4 py-2 border-t border-slate-100">
                                  <button
                                    onClick={() => { setAddingStageForPipeline(pipeline.id); setNewStageName(''); setNewStageColor('#6366f1') }}
                                    className="inline-flex items-center gap-1.5 text-xs text-emerald-600 hover:text-emerald-700 font-medium transition"
                                  >
                                    <Plus className="w-3.5 h-3.5" /> Agregar etapa
                                  </button>
                                </div>
                              )}
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>

              <div className="pt-6 border-t border-slate-200">
                <h3 className="text-sm font-medium text-red-600 mb-2">Zona de Peligro</h3>
                <p className="text-xs text-slate-500 mb-4">
                  Estas acciones son irreversibles. Por favor, ten cuidado.
                </p>
                <div className="flex flex-wrap gap-3">
                  <button
                    onClick={async () => {
                      const confirmText = prompt('Para eliminar TODOS los leads de esta cuenta, escribe "ELIMINAR" (en mayúsculas):')
                      if (confirmText !== 'ELIMINAR') {
                        if (confirmText !== null) showMessage('error', 'Texto incorrecto. Escribe ELIMINAR para confirmar.')
                        return
                      }
                      const token = localStorage.getItem('token')
                      try {
                        const res = await fetch('/api/leads/batch', {
                          method: 'DELETE',
                          headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
                          body: JSON.stringify({ delete_all: true }),
                        })
                        const data = await res.json()
                        if (data.success) {
                          showMessage('success', 'Todos los leads han sido eliminados.')
                        } else {
                          showMessage('error', data.error || 'Error al eliminar leads')
                        }
                      } catch {
                        showMessage('error', 'Error de conexión')
                      }
                    }}
                    className="px-4 py-2 border border-red-200 text-red-600 rounded-xl hover:bg-red-50 text-sm transition"
                  >
                    Eliminar todos los leads
                  </button>
                  <button
                    onClick={async () => {
                      const confirmText = prompt('Para eliminar TODOS los contactos de esta cuenta, escribe "ELIMINAR" (en mayúsculas):')
                      if (confirmText !== 'ELIMINAR') {
                        if (confirmText !== null) showMessage('error', 'Texto incorrecto. Escribe ELIMINAR para confirmar.')
                        return
                      }
                      const token = localStorage.getItem('token')
                      try {
                        const res = await fetch('/api/contacts/batch', {
                          method: 'DELETE',
                          headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
                          body: JSON.stringify({ delete_all: true }),
                        })
                        const data = await res.json()
                        if (data.success) {
                          showMessage('success', 'Todos los contactos han sido eliminados.')
                        } else {
                          showMessage('error', data.error || 'Error al eliminar contactos')
                        }
                      } catch {
                        showMessage('error', 'Error de conexión')
                      }
                    }}
                    className="px-4 py-2 border border-red-200 text-red-600 rounded-xl hover:bg-red-50 text-sm transition"
                  >
                    Eliminar todos los contactos
                  </button>
                  <button
                    onClick={async () => {
                      const confirmText = prompt('Para eliminar TODOS los chats de esta cuenta, escribe "ELIMINAR" (en mayúsculas):')
                      if (confirmText !== 'ELIMINAR') {
                        if (confirmText !== null) showMessage('error', 'Texto incorrecto. Escribe ELIMINAR para confirmar.')
                        return
                      }
                      const token = localStorage.getItem('token')
                      try {
                        const res = await fetch('/api/chats/batch', {
                          method: 'DELETE',
                          headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
                          body: JSON.stringify({ delete_all: true }),
                        })
                        const data = await res.json()
                        if (data.success) {
                          showMessage('success', 'Todos los chats han sido eliminados.')
                        } else {
                          showMessage('error', data.error || 'Error al eliminar chats')
                        }
                      } catch {
                        showMessage('error', 'Error de conexión')
                      }
                    }}
                    className="px-4 py-2 border border-red-200 text-red-600 rounded-xl hover:bg-red-50 text-sm transition"
                  >
                    Eliminar todos los chats
                  </button>
                  <button
                    onClick={async () => {
                      const confirmText = prompt('Para eliminar TODAS las etiquetas de esta cuenta, escribe "ELIMINAR" (en mayúsculas):')
                      if (confirmText !== 'ELIMINAR') {
                        if (confirmText !== null) showMessage('error', 'Texto incorrecto. Escribe ELIMINAR para confirmar.')
                        return
                      }
                      const token = localStorage.getItem('token')
                      try {
                        const res = await fetch('/api/tags/batch', {
                          method: 'DELETE',
                          headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
                          body: JSON.stringify({ delete_all: true }),
                        })
                        const data = await res.json()
                        if (data.success) {
                          showMessage('success', 'Todas las etiquetas han sido eliminadas.')
                        } else {
                          showMessage('error', data.error || 'Error al eliminar etiquetas')
                        }
                      } catch {
                        showMessage('error', 'Error de conexión')
                      }
                    }}
                    className="px-4 py-2 border border-red-200 text-red-600 rounded-xl hover:bg-red-50 text-sm transition"
                  >
                    Eliminar todas las etiquetas
                  </button>
                  <button className="px-4 py-2 border border-red-200 text-red-600 rounded-xl hover:bg-red-50 text-sm transition">
                    Eliminar Cuenta
                  </button>
                </div>
              </div>
            </div>
          )}

          {activeTab === 'storage' && (
            <div className="space-y-5">
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-slate-900">Espacio de la cuenta</h3>
                  <p className="text-xs text-slate-500">Gestiona archivos multimedia almacenados desde chats de WhatsApp.</p>
                </div>
                <button
                  onClick={fetchStorage}
                  disabled={storageLoading}
                  className="inline-flex items-center justify-center gap-2 px-3 py-2 rounded-xl border border-slate-200 text-sm text-slate-600 hover:bg-slate-50 disabled:opacity-50"
                >
                  <RefreshCw className={`w-4 h-4 ${storageLoading ? 'animate-spin' : ''}`} />
                  Actualizar
                </button>
              </div>

              <div className="bg-slate-900 rounded-2xl p-5 text-white">
                <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-6">
                  <div className="flex items-center gap-4">
                    <div className="w-16 h-16 rounded-2xl bg-emerald-500/15 flex items-center justify-center">
                      <HardDrive className="w-8 h-8 text-emerald-300" />
                    </div>
                    <div>
                      <p className="text-xs uppercase tracking-wide text-slate-400">Usado</p>
                      <div className="text-3xl font-semibold tabular-nums">{formatBytes(storageUsage?.used_bytes)}</div>
                      <p className="text-xs text-slate-400">
                        {storageUsage?.limit_bytes ? `${formatBytes(storageUsage.available_bytes)} disponibles de ${formatBytes(storageUsage.limit_bytes)}` : 'Sin límite configurado'}
                      </p>
                    </div>
                  </div>
                  <div className="md:w-72">
                    <div className="flex justify-between text-xs text-slate-400 mb-2">
                      <span>{storageUsage?.object_count || 0} archivos</span>
                      <span>{storageUsage?.limit_bytes ? `${Math.round(storageUsage.percent_used)}%` : 'Ilimitado'}</span>
                    </div>
                    <div className="h-3 rounded-full bg-slate-700 overflow-hidden">
                      <div
                        className={`h-full rounded-full ${(storageUsage?.percent_used || 0) >= 90 ? 'bg-red-500' : (storageUsage?.percent_used || 0) >= 75 ? 'bg-amber-400' : 'bg-emerald-400'}`}
                        style={{ width: `${storageUsage?.limit_bytes ? Math.min(100, storageUsage.percent_used) : 0}%` }}
                      />
                    </div>
                  </div>
                </div>
              </div>

              <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
                {[
                  { key: 'image', label: 'Imágenes', icon: Image },
                  { key: 'video', label: 'Videos', icon: Video },
                  { key: 'audio', label: 'Audios', icon: Volume2 },
                  { key: 'document', label: 'Documentos', icon: File },
                ].map(item => {
                  const Icon = item.icon
                  return (
                    <button
                      key={item.key}
                      onClick={() => setStorageType(storageType === item.key ? '' : item.key)}
                      className={`text-left p-4 rounded-xl border transition ${storageType === item.key ? 'border-emerald-300 bg-emerald-50' : 'border-slate-200 hover:bg-slate-50'}`}
                    >
                      <Icon className="w-5 h-5 text-emerald-600 mb-2" />
                      <p className="text-sm font-medium text-slate-900">{item.label}</p>
                      <p className="text-xs text-slate-500">{formatBytes(storageUsage?.by_type?.[item.key] || 0)}</p>
                    </button>
                  )
                })}
              </div>

              <div className="border border-slate-200 rounded-xl overflow-hidden">
                <div className="px-4 py-3 border-b border-slate-200 bg-slate-50 flex items-center justify-between gap-3">
                  <div>
                    <h4 className="text-sm font-medium text-slate-900">Multimedia de chats</h4>
                    <p className="text-xs text-slate-500">Al eliminar un archivo, el mensaje queda visible pero sin multimedia.</p>
                  </div>
                  {storageType && (
                    <button onClick={() => setStorageType('')} className="text-xs text-emerald-600 hover:text-emerald-700 whitespace-nowrap">Quitar filtro</button>
                  )}
                </div>
                {storageLoading ? (
                  <div className="p-4 space-y-2">
                    {[...Array(5)].map((_, i) => <div key={i} className="h-12 bg-slate-100 rounded-lg animate-pulse" />)}
                  </div>
                ) : storageFiles.length === 0 ? (
                  <div className="py-12 text-center">
                    <HardDrive className="w-10 h-10 text-slate-300 mx-auto mb-2" />
                    <p className="text-sm font-medium text-slate-600">No hay archivos para mostrar</p>
                    <p className="text-xs text-slate-400">Los archivos de chats aparecerán aquí cuando existan.</p>
                  </div>
                ) : (
                  <div className="divide-y divide-slate-100">
                    {storageFiles.map(file => (
                      <div key={file.object_key} className="px-4 py-3 flex items-center gap-3 hover:bg-slate-50">
                        <div className="w-10 h-10 rounded-xl bg-slate-100 flex items-center justify-center shrink-0">
                          {file.media_type === 'image' ? <Image className="w-5 h-5 text-emerald-600" /> :
                           file.media_type === 'video' ? <Video className="w-5 h-5 text-emerald-600" /> :
                           file.media_type === 'audio' ? <Volume2 className="w-5 h-5 text-emerald-600" /> :
                           <File className="w-5 h-5 text-emerald-600" />}
                        </div>
                        <div className="min-w-0 flex-1">
                          <p className="text-sm font-medium text-slate-900 truncate">{file.filename || 'Archivo'}</p>
                          <p className="text-xs text-slate-500">{formatBytes(file.size_bytes)} · {file.references_count} referencia{file.references_count === 1 ? '' : 's'}</p>
                        </div>
                        {storageUsage?.can_manage && (
                          <button
                            onClick={() => handleDeleteStorageFile(file)}
                            disabled={storageDeleting === file.object_key}
                            className="p-2 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-lg disabled:opacity-50"
                            title="Eliminar archivo"
                          >
                            {storageDeleting === file.object_key ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Integrations Tab */}
          {activeTab === 'integrations' && (
            <div className="space-y-6">

              {/* === INTEGRATION LIST VIEW === */}
              {integrationView === 'list' && (
                <>
                  <div>
                    <h3 className="text-sm font-medium text-slate-900">Integraciones</h3>
                    <p className="text-xs text-slate-500 mt-1">Conecta servicios externos para sincronizar datos automáticamente.</p>
                  </div>

                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    {/* Google Contacts Card */}
                    <button
                      onClick={() => setIntegrationView('google')}
                      className="text-left border border-slate-200 rounded-xl p-5 hover:border-blue-300 hover:shadow-md transition-all group bg-white"
                    >
                      <div className="flex items-center gap-3 mb-3">
                        <div className={`w-10 h-10 rounded-xl flex items-center justify-center ${googleStatus?.connected ? 'bg-blue-100' : 'bg-slate-100'}`}>
                          <Users className={`w-5 h-5 ${googleStatus?.connected ? 'text-blue-600' : 'text-slate-400'}`} />
                        </div>
                        <div className="flex-1 min-w-0">
                          <p className="text-sm font-semibold text-slate-900 group-hover:text-blue-700 transition">Google Contacts</p>
                          <p className="text-[10px] text-slate-500 truncate">Sincroniza contactos a Android</p>
                        </div>
                        <div className={`w-2.5 h-2.5 rounded-full ${googleStatus?.connected ? 'bg-blue-500 animate-pulse' : 'bg-slate-300'}`} />
                      </div>
                      <div className="flex items-center gap-2 text-[10px]">
                        <span className={`px-2 py-0.5 rounded-full ${googleStatus?.connected ? 'bg-blue-50 text-blue-700' : 'bg-slate-100 text-slate-500'}`}>
                          {googleStatus?.connected ? 'Conectado' : googleStatus?.configured ? 'No conectado' : 'No configurado'}
                        </span>
                        {googleStatus?.connected && (
                          <span className="px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-700">
                            {googleStatus.sync_count} contacto{googleStatus.sync_count !== 1 ? 's' : ''}
                          </span>
                        )}
                      </div>
                    </button>
                  </div>
                </>
              )}

              {/* === GOOGLE CONTACTS DETAIL VIEW === */}
              {integrationView === 'google' && (
                <>
                  <button
                    onClick={() => setIntegrationView('list')}
                    className="inline-flex items-center gap-1.5 text-xs text-slate-500 hover:text-slate-700 transition -mb-2"
                  >
                    <ArrowLeft className="w-3.5 h-3.5" /> Integraciones
                  </button>

                  <div>
                    <h3 className="text-sm font-medium text-slate-900">Google Contacts</h3>
                    <p className="text-xs text-slate-500 mt-1">
                      Sincroniza contactos de Clarin a Google Contacts para que WhatsApp muestre los nombres en tu teléfono Android.
                    </p>
                  </div>

                  {/* Google Connection Status */}
                  <div className="bg-slate-50 rounded-xl p-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        {googleStatus?.connected ? (
                          <CheckCircle2 className="w-5 h-5 text-emerald-600" />
                        ) : googleStatus?.configured ? (
                          <Link2 className="w-5 h-5 text-slate-400" />
                        ) : (
                          <XCircle className="w-5 h-5 text-red-400" />
                        )}
                        <div>
                          <p className="text-sm font-medium text-slate-900">
                            {googleStatus?.connected
                              ? 'Conectado'
                              : googleStatus?.configured
                              ? 'No conectado'
                              : 'No configurado (faltan credenciales)'}
                          </p>
                          {googleStatus?.email && (
                            <p className="text-xs text-slate-500">{googleStatus.email}</p>
                          )}
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        <button
                          onClick={fetchGoogleStatus}
                          className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-white rounded-lg transition"
                          title="Verificar conexión"
                        >
                          <RefreshCw className="w-4 h-4" />
                        </button>
                        {googleStatus?.connected ? (
                          <button
                            onClick={handleGoogleDisconnect}
                            disabled={googleDisconnecting}
                            className="inline-flex items-center gap-1.5 bg-red-50 text-red-600 hover:bg-red-100 px-3 py-1.5 rounded-lg transition text-xs font-medium"
                          >
                            {googleDisconnecting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <XCircle className="w-3.5 h-3.5" />}
                            Desconectar
                          </button>
                        ) : googleStatus?.configured ? (
                          <button
                            onClick={handleGoogleConnect}
                            disabled={googleLoading}
                            className="inline-flex items-center gap-1.5 bg-emerald-600 text-white hover:bg-emerald-700 px-3 py-1.5 rounded-lg transition text-xs font-medium shadow-sm"
                          >
                            {googleLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Link2 className="w-3.5 h-3.5" />}
                            Conectar Google
                          </button>
                        ) : null}
                      </div>
                    </div>

                    {/* Sync Quota */}
                    {googleStatus?.connected && (
                      <div className="mt-3 pt-3 border-t border-slate-200">
                        {googleStatus.token_valid === false && (
                          <div className="mb-3 p-2.5 bg-amber-50 border border-amber-200 rounded-lg">
                            <p className="text-xs text-amber-800 font-medium">⚠️ El token de Google ha expirado</p>
                            <p className="text-[10px] text-amber-600 mt-0.5">Desconecta y vuelve a conectar Google para renovar la autenticación.</p>
                          </div>
                        )}
                        <div className="flex items-center justify-between mb-1.5">
                          <span className="text-xs text-slate-500">Contactos sincronizados</span>
                          <span className="text-xs font-medium text-slate-700">
                            {googleStatus.sync_count.toLocaleString()} / {googleStatus.sync_limit.toLocaleString()}
                          </span>
                        </div>
                        <div className="w-full bg-slate-200 rounded-full h-2">
                          <div
                            className="bg-emerald-500 h-2 rounded-full transition-all"
                            style={{ width: `${Math.min(100, (googleStatus.sync_count / googleStatus.sync_limit) * 100)}%` }}
                          />
                        </div>
                        <p className="text-[10px] text-slate-400 mt-1">
                          Sincroniza contactos individuales desde la página de Contactos o Leads.
                        </p>
                      </div>
                    )}
                  </div>

                  {/* Google info */}
                  <div className="text-xs text-slate-500 bg-blue-50 border border-blue-100 rounded-xl p-3">
                    <strong>¿Cómo funciona?</strong> Al sincronizar un contacto, se crea en Google Contacts con nombre, teléfono, email y empresa. En un teléfono Android con esa cuenta Google, WhatsApp mostrará los nombres actualizados.
                  </div>
                </>
              )}

            </div>
          )}

          {/* Devices Tab */}
          {activeTab === 'devices' && (
            <div className="space-y-5">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-medium text-slate-900">Dispositivos WhatsApp</h3>
                  <p className="text-xs text-slate-500 mt-0.5">Gestiona tus conexiones de WhatsApp</p>
                </div>
                <button onClick={() => setDevShowCreate(true)} className="flex items-center gap-1.5 bg-emerald-600 hover:bg-emerald-700 text-white px-3 py-2 rounded-xl transition text-xs font-medium shadow-sm">
                  <Plus className="w-3.5 h-3.5" /> Agregar
                </button>
              </div>

              {devLoading ? (
                <div className="flex items-center justify-center py-12"><div className="animate-spin rounded-full h-6 w-6 border-2 border-emerald-200 border-t-emerald-600" /></div>
              ) : devDevices.length === 0 ? (
                <div className="border-2 border-dashed border-slate-200 rounded-xl p-8 text-center">
                  <Smartphone className="w-8 h-8 text-slate-300 mx-auto mb-2" />
                  <p className="text-sm font-medium text-slate-700 mb-1">No hay dispositivos</p>
                  <p className="text-xs text-slate-500 mb-3">Agrega tu primer dispositivo WhatsApp</p>
                  <button onClick={() => setDevShowCreate(true)} className="inline-flex items-center gap-1.5 bg-emerald-600 hover:bg-emerald-700 text-white px-3 py-2 rounded-xl transition text-xs font-medium shadow-sm">
                    <Plus className="w-3.5 h-3.5" /> Agregar Dispositivo
                  </button>
                </div>
              ) : (
                <div className="border border-slate-200 rounded-xl divide-y divide-slate-100">
                  {devDevices.map((device) => (
                    <div key={device.id} className="p-3.5 flex items-center justify-between hover:bg-slate-50/50 transition">
                      <div className="flex items-center gap-3">
                        <div className={`w-9 h-9 rounded-lg flex items-center justify-center ${isApiDevice(device) ? 'bg-sky-50' : 'bg-slate-100'}`}>
                          {isApiDevice(device) ? <Globe className="w-4.5 h-4.5 text-sky-600" /> : <Smartphone className="w-4.5 h-4.5 text-slate-500" />}
                        </div>
                        <div className="min-w-0">
                          <div className="flex items-center gap-2 flex-wrap">
                            <p className="text-sm font-medium text-slate-900 truncate">{device.name || 'Dispositivo'}</p>
                            {getDevProviderBadge(device)}
                          </div>
                          <p className="text-xs text-slate-500 truncate">{device.api_display_phone || device.phone || device.phone_number_id || device.jid || 'Sin numero'}</p>
                          {getApiGuardBadges(device)}
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        {!isApiDevice(device) && getDevStatusBadge(device.status)}
                        <div className="flex items-center gap-1.5">
                          <span className="text-[10px] text-slate-400 hidden sm:inline">{device.receive_messages ? 'Recibe' : 'No recibe'}</span>
                          <button
                            onClick={() => handleToggleReceiveMessages(device)}
                            className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${device.receive_messages ? 'bg-emerald-500' : 'bg-slate-300'}`}
                            title={device.receive_messages ? 'Recepción activa — clic para desactivar' : 'Recepción desactivada — clic para activar'}
                          >
                            <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform ${device.receive_messages ? 'translate-x-[18px]' : 'translate-x-[3px]'}`} />
                          </button>
                        </div>
                        <button onClick={() => openDevEdit(device)} className="p-1.5 text-slate-400 hover:text-blue-600 hover:bg-blue-50 rounded-lg transition" title="Editar"><Edit className="w-3.5 h-3.5" /></button>
                        {isApiDevice(device) ? null : device.status === 'connected' ? (
                          <>
                            <button onClick={() => handleDevReset(device.id)} className="p-1.5 text-slate-400 hover:text-purple-600 hover:bg-purple-50 rounded-lg transition" title="Re-vincular (sincronizar historial completo)"><RefreshCw className="w-3.5 h-3.5" /></button>
                            <button onClick={() => handleDevDisconnect(device.id)} className="p-1.5 text-slate-400 hover:text-orange-600 hover:bg-orange-50 rounded-lg transition" title="Desconectar"><Power className="w-3.5 h-3.5" /></button>
                          </>
                        ) : device.status === 'connecting' ? (
                          <button onClick={() => setDevSelected(device)} className="p-1.5 text-emerald-600 hover:bg-emerald-50 rounded-lg transition" title="Ver QR"><QrCode className="w-3.5 h-3.5" /></button>
                        ) : (
                          <button onClick={() => { handleDevConnect(device.id); setDevSelected(device) }} className="p-1.5 text-slate-400 hover:text-emerald-600 hover:bg-emerald-50 rounded-lg transition" title="Conectar"><Power className="w-3.5 h-3.5" /></button>
                        )}
                        <button onClick={() => handleDevDelete(device.id)} className="p-1.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-lg transition" title="Eliminar"><Trash2 className="w-3.5 h-3.5" /></button>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {/* Create device modal */}
              {devShowCreate && (
                <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-50">
                  <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-lg mx-4 border border-slate-100">
                    <h2 className="text-lg font-semibold text-slate-900 mb-4">Nuevo Canal WhatsApp</h2>
                    <div className="grid grid-cols-2 gap-2 mb-4">
                      <button
                        type="button"
                        onClick={() => setDevCreateProvider('whatsapp_web')}
                        className={`flex items-center gap-2 px-3 py-2 rounded-xl border text-sm transition ${devCreateProvider === 'whatsapp_web' ? 'border-emerald-500 bg-emerald-50 text-emerald-700' : 'border-slate-200 text-slate-600 hover:bg-slate-50'}`}
                      >
                        <QrCode className="w-4 h-4" /> QR WhatsApp Web
                      </button>
                      <button
                        type="button"
                        onClick={() => setDevCreateProvider('whatsapp_cloud_api')}
                        className={`flex items-center gap-2 px-3 py-2 rounded-xl border text-sm transition ${devCreateProvider === 'whatsapp_cloud_api' ? 'border-sky-500 bg-sky-50 text-sky-700' : 'border-slate-200 text-slate-600 hover:bg-slate-50'}`}
                      >
                        <Globe className="w-4 h-4" /> API Oficial
                      </button>
                    </div>

                    <div className="space-y-3">
                      <div>
                        <label className="block text-xs font-medium text-slate-600 mb-1">Nombre</label>
                        <input type="text" value={devNewName} onChange={(e) => setDevNewName(e.target.value)} placeholder="Nombre del canal" className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400" autoFocus onKeyDown={(e) => { if (e.key === 'Enter' && devCreateProvider === 'whatsapp_web') handleDevCreate() }} />
                      </div>

                      {devCreateProvider === 'whatsapp_cloud_api' && (
                        <>
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                            <div>
                              <label className="block text-xs font-medium text-slate-600 mb-1">Telefono visible</label>
                              <input type="text" value={devNewApiDisplayPhone} onChange={(e) => setDevNewApiDisplayPhone(e.target.value)} placeholder="+51..." className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-sky-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400" />
                            </div>
                            <div>
                              <label className="block text-xs font-medium text-slate-600 mb-1">Phone Number ID</label>
                              <input type="text" value={devNewApiPhoneNumberID} onChange={(e) => setDevNewApiPhoneNumberID(e.target.value)} placeholder="Meta phone ID" className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-sky-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400" />
                            </div>
                          </div>
                          <div>
                            <label className="block text-xs font-medium text-slate-600 mb-1">WABA ID</label>
                            <input type="text" value={devNewApiWabaID} onChange={(e) => setDevNewApiWabaID(e.target.value)} placeholder="WhatsApp Business Account ID" className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-sky-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400" />
                          </div>
                          <div className="flex items-start gap-2 rounded-xl border border-amber-100 bg-amber-50 px-3 py-2 text-xs text-amber-700">
                            <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
                            <span>El canal queda guardado para configuracion. Envio API y plantillas permanecen bloqueados.</span>
                          </div>
                        </>
                      )}
                    </div>

                    <div className="flex gap-3 mt-5">
                      <button onClick={() => { setDevShowCreate(false); resetDevCreateForm() }} className="flex-1 px-4 py-2 border border-slate-200 rounded-xl hover:bg-slate-50 text-sm text-slate-600">Cancelar</button>
                      <button onClick={handleDevCreate} disabled={devCreating || !devNewName.trim()} className="flex-1 px-4 py-2 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 disabled:opacity-50 text-sm font-medium shadow-sm">{devCreating ? 'Creando...' : devCreateProvider === 'whatsapp_web' ? 'Crear y Conectar' : 'Crear Canal'}</button>
                    </div>
                  </div>
                </div>
              )}

              {/* QR modal */}
              {devSelected && devSelected.status === 'connecting' && devSelected.qr_code && (
                <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-50">
                  <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-md mx-4 text-center border border-slate-100">
                    <div className="flex items-center justify-center gap-2 mb-3"><QrCode className="w-5 h-5 text-emerald-600" /><h2 className="text-lg font-semibold text-slate-900">Escanea el código QR</h2></div>
                    <p className="text-sm text-slate-500 mb-4">Abre WhatsApp en tu teléfono y escanea este código</p>
                    <div className="bg-white p-4 rounded-xl border border-slate-100 inline-block mb-4"><img src={devSelected.qr_code} alt="QR Code" className="w-56 h-56" /></div>
                    <div className="flex items-center justify-center gap-2 text-xs text-slate-500 mb-4"><RefreshCw className="w-3.5 h-3.5 animate-spin" /> Esperando escaneo...</div>
                    <button onClick={() => setDevSelected(null)} className="px-4 py-2 border border-slate-200 rounded-xl hover:bg-slate-50 text-sm text-slate-600">Cerrar</button>
                  </div>
                </div>
              )}

              {/* Edit device modal */}
              {devEditing && (
                <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-50">
                  <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-md mx-4 border border-slate-100">
                    <div className="flex items-center justify-between mb-4">
                      <h2 className="text-lg font-semibold text-slate-900">Editar Canal</h2>
                      {getDevProviderBadge(devEditing)}
                    </div>
                    <div className="space-y-4">
                      <div><label className="block text-xs font-medium text-slate-600 mb-1">Nombre</label><input type="text" value={devEditName} onChange={(e) => setDevEditName(e.target.value)} className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900" autoFocus onKeyDown={(e) => { if (e.key === 'Enter') handleDevUpdate() }} /></div>
                      {isApiDevice(devEditing) ? (
                        <>
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                            <div><label className="block text-xs font-medium text-slate-600 mb-1">Telefono visible</label><input type="text" value={devEditApiDisplayPhone} onChange={(e) => setDevEditApiDisplayPhone(e.target.value)} className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-sky-500 focus:border-transparent text-sm text-slate-900" /></div>
                            <div><label className="block text-xs font-medium text-slate-600 mb-1">Phone Number ID</label><input type="text" value={devEditApiPhoneNumberID} onChange={(e) => setDevEditApiPhoneNumberID(e.target.value)} className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-sky-500 focus:border-transparent text-sm text-slate-900" /></div>
                          </div>
                          <div><label className="block text-xs font-medium text-slate-600 mb-1">WABA ID</label><input type="text" value={devEditApiWabaID} onChange={(e) => setDevEditApiWabaID(e.target.value)} className="w-full px-3 py-2.5 border border-slate-200 rounded-xl focus:ring-2 focus:ring-sky-500 focus:border-transparent text-sm text-slate-900" /></div>
                          <div className="grid grid-cols-2 gap-2 text-xs">
                            <div className="rounded-xl bg-slate-50 border border-slate-100 px-3 py-2">
                              <p className="text-slate-400">Webhook</p>
                              <p className="font-medium text-slate-700">{devEditing.api_webhook_status || 'not_configured'}</p>
                            </div>
                            <div className="rounded-xl bg-slate-50 border border-slate-100 px-3 py-2">
                              <p className="text-slate-400">Facturacion</p>
                              <p className="font-medium text-slate-700">{devEditing.api_billing_status || 'not_configured'}</p>
                            </div>
                          </div>
                          {getApiGuardBadges(devEditing)}
                        </>
                      ) : (
                        <>
                          <div><label className="block text-xs font-medium text-slate-600 mb-1">Teléfono</label><input type="text" value={devEditing.phone || 'Sin número'} disabled className="w-full px-3 py-2.5 border border-slate-100 rounded-xl bg-slate-50 text-sm text-slate-400" /><p className="text-[10px] text-slate-400 mt-1">Se asigna automáticamente al conectar</p></div>
                          <div><label className="block text-xs font-medium text-slate-600 mb-1">Estado</label><div className="px-3 py-2">{getDevStatusBadge(devEditing.status)}</div></div>
                        </>
                      )}
                      <div className="flex items-center justify-between">
                        <div>
                          <label className="block text-xs font-medium text-slate-600">Recibir mensajes</label>
                          <p className="text-[10px] text-slate-400 mt-0.5">Si se desactiva, los mensajes entrantes se ignoran silenciosamente</p>
                        </div>
                        <button
                          onClick={() => { handleToggleReceiveMessages(devEditing); setDevEditing({ ...devEditing, receive_messages: !devEditing.receive_messages }) }}
                          className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${devEditing.receive_messages ? 'bg-emerald-500' : 'bg-slate-300'}`}
                        >
                          <span className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform ${devEditing.receive_messages ? 'translate-x-[18px]' : 'translate-x-[3px]'}`} />
                        </button>
                      </div>
                    </div>
                    <div className="flex gap-3 mt-5">
                      <button onClick={() => setDevEditing(null)} className="flex-1 px-4 py-2 border border-slate-200 rounded-xl hover:bg-slate-50 text-sm text-slate-600">Cancelar</button>
                      <button onClick={handleDevUpdate} disabled={devSaving || !devEditName.trim()} className="flex-1 px-4 py-2 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 disabled:opacity-50 text-sm font-medium shadow-sm">{devSaving ? 'Guardando...' : 'Guardar'}</button>
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}

          {activeTab === 'whatsapp-api' && <WhatsAppAPISettingsPanel />}

          {/* Quick Replies Tab */}
          {activeTab === 'quick-replies' && (
            <div className="space-y-6">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-medium text-slate-900">Respuestas Rápidas</h3>
                  <p className="text-xs text-slate-500 mt-1">
                    Crea respuestas predefinidas. Escribe <code className="px-1 py-0.5 bg-slate-100 rounded text-emerald-700 text-[10px]">/</code> en el chat para buscarlas.
                  </p>
                </div>
                <button
                  onClick={() => setEditingQR({ shortcut: '', title: '', body: '', media_url: '', media_type: '', media_filename: '', attachments: [] })}
                  className="inline-flex items-center gap-1.5 bg-emerald-600 text-white px-3 py-1.5 rounded-xl hover:bg-emerald-700 text-xs font-medium shadow-sm"
                >
                  <Plus className="w-3.5 h-3.5" />
                  Nueva
                </button>
              </div>

              {/* Edit/Create form */}
              {editingQR && (
                <div className="bg-emerald-50/50 border border-emerald-100 rounded-xl p-4 space-y-3">
                  <div className="flex items-center justify-between">
                    <h4 className="text-xs font-medium text-slate-900">
                      {editingQR.id ? 'Editar respuesta rápida' : 'Nueva respuesta rápida'}
                    </h4>
                    <button onClick={() => setEditingQR(null)} className="p-1 text-slate-400 hover:text-slate-600">
                      <X className="w-4 h-4" />
                    </button>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <div>
                      <label className="block text-xs font-medium text-slate-600 mb-1">Atajo</label>
                      <div className="relative">
                        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-emerald-600 font-mono text-xs">/</span>
                        <input
                          type="text"
                          value={editingQR.shortcut}
                          onChange={e => setEditingQR({ ...editingQR, shortcut: e.target.value.replace(/\s/g, '').toLowerCase() })}
                          placeholder="saludo"
                          className="w-full pl-7 pr-3 py-2 border border-slate-200 rounded-xl text-slate-900 placeholder:text-slate-400 focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm"
                        />
                      </div>
                    </div>
                    <div>
                      <label className="block text-xs font-medium text-slate-600 mb-1">Título (opcional)</label>
                      <input
                        type="text"
                        value={editingQR.title}
                        onChange={e => setEditingQR({ ...editingQR, title: e.target.value })}
                        placeholder="Saludo inicial"
                        className="w-full px-3 py-2 border border-slate-200 rounded-xl text-slate-900 placeholder:text-slate-400 focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm"
                      />
                    </div>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Mensaje</label>
                    <textarea
                      value={editingQR.body}
                      onChange={e => setEditingQR({ ...editingQR, body: e.target.value })}
                      placeholder="¡Hola! Gracias por contactarnos. ¿En qué podemos ayudarte?"
                      rows={3}
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl text-slate-900 placeholder:text-slate-400 focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm resize-none"
                    />
                  </div>

                  {/* Multi-attachment section */}
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">
                      Adjuntos ({editingQR.attachments.length}/5)
                    </label>
                    {editingQR.attachments.length > 0 && (
                      <div className="space-y-2 mb-2">
                        {editingQR.attachments.map((att, idx) => (
                          <div key={idx} className="bg-white border border-slate-200 rounded-xl p-2.5 space-y-2">
                            <div className="flex items-center gap-3">
                              <div className="w-8 h-8 bg-emerald-50 rounded-lg flex items-center justify-center shrink-0">
                                {att.media_type === 'image' ? <Image className="w-4 h-4 text-emerald-600" /> :
                                 att.media_type === 'video' ? <Video className="w-4 h-4 text-emerald-600" /> :
                                 <File className="w-4 h-4 text-emerald-600" />}
                              </div>
                              {att.media_type === 'image' && (
                                <img src={att.media_url} alt="" className="w-10 h-10 object-cover rounded-lg shrink-0" />
                              )}
                              <div className="flex-1 min-w-0">
                                <p className="text-xs font-medium text-slate-900 truncate">{att.media_filename || 'Archivo'}</p>
                                <p className="text-[10px] text-slate-400">{att.media_type}</p>
                              </div>
                              <div className="flex items-center gap-1 shrink-0">
                                {idx > 0 && (
                                  <button onClick={() => { const atts = [...editingQR.attachments]; [atts[idx-1], atts[idx]] = [atts[idx], atts[idx-1]]; setEditingQR({ ...editingQR, attachments: atts.map((a, i) => ({ ...a, position: i })) }) }} className="p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-50 rounded-lg transition" title="Subir"><ChevronDown className="w-3.5 h-3.5 rotate-180" /></button>
                                )}
                                {idx < editingQR.attachments.length - 1 && (
                                  <button onClick={() => { const atts = [...editingQR.attachments]; [atts[idx], atts[idx+1]] = [atts[idx+1], atts[idx]]; setEditingQR({ ...editingQR, attachments: atts.map((a, i) => ({ ...a, position: i })) }) }} className="p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-50 rounded-lg transition" title="Bajar"><ChevronDown className="w-3.5 h-3.5" /></button>
                                )}
                                <button onClick={() => { const atts = editingQR.attachments.filter((_, i) => i !== idx).map((a, i) => ({ ...a, position: i })); setEditingQR({ ...editingQR, attachments: atts }) }} className="p-1 text-red-400 hover:text-red-600 hover:bg-red-50 rounded-lg transition" title="Eliminar"><X className="w-3.5 h-3.5" /></button>
                              </div>
                            </div>
                            {(att.media_type === 'image' || att.media_type === 'video') && (
                              <input
                                type="text"
                                value={att.caption}
                                onChange={e => { const atts = [...editingQR.attachments]; atts[idx] = { ...atts[idx], caption: e.target.value }; setEditingQR({ ...editingQR, attachments: atts }) }}
                                placeholder="Pie de foto (opcional)"
                                className="w-full px-2.5 py-1.5 border border-slate-200 rounded-lg text-xs text-slate-900 placeholder:text-slate-400 focus:ring-2 focus:ring-emerald-500 focus:border-transparent"
                              />
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                    {editingQR.attachments.length < 5 && (
                      <label className="flex items-center gap-2 px-3 py-2 border border-dashed border-slate-300 rounded-xl text-slate-500 hover:bg-white hover:border-emerald-300 hover:text-emerald-600 transition cursor-pointer text-xs">
                        {uploadingQRMedia ? (
                          <Loader2 className="w-3.5 h-3.5 animate-spin" />
                        ) : (
                          <Paperclip className="w-3.5 h-3.5" />
                        )}
                        {uploadingQRMedia ? 'Subiendo...' : `Adjuntar imagen, video o documento${editingQR.attachments.length > 0 ? ` (${5 - editingQR.attachments.length} más)` : ''}`}
                        <input
                          type="file"
                          multiple
                          accept="image/*,video/*,audio/*,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt"
                          onChange={handleQRMediaUpload}
                          disabled={uploadingQRMedia}
                          className="hidden"
                        />
                      </label>
                    )}
                  </div>

                  {/* Preview */}
                  {(editingQR.body.trim() || editingQR.attachments.length > 0) && (
                    <div>
                      <label className="block text-xs font-medium text-slate-600 mb-1">Vista previa</label>
                      <div className="bg-[#e5ddd5] rounded-xl p-3 space-y-1 flex flex-col items-end">
                        {editingQR.attachments.map((att, idx) => (
                          <div key={idx} className="max-w-[280px] w-full">
                            {att.media_type === 'image' ? (
                              <div className="bg-white rounded-lg overflow-hidden shadow-sm">
                                <img src={att.media_url} alt="" className="w-full max-h-[200px] object-cover" />
                                {att.caption && <div className="px-2 py-1.5"><p className="text-xs text-slate-800 whitespace-pre-wrap">{att.caption}</p></div>}
                              </div>
                            ) : att.media_type === 'video' ? (
                              <div className="bg-white rounded-lg overflow-hidden shadow-sm">
                                <video src={att.media_url} className="w-full max-h-[200px] object-cover" />
                                {att.caption && <div className="px-2 py-1.5"><p className="text-xs text-slate-800 whitespace-pre-wrap">{att.caption}</p></div>}
                              </div>
                            ) : (
                              <div className="bg-white rounded-lg p-2 shadow-sm">
                                <div className="flex items-center gap-2 bg-emerald-50 rounded-lg p-2">
                                  <File className="w-5 h-5 text-emerald-600 shrink-0" />
                                  <div className="min-w-0">
                                    <p className="text-xs font-medium text-slate-900 truncate">{att.media_filename}</p>
                                    <p className="text-[10px] text-slate-400">{att.media_type}</p>
                                  </div>
                                </div>
                              </div>
                            )}
                          </div>
                        ))}
                        {editingQR.body.trim() && (
                          <div className="max-w-[280px]">
                            <div className="bg-[#dcf8c6] rounded-lg px-2.5 py-1.5 shadow-sm">
                              <p className="text-xs text-slate-800 whitespace-pre-wrap">{editingQR.body}</p>
                            </div>
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  <div className="flex justify-end gap-2">
                    <button
                      onClick={() => setEditingQR(null)}
                      className="px-3 py-1.5 text-slate-600 hover:bg-slate-100 rounded-xl text-xs"
                    >
                      Cancelar
                    </button>
                    <button
                      onClick={handleSaveQuickReply}
                      disabled={savingQR || !editingQR.shortcut.trim() || (!editingQR.body.trim() && editingQR.attachments.length === 0)}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 disabled:opacity-50 text-xs font-medium shadow-sm"
                    >
                      {savingQR ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
                      Guardar
                    </button>
                  </div>
                </div>
              )}

              {/* List */}
              {quickReplies.length === 0 ? (
                <div className="text-center py-8 text-slate-500">
                  <Zap className="w-8 h-8 mx-auto mb-2 text-slate-300" />
                  <p className="text-sm">No hay respuestas rápidas aún</p>
                  <p className="text-xs text-slate-400 mt-1">Crea una para responder más rápido en los chats</p>
                </div>
              ) : (
                <div className="space-y-2">
                  {quickReplies.map(qr => (
                    <div key={qr.id} className="flex items-start gap-3 p-3 bg-slate-50 rounded-xl group">
                      <span className="inline-block px-2 py-0.5 bg-emerald-50 text-emerald-700 text-[10px] font-mono rounded-full mt-0.5 flex-shrink-0">
                        /{qr.shortcut}
                      </span>
                      <div className="flex-1 min-w-0">
                        {qr.title && <p className="text-sm font-medium text-slate-900">{qr.title}</p>}
                        {qr.attachments && qr.attachments.length > 0 && (
                          <div className="flex items-center gap-1.5 mb-1 flex-wrap">
                            {qr.attachments.map((att, i) => (
                              <span key={i} className="inline-flex items-center gap-1 bg-emerald-50 rounded px-1.5 py-0.5">
                                {att.media_type === 'image' ? <Image className="w-3 h-3 text-emerald-500" /> :
                                 att.media_type === 'video' ? <Video className="w-3 h-3 text-emerald-500" /> :
                                 <File className="w-3 h-3 text-emerald-500" />}
                                <span className="text-[10px] text-emerald-600 font-medium truncate max-w-[100px]">{att.media_filename || att.media_type}</span>
                              </span>
                            ))}
                          </div>
                        )}
                        {qr.attachments.length === 0 && qr.media_url && (
                          <div className="flex items-center gap-1.5 mb-1">
                            {qr.media_type === 'image' ? <Image className="w-3 h-3 text-emerald-500" /> :
                             qr.media_type === 'video' ? <Video className="w-3 h-3 text-emerald-500" /> :
                             <File className="w-3 h-3 text-emerald-500" />}
                            <span className="text-[10px] text-emerald-600 font-medium">{qr.media_filename || qr.media_type}</span>
                          </div>
                        )}
                        {qr.body && <p className="text-xs text-slate-600 whitespace-pre-wrap">{qr.body}</p>}
                      </div>
                      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                        <button
                          onClick={() => setEditingQR({ id: qr.id, shortcut: qr.shortcut, title: qr.title, body: qr.body, media_url: qr.media_url || '', media_type: qr.media_type || '', media_filename: qr.media_filename || '', attachments: qr.attachments || [] })}
                          className="p-1.5 text-slate-400 hover:text-emerald-600 hover:bg-white rounded-lg"
                          title="Editar"
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => handleDeleteQuickReply(qr.id)}
                          className="p-1.5 text-slate-400 hover:text-red-600 hover:bg-white rounded-lg"
                          title="Eliminar"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Notifications Tab */}
          {activeTab === 'notifications' && notifSettings && (
            <div className="space-y-6">
              {/* Sound Settings */}
              <div>
                <h3 className="text-sm font-medium text-slate-900 mb-1">Sonido de Notificación</h3>
                <p className="text-xs text-slate-500 mb-4">Configura el sonido que se reproduce al recibir un mensaje nuevo en esta cuenta.</p>

                {/* Enable toggle */}
                <div className="flex items-center justify-between p-4 bg-slate-50 rounded-xl mb-4">
                  <div className="flex items-center gap-3">
                    {notifSettings.sound_enabled ? (
                      <Volume2 className="w-4 h-4 text-emerald-600" />
                    ) : (
                      <VolumeX className="w-4 h-4 text-slate-400" />
                    )}
                    <div>
                      <p className="text-sm font-medium text-slate-900">Sonido activado</p>
                      <p className="text-xs text-slate-500">Reproduce un sonido al recibir mensaje</p>
                    </div>
                  </div>
                  <button
                    onClick={() => setNotifSettings({ ...notifSettings, sound_enabled: !notifSettings.sound_enabled, sound_type: !notifSettings.sound_enabled && notifSettings.sound_type === 'none' ? 'whatsapp' : notifSettings.sound_type })}
                    className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${notifSettings.sound_enabled ? 'bg-emerald-600' : 'bg-slate-300'}`}
                  >
                    <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${notifSettings.sound_enabled ? 'translate-x-6' : 'translate-x-1'}`} />
                  </button>
                </div>

                {/* Sound type selector */}
                {notifSettings.sound_enabled && (
                  <div className="space-y-3">
                    <label className="block text-xs font-medium text-slate-600">Tipo de sonido</label>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                      {SOUND_OPTIONS.filter(s => s.value !== 'none').map((opt) => (
                        <button
                          key={opt.value}
                          onClick={() => setNotifSettings({ ...notifSettings, sound_type: opt.value })}
                          className={`flex items-center gap-3 p-3 rounded-xl border transition text-left ${
                            notifSettings.sound_type === opt.value
                              ? 'border-emerald-500 bg-emerald-50/50 ring-1 ring-emerald-500'
                              : 'border-slate-200 hover:border-slate-300 hover:bg-slate-50'
                          }`}
                        >
                          <div className="flex-1">
                            <p className={`text-sm font-medium ${notifSettings.sound_type === opt.value ? 'text-emerald-700' : 'text-slate-900'}`}>{opt.label}</p>
                            <p className="text-[10px] text-slate-500">{opt.description}</p>
                          </div>
                          <button
                            onClick={(e) => { e.stopPropagation(); playNotificationSound(opt.value, notifSettings.sound_volume) }}
                            className="p-1.5 rounded-full hover:bg-white/80 text-slate-500 hover:text-emerald-600 transition"
                            title="Previsualizar"
                          >
                            <Play className="w-3.5 h-3.5" />
                          </button>
                        </button>
                      ))}
                    </div>

                    {/* Volume slider */}
                    <div className="mt-4">
                      <label className="block text-xs font-medium text-slate-600 mb-2">
                        Volumen: {Math.round(notifSettings.sound_volume * 100)}%
                      </label>
                      <div className="flex items-center gap-3">
                        <VolumeX className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                        <input
                          type="range"
                          min="0"
                          max="100"
                          value={Math.round(notifSettings.sound_volume * 100)}
                          onChange={(e) => setNotifSettings({ ...notifSettings, sound_volume: parseInt(e.target.value) / 100 })}
                          className="flex-1 h-1.5 bg-slate-200 rounded-lg appearance-none cursor-pointer accent-emerald-600"
                        />
                        <Volume2 className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                      </div>
                    </div>

                    {/* Test button */}
                    <button
                      onClick={handlePreviewSound}
                      className="inline-flex items-center gap-2 px-3 py-1.5 text-xs text-emerald-700 bg-emerald-50 border border-emerald-100 rounded-xl hover:bg-emerald-100 transition"
                    >
                      <Play className="w-3.5 h-3.5" />
                      Probar sonido
                    </button>
                  </div>
                )}
              </div>

              {/* Browser Notifications */}
              <div className="pt-6 border-t border-slate-200">
                <h3 className="text-sm font-medium text-slate-900 mb-1">Notificaciones del Navegador</h3>
                <p className="text-xs text-slate-500 mb-4">Muestra una notificación emergente tipo WhatsApp Web cuando recibes un mensaje y la pestaña no está activa.</p>

                {/* Permission status */}
                {notifPermission === 'denied' && (
                  <div className="p-3 bg-red-50 text-red-700 text-xs rounded-xl mb-4">
                    Las notificaciones están bloqueadas por tu navegador. Para activarlas, haz clic en el icono de candado en la barra de direcciones y permite las notificaciones.
                  </div>
                )}

                {notifPermission === 'default' && notifSettings.browser_notifications && (
                  <div className="p-3 bg-amber-50 text-amber-700 text-xs rounded-xl mb-4 flex items-center justify-between">
                    <span>Necesitas dar permiso al navegador para mostrar notificaciones.</span>
                    <button
                      onClick={handleRequestPermission}
                      className="ml-3 px-3 py-1 bg-amber-100 border border-amber-200 rounded-xl text-xs font-medium hover:bg-amber-200 transition whitespace-nowrap"
                    >
                      Permitir
                    </button>
                  </div>
                )}

                {/* Enable toggle */}
                <div className="flex items-center justify-between p-4 bg-slate-50 rounded-xl mb-4">
                  <div className="flex items-center gap-3">
                    {notifSettings.browser_notifications ? (
                      <BellRing className="w-4 h-4 text-emerald-600" />
                    ) : (
                      <BellOff className="w-4 h-4 text-slate-400" />
                    )}
                    <div>
                      <p className="text-sm font-medium text-slate-900">Notificaciones emergentes</p>
                      <p className="text-xs text-slate-500">Muestra alerta visual cuando la pestaña está en segundo plano</p>
                    </div>
                  </div>
                  <button
                    onClick={() => {
                      const enabling = !notifSettings.browser_notifications
                      setNotifSettings({ ...notifSettings, browser_notifications: enabling })
                      if (enabling && notifPermission === 'default') {
                        handleRequestPermission()
                      }
                    }}
                    className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${notifSettings.browser_notifications ? 'bg-emerald-600' : 'bg-slate-300'}`}
                  >
                    <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${notifSettings.browser_notifications ? 'translate-x-6' : 'translate-x-1'}`} />
                  </button>
                </div>

                {/* Show preview toggle */}
                {notifSettings.browser_notifications && (
                  <div className="flex items-center justify-between p-4 bg-slate-50 rounded-xl">
                    <div className="flex items-center gap-3">
                      {notifSettings.show_preview ? (
                        <Eye className="w-4 h-4 text-emerald-600" />
                      ) : (
                        <EyeOff className="w-4 h-4 text-slate-400" />
                      )}
                      <div>
                        <p className="text-sm font-medium text-slate-900">Mostrar vista previa</p>
                        <p className="text-xs text-slate-500">Muestra el contenido del mensaje en la notificación</p>
                      </div>
                    </div>
                    <button
                      onClick={() => setNotifSettings({ ...notifSettings, show_preview: !notifSettings.show_preview })}
                      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${notifSettings.show_preview ? 'bg-emerald-600' : 'bg-slate-300'}`}
                    >
                      <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${notifSettings.show_preview ? 'translate-x-6' : 'translate-x-1'}`} />
                    </button>
                  </div>
                )}
              </div>

              {/* Account context note */}
              <div className="text-xs text-slate-500 bg-blue-50 border border-blue-100 rounded-xl p-3">
                <strong>Nota:</strong> Esta configuración aplica solo a la cuenta <strong>{account?.name || 'actual'}</strong>. Al cambiar de cuenta, se aplicarán las preferencias guardadas para esa cuenta.
              </div>

              {/* Save button */}
              <button
                onClick={handleSaveNotifications}
                className="inline-flex items-center gap-2 bg-emerald-600 text-white px-4 py-2 rounded-xl hover:bg-emerald-700 transition text-sm font-medium shadow-sm"
              >
                <Save className="w-4 h-4" />
                Guardar Preferencias
              </button>
            </div>
          )}

          {/* API Keys / MCP Tab */}
          {activeTab === 'api-keys' && (
            <APIKeysPanel />
          )}

          {/* Custom Fields Tab */}
          {activeTab === 'custom-fields' && (
            <CustomFieldsPanel />
          )}

          {/* Security Tab */}
          {activeTab === 'security' && (
            <div className="space-y-6">
              <div>
                <h3 className="text-sm font-medium text-slate-900 mb-4">Cambiar Contraseña</h3>
                <div className="space-y-4 max-w-md">
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Contraseña Actual</label>
                    <input
                      type="password"
                      value={formData.currentPassword}
                      onChange={(e) => setFormData({ ...formData, currentPassword: e.target.value })}
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Nueva Contraseña</label>
                    <input
                      type="password"
                      value={formData.newPassword}
                      onChange={(e) => setFormData({ ...formData, newPassword: e.target.value })}
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-600 mb-1">Confirmar Contraseña</label>
                    <input
                      type="password"
                      value={formData.confirmPassword}
                      onChange={(e) => setFormData({ ...formData, confirmPassword: e.target.value })}
                      className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-sm text-slate-900 placeholder:text-slate-400"
                    />
                  </div>
                  <button
                    onClick={handleChangePassword}
                    disabled={saving || !formData.currentPassword || !formData.newPassword}
                    className="inline-flex items-center gap-2 bg-emerald-600 text-white px-4 py-2 rounded-xl hover:bg-emerald-700 disabled:opacity-50 text-sm font-medium shadow-sm"
                  >
                    {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Shield className="w-4 h-4" />}
                    Cambiar Contraseña
                  </button>
                </div>
              </div>

              <div className="pt-6 border-t border-slate-200">
                <h3 className="text-sm font-medium text-slate-900 mb-4">Sesión</h3>
                <button
                  onClick={handleLogout}
                  className="inline-flex items-center gap-2 px-4 py-2 border border-slate-200 text-slate-700 rounded-xl hover:bg-slate-50 text-sm"
                >
                  <LogOut className="w-4 h-4" />
                  Cerrar Sesión
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
