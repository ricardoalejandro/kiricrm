'use client'

import { useEffect, useState, useCallback, useRef } from 'react'
import { Plus, Trash2, Edit, Tag, X, Check, LayoutGrid, List, Grid3X3, Search, Loader2 } from 'lucide-react'

interface TagItem {
  id: string
  account_id: string
  name: string
  color: string
  created_at: string
  updated_at: string
}

const PRESET_COLORS = [
  '#ef4444', '#f97316', '#f59e0b', '#eab308', '#84cc16', '#22c55e',
  '#14b8a6', '#06b6d4', '#0ea5e9', '#3b82f6', '#6366f1', '#8b5cf6',
  '#a855f7', '#d946ef', '#ec4899', '#f43f5e', '#64748b', '#78716c',
  '#0d9488', '#059669', '#dc2626', '#9333ea', '#c026d3', '#db2777',
]

const PAGE_SIZE = 50

type ViewMode = 'grid' | 'list' | 'compact'

export default function TagsPage() {
  const [tags, setTags] = useState<TagItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const offsetRef = useRef(0)
  const requestSeqRef = useRef(0)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const [showCreateModal, setShowCreateModal] = useState(false)
  const [editingTag, setEditingTag] = useState<TagItem | null>(null)
  const [formName, setFormName] = useState('')
  const [formColor, setFormColor] = useState('#6366f1')
  const [customColor, setCustomColor] = useState('')
  const [viewMode, setViewMode] = useState<ViewMode>('grid')
  const [searchQuery, setSearchQuery] = useState('')
  const [debouncedSearchQuery, setDebouncedSearchQuery] = useState('')
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const token = typeof window !== 'undefined' ? localStorage.getItem('token') : null

  const fetchTags = useCallback(async (reset: boolean = true, search: string = debouncedSearchQuery) => {
    if (!token) return
    const offset = reset ? 0 : offsetRef.current
    const requestSeq = ++requestSeqRef.current
    if (reset) setRefreshing(true)
    else setLoadingMore(true)

    try {
      const params = new URLSearchParams()
      params.set('limit', String(PAGE_SIZE))
      params.set('offset', String(offset))
      const trimmedSearch = search.trim()
      if (trimmedSearch) params.set('search', trimmedSearch)

      const res = await fetch(`/api/tags?${params.toString()}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (requestSeq !== requestSeqRef.current) return
      if (data.success) {
        const newTags: TagItem[] = data.tags || []
        const serverTotal: number = data.total ?? 0
        setTotal(serverTotal)

        if (reset) {
          setTags(newTags)
          offsetRef.current = newTags.length
        } else {
          setTags(prev => {
            const existingIds = new Set(prev.map(t => t.id))
            const unique = newTags.filter(t => !existingIds.has(t.id))
            return [...prev, ...unique]
          })
          offsetRef.current = offset + newTags.length
        }
        setHasMore((offset + newTags.length) < serverTotal)
      }
    } catch (err) {
      console.error('Failed to fetch tags:', err)
    } finally {
      if (requestSeq === requestSeqRef.current) {
        setLoading(false)
        setRefreshing(false)
        setLoadingMore(false)
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, debouncedSearchQuery])

  const loadMore = useCallback(() => {
    if (loadingMore || !hasMore) return
    fetchTags(false, debouncedSearchQuery)
  }, [loadingMore, hasMore, fetchTags, debouncedSearchQuery])

  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current
    if (!el || !hasMore || loadingMore) return
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 300) {
      loadMore()
    }
  }, [hasMore, loadingMore, loadMore])

  // IntersectionObserver sentinel for cases where content doesn't overflow (e.g. grid view)
  const sentinelRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = sentinelRef.current
    if (!el) return
    const observer = new IntersectionObserver(
      ([entry]) => { if (entry.isIntersecting) loadMore() },
      { threshold: 0 }
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [loadMore])

  useEffect(() => { fetchTags(true, debouncedSearchQuery) }, [fetchTags, debouncedSearchQuery])

  // Debounced search
  useEffect(() => {
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
    searchTimerRef.current = setTimeout(() => {
      setDebouncedSearchQuery(searchQuery)
    }, 300)
    return () => { if (searchTimerRef.current) clearTimeout(searchTimerRef.current) }
  }, [searchQuery])

  // Close modals on Escape
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && (showCreateModal || editingTag)) { setShowCreateModal(false); setEditingTag(null); setCustomColor('') }
    }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [showCreateModal, editingTag])

  const handleCreate = async () => {
    if (!formName.trim()) return
    try {
      const res = await fetch('/api/tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ name: formName.trim(), color: formColor }),
      })
      const data = await res.json()
      if (data.success) {
        setShowCreateModal(false)
        setFormName('')
        setFormColor('#6366f1')
        setCustomColor('')
        fetchTags(true)
      } else {
        alert(data.error || 'Error al crear etiqueta')
      }
    } catch {
      alert('Error al crear etiqueta')
    }
  }

  const handleUpdate = async () => {
    if (!editingTag || !formName.trim()) return
    try {
      const res = await fetch(`/api/tags/${editingTag.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ name: formName.trim(), color: formColor }),
      })
      const data = await res.json()
      if (data.success) {
        setEditingTag(null)
        setFormName('')
        setFormColor('#6366f1')
        setCustomColor('')
        fetchTags(true)
      } else {
        alert(data.error || 'Error al actualizar etiqueta')
      }
    } catch {
      alert('Error al actualizar etiqueta')
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('¿Eliminar esta etiqueta? Se removerá de todos los contactos, leads y chats.')) return
    try {
      const res = await fetch(`/api/tags/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) fetchTags(true)
      else alert(data.error || 'Error al eliminar etiqueta')
    } catch {
      alert('Error al eliminar etiqueta')
    }
  }

  const openEdit = (tag: TagItem) => {
    setEditingTag(tag)
    setFormName(tag.name)
    setFormColor(tag.color)
    setCustomColor(PRESET_COLORS.includes(tag.color) ? '' : tag.color)
  }

  const openCreate = () => {
    setFormName('')
    setFormColor('#6366f1')
    setCustomColor('')
    setShowCreateModal(true)
  }

  const handleCustomColorChange = (hex: string) => {
    setCustomColor(hex)
    if (/^#[0-9a-fA-F]{6}$/.test(hex)) {
      setFormColor(hex)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-green-600" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 flex-1 min-h-0">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 shrink-0">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Etiquetas</h1>
          <p className="text-gray-600 mt-1">{total} etiquetas globales — se comparten en contactos, leads y chats</p>
        </div>
        <button
          onClick={openCreate}
          className="inline-flex items-center gap-2 bg-green-600 text-white px-4 py-2 rounded-lg hover:bg-green-700 transition"
        >
          <Plus className="w-5 h-5" />
          Nueva Etiqueta
        </button>
      </div>

      {/* Search & View Toggle */}
      <div className="flex items-center gap-3 shrink-0">
        <div className="relative flex-1 max-w-xs">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
          <input
            type="text"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            placeholder="Buscar etiquetas..."
            className="w-full pl-9 pr-8 py-2 border border-gray-300 rounded-lg text-sm focus:ring-2 focus:ring-green-500 focus:border-transparent text-gray-900 placeholder:text-gray-400"
          />
          {refreshing && (
            <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-green-600 animate-spin" />
          )}
        </div>
        <div className="flex items-center bg-gray-100 rounded-lg p-0.5">
          <button
            onClick={() => setViewMode('grid')}
            className={`p-2 rounded-md transition ${viewMode === 'grid' ? 'bg-white shadow text-green-600' : 'text-gray-500 hover:text-gray-700'}`}
            title="Cuadrícula"
          >
            <LayoutGrid className="w-4 h-4" />
          </button>
          <button
            onClick={() => setViewMode('list')}
            className={`p-2 rounded-md transition ${viewMode === 'list' ? 'bg-white shadow text-green-600' : 'text-gray-500 hover:text-gray-700'}`}
            title="Lista"
          >
            <List className="w-4 h-4" />
          </button>
          <button
            onClick={() => setViewMode('compact')}
            className={`p-2 rounded-md transition ${viewMode === 'compact' ? 'bg-white shadow text-green-600' : 'text-gray-500 hover:text-gray-700'}`}
            title="Compacto"
          >
            <Grid3X3 className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Counter */}
      <div className="px-1 shrink-0">
        <p className="text-xs text-gray-500">
          Mostrando {tags.length} de {total.toLocaleString()} etiquetas
        </p>
      </div>

      {/* Tags display - scrollable container */}
      <div
        ref={scrollContainerRef}
        onScroll={handleScroll}
        className="overflow-y-auto flex-1 min-h-0"
      >
      {tags.length === 0 ? (
        <div className="bg-white rounded-xl border border-gray-200 p-12 text-center">
          <Tag className="w-12 h-12 text-gray-300 mx-auto mb-4" />
          <h3 className="text-lg font-medium text-gray-900">{searchQuery ? 'Sin resultados' : 'Sin etiquetas'}</h3>
          <p className="text-gray-500 mt-1">
            {searchQuery ? 'No se encontraron etiquetas con ese nombre' : 'Crea etiquetas para organizar tus contactos, leads y chats'}
          </p>
        </div>
      ) : viewMode === 'grid' ? (
        /* Grid View */
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-2">
          {tags.map(tag => (
            <div
              key={tag.id}
              className="bg-white rounded-lg border border-gray-200 px-3 py-2.5 hover:shadow-sm transition group flex items-center gap-2"
            >
              <div className="w-3 h-3 rounded-full shrink-0" style={{ backgroundColor: tag.color }} />
              <span className="font-medium text-sm text-gray-800 truncate flex-1">{tag.name}</span>
              <div className="flex items-center gap-0.5 shrink-0 sm:opacity-0 sm:group-hover:opacity-100 transition-opacity">
                <button onClick={() => openEdit(tag)} className="p-1 text-gray-400 hover:text-blue-600 rounded" title="Editar">
                  <Edit className="w-3.5 h-3.5" />
                </button>
                <button onClick={() => handleDelete(tag.id)} className="p-1 text-gray-400 hover:text-red-600 rounded" title="Eliminar">
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          ))}
        </div>
      ) : viewMode === 'list' ? (
        /* List View */
        <div className="bg-white rounded-xl border border-gray-200 divide-y divide-gray-100">
          {tags.map(tag => (
            <div
              key={tag.id}
              className="flex items-center gap-4 px-4 py-3 hover:bg-gray-50 transition group"
            >
              <div className="w-4 h-4 rounded-full shrink-0" style={{ backgroundColor: tag.color }} />
              <span className="font-medium text-sm text-gray-900 flex-1">{tag.name}</span>
              <span className="text-xs text-gray-400 font-mono">{tag.color}</span>
              <span className="text-xs text-gray-400">{new Date(tag.created_at).toLocaleDateString('es-PE')}</span>
              <div className="flex items-center gap-1 shrink-0 sm:opacity-0 sm:group-hover:opacity-100 transition-opacity">
                <button onClick={() => openEdit(tag)} className="p-1.5 text-gray-400 hover:text-blue-600 rounded hover:bg-blue-50" title="Editar">
                  <Edit className="w-4 h-4" />
                </button>
                <button onClick={() => handleDelete(tag.id)} className="p-1.5 text-gray-400 hover:text-red-600 rounded hover:bg-red-50" title="Eliminar">
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        /* Compact View - colored chips */
        <div className="flex flex-wrap gap-2">
          {tags.map(tag => (
            <div
              key={tag.id}
              className="inline-flex items-center gap-1.5 pl-3 pr-1 py-1 rounded-full text-white text-sm font-medium group cursor-default"
              style={{ backgroundColor: tag.color }}
            >
              <span>{tag.name}</span>
              <button
                onClick={() => openEdit(tag)}
                className="p-0.5 rounded-full hover:bg-white/20 transition opacity-0 group-hover:opacity-100"
                title="Editar"
              >
                <Edit className="w-3 h-3" />
              </button>
              <button
                onClick={() => handleDelete(tag.id)}
                className="p-0.5 rounded-full hover:bg-white/20 transition opacity-0 group-hover:opacity-100"
                title="Eliminar"
              >
                <X className="w-3 h-3" />
              </button>
            </div>
          ))}
        </div>
      )}
      {/* Sentinel for IntersectionObserver — triggers loadMore when visible */}
      {hasMore && <div ref={sentinelRef} className="h-4" />}
      {loadingMore && (
        <div className="flex items-center justify-center py-4 gap-2 text-sm text-green-600">
          <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-green-600" />
          Cargando más...
        </div>
      )}
      </div>

      {/* Create/Edit Modal */}
      {(showCreateModal || editingTag) && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-xl p-6 w-full max-w-md">
            <h2 className="text-xl font-bold text-gray-900 mb-4">
              {editingTag ? 'Editar Etiqueta' : 'Nueva Etiqueta'}
            </h2>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Nombre *</label>
                <input
                  type="text"
                  value={formName}
                  onChange={e => setFormName(e.target.value)}
                  className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-green-500 text-gray-900"
                  placeholder="Ej: VIP, Urgente, Nuevo..."
                  autoFocus
                  onKeyDown={e => {
                    if (e.key === 'Enter') editingTag ? handleUpdate() : handleCreate()
                  }}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-2">Color</label>
                {/* Preset colors */}
                <div className="flex flex-wrap gap-2 mb-3">
                  {PRESET_COLORS.map(color => (
                    <button
                      key={color}
                      onClick={() => { setFormColor(color); setCustomColor('') }}
                      className={`w-7 h-7 rounded-full border-2 transition hover:scale-110 ${
                        formColor === color && !customColor ? 'border-gray-900 scale-110 ring-2 ring-gray-300' : 'border-transparent'
                      }`}
                      style={{ backgroundColor: color }}
                    >
                      {formColor === color && !customColor && <Check className="w-3.5 h-3.5 text-white mx-auto" />}
                    </button>
                  ))}
                </div>
                {/* Custom color */}
                <div className="flex items-center gap-3 mt-2">
                  <div className="flex items-center gap-2 flex-1">
                    <input
                      type="color"
                      value={formColor}
                      onChange={e => { setFormColor(e.target.value); setCustomColor(e.target.value) }}
                      className="w-9 h-9 rounded-lg border border-gray-300 cursor-pointer p-0.5"
                      title="Selector de color personalizado"
                    />
                    <input
                      type="text"
                      value={customColor || formColor}
                      onChange={e => handleCustomColorChange(e.target.value)}
                      placeholder="#6366f1"
                      maxLength={7}
                      className="w-24 px-3 py-1.5 border border-gray-300 rounded-lg text-sm font-mono text-gray-900 focus:ring-2 focus:ring-green-500 focus:border-transparent"
                    />
                  </div>
                  <span className="text-xs text-gray-400">Color personalizado</span>
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">Vista previa</label>
                <span
                  className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-white text-sm font-medium"
                  style={{ backgroundColor: formColor }}
                >
                  {formName || 'Etiqueta'}
                </span>
              </div>
            </div>
            <div className="flex gap-3 mt-6">
              <button
                onClick={() => { setShowCreateModal(false); setEditingTag(null); setCustomColor('') }}
                className="flex-1 px-4 py-2 border border-gray-300 text-gray-700 rounded-lg hover:bg-gray-50"
              >
                Cancelar
              </button>
              <button
                onClick={editingTag ? handleUpdate : handleCreate}
                disabled={!formName.trim()}
                className="flex-1 px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50"
              >
                {editingTag ? 'Guardar' : 'Crear'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
