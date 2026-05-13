'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import { X, Clock, FileText, Phone, Trash2, Plus, XCircle, ChevronDown, Filter, SlidersHorizontal } from 'lucide-react'
import { format } from 'date-fns'
import { es } from 'date-fns/locale'

export interface HistoryObservation {
  id: string
  contact_id: string | null
  lead_id: string | null
  type: string
  direction: string | null
  outcome: string | null
  notes: string | null
  created_by_name: string | null
  created_at: string
}

interface ObservationHistoryModalProps {
  isOpen: boolean
  onClose: () => void
  /** lead_id for API calls */
  leadId: string
  /** Event participant ID — when present, history is resolved through the participant/contact/lead graph */
  participantId?: string | null
  /** Event ID used when creating participant observations */
  eventId?: string | null
  /** Contact ID — if provided, API calls use /api/contacts/:id/interactions */
  contactId?: string | null
  /** Display name for header */
  name: string
  /** Initial observations (caller already has them). Component refreshes internally after add/delete. */
  observations: HistoryObservation[]
  /** Called after an observation is added or deleted so caller can refresh its own list */
  onObservationChange?: () => void
}

const PAGE_SIZE = 20

export default function ObservationHistoryModal({
  isOpen,
  onClose,
  leadId,
  participantId,
  eventId,
  contactId,
  name,
  observations: initialObservations,
  onObservationChange,
}: ObservationHistoryModalProps) {
  const kommoEnabled = typeof window !== 'undefined' && localStorage.getItem('kommo_enabled') === 'true'
  const [observations, setObservations] = useState<HistoryObservation[]>(initialObservations)
  const [filterType, setFilterType] = useState('')
  const [filterFrom, setFilterFrom] = useState('')
  const [filterTo, setFilterTo] = useState('')

  // Toolbar accordion
  const [toolbarOpen, setToolbarOpen] = useState(false)

  // Add observation form
  const [newType, setNewType] = useState<'note' | 'call'>('call')
  const [newText, setNewText] = useState('')
  const [saving, setSaving] = useState(false)

  // Infinite scroll
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE)
  const [loadingMore, setLoadingMore] = useState(false)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  // Sync observations when prop changes
  useEffect(() => { setObservations(initialObservations) }, [initialObservations])

  // Reset state when modal closes
  useEffect(() => {
    if (!isOpen) {
      setFilterType(''); setFilterFrom(''); setFilterTo(''); setNewText('')
      setToolbarOpen(false); setVisibleCount(PAGE_SIZE)
    }
  }, [isOpen])

  // Escape key — capture phase to prevent parent handlers from firing
  useEffect(() => {
    if (!isOpen) return
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.stopPropagation(); e.preventDefault(); onClose() }
    }
    document.addEventListener('keydown', h, true)
    return () => document.removeEventListener('keydown', h, true)
  }, [isOpen, onClose])

  const fetchObservations = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      const url = participantId
        ? `/api/interactions?participant_id=${participantId}&limit=200`
        : contactId
        ? `/api/contacts/${contactId}/interactions?limit=200`
        : `/api/leads/${leadId}/interactions?limit=200`
      const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) {
        setObservations(data.interactions || [])
        setVisibleCount(PAGE_SIZE)
      }
    } catch (err) {
      console.error('Failed to fetch observations:', err)
    }
  }, [leadId, participantId, contactId])

  // Infinite scroll with IntersectionObserver
  useEffect(() => {
    if (!isOpen) return
    const sentinel = sentinelRef.current
    if (!sentinel) return

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) {
          setVisibleCount(prev => prev + PAGE_SIZE)
        }
      },
      { root: scrollRef.current, rootMargin: '100px' }
    )
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [isOpen, observations.length, filterType, filterFrom, filterTo])

  const handleAdd = async () => {
    if (!newText.trim()) return
    setSaving(true)
    const token = localStorage.getItem('token')
    try {
      const body = participantId
        ? { event_id: eventId || undefined, participant_id: participantId, contact_id: contactId || undefined, lead_id: leadId, type: newType, notes: newText.trim() }
        : contactId
        ? { contact_id: contactId, type: newType, notes: newText.trim() }
        : { lead_id: leadId, type: newType, notes: newText.trim() }
      const res = await fetch('/api/interactions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify(body),
      })
      const data = await res.json()
      if (data.success) {
        setNewText('')
        setToolbarOpen(false)
        await fetchObservations()
        onObservationChange?.()
      }
    } catch (err) {
      console.error('Failed to add observation:', err)
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (obsId: string) => {
    if (!confirm('¿Eliminar esta observación?')) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/interactions/${obsId}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        await fetchObservations()
        onObservationChange?.()
      }
    } catch (err) {
      console.error('Failed to delete observation:', err)
    }
  }

  if (!isOpen) return null

  const hasFilters = !!(filterType || filterFrom || filterTo)

  const filtered = observations.filter(obs => {
    if (filterType && obs.type !== filterType) return false
    if (filterFrom && new Date(obs.created_at) < new Date(filterFrom)) return false
    if (filterTo) {
      const to = new Date(filterTo)
      to.setDate(to.getDate() + 1)
      if (new Date(obs.created_at) >= to) return false
    }
    return true
  })

  const visible = filtered.slice(0, visibleCount)
  const hasMore = visibleCount < filtered.length

  const typeLabel = (t: string) =>
    t === 'note' ? 'Nota' : t === 'call' ? 'Llamada' : t === 'whatsapp' ? 'WhatsApp' : t === 'email' ? 'Email' : t === 'meeting' ? 'Reunión' : t

  const typeStyle = (t: string) =>
    t === 'note' ? 'bg-amber-50 text-amber-700 border-amber-200/60'
    : t === 'call' ? 'bg-blue-50 text-blue-700 border-blue-200/60'
    : t === 'whatsapp' ? 'bg-green-50 text-green-700 border-green-200/60'
    : t === 'email' ? 'bg-purple-50 text-purple-700 border-purple-200/60'
    : 'bg-orange-50 text-orange-700 border-orange-200/60'

  return (
    <div className="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center z-[60] p-4 animate-in fade-in duration-150" onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-2xl w-full max-w-3xl max-h-[85vh] overflow-hidden flex flex-col border border-slate-200/60 animate-in zoom-in-95 duration-200" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-slate-100 bg-gradient-to-r from-slate-50 to-white shrink-0">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg bg-emerald-100 flex items-center justify-center">
              <Clock className="w-4 h-4 text-emerald-600" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-slate-900">Historial de Observaciones</h2>
              <p className="text-[11px] text-slate-500">{name || 'Sin nombre'} · {observations.length} registro{observations.length !== 1 ? 's' : ''}</p>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setToolbarOpen(!toolbarOpen)}
              className={`p-2 rounded-lg transition-all ${toolbarOpen || hasFilters ? 'text-emerald-600 bg-emerald-50 hover:bg-emerald-100' : 'text-slate-400 hover:text-slate-600 hover:bg-slate-100'}`}
              title="Filtrar y agregar"
            >
              <SlidersHorizontal className="w-4 h-4" />
            </button>
            <button onClick={onClose} className="p-2 text-slate-400 hover:text-slate-600 rounded-lg hover:bg-slate-100 transition-all" title="Cerrar (Esc)">
              <X className="w-4.5 h-4.5" />
            </button>
          </div>
        </div>

        {/* Collapsible Toolbar: Filters + Add Form */}
        {toolbarOpen && (
          <div className="border-b border-slate-100 bg-slate-50/50 shrink-0 animate-in slide-in-from-top-2 duration-150">
            {/* Filters row */}
            <div className="px-5 py-2.5 flex items-end gap-3 flex-wrap">
              <div>
                <label className="text-[10px] text-slate-500 uppercase tracking-wider mb-0.5 block font-semibold">Tipo</label>
                <select value={filterType} onChange={(e) => setFilterType(e.target.value)} className="px-2.5 py-1 border border-slate-200 rounded-lg text-xs text-slate-700 focus:ring-2 focus:ring-emerald-500 focus:border-emerald-300 bg-white transition">
                  <option value="">Todos</option>
                  <option value="note">Nota</option>
                  <option value="call">Llamada</option>
                  <option value="whatsapp">WhatsApp</option>
                  <option value="email">Email</option>
                  <option value="meeting">Reunión</option>
                </select>
              </div>
              <div>
                <label className="text-[10px] text-slate-500 uppercase tracking-wider mb-0.5 block font-semibold">Desde</label>
                <input type="date" value={filterFrom} onChange={(e) => setFilterFrom(e.target.value)} className="px-2.5 py-1 border border-slate-200 rounded-lg text-xs text-slate-700 focus:ring-2 focus:ring-emerald-500 focus:border-emerald-300 bg-white transition" />
              </div>
              <div>
                <label className="text-[10px] text-slate-500 uppercase tracking-wider mb-0.5 block font-semibold">Hasta</label>
                <input type="date" value={filterTo} onChange={(e) => setFilterTo(e.target.value)} className="px-2.5 py-1 border border-slate-200 rounded-lg text-xs text-slate-700 focus:ring-2 focus:ring-emerald-500 focus:border-emerald-300 bg-white transition" />
              </div>
              {hasFilters && (
                <button onClick={() => { setFilterType(''); setFilterFrom(''); setFilterTo('') }} className="px-2 py-1 text-[11px] text-slate-500 hover:text-red-600 hover:bg-red-50 flex items-center gap-1 transition rounded-lg">
                  <XCircle className="w-3 h-3" /> Limpiar
                </button>
              )}
            </div>
            {/* Add form */}
            <div className="px-5 py-2.5 border-t border-slate-100/80">
              <div className="flex items-center gap-1.5 mb-1.5">
                <button
                  onClick={() => setNewType('note')}
                  className={`flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md transition font-medium ${
                    newType === 'note' ? 'bg-yellow-100 text-yellow-700 ring-1 ring-yellow-300' : 'bg-slate-100 text-slate-500 hover:bg-slate-200'
                  }`}
                >
                  <FileText className="w-3 h-3" /> Nota
                </button>
                <button
                  onClick={() => setNewType('call')}
                  className={`flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md transition font-medium ${
                    newType === 'call' ? 'bg-blue-100 text-blue-700 ring-1 ring-blue-300' : 'bg-slate-100 text-slate-500 hover:bg-slate-200'
                  }`}
                >
                  <Phone className="w-3 h-3" /> Llamada
                </button>
              </div>
              <div className="flex gap-2">
                <textarea
                  value={newText}
                  onChange={(e) => setNewText(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && newText.trim() && !saving) {
                      e.preventDefault()
                      handleAdd()
                    }
                  }}
                  placeholder={newType === 'call' ? 'Resultado de llamada... (Ctrl+Enter)' : 'Escribir observación... (Ctrl+Enter)'}
                  rows={2}
                  className="flex-1 px-3 py-1.5 border border-slate-200 rounded-lg focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400 resize-none"
                />
                <button
                  onClick={handleAdd}
                  disabled={!newText.trim() || saving}
                  className="self-end px-3 py-1.5 bg-emerald-600 text-white text-xs rounded-lg hover:bg-emerald-700 disabled:opacity-50 transition flex items-center gap-1.5 shrink-0"
                >
                  {saving ? <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-white" /> : <Plus className="w-3 h-3" />}
                  Agregar
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Active filters indicator (when toolbar is closed) */}
        {!toolbarOpen && hasFilters && (
          <div className="px-5 py-1.5 border-b border-slate-100 bg-emerald-50/50 shrink-0 flex items-center gap-2">
            <Filter className="w-3 h-3 text-emerald-600" />
            <span className="text-[11px] text-emerald-700 font-medium">
              Filtros activos — {filtered.length} de {observations.length} registros
            </span>
            <button onClick={() => { setFilterType(''); setFilterFrom(''); setFilterTo('') }} className="ml-auto text-[11px] text-emerald-600 hover:text-red-600 transition">
              Limpiar
            </button>
          </div>
        )}

        {/* Content — observations list */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto px-5 py-3">
          {filtered.length === 0 ? (
            <div className="text-center py-16">
              <FileText className="w-10 h-10 text-slate-200 mx-auto mb-3" />
              <p className="text-sm text-slate-400">No hay registros{hasFilters ? ' con los filtros seleccionados' : ''}</p>
              {!toolbarOpen && (
                <button onClick={() => setToolbarOpen(true)} className="mt-3 text-xs text-emerald-600 hover:text-emerald-700 font-medium">
                  + Agregar observación
                </button>
              )}
            </div>
          ) : (
            <div className="space-y-1.5">
              {visible.map((obs) => (
                <div key={obs.id} className="px-3.5 py-2.5 bg-white rounded-xl group relative border border-slate-100 hover:border-slate-200 hover:shadow-sm transition-all">
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <span className={`px-2 py-px text-[10px] rounded font-semibold tracking-wide border ${typeStyle(obs.type)}`}>
                          {typeLabel(obs.type)}
                        </span>
                        <span className="text-[11px] text-slate-400">{format(new Date(obs.created_at), "d MMM yyyy, HH:mm", { locale: es })}</span>
                        {obs.created_by_name && <span className="text-[10px] text-slate-400 hidden sm:inline">· {obs.created_by_name}</span>}
                        {kommoEnabled && obs.notes?.startsWith('(sinc)') && <span className="px-1.5 py-px bg-emerald-50 text-emerald-600 text-[9px] rounded-full font-medium border border-emerald-100">Kommo</span>}
                      </div>
                      <p className="text-sm text-slate-700 whitespace-pre-wrap break-words leading-relaxed">{obs.notes?.startsWith('(sinc) ') ? obs.notes.slice(7) : (obs.notes || '(sin contenido)')}</p>
                    </div>
                    <button onClick={() => handleDelete(obs.id)} className="p-1 text-slate-300 hover:text-red-500 hover:bg-red-50 rounded-md sm:opacity-0 sm:group-hover:opacity-100 transition-all shrink-0" title="Eliminar">
                      <Trash2 className="w-3 h-3" />
                    </button>
                  </div>
                </div>
              ))}
              {/* Infinite scroll sentinel */}
              <div ref={sentinelRef} className="h-1" />
              {hasMore && (
                <div className="text-center py-2">
                  <div className="inline-flex items-center gap-2 text-[11px] text-slate-400">
                    <div className="animate-spin rounded-full h-3 w-3 border-b-2 border-slate-400" />
                    Cargando más...
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
