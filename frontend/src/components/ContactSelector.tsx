'use client'

import { useEffect, useState, useCallback, useRef } from 'react'
import { Search, X, Filter, Users, CheckCircle2, User, Tag, ChevronDown, CheckSquare, FileText, Code, Calendar, Smartphone } from 'lucide-react'
import FormulaEditor from '@/components/FormulaEditor'

interface PersonResult {
  id: string
  name: string
  phone: string
  email: string
  source_type: 'contact' | 'lead'
  tags?: { id: string; name: string; color: string }[]
}

interface TagItem {
  id: string
  name: string
  color: string
}

export interface SelectedPerson {
  id: string
  name: string
  phone: string
  email: string
  source_type: 'contact' | 'lead'
  tags?: { id: string; name: string; color: string }[]
}

interface DeviceItem {
  id: string
  name: string
  phone: string | null
  phone_number?: string
  status: string
}

const DATE_PRESETS = [
  { key: 'last_15m', label: 'Últimos 15 min' },
  { key: 'last_hour', label: 'Última hora' },
  { key: 'today', label: 'Hoy' },
  { key: 'yesterday', label: 'Ayer' },
  { key: 'last_7d', label: 'Últimos 7 días' },
  { key: 'this_week', label: 'Esta semana' },
  { key: 'this_month', label: 'Este mes' },
  { key: 'last_30d', label: 'Últimos 30 días' },
  { key: 'custom', label: 'Rango personalizado' },
] as const

function resolveDatePreset(preset: string, customFrom?: string, customTo?: string): { from: string; to: string } | null {
  const now = new Date()
  switch (preset) {
    case 'last_15m': { const f = new Date(now.getTime() - 15 * 60 * 1000); return { from: f.toISOString(), to: now.toISOString() } }
    case 'last_hour': { const f = new Date(now.getTime() - 60 * 60 * 1000); return { from: f.toISOString(), to: now.toISOString() } }
    case 'today': { const s = new Date(now); s.setHours(0, 0, 0, 0); return { from: s.toISOString(), to: now.toISOString() } }
    case 'yesterday': { const s = new Date(now); s.setDate(s.getDate() - 1); s.setHours(0, 0, 0, 0); const e = new Date(s); e.setHours(23, 59, 59, 999); return { from: s.toISOString(), to: e.toISOString() } }
    case 'last_7d': { const f = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000); return { from: f.toISOString(), to: now.toISOString() } }
    case 'this_week': { const s = new Date(now); const dow = s.getDay(); s.setDate(s.getDate() - (dow === 0 ? 6 : dow - 1)); s.setHours(0, 0, 0, 0); return { from: s.toISOString(), to: now.toISOString() } }
    case 'this_month': { const s = new Date(now.getFullYear(), now.getMonth(), 1); return { from: s.toISOString(), to: now.toISOString() } }
    case 'last_30d': { const f = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000); return { from: f.toISOString(), to: now.toISOString() } }
    case 'custom': { if (!customFrom && !customTo) return null; const f = customFrom ? new Date(customFrom + 'T00:00:00').toISOString() : ''; const t = customTo ? new Date(customTo + 'T23:59:59').toISOString() : ''; return { from: f, to: t } }
    default: return null
  }
}

interface ContactSelectorProps {
  open: boolean
  onClose: () => void
  onConfirm: (selected: SelectedPerson[]) => void
  title?: string
  subtitle?: string
  confirmLabel?: string
  /** Exclude these IDs from results (e.g. already-added participants) */
  excludeIds?: Set<string>
  /** Force a source type and hide the type filter */
  sourceFilter?: 'contact' | 'lead'
  /** Enable advanced filter panel (device, date, tag include/exclude, formula) */
  advancedFilters?: boolean
  /** When selecting contacts, only show contacts without an active lead */
  withoutActiveLead?: boolean
}

export default function ContactSelector({
  open,
  onClose,
  onConfirm,
  title = 'Seleccionar Personas',
  subtitle = 'Busca entre tus contactos y leads',
  confirmLabel = 'Agregar',
  excludeIds,
  sourceFilter,
  advancedFilters = false,
  withoutActiveLead = false,
}: ContactSelectorProps) {
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [results, setResults] = useState<PersonResult[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<Map<string, SelectedPerson>>(new Map())

  // Basic Filters
  const [sourceType, setSourceType] = useState<'all' | 'contact' | 'lead'>(sourceFilter || 'all')
  const [showFilterDropdown, setShowFilterDropdown] = useState(false)
  const [allTags, setAllTags] = useState<TagItem[]>([])
  const [filterTagIds, setFilterTagIds] = useState<Set<string>>(new Set())
  const [tagSearch, setTagSearch] = useState('')
  const [hasPhone, setHasPhone] = useState(false)

  // Advanced Filters (only used when advancedFilters=true)
  const useAdvanced = advancedFilters && sourceFilter === 'contact'
  const [filterTagNames, setFilterTagNames] = useState<Set<string>>(new Set())
  const [excludeFilterTagNames, setExcludeFilterTagNames] = useState<Set<string>>(new Set())
  const [tagFilterMode, setTagFilterMode] = useState<'OR' | 'AND'>('OR')
  const [formulaType, setFormulaType] = useState<'simple' | 'advanced'>('simple')
  const [formulaText, setFormulaText] = useState('')
  const [formulaIsValid, setFormulaIsValid] = useState(true)
  const [filterDevice, setFilterDevice] = useState('')
  const [filterDateField, setFilterDateField] = useState<'created_at' | 'updated_at'>('created_at')
  const [filterDatePreset, setFilterDatePreset] = useState('')
  const [filterDateFrom, setFilterDateFrom] = useState('')
  const [filterDateTo, setFilterDateTo] = useState('')
  const [advDevices, setAdvDevices] = useState<DeviceItem[]>([])

  const filterRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const token = typeof window !== 'undefined' ? localStorage.getItem('token') : ''

  // Close on Escape
  useEffect(() => {
    if (!open) return
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [open, onClose])

  // Debounce search
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 500)
    return () => clearTimeout(t)
  }, [search])

  // Focus search on open
  useEffect(() => {
    if (open) {
      setTimeout(() => searchRef.current?.focus(), 100)
      fetchTags()
      if (useAdvanced) fetchDevices()
    } else {
      // Reset state on close
      setSearch('')
      setDebouncedSearch('')
      setResults([])
      setSelected(new Map())
      setSourceType(sourceFilter || 'all')
      setFilterTagIds(new Set())
      setHasPhone(false)
      setShowFilterDropdown(false)
      setTagSearch('')
      // Reset advanced state
      setFilterTagNames(new Set())
      setExcludeFilterTagNames(new Set())
      setTagFilterMode('OR')
      setFormulaType('simple')
      setFormulaText('')
      setFilterDevice('')
      setFilterDatePreset('')
      setFilterDateFrom('')
      setFilterDateTo('')
    }
  }, [open])

  // Fetch people when search/filters change
  useEffect(() => {
    if (!open) return
    fetchPeople()
  }, [debouncedSearch, sourceType, filterTagIds, hasPhone, open, filterTagNames, excludeFilterTagNames, tagFilterMode, filterDevice, filterDatePreset, filterDateField, filterDateFrom, filterDateTo, formulaType, formulaText])

  // Click outside to close filter dropdown
  useEffect(() => {
    if (!showFilterDropdown) { setTagSearch(''); return }
    const handler = (e: MouseEvent) => {
      if (filterRef.current && !filterRef.current.contains(e.target as Node)) {
        setShowFilterDropdown(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showFilterDropdown])

  const fetchTags = useCallback(async () => {
    try {
      const res = await fetch('/api/tags', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) setAllTags(data.tags || [])
    } catch (e) { console.error(e) }
  }, [token])

  const fetchDevices = useCallback(async () => {
    try {
      const res = await fetch('/api/devices', { headers: { Authorization: `Bearer ${token}` } })
      const data = await res.json()
      if (data.success) setAdvDevices(data.devices || [])
    } catch (e) { console.error(e) }
  }, [token])

  const fetchPeople = useCallback(async () => {
    setLoading(true)
    try {
      if (useAdvanced) {
        // Advanced path: use /api/contacts with full filter support
        const params = new URLSearchParams()
        if (debouncedSearch) params.set('search', debouncedSearch)
        if (filterDevice) params.set('device_id', filterDevice)

        // Formula vs simple tag filter
        if (formulaType === 'advanced' && formulaText) {
          params.set('tag_formula', formulaText)
        } else {
          if (filterTagNames.size > 0) params.set('tag_names', Array.from(filterTagNames).join(','))
          if (excludeFilterTagNames.size > 0) params.set('exclude_tag_names', Array.from(excludeFilterTagNames).join(','))
          if (filterTagNames.size > 0 || excludeFilterTagNames.size > 0) params.set('tag_mode', tagFilterMode)
        }

        // Date filter
        if (filterDatePreset) {
          const resolved = resolveDatePreset(filterDatePreset, filterDateFrom, filterDateTo)
          if (resolved) {
            params.set('date_field', filterDateField)
            if (resolved.from) params.set('date_from', resolved.from)
            if (resolved.to) params.set('date_to', resolved.to)
          }
        }

        params.set('limit', '100')
        params.set('has_phone', 'false')
        if (withoutActiveLead) params.set('without_active_lead', 'true')

        const res = await fetch(`/api/contacts?${params}`, {
          headers: { Authorization: `Bearer ${token}` },
        })
        const data = await res.json()
        if (data.success) {
          // Map Contact → PersonResult
          const contacts = (data.contacts || []) as any[]
          const mapped: PersonResult[] = contacts.map((c: any) => ({
            id: c.id,
            name: c.custom_name || c.name || c.push_name || c.phone || '',
            phone: c.phone || '',
            email: c.email || '',
            source_type: 'contact' as const,
            tags: (c.structured_tags || []).map((t: any) => ({ id: t.id, name: t.name, color: t.color })),
          }))
          const filtered = excludeIds
            ? mapped.filter(p => !excludeIds.has(p.id))
            : mapped
          setResults(filtered)
          setTotal(data.total || 0)
        }
      } else {
        // Basic path: use /api/people/search
        const params = new URLSearchParams()
        if (debouncedSearch) params.set('search', debouncedSearch)
        if (sourceType !== 'all') params.set('type', sourceType)
        if (filterTagIds.size > 0) params.set('tag_ids', Array.from(filterTagIds).join(','))
        if (hasPhone) params.set('has_phone', 'true')
        params.set('limit', '100')

        const res = await fetch(`/api/people/search?${params}`, {
          headers: { Authorization: `Bearer ${token}` },
        })
        const data = await res.json()
        if (data.success) {
          const filtered = excludeIds
            ? (data.people || []).filter((p: PersonResult) => !excludeIds.has(p.id))
            : data.people || []
          setResults(filtered)
          setTotal(data.total || 0)
        }
      }
    } catch (e) { console.error(e) } finally { setLoading(false) }
  }, [debouncedSearch, sourceType, filterTagIds, hasPhone, token, excludeIds, useAdvanced, filterDevice, filterTagNames, excludeFilterTagNames, tagFilterMode, formulaType, formulaText, filterDatePreset, filterDateField, filterDateFrom, filterDateTo, withoutActiveLead])

  const toggleSelect = (person: PersonResult) => {
    const next = new Map(selected)
    if (next.has(person.id)) {
      next.delete(person.id)
    } else {
      next.set(person.id, {
        id: person.id,
        name: person.name,
        phone: person.phone,
        email: person.email,
        source_type: person.source_type,
        tags: person.tags,
      })
    }
    setSelected(next)
  }

  const selectAll = () => {
    const next = new Map(selected)
    results.forEach(p => {
      if (!next.has(p.id)) {
        next.set(p.id, { id: p.id, name: p.name, phone: p.phone, email: p.email, source_type: p.source_type, tags: p.tags })
      }
    })
    setSelected(next)
  }

  const handleConfirm = () => {
    onConfirm(Array.from(selected.values()))
  }

  const activeFilterCount = useAdvanced
    ? (filterTagNames.size > 0 ? 1 : 0) + (excludeFilterTagNames.size > 0 ? 1 : 0) + (filterDevice ? 1 : 0) + (filterDatePreset ? 1 : 0) + (formulaType === 'advanced' && formulaText ? 1 : 0)
    : (!sourceFilter && sourceType !== 'all' ? 1 : 0) + (filterTagIds.size > 0 ? 1 : 0) + (hasPhone ? 1 : 0)

  // Tag search with wildcard support (% as wildcard, like leads page)
  const filteredTags = allTags.filter(tag => {
    if (!tagSearch.trim()) return true
    const term = tagSearch.trim()
    if (term.includes('%')) {
      const escaped = term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/%/g, '.*')
      try { return new RegExp(`^${escaped}$`, 'i').test(tag.name) } catch { return true }
    }
    return tag.name.toLowerCase().includes(term.toLowerCase())
  })

  if (!open) return null

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-xl shadow-2xl w-full max-w-5xl max-h-[90vh] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200">
          <div>
            <h2 className="text-lg font-semibold text-gray-900">{title}</h2>
            <p className="text-sm text-gray-500 mt-0.5">{subtitle}</p>
          </div>
          <button onClick={onClose} className="p-1.5 text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-lg transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Search + Filters */}
        <div className="px-6 py-4 border-b border-gray-100 space-y-3">
          <div className="flex gap-3">
            <div ref={filterRef} className="relative flex-1">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
              <input
                ref={searchRef}
                value={search}
                onChange={e => setSearch(e.target.value)}
                onFocus={() => setShowFilterDropdown(true)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); setShowFilterDropdown(false) } }}
                placeholder="Buscar por nombre, teléfono, email..."
                className="w-full pl-10 pr-10 py-2.5 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500 focus:border-transparent text-gray-900 text-sm"
              />
              <button
                onClick={() => setShowFilterDropdown(!showFilterDropdown)}
                className={`absolute right-2 top-1/2 -translate-y-1/2 p-1.5 rounded-md transition ${activeFilterCount > 0 ? 'bg-green-100 text-green-700' : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100'}`}
              >
                <Filter className="w-4 h-4" />
                {activeFilterCount > 0 && (
                  <span className="absolute -top-1 -right-1 w-4 h-4 bg-green-600 text-white text-[10px] rounded-full flex items-center justify-center">{activeFilterCount}</span>
                )}
              </button>

              {/* Filter Dropdown */}
              {showFilterDropdown && (
                <div className={`absolute top-full left-0 mt-1 bg-white border border-slate-200 rounded-xl shadow-xl z-30 ${useAdvanced ? 'right-0 max-h-[520px] overflow-y-auto' : 'right-0 max-h-[400px] overflow-y-auto'}`}>
                  <div className="p-3 border-b border-slate-100 flex items-center justify-between">
                    <span className="text-sm font-semibold text-slate-700">Filtros</span>
                    <div className="flex items-center gap-2">
                      {activeFilterCount > 0 && (
                        <button
                          onClick={() => {
                            if (useAdvanced) {
                              setFilterTagNames(new Set()); setExcludeFilterTagNames(new Set()); setTagFilterMode('OR')
                              setFormulaType('simple'); setFormulaText(''); setFilterDevice('')
                              setFilterDatePreset(''); setFilterDateFrom(''); setFilterDateTo('')
                            } else {
                              setSourceType(sourceFilter || 'all'); setFilterTagIds(new Set()); setHasPhone(false)
                            }
                          }}
                          className="text-xs text-red-500 hover:text-red-700"
                        >
                          Limpiar filtros
                        </button>
                      )}
                      <button onClick={() => setShowFilterDropdown(false)} className="p-0.5 hover:bg-slate-100 rounded">
                        <X className="w-4 h-4 text-slate-400" />
                      </button>
                    </div>
                  </div>

                  {useAdvanced ? (
                    /* ====== ADVANCED FILTER PANEL (2 columns) ====== */
                    <div className="grid grid-cols-2 divide-x divide-slate-100">
                      {/* LEFT COLUMN: Device + Date */}
                      <div className="p-3 space-y-4">
                        {/* Device filter */}
                        <div>
                          <div className="flex items-center gap-2 mb-2">
                            <Smartphone className="w-3.5 h-3.5 text-slate-400" />
                            <p className="text-[10px] font-bold text-slate-500 uppercase tracking-widest">Dispositivo</p>
                          </div>
                          <select
                            value={filterDevice}
                            onChange={e => setFilterDevice(e.target.value)}
                            className="w-full px-2.5 py-1.5 bg-white border border-slate-200 rounded-lg text-xs text-slate-700 focus:ring-1 focus:ring-emerald-500"
                          >
                            <option value="">Todos</option>
                            {advDevices.map(d => (
                              <option key={d.id} value={d.id}>{d.name} {d.phone ? `(${d.phone})` : d.phone_number ? `(${d.phone_number})` : ''}</option>
                            ))}
                          </select>
                        </div>

                        {/* Date filter */}
                        <div>
                          <div className="flex items-center gap-2 mb-2">
                            <Calendar className="w-3.5 h-3.5 text-slate-400" />
                            <p className="text-[10px] font-bold text-slate-500 uppercase tracking-widest">Fecha</p>
                          </div>
                          <div className="flex rounded-lg border border-slate-200 bg-slate-50/50 overflow-hidden mb-2">
                            <button
                              onClick={() => setFilterDateField('created_at')}
                              className={`flex-1 px-2 py-1.5 text-[10px] font-semibold transition-all ${filterDateField === 'created_at' ? 'bg-blue-500 text-white shadow-sm' : 'text-slate-500 hover:bg-white'}`}
                            >
                              Creación
                            </button>
                            <button
                              onClick={() => setFilterDateField('updated_at')}
                              className={`flex-1 px-2 py-1.5 text-[10px] font-semibold transition-all ${filterDateField === 'updated_at' ? 'bg-blue-500 text-white shadow-sm' : 'text-slate-500 hover:bg-white'}`}
                            >
                              Modificación
                            </button>
                          </div>
                          <div className="grid grid-cols-2 gap-1">
                            {DATE_PRESETS.map(p => (
                              <button
                                key={p.key}
                                onClick={() => {
                                  if (filterDatePreset === p.key) { setFilterDatePreset(''); setFilterDateFrom(''); setFilterDateTo('') }
                                  else { setFilterDatePreset(p.key); if (p.key !== 'custom') { setFilterDateFrom(''); setFilterDateTo('') } }
                                }}
                                className={`px-2 py-1.5 rounded-lg text-[10px] font-medium transition-all border ${
                                  filterDatePreset === p.key
                                    ? 'bg-blue-500 text-white border-blue-500 shadow-sm'
                                    : 'border-slate-200 text-slate-600 hover:bg-white hover:shadow-sm'
                                }`}
                              >
                                {p.label}
                              </button>
                            ))}
                          </div>
                          {filterDatePreset === 'custom' && (
                            <div className="mt-2 space-y-1.5">
                              <div>
                                <label className="text-[9px] font-semibold text-slate-400 uppercase">Desde</label>
                                <input type="date" value={filterDateFrom} onChange={e => setFilterDateFrom(e.target.value)}
                                  className="w-full px-2 py-1.5 text-xs border border-slate-200 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-400 focus:border-blue-400" />
                              </div>
                              <div>
                                <label className="text-[9px] font-semibold text-slate-400 uppercase">Hasta</label>
                                <input type="date" value={filterDateTo} onChange={e => setFilterDateTo(e.target.value)}
                                  className="w-full px-2 py-1.5 text-xs border border-slate-200 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-400 focus:border-blue-400" />
                              </div>
                            </div>
                          )}
                        </div>
                      </div>

                      {/* RIGHT COLUMN: Tags + Formula */}
                      <div className="p-3 space-y-3">
                        {/* Simple / Advanced toggle */}
                        <div className="flex rounded-xl border border-slate-200 bg-slate-50/50 overflow-hidden">
                          <button type="button" onClick={() => setFormulaType('simple')}
                            className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-[11px] font-semibold transition-all ${
                              formulaType === 'simple' ? 'bg-emerald-500 text-white shadow-sm' : 'text-slate-500 hover:bg-white hover:text-slate-700'
                            }`}>
                            <FileText className="w-3.5 h-3.5" />
                            Simple
                          </button>
                          <button type="button" onClick={() => setFormulaType('advanced')}
                            className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-[11px] font-semibold transition-all ${
                              formulaType === 'advanced' ? 'bg-violet-500 text-white shadow-sm' : 'text-slate-500 hover:bg-white hover:text-slate-700'
                            }`}>
                            <Code className="w-3.5 h-3.5" />
                            Avanzado
                          </button>
                        </div>

                        {formulaType === 'simple' ? (
                          <>
                            {/* AND/OR toggle */}
                            <div className="flex items-center gap-2">
                              <div className="inline-flex rounded-lg border border-slate-200 overflow-hidden">
                                <button onClick={() => setTagFilterMode('OR')}
                                  className={`px-3 py-1 text-[10px] font-bold tracking-wide transition-all ${tagFilterMode === 'OR' ? 'bg-emerald-500 text-white' : 'bg-white text-slate-400 hover:bg-slate-50'}`}>
                                  OR
                                </button>
                                <button onClick={() => setTagFilterMode('AND')}
                                  className={`px-3 py-1 text-[10px] font-bold tracking-wide transition-all ${tagFilterMode === 'AND' ? 'bg-blue-500 text-white' : 'bg-white text-slate-400 hover:bg-slate-50'}`}>
                                  AND
                                </button>
                              </div>
                              <p className="text-[10px] text-slate-400 leading-tight">
                                {tagFilterMode === 'AND' ? 'Todas' : 'Al menos una'}
                              </p>
                            </div>

                            {/* Tag search */}
                            <div className="relative">
                              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400" />
                              <input
                                value={tagSearch}
                                onChange={e => setTagSearch(e.target.value)}
                                placeholder="Buscar etiquetas..."
                                className="w-full pl-8 pr-3 py-1.5 bg-slate-50 border border-slate-200 rounded-lg text-xs text-slate-800 placeholder:text-slate-400 focus:ring-2 focus:ring-emerald-500 focus:border-emerald-500"
                              />
                            </div>

                            {/* Tag list with 3-click cycle */}
                            <div className="max-h-[220px] overflow-y-auto space-y-0.5">
                              {filteredTags.map(tag => {
                                const isIncluded = filterTagNames.has(tag.name)
                                const isExcluded = excludeFilterTagNames.has(tag.name)
                                return (
                                  <div
                                    key={tag.id}
                                    onClick={() => {
                                      if (!isIncluded && !isExcluded) {
                                        const next = new Set(filterTagNames); next.add(tag.name); setFilterTagNames(next)
                                      } else if (isIncluded) {
                                        const incl = new Set(filterTagNames); incl.delete(tag.name); setFilterTagNames(incl)
                                        const excl = new Set(excludeFilterTagNames); excl.add(tag.name); setExcludeFilterTagNames(excl)
                                      } else {
                                        const next = new Set(excludeFilterTagNames); next.delete(tag.name); setExcludeFilterTagNames(next)
                                      }
                                    }}
                                    className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-xl cursor-pointer select-none transition-all ${
                                      isIncluded ? 'bg-emerald-50 ring-1 ring-emerald-200' : isExcluded ? 'bg-red-50 ring-1 ring-red-200' : 'hover:bg-white hover:shadow-sm'
                                    }`}
                                  >
                                    {isIncluded ? (
                                      <div className="w-4 h-4 rounded-full shrink-0 bg-emerald-500 flex items-center justify-center"><CheckSquare className="w-2.5 h-2.5 text-white" /></div>
                                    ) : isExcluded ? (
                                      <div className="w-4 h-4 rounded-full shrink-0 bg-red-500 flex items-center justify-center"><X className="w-2.5 h-2.5 text-white" /></div>
                                    ) : (
                                      <div className="w-3 h-3 rounded-full shrink-0 ring-2 ring-white shadow-sm" style={{ backgroundColor: tag.color || '#6b7280' }} />
                                    )}
                                    <span className={`flex-1 text-[11px] transition-colors ${
                                      isIncluded ? 'text-emerald-700 font-semibold' : isExcluded ? 'text-red-400 line-through' : 'text-slate-700'
                                    }`}>{tag.name}</span>
                                  </div>
                                )
                              })}
                              {filteredTags.length === 0 && tagSearch.trim() && (
                                <p className="text-xs text-slate-400 text-center py-2">Sin resultados</p>
                              )}
                            </div>
                          </>
                        ) : (
                          /* FormulaEditor (advanced mode) */
                          <div className="space-y-2">
                            <div className="p-2 bg-slate-50 rounded-lg border border-slate-100">
                              <p className="text-[10px] text-slate-500 leading-relaxed">
                                Sintaxis: <code className="bg-white px-1 rounded text-[9px]">{'"tag" and "tag2" or not "tag3"'}</code>
                              </p>
                            </div>
                            <FormulaEditor
                              value={formulaText}
                              onChange={setFormulaText}
                              tags={allTags}
                              compact
                              rows={4}
                              onValidChange={setFormulaIsValid}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  ) : (
                    /* ====== BASIC FILTER PANEL (original) ====== */
                    <>
                      {/* Source type filter */}
                      {!sourceFilter && (
                      <div className="p-3 border-b border-gray-100">
                        <p className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-2">Tipo</p>
                        <div className="flex flex-wrap gap-1.5">
                          {[
                            { value: 'all' as const, label: 'Todos' },
                            { value: 'contact' as const, label: 'Contactos' },
                            { value: 'lead' as const, label: 'Leads' },
                          ].map(opt => (
                            <button
                              key={opt.value}
                              onClick={() => setSourceType(opt.value)}
                              className={`px-3 py-1.5 rounded-full text-xs font-medium transition border ${
                                sourceType === opt.value
                                  ? 'border-green-300 bg-green-50 text-green-700'
                                  : 'border-gray-200 text-gray-600 hover:bg-gray-50'
                              }`}
                            >
                              {opt.label}
                            </button>
                          ))}
                        </div>
                      </div>
                      )}

                      {/* Has phone filter */}
                      <div className="p-3 border-b border-gray-100">
                        <p className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-2">Teléfono</p>
                        <button
                          onClick={() => setHasPhone(!hasPhone)}
                          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors ${
                            hasPhone ? 'bg-green-50 border-green-300 text-green-700' : 'bg-white border-gray-200 text-gray-600 hover:bg-gray-50'
                          }`}
                        >
                          Solo con teléfono
                        </button>
                      </div>

                      {/* Tag filters */}
                      {allTags.length > 0 && (
                        <div className="p-3">
                          <p className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-2">Etiquetas</p>
                          <div className="relative mb-2">
                            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" />
                            <input
                              value={tagSearch}
                              onChange={e => setTagSearch(e.target.value)}
                              placeholder="Buscar... (usa % como comodín)"
                              className="w-full pl-8 pr-3 py-1.5 bg-gray-50 border border-gray-200 rounded-lg text-xs text-gray-800 placeholder:text-gray-400 focus:ring-2 focus:ring-green-500 focus:border-green-500"
                            />
                          </div>
                          {filterTagIds.size > 0 && (
                            <div className="flex flex-wrap gap-1 mb-2">
                              {Array.from(filterTagIds).map(tid => {
                                const tag = allTags.find(t => t.id === tid)
                                if (!tag) return null
                                return (
                                  <span
                                    key={tid}
                                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium text-white"
                                    style={{ backgroundColor: tag.color || '#6b7280' }}
                                  >
                                    {tag.name}
                                    <button onClick={() => { const n = new Set(filterTagIds); n.delete(tid); setFilterTagIds(n) }} className="hover:opacity-75">
                                      <X className="w-2.5 h-2.5" />
                                    </button>
                                  </span>
                                )
                              })}
                            </div>
                          )}
                          <div className="max-h-[200px] overflow-y-auto space-y-0.5">
                            {filteredTags.map(tag => {
                              const isActive = filterTagIds.has(tag.id)
                              return (
                                <label
                                  key={tag.id}
                                  className="flex items-center gap-2 px-2 py-1.5 rounded-lg hover:bg-gray-50 cursor-pointer transition"
                                >
                                  <input
                                    type="checkbox"
                                    checked={isActive}
                                    onChange={() => {
                                      const next = new Set(filterTagIds)
                                      if (isActive) next.delete(tag.id); else next.add(tag.id)
                                      setFilterTagIds(next)
                                    }}
                                    className="w-3.5 h-3.5 rounded border-gray-300 text-green-600 focus:ring-green-500"
                                  />
                                  <div className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: tag.color || '#6b7280' }} />
                                  <span className="flex-1 text-xs text-gray-700">{tag.name}</span>
                                </label>
                              )
                            })}
                            {filteredTags.length === 0 && tagSearch.trim() && (
                              <p className="text-xs text-gray-400 text-center py-2">Sin resultados</p>
                            )}
                          </div>
                        </div>
                      )}
                    </>
                  )}

                  <div className="p-3 border-t border-slate-100 sticky bottom-0 bg-white rounded-b-xl">
                    <button
                      onClick={() => setShowFilterDropdown(false)}
                      disabled={useAdvanced && formulaType === 'advanced' && !formulaIsValid}
                      className="w-full px-4 py-2 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 disabled:opacity-50 disabled:cursor-not-allowed transition text-sm font-medium"
                    >
                      Aplicar
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Active filter badges */}
          {activeFilterCount > 0 && (
            <div className="flex flex-wrap gap-2">
              {useAdvanced ? (
                <>
                  {filterDevice && (
                    <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-emerald-50 text-emerald-700 rounded-full text-xs font-medium border border-emerald-200">
                      Dispositivo
                      <button onClick={() => setFilterDevice('')} className="hover:text-emerald-900"><X className="w-3 h-3" /></button>
                    </span>
                  )}
                  {filterDatePreset && (
                    <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-blue-50 text-blue-700 rounded-full text-xs font-medium border border-blue-200">
                      {DATE_PRESETS.find(p => p.key === filterDatePreset)?.label || 'Fecha'}
                      <button onClick={() => { setFilterDatePreset(''); setFilterDateFrom(''); setFilterDateTo('') }} className="hover:text-blue-900"><X className="w-3 h-3" /></button>
                    </span>
                  )}
                  {formulaType === 'advanced' && formulaText ? (
                    <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-violet-50 text-violet-700 rounded-full text-xs font-medium border border-violet-200">
                      Fórmula
                      <button onClick={() => { setFormulaText(''); setFormulaType('simple') }} className="hover:text-violet-900"><X className="w-3 h-3" /></button>
                    </span>
                  ) : (
                    <>
                      {Array.from(filterTagNames).map(name => {
                        const tag = allTags.find(t => t.name === name)
                        return (
                          <span key={`inc-${name}`} className="inline-flex items-center gap-1 px-2.5 py-1 bg-emerald-50 text-emerald-700 rounded-full text-xs font-medium border border-emerald-200">
                            {name}
                            <button onClick={() => { const n = new Set(filterTagNames); n.delete(name); setFilterTagNames(n) }} className="hover:text-emerald-900"><X className="w-3 h-3" /></button>
                          </span>
                        )
                      })}
                      {Array.from(excludeFilterTagNames).map(name => (
                        <span key={`exc-${name}`} className="inline-flex items-center gap-1 px-2.5 py-1 bg-red-50 text-red-600 rounded-full text-xs font-medium border border-red-200 line-through">
                          {name}
                          <button onClick={() => { const n = new Set(excludeFilterTagNames); n.delete(name); setExcludeFilterTagNames(n) }} className="hover:text-red-800"><X className="w-3 h-3" /></button>
                        </span>
                      ))}
                    </>
                  )}
                </>
              ) : (
                <>
                  {sourceType !== 'all' && (
                    <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-green-50 text-green-700 rounded-full text-xs font-medium border border-green-200">
                      {sourceType === 'contact' ? 'Contactos' : 'Leads'}
                      <button onClick={() => setSourceType('all')} className="hover:text-green-900"><X className="w-3 h-3" /></button>
                    </span>
                  )}
                  {hasPhone && (
                    <span className="inline-flex items-center gap-1 px-2.5 py-1 bg-green-50 text-green-700 rounded-full text-xs font-medium border border-green-200">
                      Con teléfono
                      <button onClick={() => setHasPhone(false)} className="hover:text-green-900"><X className="w-3 h-3" /></button>
                    </span>
                  )}
                  {Array.from(filterTagIds).map(tid => {
                    const tag = allTags.find(t => t.id === tid)
                    if (!tag) return null
                    return (
                      <span
                        key={tid}
                        className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium text-white"
                        style={{ backgroundColor: tag.color || '#6b7280' }}
                      >
                        {tag.name}
                        <button onClick={() => { const n = new Set(filterTagIds); n.delete(tid); setFilterTagIds(n) }} className="hover:opacity-75"><X className="w-3 h-3" /></button>
                      </span>
                    )
                  })}
                </>
              )}
            </div>
          )}
        </div>

        {/* Results */}
        <div className="flex-1 overflow-hidden flex flex-col min-h-0">
          {/* Selection info bar */}
          <div className="flex items-center justify-between px-6 py-2.5 bg-gray-50 border-b border-gray-100">
            <div className="flex items-center gap-3">
              <span className="text-xs text-gray-500">
                {loading ? 'Buscando...' : `${results.length} resultado${results.length !== 1 ? 's' : ''}`}
                {total > results.length && ` de ${total}`}
              </span>
              {results.length > 0 && (
                <button onClick={selectAll} className="text-xs text-green-600 hover:text-green-700 font-medium">
                  Seleccionar todos
                </button>
              )}
            </div>
            {selected.size > 0 && (
              <span className="text-xs font-medium text-green-700 bg-green-100 px-2.5 py-1 rounded-full">
                {selected.size} seleccionado{selected.size !== 1 ? 's' : ''}
              </span>
            )}
          </div>

          {/* Selected pills */}
          {selected.size > 0 && (
            <div className="px-6 py-3 border-b border-gray-100 bg-green-50/50">
              <div className="flex flex-wrap gap-1.5">
                {Array.from(selected.values()).map(p => (
                  <span key={p.id} className="inline-flex items-center gap-1 px-2.5 py-1 bg-green-100 text-green-700 rounded-full text-xs font-medium">
                    {p.name || p.phone || 'Sin nombre'}
                    <span className={`px-1 py-0 rounded text-[9px] font-bold ${p.source_type === 'contact' ? 'bg-blue-100 text-blue-600' : 'bg-purple-100 text-purple-600'}`}>
                      {p.source_type === 'contact' ? 'C' : 'L'}
                    </span>
                    <button onClick={() => { const n = new Map(selected); n.delete(p.id); setSelected(n) }} className="hover:text-green-900">
                      <X className="w-3 h-3" />
                    </button>
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Results list */}
          <div className="flex-1 overflow-y-auto px-6 py-2">
            {loading ? (
              <div className="space-y-2 py-2">
                {[...Array(6)].map((_, i) => (
                  <div key={i} className="h-14 bg-gray-100 rounded-lg animate-pulse" />
                ))}
              </div>
            ) : results.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-16 text-center">
                <Users className="w-12 h-12 text-gray-300 mb-3" />
                <p className="text-gray-500 font-medium">
                  {debouncedSearch || activeFilterCount > 0 ? 'No se encontraron resultados' : 'Escribe para buscar contactos y leads'}
                </p>
                <p className="text-gray-400 text-sm mt-1">
                  {debouncedSearch ? 'Intenta con otro término o ajusta los filtros' : 'La búsqueda es por nombre, teléfono o email'}
                </p>
              </div>
            ) : (
              <div className="space-y-1 py-1">
                {results.map(person => {
                  const isSelected = selected.has(person.id)
                  return (
                    <button
                      key={person.id}
                      onClick={() => toggleSelect(person)}
                      className={`w-full flex items-center gap-3 px-4 py-3 rounded-lg border text-left transition-all ${
                        isSelected ? 'border-green-300 bg-green-50 shadow-sm' : 'border-gray-200 hover:bg-gray-50 hover:border-gray-300'
                      }`}
                    >
                      <div className={`w-9 h-9 rounded-full flex items-center justify-center text-xs font-semibold flex-shrink-0 ${
                        isSelected ? 'bg-green-200 text-green-700' : 'bg-gray-200 text-gray-600'
                      }`}>
                        {person.name ? person.name.charAt(0).toUpperCase() : '?'}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <p className="text-sm font-medium text-gray-900 truncate">{person.name || 'Sin nombre'}</p>
                          <span className={`px-1.5 py-0.5 rounded text-[10px] font-bold ${
                            person.source_type === 'contact'
                              ? 'bg-blue-100 text-blue-600'
                              : 'bg-purple-100 text-purple-600'
                          }`}>
                            {person.source_type === 'contact' ? 'Contacto' : 'Lead'}
                          </span>
                        </div>
                        <div className="flex items-center gap-3 mt-0.5">
                          {person.phone && <span className="text-xs text-gray-500">{person.phone}</span>}
                          {person.email && <span className="text-xs text-gray-400">{person.email}</span>}
                        </div>
                        {person.tags && person.tags.length > 0 && (
                          <div className="flex flex-wrap gap-1 mt-1">
                            {person.tags.slice(0, 4).map(tag => (
                              <span
                                key={tag.id}
                                className="px-1.5 py-0.5 text-[10px] rounded-full text-white font-medium"
                                style={{ backgroundColor: tag.color || '#6b7280' }}
                              >
                                {tag.name}
                              </span>
                            ))}
                            {person.tags.length > 4 && (
                              <span className="text-[10px] text-gray-400">+{person.tags.length - 4}</span>
                            )}
                          </div>
                        )}
                      </div>
                      {isSelected && <CheckCircle2 className="w-5 h-5 text-green-600 flex-shrink-0" />}
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="px-6 py-4 border-t border-gray-200 flex items-center justify-between">
          <button onClick={onClose} className="px-4 py-2 text-gray-600 hover:bg-gray-100 rounded-lg transition-colors text-sm">
            Cancelar
          </button>
          <button
            onClick={handleConfirm}
            disabled={selected.size === 0}
            className="px-6 py-2.5 bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50 font-medium text-sm transition-colors shadow-sm"
          >
            {confirmLabel} {selected.size > 0 ? `(${selected.size})` : ''}
          </button>
        </div>
      </div>
    </div>
  )
}
