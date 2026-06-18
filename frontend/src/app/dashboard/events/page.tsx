'use client'

import { useEffect, useState, useCallback, useRef, useMemo } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import {
  CalendarDays, Plus, Search, MapPin, Users, Clock, Edit2, Trash2,
  Eye, LayoutGrid, List, ChevronRight, Home, FolderPlus, MoreHorizontal,
  LayoutTemplate, FolderOpen, ArrowLeft, MoveRight, Tag, X, ChevronDown, Check,
  Code, FileText, AlertCircle, CheckCircle2, Copy,
} from 'lucide-react'
import { format } from 'date-fns'
import { es } from 'date-fns/locale'
import FormulaEditor from '@/components/FormulaEditor'

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Extract quoted tag names from a formula string, e.g. '("foo" or "bar%") and "baz"' → ['foo','bar%','baz'] */
function extractFormulaTags(formula: string): string[] {
  const matches = formula.match(/"([^"]+)"/g)
  if (!matches) return []
  return Array.from(new Set(matches.map(m => m.slice(1, -1))))
}

/** Render tag pills for an event's formula or simple tags */
function FormulaTagDisplay({ ev, maxTags = 3, size = 'sm' }: { ev: { tags?: { id: string; name: string; color: string; negate?: boolean }[]; tag_formula?: string; tag_formula_type?: string; tag_formula_mode?: string }; maxTags?: number; size?: 'sm' | 'xs' }) {
  const isAdvanced = ev.tag_formula_type === 'advanced' && !!ev.tag_formula
  const tags = ev.tags || []
  const formulaTags = isAdvanced ? extractFormulaTags(ev.tag_formula!) : []
  const tagColorMap = new Map(tags.map(t => [t.name.toLowerCase(), t.color]))
  const px = size === 'sm' ? 'px-2 py-0.5' : 'px-1.5 py-0.5'
  const text = size === 'sm' ? 'text-[11px]' : 'text-[10px]'

  if (isAdvanced) {
    const display = formulaTags.slice(0, maxTags)
    const extra = formulaTags.length - maxTags
    return (
      <div className="flex items-center gap-1 flex-wrap">
        {display.map((name, i) => {
          const color = tagColorMap.get(name.toLowerCase()) || '#8b5cf6'
          return <span key={i} className={`inline-flex items-center gap-0.5 ${px} rounded-full ${text} font-medium text-white`} style={{ backgroundColor: color }}>{name}</span>
        })}
        {extra > 0 && <span className={`${text} text-slate-400`}>+{extra}</span>}
        <span className={`inline-flex items-center gap-0.5 ${px} rounded-full ${text} font-medium bg-violet-100 text-violet-700`}>
          <Code className="w-2.5 h-2.5" />Avanzado
        </span>
      </div>
    )
  }

  if (tags.length > 0) {
    const display = tags.slice(0, maxTags)
    const extra = tags.length - maxTags
    const mode = ev.tag_formula_mode || 'OR'
    return (
      <div className="flex items-center gap-1 flex-wrap">
        {display.map(tag => (
          <span key={tag.id} className={`inline-flex items-center ${px} rounded-full ${text} font-medium text-white`} style={{ backgroundColor: tag.color }}>{tag.name}</span>
        ))}
        {extra > 0 && <span className={`${text} text-slate-400`}>+{extra}</span>}
        <span className={`${text} text-slate-400 font-medium`}>{mode}</span>
      </div>
    )
  }

  return <span className="text-[10px] text-slate-300">-</span>
}

// ─── Types ────────────────────────────────────────────────────────────────────

interface TagItem {
  id: string
  name: string
  color: string
  negate?: boolean
}

interface Event {
  id: string
  name: string
  description?: string
  event_date?: string
  event_end?: string
  location?: string
  status: string
  color: string
  tag_formula_mode?: string
  tag_formula?: string
  tag_formula_type?: string
  folder_id?: string | null
  created_at: string
  total_participants: number
  participant_counts?: Record<string, number>
  stage_counts?: Record<string, number>
  tags?: TagItem[]
}

interface EventFolder {
  id: string
  account_id: string
  parent_id?: string | null
  name: string
  color: string
  icon: string
  position: number
  event_count: number
  created_at: string
  updated_at: string
}

// ─── Constants ────────────────────────────────────────────────────────────────

const STATUS_OPTIONS = [
  { value: 'active', label: 'Activo', desc: 'Sincronizando etiquetas', color: 'bg-emerald-100 text-emerald-700', syncing: true },
  { value: 'draft', label: 'Borrador', desc: 'Sin sincronizar', color: 'bg-slate-100 text-slate-700', syncing: false },
  { value: 'completed', label: 'Completado', desc: 'Evento finalizado, sin sincronizar', color: 'bg-blue-100 text-blue-700', syncing: false },
  { value: 'cancelled', label: 'Cancelado', desc: 'Evento eliminado, sin sincronizar', color: 'bg-red-100 text-red-700', syncing: false },
]

const COLOR_OPTIONS = [
  '#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899',
  '#06b6d4', '#f97316', '#14b8a6', '#6366f1',
]

const FOLDER_ICONS = ['📁', '📂', '🎓', '🎉', '🏋️', '📊', '📝', '🎯', '📌', '🗂️']

const PARTICIPANT_STATUSES = [
  { key: 'invited', label: 'Invitados', color: 'bg-blue-500' },
  { key: 'contacted', label: 'Contactados', color: 'bg-yellow-500' },
  { key: 'confirmed', label: 'Confirmados', color: 'bg-green-500' },
  { key: 'declined', label: 'Declinados', color: 'bg-red-500' },
  { key: 'attended', label: 'Asistieron', color: 'bg-emerald-600' },
]

// ─── Main Component ───────────────────────────────────────────────────────────

export default function EventsPage() {
  const router = useRouter()
  const searchParams = useSearchParams()
  // Events & Folders state
  const [events, setEvents] = useState<Event[]>([])
  const [folders, setFolders] = useState<EventFolder[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(true)
  const [total, setTotal] = useState(0)
  const offsetRef = useRef(0)
  const eventsScrollRef = useRef<HTMLDivElement>(null)
  const EVENTS_PAGE_SIZE = 50

  // Navigation state
  const [currentFolderID, setCurrentFolderID] = useState<string | null>(null)
  const [folderPath, setFolderPath] = useState<EventFolder[]>([]) // breadcrumb

  // Filters & View
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('active')
  const [hideCancelled, setHideCancelled] = useState(true)
  const [viewMode, setViewMode] = useState<'grid' | 'compact' | 'list'>('list')

  // Sort state for list view
  const [sortField, setSortField] = useState<string | null>('name')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('desc')

  // Event modal
  const [showCreate, setShowCreate] = useState(false)
  const [editEvent, setEditEvent] = useState<Event | null>(null)
  const [formData, setFormData] = useState({
    name: '', description: '', event_date: '', event_end: '', location: '', color: '#3b82f6', status: 'active',
    tag_ids: [] as string[], formula_mode: 'OR' as 'AND' | 'OR', include_tag_ids: [] as string[], exclude_tag_ids: [] as string[],
    tag_formula: '', tag_formula_type: 'simple' as 'simple' | 'advanced',
  })

  // Tags for auto-sync
  const [availableTags, setAvailableTags] = useState<TagItem[]>([])
  const [showTagDropdown, setShowTagDropdown] = useState(false)
  const [tagSearch, setTagSearch] = useState('')
  const tagDropdownRef = useRef<HTMLDivElement>(null)

  // Formula validation state
  const [formulaIsValid, setFormulaIsValid] = useState(true)

  // Folder modal
  const [showFolderModal, setShowFolderModal] = useState(false)
  const [editFolder, setEditFolder] = useState<EventFolder | null>(null)
  const [folderForm, setFolderForm] = useState({ name: '', color: '#3b82f6', icon: '📁' })

  // Event context menus
  const [menuEventID, setMenuEventID] = useState<string | null>(null)
  const [showMoveMenu, setShowMoveMenu] = useState<string | null>(null)
  const [duplicatingEventID, setDuplicatingEventID] = useState<string | null>(null)

  // Drag & drop
  const [dragOverFolderID, setDragOverFolderID] = useState<string | null>(null)
  const dragEventIDRef = useRef<string | null>(null)

  const token = typeof window !== 'undefined' ? localStorage.getItem('token') : ''

  // ─── Data Fetching ──────────────────────────────────────────────────────────

  const fetchTags = useCallback(async () => {
    try {
      const res = await fetch('/api/tags', {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) setAvailableTags(data.tags || [])
    } catch (e) {
      console.error(e)
    }
  }, [token])

  const fetchFolders = useCallback(async () => {
    try {
      const res = await fetch('/api/events/folders', {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) setFolders(data.folders || [])
    } catch (e) {
      console.error(e)
    }
  }, [token])

  const fetchEvents = useCallback(async (reset: boolean = true) => {
    if (!reset && !hasMore) return
    if (!reset) setLoadingMore(true)
    try {
      if (reset) offsetRef.current = 0
      const params = new URLSearchParams()
      if (search) params.set('search', search)
      if (statusFilter) params.set('status', statusFilter)
      params.set('folder', currentFolderID ?? 'root')
      params.set('limit', String(EVENTS_PAGE_SIZE))
      params.set('offset', String(offsetRef.current))
      const res = await fetch(`/api/events?${params}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        const newEvents: Event[] = data.events || []
        if (reset) {
          setEvents(newEvents)
        } else {
          setEvents(prev => {
            const ids = new Set(prev.map(e => e.id))
            return [...prev, ...newEvents.filter(e => !ids.has(e.id))]
          })
        }
        const serverTotal = data.total ?? newEvents.length
        setTotal(serverTotal)
        offsetRef.current += newEvents.length
        setHasMore(offsetRef.current < serverTotal)
      }
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }, [token, search, statusFilter, currentFolderID, hasMore])

  // Restore folder from URL on mount
  useEffect(() => {
    const folderFromUrl = searchParams.get('folder')
    if (folderFromUrl && !currentFolderID) {
      setCurrentFolderID(folderFromUrl)
      // Build folder path by fetching folder ancestors
      fetch('/api/events/folders', { headers: { Authorization: `Bearer ${token}` } })
        .then(r => r.json())
        .then(data => {
          if (!data.success) return
          const allFolders: EventFolder[] = data.folders || []
          const path: EventFolder[] = []
          let fid: string | null | undefined = folderFromUrl
          while (fid) {
            const f = allFolders.find(x => x.id === fid)
            if (f) { path.unshift(f); fid = f.parent_id } else break
          }
          setFolderPath(path)
        })
        .catch(() => {})
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    setLoading(true)
    fetchFolders()
    fetchEvents()
    fetchTags()
  }, [fetchFolders, fetchEvents, fetchTags])

  // Infinite scroll for events
  useEffect(() => {
    if (!hasMore || loadingMore || loading) return
    const el = eventsScrollRef.current
    if (!el) return
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      if (scrollHeight - scrollTop - clientHeight < 300) {
        fetchEvents(false)
      }
    }
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [hasMore, loadingMore, loading, fetchEvents])

  // Close modals on Escape
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setShowCreate(false); setEditEvent(null)
        setShowFolderModal(false); setEditFolder(null)
        setMenuEventID(null); setShowMoveMenu(null)
        setShowTagDropdown(false)
        resetEventForm()
      }
    }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [])

  // Close tag dropdown on outside click
  useEffect(() => {
    const h = (e: MouseEvent) => {
      if (tagDropdownRef.current && !tagDropdownRef.current.contains(e.target as Node)) {
        setShowTagDropdown(false)
        setTagSearch('')
      }
    }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [])

  // Close context menus on outside click
  useEffect(() => {
    const h = (e: MouseEvent) => {
      // Only close if the click was NOT on a menu toggle button
      const target = e.target as HTMLElement
      if (target.closest('[data-menu-toggle]')) return
      setMenuEventID(null)
      setShowMoveMenu(null)
    }
    document.addEventListener('click', h)
    return () => document.removeEventListener('click', h)
  }, [])

  // ─── Folder Navigation ──────────────────────────────────────────────────────

  const visibleFolders = folders.filter(f =>
    currentFolderID ? f.parent_id === currentFolderID : !f.parent_id
  )

  // Filter out cancelled events when hideCancelled is true
  const filteredEvents = hideCancelled && !statusFilter ? events.filter(ev => ev.status !== 'cancelled') : events

  // Sorted events for list view
  const sortedEvents = useMemo(() => {
    if (!sortField) return filteredEvents
    return [...filteredEvents].sort((a, b) => {
      let va: string | number = 0, vb: string | number = 0
      switch (sortField) {
        case 'name': va = a.name.toLowerCase(); vb = b.name.toLowerCase(); break
        case 'status': va = a.status; vb = b.status; break
        case 'formula': va = (a.tag_formula || (a.tags && a.tags.length > 0)) ? 1 : 0; vb = (b.tag_formula || (b.tags && b.tags.length > 0)) ? 1 : 0; break
        case 'participants': va = a.total_participants; vb = b.total_participants; break
        case 'asistentes': va = a.stage_counts?.['Asistieron'] || 0; vb = b.stage_counts?.['Asistieron'] || 0; break
        case 'preinscritos': va = a.stage_counts?.['Pre inscritos'] || 0; vb = b.stage_counts?.['Pre inscritos'] || 0; break
        case 'inscritos': va = a.stage_counts?.['Inscrito'] || 0; vb = b.stage_counts?.['Inscrito'] || 0; break
      }
      if (va < vb) return sortDirection === 'asc' ? -1 : 1
      if (va > vb) return sortDirection === 'asc' ? 1 : -1
      return 0
    })
  }, [filteredEvents, sortField, sortDirection])

  const toggleSort = (field: string) => {
    if (sortField === field) {
      setSortDirection(d => d === 'asc' ? 'desc' : 'asc')
    } else {
      setSortField(field)
      setSortDirection('asc')
    }
  }

  const SortIcon = ({ field }: { field: string }) => (
    <span className="inline-flex flex-col ml-1 -space-y-1 text-[8px] leading-none">
      <span className={sortField === field && sortDirection === 'asc' ? 'text-emerald-600' : 'text-slate-300'}>▲</span>
      <span className={sortField === field && sortDirection === 'desc' ? 'text-emerald-600' : 'text-slate-300'}>▼</span>
    </span>
  )

  const navigateIntoFolder = (folder: EventFolder) => {
    setCurrentFolderID(folder.id)
    setFolderPath(prev => [...prev, folder])
    setSearch('')
    setStatusFilter('')
    setLoading(true)
  }

  const navigateToBreadcrumb = (index: number) => {
    if (index === -1) {
      setCurrentFolderID(null)
      setFolderPath([])
    } else {
      setCurrentFolderID(folderPath[index].id)
      setFolderPath(prev => prev.slice(0, index + 1))
    }
    setSearch('')
    setStatusFilter('')
    setLoading(true)
  }

  // ─── Drag & Drop ────────────────────────────────────────────────────────────

  const handleDragStart = (e: React.DragEvent, eventID: string) => {
    dragEventIDRef.current = eventID
    e.dataTransfer.effectAllowed = 'move'
  }

  const handleFolderDragOver = (e: React.DragEvent, folderID: string) => {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    setDragOverFolderID(folderID)
  }

  const handleFolderDrop = async (e: React.DragEvent, targetFolderID: string) => {
    e.preventDefault()
    setDragOverFolderID(null)
    const eventID = dragEventIDRef.current
    if (!eventID) return
    dragEventIDRef.current = null
    await moveEventToFolder(eventID, targetFolderID)
  }

  const moveEventToFolder = async (eventID: string, folderID: string | null) => {
    await fetch(`/api/events/${eventID}/move-folder`, {
      method: 'PATCH',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ folder_id: folderID }),
    })
    fetchEvents()
    fetchFolders()
  }

  // ─── Event CRUD ─────────────────────────────────────────────────────────────

  const resetEventForm = () => {
    setFormData({ name: '', description: '', event_date: '', event_end: '', location: '', color: '#3b82f6', status: 'active', tag_ids: [], formula_mode: 'OR', include_tag_ids: [], exclude_tag_ids: [], tag_formula: '', tag_formula_type: 'simple' })
    setShowTagDropdown(false)
    setTagSearch('')
    setFormulaIsValid(true)
  }

  const openEditEvent = async (ev: Event) => {
    // Fetch tags for this event
    let includeIds: string[] = []
    let excludeIds: string[] = []
    let formulaMode: 'AND' | 'OR' = (ev.tag_formula_mode as 'AND' | 'OR') || 'OR'
    let tagFormula = ''
    let tagFormulaType: 'simple' | 'advanced' = 'simple'
    try {
      const res = await fetch(`/api/events/${ev.id}/tags`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success && data.tags) {
        for (const t of data.tags as TagItem[]) {
          if (t.negate) {
            excludeIds.push(t.id)
          } else {
            includeIds.push(t.id)
          }
        }
        if (data.formula_mode) formulaMode = data.formula_mode
        if (data.tag_formula) tagFormula = data.tag_formula
        if (data.tag_formula_type) tagFormulaType = data.tag_formula_type
      }
    } catch (e) {
      console.error('Failed to fetch event tags:', e)
    }

    setFormData({
      name: ev.name,
      description: ev.description || '',
      event_date: ev.event_date ? new Date(ev.event_date).toISOString().slice(0, 16) : '',
      event_end: ev.event_end ? new Date(ev.event_end).toISOString().slice(0, 16) : '',
      location: ev.location || '',
      color: ev.color,
      status: ev.status,
      tag_ids: [...includeIds, ...excludeIds],
      formula_mode: formulaMode,
      include_tag_ids: includeIds,
      exclude_tag_ids: excludeIds,
      tag_formula: tagFormula,
      tag_formula_type: tagFormulaType,
    })
    setEditEvent(ev)
  }

  const handleCreateEvent = async () => {
    const body: Record<string, unknown> = { ...formData }
    delete body.tag_ids
    delete body.formula_mode
    delete body.include_tag_ids
    delete body.exclude_tag_ids
    delete body.tag_formula
    delete body.tag_formula_type
    if (formData.event_date) body.event_date = new Date(formData.event_date).toISOString()
    else delete body.event_date
    if (formData.event_end) body.event_end = new Date(formData.event_end).toISOString()
    else delete body.event_end
    if (!formData.description) delete body.description
    if (!formData.location) delete body.location
    if (currentFolderID) body.folder_id = currentFolderID

    const res = await fetch('/api/events', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const data = await res.json()
    if (data.success) {
      // Save tags with formula if there's any formula config
      const hasFormula = formData.tag_formula_type === 'advanced'
        ? formData.tag_formula.trim() !== ''
        : (formData.include_tag_ids.length > 0 || formData.exclude_tag_ids.length > 0)
      if (hasFormula && data.event?.id) {
        try {
          await fetch(`/api/events/${data.event.id}/tags`, {
            method: 'PUT',
            headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
            body: JSON.stringify({
              formula_mode: formData.formula_mode,
              include_tag_ids: formData.include_tag_ids,
              exclude_tag_ids: formData.exclude_tag_ids,
              tag_formula: formData.tag_formula,
              tag_formula_type: formData.tag_formula_type,
            }),
          })
        } catch (e) {
          console.error('Failed to save event tags:', e)
        }
      }
      setShowCreate(false)
      resetEventForm()
      fetchEvents()
      fetchFolders()
    }
  }

  const handleUpdateEvent = async () => {
    if (!editEvent) return
    const body: Record<string, unknown> = { ...formData }
    delete body.tag_ids
    delete body.formula_mode
    delete body.include_tag_ids
    delete body.exclude_tag_ids
    delete body.tag_formula
    delete body.tag_formula_type
    if (formData.event_date) body.event_date = new Date(formData.event_date).toISOString()
    else delete body.event_date
    if (formData.event_end) body.event_end = new Date(formData.event_end).toISOString()
    else delete body.event_end
    if (!formData.description) delete body.description
    if (!formData.location) delete body.location

    const res = await fetch(`/api/events/${editEvent.id}`, {
      method: 'PUT',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const data = await res.json()
    if (data.success) {
      // Save tags with formula
      try {
        await fetch(`/api/events/${editEvent.id}/tags`, {
          method: 'PUT',
          headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
          body: JSON.stringify({
            formula_mode: formData.formula_mode,
            include_tag_ids: formData.include_tag_ids,
            exclude_tag_ids: formData.exclude_tag_ids,
            tag_formula: formData.tag_formula,
            tag_formula_type: formData.tag_formula_type,
          }),
        })
      } catch (e) {
        console.error('Failed to save event tags:', e)
      }
      setEditEvent(null)
      resetEventForm()
      fetchEvents()
    }
  }

  const handleDeleteEvent = async (id: string) => {
    if (!confirm('¿Eliminar este evento y todos sus participantes?')) return
    await fetch(`/api/events/${id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${token}` },
    })
    fetchEvents()
    fetchFolders()
  }

  const handleDuplicateEvent = async (id: string) => {
    if (duplicatingEventID) return
    setDuplicatingEventID(id)
    try {
      const res = await fetch(`/api/events/${id}/duplicate`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (!data.success || !data.event?.id) {
        alert(data.error || 'No se pudo duplicar el evento')
        return
      }
      router.push(`/dashboard/events/${data.event.id}`)
    } catch (e) {
      console.error('Failed to duplicate event:', e)
      alert('No se pudo duplicar el evento')
    } finally {
      setDuplicatingEventID(null)
    }
  }

  // ─── Folder CRUD ────────────────────────────────────────────────────────────

  const openCreateFolder = () => {
    setEditFolder(null)
    setFolderForm({ name: '', color: '#3b82f6', icon: '📁' })
    setShowFolderModal(true)
  }

  const openEditFolder = (folder: EventFolder, e: React.MouseEvent) => {
    e.stopPropagation()
    setEditFolder(folder)
    setFolderForm({ name: folder.name, color: folder.color, icon: folder.icon })
    setShowFolderModal(true)
  }

  const handleSaveFolder = async () => {
    if (editFolder) {
      await fetch(`/api/events/folders/${editFolder.id}`, {
        method: 'PUT',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify(folderForm),
      })
    } else {
      await fetch('/api/events/folders', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...folderForm, parent_id: currentFolderID || undefined }),
      })
    }
    setShowFolderModal(false)
    setEditFolder(null)
    fetchFolders()
  }

  const handleDeleteFolder = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (!confirm('¿Eliminar carpeta? Los eventos se moverán a la carpeta padre.')) return
    await fetch(`/api/events/folders/${id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${token}` },
    })
    fetchFolders()
    fetchEvents()
  }

  // ─── Loading ─────────────────────────────────────────────────────────────────

  if (loading && events.length === 0 && folders.length === 0) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-emerald-600" />
      </div>
    )
  }

  // ─── Render Helpers ───────────────────────────────────────────────────────────

  const renderEventForm = (onSubmit: () => void, submitLabel: string) => (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl shadow-2xl w-full max-w-5xl h-[calc(100vh-2rem)] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-200 flex-shrink-0">
          <h2 className="text-lg font-semibold text-slate-900 flex items-center gap-2">
            <CalendarDays className="w-5 h-5 text-emerald-600" />
            {submitLabel === 'Crear' ? 'Nuevo Evento' : 'Editar Evento'}
          </h2>
          <button onClick={() => { setShowCreate(false); setEditEvent(null); resetEventForm() }}
            className="p-1.5 rounded-lg hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body — 2 columns */}
        <div className="flex-1 overflow-y-auto">
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-0 lg:divide-x lg:divide-slate-200 h-full">
            {/* LEFT COLUMN — Event details */}
            <div className="p-6 space-y-4 overflow-y-auto">
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Nombre *</label>
                <input value={formData.name} onChange={e => setFormData({ ...formData, name: e.target.value })} autoFocus
                  className="w-full px-4 py-2.5 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900"
                  placeholder="Ej: Clase Gratuita de Marketing" />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Descripción</label>
                <textarea value={formData.description} onChange={e => setFormData({ ...formData, description: e.target.value })} rows={3}
                  className="w-full px-4 py-2.5 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900"
                  placeholder="Describe la actividad..." />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1">Fecha inicio</label>
                  <input type="datetime-local" value={formData.event_date}
                    onChange={e => setFormData({ ...formData, event_date: e.target.value })}
                    className="w-full px-3 py-2.5 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 text-slate-900 text-sm" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1">Fecha fin</label>
                  <input type="datetime-local" value={formData.event_end}
                    onChange={e => setFormData({ ...formData, event_end: e.target.value })}
                    className="w-full px-3 py-2.5 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 text-slate-900 text-sm" />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Ubicación</label>
                <input value={formData.location} onChange={e => setFormData({ ...formData, location: e.target.value })}
                  className="w-full px-4 py-2.5 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 text-slate-900"
                  placeholder="Ej: Zoom, Oficina central..." />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1">Color</label>
                  <div className="flex flex-wrap gap-2">
                    {COLOR_OPTIONS.map(c => (
                      <button key={c} onClick={() => setFormData({ ...formData, color: c })}
                        className={`w-7 h-7 rounded-full border-2 transition-all ${formData.color === c ? 'border-slate-900 scale-110' : 'border-transparent hover:scale-105'}`}
                        style={{ backgroundColor: c }} />
                    ))}
                  </div>
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-700 mb-1">Estado</label>
                  <select value={formData.status} onChange={e => setFormData({ ...formData, status: e.target.value })}
                    className="w-full px-4 py-2.5 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 text-slate-900">
                    {STATUS_OPTIONS.map(s => <option key={s.value} value={s.value}>{s.label} — {s.desc}</option>)}
                  </select>
                  {(() => {
                    const opt = STATUS_OPTIONS.find(s => s.value === formData.status)
                    if (!opt) return null
                    return (
                      <p className={`text-xs mt-1.5 flex items-center gap-1 ${opt.syncing ? 'text-emerald-600' : 'text-slate-400'}`}>
                        <span className={`w-1.5 h-1.5 rounded-full ${opt.syncing ? 'bg-emerald-500 animate-pulse' : 'bg-slate-300'}`} />
                        {opt.syncing ? 'Sincronización automática activa' : 'Sincronización pausada'}
                      </p>
                    )
                  })()}
                </div>
              </div>
            </div>

            {/* RIGHT COLUMN — Tag formula configuration */}
            <div className="p-6 overflow-y-auto bg-slate-50/50">
              <div className="flex items-center gap-2 mb-1">
                <Tag className="w-4 h-4 text-emerald-600" />
                <span className="text-sm font-semibold text-slate-800">Fórmula de etiquetas (auto-sync)</span>
              </div>
              <p className="text-xs text-slate-500 mb-4">
                Define qué leads se agregan como participantes automáticamente.
              </p>

              {/* Simple / Advanced toggle tabs */}
              <div className="flex rounded-lg border border-slate-200 bg-white mb-4 overflow-hidden">
                <button type="button"
                  onClick={() => setFormData({ ...formData, tag_formula_type: 'simple' })}
                  className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-xs font-semibold transition-colors ${
                    formData.tag_formula_type === 'simple'
                      ? 'bg-emerald-500 text-white'
                      : 'text-slate-600 hover:bg-slate-50'
                  }`}>
                  <FileText className="w-3.5 h-3.5" />
                  Simple
                </button>
                <button type="button"
                  onClick={() => setFormData({ ...formData, tag_formula_type: 'advanced' })}
                  className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-xs font-semibold transition-colors ${
                    formData.tag_formula_type === 'advanced'
                      ? 'bg-violet-500 text-white'
                      : 'text-slate-600 hover:bg-slate-50'
                  }`}>
                  <Code className="w-3.5 h-3.5" />
                  Avanzado
                </button>
              </div>

              {/* ─── SIMPLE MODE ─── */}
              {formData.tag_formula_type === 'simple' && (
                <div className="space-y-3">
                  {/* Formula mode toggle */}
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-medium text-slate-500">Modo:</span>
                    <div className="inline-flex rounded-lg border border-slate-200 overflow-hidden">
                      <button type="button"
                        onClick={() => setFormData({ ...formData, formula_mode: 'OR' })}
                        className={`px-3 py-1 text-xs font-semibold transition-colors ${
                          formData.formula_mode === 'OR'
                            ? 'bg-emerald-500 text-white'
                            : 'bg-white text-slate-600 hover:bg-slate-50'
                        }`}>
                        OR (cualquiera)
                      </button>
                      <button type="button"
                        onClick={() => setFormData({ ...formData, formula_mode: 'AND' })}
                        className={`px-3 py-1 text-xs font-semibold transition-colors ${
                          formData.formula_mode === 'AND'
                            ? 'bg-blue-500 text-white'
                            : 'bg-white text-slate-600 hover:bg-slate-50'
                        }`}>
                        AND (todas)
                      </button>
                    </div>
                    <span className="text-[10px] text-slate-400">
                      {formData.formula_mode === 'AND' ? 'Lead debe tener TODAS' : 'Lead debe tener al menos UNA'}
                    </span>
                  </div>

                  {/* Formula preview */}
                  {(formData.include_tag_ids.length > 0 || formData.exclude_tag_ids.length > 0) && (
                    <div className="p-2.5 bg-white rounded-lg border border-slate-200">
                      <div className="text-[10px] font-medium text-slate-400 mb-1">FÓRMULA:</div>
                      <div className="flex flex-wrap items-center gap-1 text-xs">
                        {formData.include_tag_ids.map((id, idx) => {
                          const tag = availableTags.find(t => t.id === id)
                          if (!tag) return null
                          return (
                            <span key={id} className="flex items-center gap-0.5">
                              {idx > 0 && <span className="font-bold text-slate-500 mx-0.5">{formData.formula_mode}</span>}
                              <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-white text-[10px] font-medium" style={{ backgroundColor: tag.color }}>{tag.name}</span>
                            </span>
                          )
                        })}
                        {formData.exclude_tag_ids.map(id => {
                          const tag = availableTags.find(t => t.id === id)
                          if (!tag) return null
                          return (
                            <span key={id} className="flex items-center gap-0.5">
                              <span className="font-bold text-red-500 mx-0.5">NOT</span>
                              <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-white text-[10px] font-medium line-through opacity-75" style={{ backgroundColor: tag.color }}>{tag.name}</span>
                            </span>
                          )
                        })}
                      </div>
                    </div>
                  )}

                  {/* Include tags */}
                  <div>
                    <div className="text-xs font-medium text-emerald-600 mb-1 flex items-center gap-1">
                      <Check className="w-3 h-3" /> Incluir ({formData.formula_mode === 'AND' ? 'TODAS requeridas' : 'cualquiera'})
                    </div>
                    {formData.include_tag_ids.length > 0 && (
                      <div className="flex flex-wrap gap-1.5 mb-1.5">
                        {formData.include_tag_ids.map(id => {
                          const tag = availableTags.find(t => t.id === id)
                          if (!tag) return null
                          return (
                            <span key={id} className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium text-white"
                              style={{ backgroundColor: tag.color }}>
                              {tag.name}
                              <button type="button" onClick={() => setFormData({
                                ...formData,
                                include_tag_ids: formData.include_tag_ids.filter(tid => tid !== id),
                                tag_ids: formData.tag_ids.filter(tid => tid !== id),
                              })}
                                className="hover:bg-white/20 rounded-full p-0.5 transition-colors">
                                <X className="w-3 h-3" />
                              </button>
                            </span>
                          )
                        })}
                      </div>
                    )}
                  </div>

                  {/* Exclude tags */}
                  <div>
                    <div className="text-xs font-medium text-red-500 mb-1 flex items-center gap-1">
                      <X className="w-3 h-3" /> Excluir (NOT)
                    </div>
                    {formData.exclude_tag_ids.length > 0 && (
                      <div className="flex flex-wrap gap-1.5 mb-1.5">
                        {formData.exclude_tag_ids.map(id => {
                          const tag = availableTags.find(t => t.id === id)
                          if (!tag) return null
                          return (
                            <span key={id} className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium text-white opacity-75 line-through"
                              style={{ backgroundColor: tag.color }}>
                              {tag.name}
                              <button type="button" onClick={() => setFormData({
                                ...formData,
                                exclude_tag_ids: formData.exclude_tag_ids.filter(tid => tid !== id),
                                tag_ids: formData.tag_ids.filter(tid => tid !== id),
                              })}
                                className="hover:bg-white/20 rounded-full p-0.5 transition-colors no-underline">
                                <X className="w-3 h-3" />
                              </button>
                            </span>
                          )
                        })}
                      </div>
                    )}
                  </div>

                  {/* Tag dropdown */}
                  <div className="relative" ref={tagDropdownRef}>
                    <button type="button" onClick={() => setShowTagDropdown(!showTagDropdown)}
                      className={`w-full flex items-center justify-between px-3 py-2 border rounded-lg text-sm transition-colors ${
                        showTagDropdown ? 'border-emerald-500 ring-2 ring-emerald-500/20' : 'border-slate-300 hover:border-slate-400'
                      }`}>
                      <span className="text-slate-500">
                        {(formData.include_tag_ids.length + formData.exclude_tag_ids.length) === 0
                          ? 'Seleccionar etiquetas...'
                          : `${formData.include_tag_ids.length} incluidas, ${formData.exclude_tag_ids.length} excluidas`}
                      </span>
                      <ChevronDown className={`w-4 h-4 text-slate-400 transition-transform ${showTagDropdown ? 'rotate-180' : ''}`} />
                    </button>
                    {showTagDropdown && (
                      <div className="absolute top-full left-0 right-0 mt-1 bg-white border border-slate-200 rounded-xl shadow-lg z-50 overflow-hidden">
                        <div className="p-2 border-b border-slate-100">
                          <input value={tagSearch} onChange={e => setTagSearch(e.target.value)}
                            placeholder="Buscar etiqueta..." autoFocus
                            className="w-full px-3 py-1.5 text-sm border border-slate-200 rounded-lg focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900" />
                        </div>
                        <div className="max-h-48 overflow-y-auto">
                          {availableTags
                            .filter(t => !tagSearch || t.name.toLowerCase().includes(tagSearch.toLowerCase()))
                            .map(tag => {
                              const isInclude = formData.include_tag_ids.includes(tag.id)
                              const isExclude = formData.exclude_tag_ids.includes(tag.id)
                              return (
                                <div key={tag.id} className={`flex items-center gap-2 px-3 py-2 text-sm hover:bg-slate-50 transition-colors ${isInclude || isExclude ? 'bg-slate-50/50' : ''}`}>
                                  <div className="w-3 h-3 rounded-full flex-shrink-0" style={{ backgroundColor: tag.color }} />
                                  <span className="flex-1 text-left text-slate-700">{tag.name}</span>
                                  <button type="button"
                                    onClick={() => {
                                      if (isInclude) {
                                        setFormData({ ...formData,
                                          include_tag_ids: formData.include_tag_ids.filter(id => id !== tag.id),
                                          tag_ids: formData.tag_ids.filter(id => id !== tag.id),
                                        })
                                      } else {
                                        setFormData({ ...formData,
                                          include_tag_ids: [...formData.include_tag_ids, tag.id],
                                          exclude_tag_ids: formData.exclude_tag_ids.filter(id => id !== tag.id),
                                          tag_ids: [...formData.tag_ids.filter(id => id !== tag.id), tag.id],
                                        })
                                      }
                                    }}
                                    className={`px-2 py-0.5 rounded text-[10px] font-semibold transition-colors ${
                                      isInclude ? 'bg-emerald-500 text-white' : 'bg-slate-100 text-slate-500 hover:bg-emerald-100 hover:text-emerald-700'
                                    }`}>
                                    {isInclude ? '✓ Incluida' : '+ Incluir'}
                                  </button>
                                  <button type="button"
                                    onClick={() => {
                                      if (isExclude) {
                                        setFormData({ ...formData,
                                          exclude_tag_ids: formData.exclude_tag_ids.filter(id => id !== tag.id),
                                          tag_ids: formData.tag_ids.filter(id => id !== tag.id),
                                        })
                                      } else {
                                        setFormData({ ...formData,
                                          exclude_tag_ids: [...formData.exclude_tag_ids, tag.id],
                                          include_tag_ids: formData.include_tag_ids.filter(id => id !== tag.id),
                                          tag_ids: [...formData.tag_ids.filter(id => id !== tag.id), tag.id],
                                        })
                                      }
                                    }}
                                    className={`px-2 py-0.5 rounded text-[10px] font-semibold transition-colors ${
                                      isExclude ? 'bg-red-500 text-white' : 'bg-slate-100 text-slate-500 hover:bg-red-100 hover:text-red-700'
                                    }`}>
                                    {isExclude ? '✗ Excluida' : '− Excluir'}
                                  </button>
                                </div>
                              )
                            })}
                          {availableTags.filter(t => !tagSearch || t.name.toLowerCase().includes(tagSearch.toLowerCase())).length === 0 && (
                            <p className="px-3 py-3 text-xs text-slate-400 text-center">No se encontraron etiquetas</p>
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* ─── ADVANCED MODE ─── */}
              {formData.tag_formula_type === 'advanced' && (
                <div className="space-y-3">
                  {/* Syntax reference */}
                  <div className="p-3 bg-white rounded-lg border border-slate-200">
                    <div className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider mb-1.5">Referencia de sintaxis</div>
                    <div className="grid grid-cols-1 gap-1 text-xs text-slate-600">
                      <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">&quot;etiqueta&quot;</code> — coincidencia exacta</div>
                      <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">&quot;mar%&quot;</code> — empieza con &quot;mar&quot; (comodín)</div>
                      <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">and</code> — el lead debe tener ambas</div>
                      <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">or</code> — el lead debe tener al menos una</div>
                      <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">not</code> — excluir leads con esta etiqueta</div>
                      <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">not in (...)</code> — excluir una lista</div>
                      <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">( )</code> — agrupar expresiones</div>
                    </div>
                    <div className="mt-2 pt-2 border-t border-slate-100">
                      <div className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider mb-1">Ejemplo</div>
                      <code className="text-[11px] text-emerald-700 bg-emerald-50 px-2 py-1 rounded block">
                        {`"kommo" and not in ("iquitos" or "conf_03-jun" or "interesados junio")`}
                      </code>
                    </div>
                  </div>

                  {/* Formula editor with autocomplete & token validation */}
                  <FormulaEditor
                    value={formData.tag_formula}
                    onChange={(v) => setFormData({ ...formData, tag_formula: v })}
                    tags={availableTags}
                    onValidChange={setFormulaIsValid}
                  />
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-3 px-6 py-4 border-t border-slate-200 flex-shrink-0 bg-white rounded-b-2xl">
          <button onClick={() => { setShowCreate(false); setEditEvent(null); resetEventForm() }}
            className="px-4 py-2.5 text-slate-600 hover:bg-slate-100 rounded-lg transition-colors font-medium">Cancelar</button>
          <button disabled={!formData.name || (formData.tag_formula_type === 'advanced' && !formulaIsValid)}
            onClick={onSubmit}
            className="px-6 py-2.5 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors font-medium">
            {submitLabel}
          </button>
        </div>
      </div>
    </div>
  )

  const renderFolderModal = () => (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      onClick={() => setShowFolderModal(false)}>
      <div className="bg-white rounded-xl shadow-xl w-full max-w-sm" onClick={e => e.stopPropagation()}>
        <div className="p-6">
          <h2 className="text-lg font-semibold text-slate-900 mb-4">
            {editFolder ? 'Editar carpeta' : 'Nueva carpeta'}
          </h2>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-slate-700 mb-1">Nombre *</label>
              <input value={folderForm.name} onChange={e => setFolderForm({ ...folderForm, name: e.target.value })} autoFocus
                className="w-full px-4 py-2 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 text-slate-900"
                placeholder="Ej: Eventos 2025" />
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-700 mb-2">Ícono</label>
              <div className="flex flex-wrap gap-2">
                {FOLDER_ICONS.map(icon => (
                  <button key={icon} onClick={() => setFolderForm({ ...folderForm, icon })}
                    className={`w-10 h-10 text-xl rounded-lg border-2 transition-all flex items-center justify-center ${folderForm.icon === icon ? 'border-emerald-500 bg-emerald-50 scale-110' : 'border-transparent hover:border-slate-200 hover:bg-slate-50'}`}>
                    {icon}
                  </button>
                ))}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-700 mb-2">Color</label>
              <div className="flex flex-wrap gap-2">
                {COLOR_OPTIONS.map(c => (
                  <button key={c} onClick={() => setFolderForm({ ...folderForm, color: c })}
                    className={`w-7 h-7 rounded-full border-2 transition-all ${folderForm.color === c ? 'border-slate-900 scale-110' : 'border-transparent hover:scale-105'}`}
                    style={{ backgroundColor: c }} />
                ))}
              </div>
            </div>
          </div>
          <div className="flex justify-end gap-3 mt-6">
            <button onClick={() => setShowFolderModal(false)}
              className="px-4 py-2 text-slate-600 hover:bg-slate-100 rounded-lg transition-colors">Cancelar</button>
            <button disabled={!folderForm.name} onClick={handleSaveFolder}
              className="px-6 py-2 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 disabled:opacity-50 transition-colors">
              {editFolder ? 'Guardar' : 'Crear'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )

  // ─── JSX ─────────────────────────────────────────────────────────────────────

  return (
    <div ref={eventsScrollRef} className="space-y-5 overflow-y-auto flex-1 min-h-0 pb-6">

      {/* Header */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-slate-900">Eventos</h1>
          <p className="text-slate-500 text-sm mt-0.5">Gestiona actividades y haz seguimiento a tus contactos</p>
        </div>
        <div className="flex gap-2">
          <button onClick={openCreateFolder}
            className="flex items-center gap-2 px-4 py-2.5 bg-white border border-slate-300 text-slate-700 rounded-lg hover:bg-slate-50 transition-colors shadow-sm text-sm font-medium">
            <FolderPlus className="w-4 h-4" />
            Nueva carpeta
          </button>
          <button onClick={() => { resetEventForm(); setShowCreate(true) }}
            className="flex items-center gap-2 px-4 py-2.5 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 transition-colors shadow-sm text-sm font-medium">
            <Plus className="w-4 h-4" />
            Nuevo Evento
          </button>
        </div>
      </div>

      {/* Breadcrumb */}
      {folderPath.length > 0 && (
        <nav className="flex items-center gap-1 text-sm">
          <button onClick={() => navigateToBreadcrumb(-1)}
            className="flex items-center gap-1.5 px-2.5 py-1 text-slate-500 hover:text-emerald-700 hover:bg-emerald-50 rounded-lg transition-colors">
            <Home className="w-3.5 h-3.5" />
            <span>Eventos</span>
          </button>
          {folderPath.map((folder, i) => (
            <div key={folder.id} className="flex items-center gap-1">
              <ChevronRight className="w-3.5 h-3.5 text-slate-400" />
              <button onClick={() => navigateToBreadcrumb(i)}
                className={`flex items-center gap-1.5 px-2.5 py-1 rounded-lg transition-colors font-medium ${i === folderPath.length - 1 ? 'text-slate-900 bg-slate-100 cursor-default' : 'text-slate-500 hover:text-emerald-700 hover:bg-emerald-50'}`}>
                <span className="text-base leading-none">{folder.icon}</span>
                {folder.name}
              </button>
            </div>
          ))}
        </nav>
      )}

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-3">
        {folderPath.length > 0 && (
          <button onClick={() => navigateToBreadcrumb(folderPath.length - 2)}
            className="flex items-center gap-1.5 px-3 py-2 text-sm text-slate-600 hover:bg-slate-100 rounded-lg border border-slate-200 transition-colors">
            <ArrowLeft className="w-4 h-4" />
            Atrás
          </button>
        )}
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
          <input value={search} onChange={e => setSearch(e.target.value)} placeholder="Buscar eventos..."
            className="w-full pl-10 pr-4 py-2 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900" />
        </div>
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)}
          className="px-4 py-2 border border-slate-300 rounded-lg focus:ring-2 focus:ring-emerald-500 text-slate-900">
          <option value="">Todos los estados</option>
          {STATUS_OPTIONS.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
        </select>
        <button
          onClick={() => setHideCancelled(!hideCancelled)}
          className={`flex items-center gap-1.5 px-3 py-2 border rounded-lg text-sm transition-colors whitespace-nowrap ${
            hideCancelled ? 'border-red-200 bg-red-50 text-red-600' : 'border-slate-300 text-slate-600 hover:bg-slate-50'
          }`}
          title={hideCancelled ? 'Mostrar cancelados' : 'Ocultar cancelados'}
        >
          {hideCancelled ? <AlertCircle className="w-3.5 h-3.5" /> : <CheckCircle2 className="w-3.5 h-3.5" />}
          {hideCancelled ? 'Cancelados ocultos' : 'Todos visibles'}
        </button>
        <div className="flex bg-slate-100 rounded-lg p-0.5">
          <button onClick={() => setViewMode('grid')} title="Cuadrícula"
            className={`p-2 rounded-md transition-colors ${viewMode === 'grid' ? 'bg-white shadow text-slate-900' : 'text-slate-500 hover:text-slate-700'}`}>
            <LayoutGrid className="w-4 h-4" />
          </button>
          <button onClick={() => setViewMode('compact')} title="Compacta"
            className={`p-2 rounded-md transition-colors ${viewMode === 'compact' ? 'bg-white shadow text-slate-900' : 'text-slate-500 hover:text-slate-700'}`}>
            <LayoutTemplate className="w-4 h-4" />
          </button>
          <button onClick={() => setViewMode('list')} title="Lista"
            className={`p-2 rounded-md transition-colors ${viewMode === 'list' ? 'bg-white shadow text-slate-900' : 'text-slate-500 hover:text-slate-700'}`}>
            <List className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* ─── Folders section ──────────────────────────────────────────────────── */}
      {visibleFolders.length > 0 && (
        <div>
          <p className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-3">Carpetas</p>
          <div className="grid gap-3 grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {visibleFolders.map(folder => (
              <div key={folder.id}
                onDragOver={e => handleFolderDragOver(e, folder.id)}
                onDragLeave={() => setDragOverFolderID(null)}
                onDrop={e => handleFolderDrop(e, folder.id)}
                onClick={() => navigateIntoFolder(folder)}
                className={`relative group bg-white border-2 rounded-xl p-4 cursor-pointer transition-all select-none ${
                  dragOverFolderID === folder.id
                    ? 'border-emerald-400 bg-emerald-50 shadow-md scale-[1.02]'
                    : 'border-slate-200 hover:border-slate-300 hover:shadow-sm'
                }`}>
                <div className="absolute top-0 left-0 right-0 h-1 rounded-t-xl" style={{ backgroundColor: folder.color }} />
                <div className="flex items-start justify-between mt-1">
                  <span className="text-3xl leading-none">{folder.icon}</span>
                  <button
                    data-menu-toggle
                    onClick={e => { e.stopPropagation(); setMenuEventID(menuEventID === `f-${folder.id}` ? null : `f-${folder.id}`) }}
                    className="opacity-0 group-hover:opacity-100 p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-md transition-all">
                    <MoreHorizontal className="w-4 h-4" />
                  </button>
                </div>
                <p className="mt-2 text-sm font-semibold text-slate-800 truncate">{folder.name}</p>
                <p className="text-xs text-slate-400 mt-0.5">{folder.event_count} evento{folder.event_count !== 1 ? 's' : ''}</p>
                {menuEventID === `f-${folder.id}` && (
                  <div className="absolute top-8 right-2 z-20 bg-white border border-slate-200 rounded-xl shadow-lg py-1 min-w-[120px]" onClick={e => e.stopPropagation()}>
                    <button onClick={e => openEditFolder(folder, e)}
                      className="w-full flex items-center gap-2 px-4 py-2 text-sm text-slate-700 hover:bg-slate-50">
                      <Edit2 className="w-3.5 h-3.5" /> Editar
                    </button>
                    <button onClick={e => handleDeleteFolder(folder.id, e)}
                      className="w-full flex items-center gap-2 px-4 py-2 text-sm text-red-600 hover:bg-red-50">
                      <Trash2 className="w-3.5 h-3.5" /> Eliminar
                    </button>
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Events section label */}
      {(visibleFolders.length > 0 || folderPath.length > 0) && (
        <p className="text-xs font-semibold text-slate-400 uppercase tracking-wider mt-5 mb-3">
          {folderPath.length > 0 ? `Eventos en "${folderPath[folderPath.length - 1].name}"` : 'Eventos sin carpeta'}
        </p>
      )}

      {/* ─── Events ──────────────────────────────────────────────────────────── */}
      {filteredEvents.length === 0 && visibleFolders.length === 0 ? (
        <div className="text-center py-16 bg-white rounded-xl border border-slate-200">
          <CalendarDays className="w-12 h-12 text-slate-300 mx-auto mb-3" />
          <p className="text-slate-500 font-medium">No hay eventos{hideCancelled && events.length > 0 ? ' visibles' : ''}</p>
          <p className="text-slate-400 text-sm mt-1">{hideCancelled && events.length > 0 ? 'Hay eventos cancelados ocultos' : 'Crea tu primer evento para empezar'}</p>
          {hideCancelled && events.length > 0 && (
            <button onClick={() => setHideCancelled(false)} className="mt-2 text-sm text-emerald-600 hover:text-emerald-700 font-medium">Mostrar cancelados</button>
          )}
        </div>
      ) : filteredEvents.length === 0 ? (
        <div className="text-center py-10 bg-white rounded-xl border border-dashed border-slate-200">
          <FolderOpen className="w-10 h-10 text-slate-300 mx-auto mb-2" />
          <p className="text-slate-500 text-sm">No hay eventos aquí</p>
          <p className="text-slate-400 text-xs mt-0.5">Arrastra eventos a esta carpeta o crea uno nuevo</p>
        </div>
      ) : viewMode === 'list' ? (
        /* List View */
        <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-slate-200 bg-slate-50">
                <th onClick={() => toggleSort('name')} className="text-left text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3 cursor-pointer hover:text-slate-700 select-none">Evento<SortIcon field="name" /></th>
                <th onClick={() => toggleSort('status')} className="text-left text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3 cursor-pointer hover:text-slate-700 select-none">Estado<SortIcon field="status" /></th>
                <th onClick={() => toggleSort('formula')} className="text-center text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3 cursor-pointer hover:text-slate-700 select-none">Fórmula<SortIcon field="formula" /></th>
                <th onClick={() => toggleSort('participants')} className="text-center text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3 cursor-pointer hover:text-slate-700 select-none">Part.<SortIcon field="participants" /></th>
                <th onClick={() => toggleSort('asistentes')} className="text-center text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3 cursor-pointer hover:text-slate-700 select-none">Asistentes<SortIcon field="asistentes" /></th>
                <th onClick={() => toggleSort('preinscritos')} className="text-center text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3 cursor-pointer hover:text-slate-700 select-none">Pre inscritos<SortIcon field="preinscritos" /></th>
                <th onClick={() => toggleSort('inscritos')} className="text-center text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3 cursor-pointer hover:text-slate-700 select-none">Inscritos<SortIcon field="inscritos" /></th>
                <th className="text-left text-xs font-semibold text-slate-500 uppercase tracking-wider px-4 py-3">Acciones</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {sortedEvents.map(ev => {
                const statusOpt = STATUS_OPTIONS.find(s => s.value === ev.status)
                return (
                  <tr key={ev.id} draggable onDragStart={e => handleDragStart(e, ev.id)}
                    className="hover:bg-slate-50 transition-colors cursor-pointer"
                    onClick={() => router.push(`/dashboard/events/${ev.id}${currentFolderID ? `?folder=${currentFolderID}` : ''}`)}>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5">
                        <div className="w-2.5 h-2.5 rounded-full flex-shrink-0" style={{ backgroundColor: ev.color }} />
                        <div className="min-w-0">
                          <p className="text-sm font-medium text-slate-900 truncate">{ev.name}</p>
                          {ev.description && <p className="text-xs text-slate-500 truncate max-w-[220px]">{ev.description}</p>}
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      {statusOpt && (
                        <span className={`${statusOpt.color} text-xs font-medium px-2 py-1 rounded-full`}>{statusOpt.label}</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-center">
                      {(ev.tag_formula || (ev.tags && ev.tags.length > 0)) ? (
                        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium bg-emerald-100 text-emerald-700">Sí</span>
                      ) : (
                        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium bg-slate-100 text-slate-400">No</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-center">
                      <span className="text-sm font-medium text-slate-700">{ev.total_participants}</span>
                    </td>
                    <td className="px-4 py-3 text-center">
                      <span className="text-sm font-medium text-emerald-600">{ev.stage_counts?.['Asistieron'] || 0}</span>
                    </td>
                    <td className="px-4 py-3 text-center">
                      <span className="text-sm font-medium text-amber-600">{ev.stage_counts?.['Pre inscritos'] || 0}</span>
                    </td>
                    <td className="px-4 py-3 text-center">
                      <span className="text-sm font-medium text-indigo-600">{ev.stage_counts?.['Inscrito'] || 0}</span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-1">
                        <button onClick={e => { e.stopPropagation(); router.push(`/dashboard/events/${ev.id}${currentFolderID ? `?folder=${currentFolderID}` : ''}`) }}
                          className="p-1.5 text-emerald-600 hover:bg-emerald-50 rounded-lg transition-colors" title="Ver detalle">
                          <Eye className="w-4 h-4" />
                        </button>
                        <div className="relative">
                          <button data-menu-toggle onClick={e => { e.stopPropagation(); setShowMoveMenu(showMoveMenu === ev.id ? null : ev.id) }}
                            className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors" title="Mover">
                            <MoveRight className="w-4 h-4" />
                          </button>
                          {showMoveMenu === ev.id && (
                            <div className="absolute right-0 z-20 bg-white border border-slate-200 rounded-xl shadow-lg py-1 min-w-[160px]" onClick={e => e.stopPropagation()}>
                              <p className="px-3 py-1.5 text-xs font-semibold text-slate-400 uppercase">Mover a</p>
                              {ev.folder_id && (
                                <button onClick={() => { moveEventToFolder(ev.id, null); setShowMoveMenu(null) }}
                                  className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-50">
                                  <Home className="w-3.5 h-3.5" /> Sin carpeta
                                </button>
                              )}
                              {folders.filter(f => f.id !== ev.folder_id).map(f => (
                                <button key={f.id} onClick={() => { moveEventToFolder(ev.id, f.id); setShowMoveMenu(null) }}
                                  className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-50">
                                  <span className="text-base">{f.icon}</span> {f.name}
                                </button>
                              ))}
                            </div>
                          )}
                        </div>
                        <button onClick={e => { e.stopPropagation(); openEditEvent(ev) }}
                          className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors">
                          <Edit2 className="w-4 h-4" />
                        </button>
                        <button
                          onClick={e => { e.stopPropagation(); handleDuplicateEvent(ev.id) }}
                          disabled={duplicatingEventID === ev.id}
                          className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors disabled:opacity-50"
                          title="Duplicar"
                        >
                          <Copy className="w-4 h-4" />
                        </button>
                        <button onClick={e => { e.stopPropagation(); handleDeleteEvent(ev.id) }}
                          className="p-1.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors">
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      ) : viewMode === 'compact' ? (
        /* Compact View */
        <div className="grid gap-3 grid-cols-2 sm:grid-cols-3 lg:grid-cols-4">
          {filteredEvents.map(ev => {
            const statusOpt = STATUS_OPTIONS.find(s => s.value === ev.status)
            return (
              <div key={ev.id} draggable onDragStart={e => handleDragStart(e, ev.id)}
                className="bg-white border border-slate-200 rounded-xl overflow-hidden hover:shadow-md hover:border-slate-300 transition-all group cursor-pointer"
                onClick={() => router.push(`/dashboard/events/${ev.id}${currentFolderID ? `?folder=${currentFolderID}` : ''}`)}>
                <div className="h-1.5" style={{ backgroundColor: ev.color }} />
                <div className="p-3.5">
                  <div className="flex items-start justify-between gap-1">
                    <p className="text-sm font-semibold text-slate-800 leading-snug line-clamp-2 flex-1">{ev.name}</p>
                    <button onClick={e => { e.stopPropagation(); openEditEvent(ev) }}
                      className="opacity-0 group-hover:opacity-100 flex-shrink-0 p-0.5 text-slate-400 hover:text-slate-600 rounded">
                      <Edit2 className="w-3.5 h-3.5" />
                    </button>
                    <button
                      onClick={e => { e.stopPropagation(); handleDuplicateEvent(ev.id) }}
                      disabled={duplicatingEventID === ev.id}
                      className="opacity-0 group-hover:opacity-100 flex-shrink-0 p-0.5 text-slate-400 hover:text-slate-600 rounded disabled:opacity-50"
                      title="Duplicar"
                    >
                      <Copy className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  {(ev.tag_formula || (ev.tags && ev.tags.length > 0)) && (
                    <div className="mt-1.5">
                      <FormulaTagDisplay ev={ev} maxTags={2} size="xs" />
                    </div>
                  )}
                  {ev.event_date && (
                    <div className="flex items-center gap-1 mt-2 text-xs text-slate-500">
                      <Clock className="w-3 h-3" />
                      {format(new Date(ev.event_date), "d MMM", { locale: es })}
                    </div>
                  )}
                  <div className="flex items-center justify-between mt-2">
                    <div className="flex items-center gap-1 text-xs text-slate-500">
                      <Users className="w-3 h-3" />{ev.total_participants}
                    </div>
                    {statusOpt && (
                      <span className={`${statusOpt.color} text-xs font-medium px-1.5 py-0.5 rounded-full`}>{statusOpt.label}</span>
                    )}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      ) : (
        /* Grid View */
        <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {filteredEvents.map(ev => {
            const statusOpt = STATUS_OPTIONS.find(s => s.value === ev.status)
            const counts = ev.participant_counts || {}
            const total = ev.total_participants || 0
            return (
              <div key={ev.id} draggable onDragStart={e => handleDragStart(e, ev.id)}
                className="bg-white rounded-xl border border-slate-200 overflow-hidden hover:shadow-md hover:border-slate-300 transition-all group cursor-grab active:cursor-grabbing">
                <div className="h-1.5" style={{ backgroundColor: ev.color }} />
                <div className="p-5">
                  <div className="flex items-start justify-between mb-3">
                    <div className="flex-1 min-w-0">
                      <h3 className="font-semibold text-slate-900 truncate">{ev.name}</h3>
                      {ev.description && <p className="text-slate-500 text-sm mt-0.5 line-clamp-2">{ev.description}</p>}
                    </div>
                    <div className="flex items-center gap-1 ml-2 flex-shrink-0">
                      {statusOpt && (
                        <span className={`${statusOpt.color} text-xs font-medium px-2 py-1 rounded-full`}>{statusOpt.label}</span>
                      )}
                      <div className="relative">
                        <button data-menu-toggle onClick={e => { e.stopPropagation(); setMenuEventID(menuEventID === ev.id ? null : ev.id) }}
                          className="p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg opacity-0 group-hover:opacity-100 transition-all">
                          <MoreHorizontal className="w-4 h-4" />
                        </button>
                        {menuEventID === ev.id && (
                          <div className="absolute right-0 top-7 z-20 bg-white border border-slate-200 rounded-xl shadow-lg py-1 min-w-[160px]" onClick={e => e.stopPropagation()}>
                            <button onClick={() => { router.push(`/dashboard/events/${ev.id}${currentFolderID ? `?folder=${currentFolderID}` : ''}`); setMenuEventID(null) }}
                              className="w-full flex items-center gap-2 px-4 py-2 text-sm text-slate-700 hover:bg-slate-50">
                              <Eye className="w-3.5 h-3.5" /> Ver detalle
                            </button>
                            <button onClick={() => { openEditEvent(ev); setMenuEventID(null) }}
                              className="w-full flex items-center gap-2 px-4 py-2 text-sm text-slate-700 hover:bg-slate-50">
                              <Edit2 className="w-3.5 h-3.5" /> Editar
                            </button>
                            <button
                              onClick={() => { handleDuplicateEvent(ev.id); setMenuEventID(null) }}
                              disabled={duplicatingEventID === ev.id}
                              className="w-full flex items-center gap-2 px-4 py-2 text-sm text-slate-700 hover:bg-slate-50 disabled:opacity-50"
                            >
                              <Copy className="w-3.5 h-3.5" /> Duplicar
                            </button>
                            <div className="border-t border-slate-100 my-1" />
                            <p className="px-3 py-1 text-xs font-semibold text-slate-400 uppercase">Mover a</p>
                            {ev.folder_id && (
                              <button onClick={() => { moveEventToFolder(ev.id, null); setMenuEventID(null) }}
                                className="w-full flex items-center gap-2 px-4 py-2 text-sm text-slate-600 hover:bg-slate-50">
                                <Home className="w-3.5 h-3.5" /> Sin carpeta
                              </button>
                            )}
                            {folders.filter(f => f.id !== ev.folder_id).map(f => (
                              <button key={f.id} onClick={() => { moveEventToFolder(ev.id, f.id); setMenuEventID(null) }}
                                className="w-full flex items-center gap-2 px-4 py-2 text-sm text-slate-600 hover:bg-slate-50">
                                <span className="text-base">{f.icon}</span> {f.name}
                              </button>
                            ))}
                            <div className="border-t border-slate-100 my-1" />
                            <button onClick={() => { handleDeleteEvent(ev.id); setMenuEventID(null) }}
                              className="w-full flex items-center gap-2 px-4 py-2 text-sm text-red-600 hover:bg-red-50">
                              <Trash2 className="w-3.5 h-3.5" /> Eliminar
                            </button>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>

                  <div className="space-y-1.5 text-sm text-slate-500 mb-4">
                    {ev.event_date && (
                      <div className="flex items-center gap-2">
                        <Clock className="w-3.5 h-3.5" />
                        <span>{format(new Date(ev.event_date), "d MMM yyyy, HH:mm", { locale: es })}</span>
                      </div>
                    )}
                    {ev.location && (
                      <div className="flex items-center gap-2">
                        <MapPin className="w-3.5 h-3.5" />
                        <span className="truncate">{ev.location}</span>
                      </div>
                    )}
                    <div className="flex items-center gap-2">
                      <Users className="w-3.5 h-3.5" />
                      <span>{total} participantes</span>
                    </div>
                  </div>

                  {/* Formula / tags display */}
                  {(ev.tag_formula || (ev.tags && ev.tags.length > 0)) && (
                    <div className="mb-3">
                      <FormulaTagDisplay ev={ev} maxTags={4} size="sm" />
                    </div>
                  )}

                  {total > 0 && (
                    <div className="mb-4">
                      <div className="flex h-1.5 rounded-full overflow-hidden bg-slate-100">
                        {PARTICIPANT_STATUSES.map(ps => {
                          const count = counts[ps.key] || 0
                          if (count === 0) return null
                          return (
                            <div key={ps.key} className={ps.color}
                              style={{ width: `${(count / total) * 100}%` }}
                              title={`${ps.label}: ${count}`} />
                          )
                        })}
                      </div>
                    </div>
                  )}

                  <div className="flex items-center gap-2 pt-3 border-t border-slate-100">
                    <button onClick={() => router.push(`/dashboard/events/${ev.id}${currentFolderID ? `?folder=${currentFolderID}` : ''}`)}
                      className="flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-sm bg-emerald-50 text-emerald-700 rounded-lg hover:bg-emerald-100 transition-colors font-medium">
                      <Eye className="w-3.5 h-3.5" />
                      Ver detalle
                    </button>
                    <button onClick={() => openEditEvent(ev)}
                      className="p-2 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors">
                      <Edit2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* Loading more indicator */}
      {loadingMore && (
        <div className="flex items-center justify-center py-4">
          <div className="w-5 h-5 border-2 border-emerald-600 border-t-transparent rounded-full animate-spin" />
          <span className="ml-2 text-sm text-slate-500">Cargando más eventos...</span>
        </div>
      )}
      {!hasMore && events.length > 0 && total > EVENTS_PAGE_SIZE && (
        <p className="text-center text-xs text-slate-400 py-2">{total} eventos cargados</p>
      )}

      {/* Drop zone to move events back to parent when inside a folder */}
      {folderPath.length > 0 && (
        <div
          onDragOver={e => { e.preventDefault(); setDragOverFolderID('__root__') }}
          onDragLeave={() => setDragOverFolderID(null)}
          onDrop={async e => {
            e.preventDefault()
            setDragOverFolderID(null)
            const eventID = dragEventIDRef.current
            if (!eventID) return
            dragEventIDRef.current = null
            await moveEventToFolder(eventID, folderPath.length > 1 ? folderPath[folderPath.length - 2].id : null)
          }}
          className={`mt-4 flex items-center justify-center gap-2 px-6 py-4 border-2 border-dashed rounded-xl transition-all text-sm ${
            dragOverFolderID === '__root__'
              ? 'border-emerald-400 bg-emerald-50 text-emerald-600'
              : 'border-slate-200 text-slate-400 hover:border-slate-300'
          }`}>
          <ArrowLeft className="w-4 h-4" />
          Suelta aquí para mover a «{folderPath.length > 1 ? folderPath[folderPath.length - 2].name : 'Raíz'}»
        </div>
      )}

      {/* Modals */}
      {showCreate && renderEventForm(handleCreateEvent, 'Crear')}
      {editEvent && renderEventForm(handleUpdateEvent, 'Guardar')}
      {showFolderModal && renderFolderModal()}
    </div>
  )
}
