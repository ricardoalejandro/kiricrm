import type { Chat } from '@/types/chat'

export interface WhatsAppDeviceOption {
  id: string
  name?: string | null
  phone?: string | null
  jid?: string | null
  status?: string | null
  provider?: string
  normalized_phone?: string
  historical_relation?: 'same_historical_number' | 'different_number' | 'history_unknown' | 'new_chat'
  matches_historical?: boolean
  has_different_number?: boolean
  history_unknown?: boolean
}

export interface WhatsAppChatResolution {
  success: boolean
  chat?: Chat | null
  phone: string
  jid: string
  historical_phone?: string
  devices: WhatsAppDeviceOption[]
  mode: 'read_only' | 'open_direct' | 'choose_device' | 'no_device'
  error?: string
}

function authHeaders() {
  return { Authorization: `Bearer ${localStorage.getItem('token') || ''}` }
}

export function cleanWhatsAppPhone(phone: string) {
  return (phone || '').replace(/[^0-9]/g, '')
}

export async function resolveWhatsAppChat(phone: string): Promise<WhatsAppChatResolution> {
  const cleanPhone = cleanWhatsAppPhone(phone)
  const res = await fetch(`/api/chats/resolve-whatsapp/${cleanPhone}`, {
    headers: authHeaders(),
  })
  return res.json()
}

export async function createWhatsAppChat(deviceID: string, phone: string): Promise<{ success: boolean; chat?: Chat; error?: string }> {
  const res = await fetch('/api/chats/new', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify({ device_id: deviceID, phone: cleanWhatsAppPhone(phone) }),
  })
  return res.json()
}

export function deviceDisplayPhone(device: WhatsAppDeviceOption) {
  return device.phone || device.normalized_phone || device.jid || ''
}

export function relationLabel(device: WhatsAppDeviceOption) {
  switch (device.historical_relation) {
    case 'same_historical_number':
      return 'Historial'
    case 'different_number':
      return 'Otro numero'
    case 'history_unknown':
      return 'Historial desconocido'
    default:
      return 'Nuevo'
  }
}

export function relationClassName(device: WhatsAppDeviceOption) {
  switch (device.historical_relation) {
    case 'same_historical_number':
      return 'bg-emerald-100 text-emerald-700'
    case 'different_number':
      return 'bg-amber-100 text-amber-700'
    case 'history_unknown':
      return 'bg-slate-100 text-slate-600'
    default:
      return 'bg-sky-100 text-sky-700'
  }
}
