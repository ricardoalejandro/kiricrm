'use client'

import { useEffect, useState, useCallback, useRef, useMemo, memo } from 'react'
import { Search, Plus, Phone, Mail, User, UserPlus, Tag, Calendar, MoreVertical, MoreHorizontal, MessageCircle, Trash2, Edit, ChevronDown, ChevronLeft, ChevronRight, Filter, CheckSquare, Square, MinusSquare, XCircle, Clock, FileText, X, Maximize2, Upload, Building2, Save, Edit2, Settings, Pencil, Eye, EyeOff, GripVertical, RefreshCw, Radio, LayoutGrid, List, ChevronUp, Code, AlertCircle, CheckCircle2, Archive, ShieldBan, ArchiveRestore, ShieldOff, Download } from 'lucide-react'
import { formatDistanceToNow, format } from 'date-fns'
import { useKanbanPan } from '@/lib/useKanbanPan'
import { es } from 'date-fns/locale'
import FormulaEditor from '@/components/FormulaEditor'
import { useVirtualizer } from '@tanstack/react-virtual'
import ImportCSVModal from '@/components/ImportCSVModal'
import ContactSelector, { type SelectedPerson } from '@/components/ContactSelector'
import TagInput from '@/components/TagInput'
import CreateCampaignModal, { CampaignFormResult } from '@/components/CreateCampaignModal'
import { useRouter } from 'next/navigation'
import { api, subscribeWebSocket } from '@/lib/api'
import { createWhatsAppChat, deviceDisplayPhone, relationClassName, relationLabel, resolveWhatsAppChat, type WhatsAppDeviceOption } from '@/lib/whatsappChatLauncher'
import ChatPanel from '@/components/chat/ChatPanel'
import LeadDetailPanel from '@/components/LeadDetailPanel'
import ObservationHistoryModal from '@/components/ObservationHistoryModal'
import BulkGenerateDocumentModal from '@/components/BulkGenerateDocumentModal'
import { Chat } from '@/types/chat'
import type { StructuredTag, PipelineStage, Pipeline, Lead, Observation } from '@/types/contact'
import type { CustomFieldDefinition, CustomFieldValue, CustomFieldFilter } from '@/types/custom-field'

interface Device {
  id: string
  name: string
  phone?: string | null
  jid?: string | null
  status: string
  normalized_phone?: string
  historical_relation?: WhatsAppDeviceOption['historical_relation']
  matches_historical?: boolean
  has_different_number?: boolean
  history_unknown?: boolean
}

interface StageData {
  id: string
  pipeline_id: string
  name: string
  color: string
  position: number
  total_count: number
  leads: Lead[]
  has_more: boolean
}

interface TagInfo {
  name: string
  color: string
  count: number
}

// --- Memoized LeadCard component (avoids re-rendering all cards on any state change) ---
interface LeadCardProps {
  lead: Lead
  isSelected: boolean
  isDetailActive: boolean
  isDragged: boolean
  selectionMode: boolean
  onToggleSelection: (id: string) => void
  onOpenDetail: (lead: Lead) => void
  onDelete: (id: string) => void
  onDragStart: (e: React.DragEvent, id: string) => void
  onDragEnd: (e: React.DragEvent) => void
}

const LeadCard = memo(function LeadCard({
  lead, isSelected, isDetailActive, isDragged, selectionMode,
  onToggleSelection, onOpenDetail, onDelete, onDragStart, onDragEnd,
}: LeadCardProps) {
  return (
    <div
      draggable={!selectionMode}
      onDragStart={(e) => onDragStart(e, lead.id)}
      onDragEnd={onDragEnd}
      className={`bg-white p-3 rounded-xl shadow-sm border hover:shadow-md transition cursor-pointer ${
        isSelected ? 'border-emerald-500 ring-2 ring-emerald-100'
        : isDetailActive ? 'border-emerald-400 ring-2 ring-emerald-200 bg-emerald-50/50'
        : 'border-slate-100'
      } ${isDragged ? 'opacity-50' : ''} ${!selectionMode ? 'cursor-grab active:cursor-grabbing' : ''}`}
      onClick={() => selectionMode ? onToggleSelection(lead.id) : onOpenDetail(lead)}
    >
      <div className="flex items-start justify-between group">
        <div className="flex items-center gap-2">
          {selectionMode ? (
            <button onClick={(e) => { e.stopPropagation(); onToggleSelection(lead.id) }} className="p-0.5">
              {isSelected ? <CheckSquare className="w-4 h-4 text-emerald-600" /> : <Square className="w-4 h-4 text-slate-300" />}
            </button>
          ) : (
            <div className="w-7 h-7 bg-emerald-50 rounded-full flex items-center justify-center">
              <span className="text-emerald-700 text-xs font-semibold">{(lead.name || '?').charAt(0).toUpperCase()}</span>
            </div>
          )}
          <p className="text-[13px] font-medium text-slate-900 truncate max-w-[150px]">{lead.name || 'Sin nombre'}</p>
          {lead.kommo_id && (
            <span title={lead.kommo_deleted_at ? `Eliminado de Kommo #${lead.kommo_id}` : `Vinculado a Kommo #${lead.kommo_id}`} className={`flex-shrink-0 flex items-center gap-0.5 px-1.5 py-0.5 rounded-full text-[10px] font-medium leading-none ${lead.kommo_deleted_at ? 'bg-amber-50 text-amber-600' : 'bg-emerald-50 text-emerald-600'}`}>
              <RefreshCw className="w-2.5 h-2.5" />{lead.kommo_deleted_at ? 'K✗' : 'K'}
            </span>
          )}
        </div>
        {!selectionMode && (
          <button
            onClick={(e) => { e.stopPropagation(); onDelete(lead.id) }}
            className="p-1 text-slate-300 hover:text-red-500 opacity-0 group-hover:opacity-100 transition-opacity"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        )}
      </div>
      {lead.phone && (
        <div className="flex items-center gap-1.5 mt-1.5 text-xs text-slate-500"><Phone className="w-3 h-3" />{lead.phone}</div>
      )}
      {lead.email && (
        <div className="flex items-center gap-1.5 mt-1 text-xs text-slate-500"><Mail className="w-3 h-3" /><span className="truncate max-w-[180px]">{lead.email}</span></div>
      )}
      {lead.company && (
        <div className="flex items-center gap-1.5 mt-1 text-xs text-slate-400"><Building2 className="w-3 h-3" /><span className="truncate max-w-[180px]">{lead.company}</span></div>
      )}
      {lead.structured_tags && lead.structured_tags.length > 0 ? (
        <div className="flex flex-wrap gap-1 mt-2">
          {lead.structured_tags.slice(0, 3).map((tag) => (
            <span key={tag.id} className="px-1.5 py-0.5 text-[10px] rounded-full text-white font-medium" style={{ backgroundColor: tag.color || '#6b7280' }}>{tag.name}</span>
          ))}
          {lead.structured_tags.length > 3 && <span className="px-1.5 py-0.5 text-slate-400 text-[10px]">+{lead.structured_tags.length - 3}</span>}
        </div>
      ) : lead.tags && lead.tags.length > 0 ? (
        <div className="flex flex-wrap gap-1 mt-2">
          {lead.tags.slice(0, 2).map((tag, i) => <span key={i} className="px-1.5 py-0.5 bg-slate-100 text-slate-500 text-[10px] rounded-full">{tag}</span>)}
          {lead.tags.length > 2 && <span className="px-1.5 py-0.5 text-slate-400 text-[10px]">+{lead.tags.length - 2}</span>}
        </div>
      ) : null}
      <div className="flex items-center justify-between mt-2 text-[10px] text-slate-400">
        <span>{formatDistanceToNow(new Date(lead.created_at), { locale: es })}</span>
        <MessageCircle className="w-3 h-3" />
      </div>
    </div>
  )
})

// --- Virtualized Kanban Column with Infinite Scroll ---
interface VirtualColumnProps {
  column: { id: string; name: string; color: string; leads: Lead[] }
  totalCount: number
  hasMore: boolean
  loadingMore: boolean
  onLoadMore: () => void
  selectedIds: Set<string>
  detailLeadId: string | null
  draggedLeadId: string | null
  dragOverColumn: string | null
  selectionMode: boolean
  onToggleSelection: (id: string) => void
  onOpenDetail: (lead: Lead) => void
  onDelete: (id: string) => void
  onDragStart: (e: React.DragEvent, id: string) => void
  onDragEnd: (e: React.DragEvent) => void
  onDragOver: (e: React.DragEvent, stageId: string) => void
  onDragLeave: (e: React.DragEvent) => void
  onDrop: (e: React.DragEvent, stageId: string) => void
}

const VirtualKanbanColumn = memo(function VirtualKanbanColumn({
  column, totalCount, hasMore, loadingMore, onLoadMore,
  selectedIds, detailLeadId, draggedLeadId, dragOverColumn, selectionMode,
  onToggleSelection, onOpenDetail, onDelete, onDragStart, onDragEnd, onDragOver, onDragLeave, onDrop,
}: VirtualColumnProps) {
  const parentRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: column.leads.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 120,
    overscan: 5,
  })

  // Infinite scroll: load more when near bottom
  useEffect(() => {
    const el = parentRef.current
    if (!el || !hasMore || loadingMore) return
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      if (scrollHeight - scrollTop - clientHeight < 300) {
        onLoadMore()
      }
    }
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [hasMore, loadingMore, onLoadMore])

  return (
    <div className="w-[272px] flex-shrink-0 flex flex-col" style={{ maxHeight: '100%' }}>
      <div
        className="px-3 py-2.5 rounded-t-xl sticky top-0 z-10 shrink-0"
        style={{ background: `linear-gradient(135deg, ${column.color}30, ${column.color}18)`, borderBottom: `3px solid ${column.color}`, boxShadow: `0 2px 8px ${column.color}20` }}
      >
        <div className="flex items-center justify-between">
          <span className="text-sm font-bold tracking-wide uppercase text-slate-800">{column.name}</span>
          <div className="flex items-center gap-1.5">
            {column.leads.length < totalCount && (
              <span className="text-[10px] text-slate-500 font-medium tabular-nums">{column.leads.length}/</span>
            )}
            <span className="text-xs px-2 py-0.5 rounded-full font-bold text-white tabular-nums" style={{ backgroundColor: column.color }}>{totalCount}</span>
          </div>
        </div>
      </div>
      <div
        ref={parentRef}
        className={`bg-slate-50/80 p-2 flex-1 overflow-y-auto kanban-col-scroll transition-colors ${
          dragOverColumn === column.id ? 'bg-emerald-50 ring-2 ring-emerald-300 ring-inset' : ''
        }`}
        style={{ minHeight: 200 }}
        onDragOver={(e) => onDragOver(e, column.id)}
        onDragLeave={onDragLeave}
        onDrop={(e) => onDrop(e, column.id)}
      >
        <div style={{ height: virtualizer.getTotalSize(), position: 'relative', width: '100%' }}>
          {virtualizer.getVirtualItems().map((virtualItem) => {
            const lead = column.leads[virtualItem.index]
            return (
              <div
                key={lead.id}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  transform: `translateY(${virtualItem.start}px)`,
                }}
              >
                <div className="pb-2">
                  <LeadCard
                    lead={lead}
                    isSelected={selectedIds.has(lead.id)}
                    isDetailActive={detailLeadId === lead.id}
                    isDragged={draggedLeadId === lead.id}
                    selectionMode={selectionMode}
                    onToggleSelection={onToggleSelection}
                    onOpenDetail={onOpenDetail}
                    onDelete={onDelete}
                    onDragStart={onDragStart}
                    onDragEnd={onDragEnd}
                  />
                </div>
              </div>
            )
          })}
        </div>
        {loadingMore && (
          <div className="flex items-center justify-center py-3">
            <div className="animate-spin rounded-full h-5 w-5 border-2 border-slate-200 border-t-emerald-500" />
          </div>
        )}
        {!hasMore && column.leads.length > 0 && column.leads.length >= totalCount && totalCount > 50 && (
          <p className="text-center text-[10px] text-slate-400 py-2">Todos cargados</p>
        )}
      </div>
    </div>
  )
})

// ─── Date Filter Presets ──────────────────────────────────────────────────────
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
    case 'last_15m': {
      const from = new Date(now.getTime() - 15 * 60 * 1000)
      return { from: from.toISOString(), to: now.toISOString() }
    }
    case 'last_hour': {
      const from = new Date(now.getTime() - 60 * 60 * 1000)
      return { from: from.toISOString(), to: now.toISOString() }
    }
    case 'today': {
      const start = new Date(now); start.setHours(0, 0, 0, 0)
      return { from: start.toISOString(), to: now.toISOString() }
    }
    case 'yesterday': {
      const start = new Date(now); start.setDate(start.getDate() - 1); start.setHours(0, 0, 0, 0)
      const end = new Date(start); end.setHours(23, 59, 59, 999)
      return { from: start.toISOString(), to: end.toISOString() }
    }
    case 'last_7d': {
      const from = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      return { from: from.toISOString(), to: now.toISOString() }
    }
    case 'this_week': {
      const start = new Date(now); const dow = start.getDay(); start.setDate(start.getDate() - (dow === 0 ? 6 : dow - 1)); start.setHours(0, 0, 0, 0)
      return { from: start.toISOString(), to: now.toISOString() }
    }
    case 'this_month': {
      const start = new Date(now.getFullYear(), now.getMonth(), 1)
      return { from: start.toISOString(), to: now.toISOString() }
    }
    case 'last_30d': {
      const from = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000)
      return { from: from.toISOString(), to: now.toISOString() }
    }
    case 'custom': {
      if (!customFrom && !customTo) return null
      const from = customFrom ? new Date(customFrom + 'T00:00:00').toISOString() : ''
      const to = customTo ? new Date(customTo + 'T23:59:59').toISOString() : ''
      return { from, to }
    }
    default: return null
  }
}

export default function LeadsPage() {
  const router = useRouter()
  // Server-side paginated data
  const [stageData, setStageData] = useState<StageData[]>([])
  const [unassignedData, setUnassignedData] = useState<{ total_count: number; leads: Lead[]; has_more: boolean }>({ total_count: 0, leads: [], has_more: false })
  const [allTags, setAllTags] = useState<TagInfo[]>([])
  const [loadingMoreStages, setLoadingMoreStages] = useState<Set<string>>(new Set())
  const [pipelines, setPipelines] = useState<Pipeline[]>([])
  const [activePipeline, setActivePipeline] = useState<Pipeline | null>(null)
  const [pipelinesLoaded, setPipelinesLoaded] = useState(false)
  const [loading, setLoading] = useState(true)
  const [searchTerm, setSearchTerm] = useState('')
  const [debouncedSearchTerm, setDebouncedSearchTerm] = useState('')
  const [showFilterDropdown, setShowFilterDropdown] = useState(false)
  const [filterStageIds, setFilterStageIds] = useState<Set<string>>(new Set())
  const [filterTagNames, setFilterTagNames] = useState<Set<string>>(new Set())
  const [excludeFilterTagNames, setExcludeFilterTagNames] = useState<Set<string>>(new Set())
  const [tagFilterMode, setTagFilterMode] = useState<'OR' | 'AND'>('OR')
  const [tagSearchTerm, setTagSearchTerm] = useState('')
  // Advanced formula filter
  const [leadFormulaType, setLeadFormulaType] = useState<'simple' | 'advanced'>('simple')
  const [leadFormulaText, setLeadFormulaText] = useState('')
  const [leadFormulaIsValid, setLeadFormulaIsValid] = useState(true)
  // Applied formula (only applied on clicking Aplicar)
  const [appliedFormulaType, setAppliedFormulaType] = useState<'simple' | 'advanced'>('simple')
  const [appliedFormulaText, setAppliedFormulaText] = useState('')
  const [showAddModal, setShowAddModal] = useState(false)
  const [showEditModal, setShowEditModal] = useState(false)
  const [selectedLead, setSelectedLead] = useState<Lead | null>(null)
  const [selectionMode, setSelectionMode] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState(false)
  const [draggedLeadId, setDraggedLeadId] = useState<string | null>(null)
  const [dragOverColumn, setDragOverColumn] = useState<string | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    phone: '',
    email: '',
    notes: '',
    tags: '',
    stage_id: '',
    dni: '',
    birth_date: '',
  })

  // Detail panel
  const [showDetailPanel, setShowDetailPanel] = useState(false)
  const [detailLead, setDetailLead] = useState<Lead | null>(null)
  const [scrollToTasks, setScrollToTasks] = useState(false)
  const [observations, setObservations] = useState<Observation[]>([])
  const [loadingObservations, setLoadingObservations] = useState(false)
  const [newObservation, setNewObservation] = useState('')
  const [newObservationType, setNewObservationType] = useState<'note' | 'call'>('note')
  const [savingObservation, setSavingObservation] = useState(false)
  const [obsDisplayCount, setObsDisplayCount] = useState(5)
  const [showHistoryModal, setShowHistoryModal] = useState(false)
  const [showImportModal, setShowImportModal] = useState(false)
  const [showContactImportModal, setShowContactImportModal] = useState(false)
  const [importingContacts, setImportingContacts] = useState(false)
  const [historyFilterType, setHistoryFilterType] = useState('')
  const [historyFilterFrom, setHistoryFilterFrom] = useState('')
  const [historyFilterTo, setHistoryFilterTo] = useState('')

  // Inline editing for lead fields
  const [editingField, setEditingField] = useState<string | null>(null)
  const [editValues, setEditValues] = useState<Record<string, string>>({})
  const [savingField, setSavingField] = useState(false)
  const [editingNotes, setEditingNotes] = useState(false)
  const [notesValue, setNotesValue] = useState('')
  const [savingNotes, setSavingNotes] = useState(false)

  // Pipeline stage management
  const [showStageModal, setShowStageModal] = useState(false)
  const [newStageName, setNewStageName] = useState('')
  const [newStageColor, setNewStageColor] = useState('#6366f1')
  const [editingStageId, setEditingStageId] = useState<string | null>(null)
  const [editStageName, setEditStageName] = useState('')
  const [editStageColor, setEditStageColor] = useState('')
  const [hiddenStageIds, setHiddenStageIds] = useState<Set<string>>(new Set())
  const [dragSrcIdx, setDragSrcIdx] = useState<number | null>(null)
  const [dragOverIdx, setDragOverIdx] = useState<number | null>(null)
  const [expandedPipelineId, setExpandedPipelineId] = useState<string | null>(null)

  // Click outside to close dropdown

  // Device selector for WhatsApp
  const [showDeviceSelector, setShowDeviceSelector] = useState(false)
  const [devices, setDevices] = useState<Device[]>([])
  const [whatsappPhone, setWhatsappPhone] = useState('')

  // Inline chat panel
  const [showInlineChat, setShowInlineChat] = useState(false)
  const [inlineChatId, setInlineChatId] = useState('')
  const [inlineChat, setInlineChat] = useState<Chat | null>(null)
  const [inlineChatDeviceId, setInlineChatDeviceId] = useState('')
  const [inlineChatReadOnly, setInlineChatReadOnly] = useState(false)
  const [existingChatForWA, setExistingChatForWA] = useState<any>(null)
  const [allDevicesForModal, setAllDevicesForModal] = useState<Device[]>([])
  const [whatsappHistoricalPhone, setWhatsappHistoricalPhone] = useState('')

  // Device filter for leads
  const [filterDeviceIds, setFilterDeviceIds] = useState<Set<string>>(new Set())
  const [showDeviceFilter, setShowDeviceFilter] = useState(false)

  // Date filter
  const [filterDateField, setFilterDateField] = useState<'created_at' | 'updated_at'>('created_at')
  const [filterDatePreset, setFilterDatePreset] = useState('')
  const [filterDateFrom, setFilterDateFrom] = useState('')
  const [filterDateTo, setFilterDateTo] = useState('')

  // Broadcast from leads
  const [showBroadcastModal, setShowBroadcastModal] = useState(false)
  const [submittingBroadcast, setSubmittingBroadcast] = useState(false)

  // View mode: kanban vs list
  const [viewMode, setViewMode] = useState<'kanban' | 'list'>('kanban')
  const prevViewModeRef = useRef<'kanban' | 'list'>('kanban')

  // Status filter: active, archived, blocked
  const [statusFilter, setStatusFilter] = useState<'active' | 'archived' | 'blocked'>('active')
  const [leadCounts, setLeadCounts] = useState({ active: 0, archived: 0, blocked: 0 })
  const [hiddenByStatus, setHiddenByStatus] = useState(0)

  // List view paginated data
  const [listLeads, setListLeads] = useState<Lead[]>([])
  const [listTotal, setListTotal] = useState(0)
  const [listHasMore, setListHasMore] = useState(false)
  const [listLoading, setListLoading] = useState(false)

  // Custom field columns for list view
  const [cfDefs, setCfDefs] = useState<CustomFieldDefinition[]>([])
  const [cfVisibleIds, setCfVisibleIds] = useState<Set<string>>(new Set())
  const [showCfColumnPicker, setShowCfColumnPicker] = useState(false)
  const cfColumnPickerRef = useRef<HTMLDivElement>(null)
  const [cfFilters, setCfFilters] = useState<CustomFieldFilter[]>([])

  // "Más" dropdown menu
  const [showMoreMenu, setShowMoreMenu] = useState(false)
  const moreMenuRef = useRef<HTMLDivElement>(null)

  // Export
  const [showExportModal, setShowExportModal] = useState(false)
  const [exportFormat, setExportFormat] = useState<'excel' | 'csv'>('excel')
  const [exportScope, setExportScope] = useState<'all' | 'filtered'>('filtered')
  const [exporting, setExporting] = useState(false)

  // Bulk document generation
  const [showBulkDocModal, setShowBulkDocModal] = useState(false)

  // Create Event from Leads modal
  const [showCreateEventModal, setShowCreateEventModal] = useState(false)
  const [createEventForm, setCreateEventForm] = useState({ name: '', description: '', event_date: '', event_end: '', location: '', color: '#10b981' })
  const [creatingEvent, setCreatingEvent] = useState(false)

  // List view observations cache
  const [listObservations, setListObservations] = useState<Map<string, Observation[]>>(new Map())

  // Google Contacts sync
  const [googleConnected, setGoogleConnected] = useState(false)
  const [googleSyncing, setGoogleSyncing] = useState(false)

  const [loadingListObs, setLoadingListObs] = useState<Set<string>>(new Set())
  const [expandedListLeadId, setExpandedListLeadId] = useState<string | null>(null)
  const [listHistoryLead, setListHistoryLead] = useState<Lead | null>(null)

  const kanbanRef = useRef<HTMLDivElement>(null)
  const topScrollRef = useRef<HTMLDivElement>(null)
  const listScrollRef = useRef<HTMLDivElement>(null)
  const listOffsetRef = useRef(0)
  const filterDropdownRef = useRef<HTMLDivElement>(null)
  const syncingScroll = useRef(false)
  const activePipelineIdRef = useRef<string | null>(null)

  useEffect(() => {
    activePipelineIdRef.current = activePipeline?.id || null
  }, [activePipeline?.id])

  // Ctrl+drag kanban panning
  useKanbanPan(kanbanRef, topScrollRef)

  // Click outside to close dropdowns
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (filterDropdownRef.current && !filterDropdownRef.current.contains(event.target as Node)) {
        setShowFilterDropdown(false)
      }
      if (moreMenuRef.current && !moreMenuRef.current.contains(event.target as Node)) {
        setShowMoreMenu(false)
      }
    }
    document.addEventListener("mousedown", handleClickOutside)
    return () => {
      document.removeEventListener("mousedown", handleClickOutside)
    }
  }, [filterDropdownRef])

  const fetchPipelines = useCallback(async (preferredPipelineId?: string | null) => {
    const token = localStorage.getItem('token')
    try {
      const pipelinesRes = await fetch('/api/pipelines', { headers: { Authorization: `Bearer ${token}` } })
      const data = await pipelinesRes.json()
      if (data.success && data.pipelines && data.pipelines.length > 0) {
        setPipelines(data.pipelines)
        const keepPipelineId = preferredPipelineId ?? activePipelineIdRef.current
        if (keepPipelineId === '__no_pipeline__') {
          setActivePipeline({ id: '__no_pipeline__', name: 'Sin pipeline', is_default: false, stages: [] })
          return
        }
        const currentP = keepPipelineId ? data.pipelines.find((p: Pipeline) => p.id === keepPipelineId) : null
        const defaultP = data.pipelines.find((p: Pipeline) => p.is_default) || data.pipelines[0]
        if (currentP || defaultP) setActivePipeline(currentP || defaultP)
      } else {
        setPipelines([])
      }
    } catch (err) {
      console.error('Failed to fetch pipelines:', err)
    } finally {
      setPipelinesLoaded(true)
    }
  }, [])

  const fetchLeadCounts = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      const params = new URLSearchParams()
      if (activePipeline) params.set('pipeline_id', activePipeline.id)
      const res = await fetch(`/api/leads/counts?${params}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        setLeadCounts({ active: data.active || 0, archived: data.archived || 0, blocked: data.blocked || 0 })
      }
    } catch (err) {
      console.error('Failed to fetch lead counts:', err)
    }
  }, [activePipeline])

  const fetchLeadsPaginated = useCallback(async () => {
    const token = localStorage.getItem('token')
    try {
      const params = new URLSearchParams()
      params.set('status_filter', statusFilter)
      if (activePipeline) params.set('pipeline_id', activePipeline.id)
      params.set('per_stage', '50')
      if (debouncedSearchTerm) params.set('search', debouncedSearchTerm)
      if (appliedFormulaType === 'advanced' && appliedFormulaText) {
        params.set('tag_formula', appliedFormulaText)
      } else {
        if (filterTagNames.size > 0) params.set('tag_names', Array.from(filterTagNames).join(','))
        if (filterTagNames.size > 0 || excludeFilterTagNames.size > 0) params.set('tag_mode', tagFilterMode)
        if (excludeFilterTagNames.size > 0) params.set('exclude_tag_names', Array.from(excludeFilterTagNames).join(','))
      }
      if (filterStageIds.size > 0) params.set('stage_ids', Array.from(filterStageIds).join(','))
      filterDeviceIds.forEach(id => params.append('device_ids', id))
      const dateRange = resolveDatePreset(filterDatePreset, filterDateFrom, filterDateTo)
      if (dateRange) {
        params.set('date_field', filterDateField)
        if (dateRange.from) params.set('date_from', dateRange.from)
        if (dateRange.to) params.set('date_to', dateRange.to)
      }
      const res = await fetch(`/api/leads/paginated?${params}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        setStageData((data.stages || []).map((s: StageData) => ({ ...s, leads: s.leads || [] })))
        const ua = data.unassigned || { total_count: 0, leads: [], has_more: false }
        setUnassignedData({ ...ua, leads: ua.leads || [] })
        setAllTags(data.all_tags || [])
        setHiddenByStatus(data.hidden_by_status || 0)
      }
    } catch (err) {
      console.error('Failed to fetch leads:', err)
    } finally {
      setLoading(false)
    }
  }, [statusFilter, activePipeline, debouncedSearchTerm, filterTagNames, excludeFilterTagNames, tagFilterMode, filterStageIds, filterDeviceIds, appliedFormulaType, appliedFormulaText, filterDateField, filterDatePreset, filterDateFrom, filterDateTo])

  const fetchListLeads = useCallback(async (reset: boolean = false) => {
    setListLoading(true)
    const offset = reset ? 0 : listOffsetRef.current
    const token = localStorage.getItem('token')
    try {
      const params = new URLSearchParams()
      params.set('status_filter', statusFilter)
      // When searching, omit pipeline_id to find leads across all pipelines
      if (activePipeline && !debouncedSearchTerm) params.set('pipeline_id', activePipeline.id)
      params.set('offset', String(offset))
      params.set('limit', '100')
      if (debouncedSearchTerm) params.set('search', debouncedSearchTerm)
      if (appliedFormulaType === 'advanced' && appliedFormulaText) {
        params.set('tag_formula', appliedFormulaText)
      } else {
        if (filterTagNames.size > 0) params.set('tag_names', Array.from(filterTagNames).join(','))
        if (filterTagNames.size > 0 || excludeFilterTagNames.size > 0) params.set('tag_mode', tagFilterMode)
        if (excludeFilterTagNames.size > 0) params.set('exclude_tag_names', Array.from(excludeFilterTagNames).join(','))
      }
      if (filterStageIds.size > 0) params.set('stage_ids', Array.from(filterStageIds).join(','))
      filterDeviceIds.forEach(id => params.append('device_ids', id))
      const dateRange = resolveDatePreset(filterDatePreset, filterDateFrom, filterDateTo)
      if (dateRange) {
        params.set('date_field', filterDateField)
        if (dateRange.from) params.set('date_from', dateRange.from)
        if (dateRange.to) params.set('date_to', dateRange.to)
      }
      if (cfVisibleIds.size > 0) params.set('include_custom_fields', 'true')
      if (cfFilters.length > 0) params.set('cf_filter', JSON.stringify(cfFilters))
      const res = await fetch(`/api/leads/list-paginated?${params}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        const newLeads = data.leads || []
        if (reset) {
          setListLeads(newLeads)
          listOffsetRef.current = newLeads.length
        } else {
          setListLeads(prev => [...prev, ...newLeads])
          listOffsetRef.current = offset + newLeads.length
        }
        setListTotal(data.total || 0)
        setListHasMore(data.has_more || false)
      }
    } catch (err) {
      console.error('Failed to fetch list leads:', err)
    } finally {
      setListLoading(false)
    }
  }, [statusFilter, activePipeline, debouncedSearchTerm, filterTagNames, excludeFilterTagNames, tagFilterMode, filterStageIds, filterDeviceIds, appliedFormulaType, appliedFormulaText, filterDateField, filterDatePreset, filterDateFrom, filterDateTo, cfVisibleIds, cfFilters])

  const loadMoreForStage = useCallback(async (stageId: string) => {
    if (loadingMoreStages.has(stageId)) return
    setLoadingMoreStages(prev => new Set(prev).add(stageId))
    const token = localStorage.getItem('token')
    try {
      const isUnassigned = stageId === '__unassigned__'
      const currentLeads = isUnassigned
        ? unassignedData.leads
        : stageData.find(s => s.id === stageId)?.leads || []
      const params = new URLSearchParams()
      params.set('status_filter', statusFilter)
      params.set('offset', String(currentLeads.length))
      params.set('limit', '50')
      if (activePipeline) params.set('pipeline_id', activePipeline.id)
      if (debouncedSearchTerm) params.set('search', debouncedSearchTerm)
      if (appliedFormulaType === 'advanced' && appliedFormulaText) {
        params.set('tag_formula', appliedFormulaText)
      } else {
        if (filterTagNames.size > 0) params.set('tag_names', Array.from(filterTagNames).join(','))
        if (filterTagNames.size > 0 || excludeFilterTagNames.size > 0) params.set('tag_mode', tagFilterMode)
        if (excludeFilterTagNames.size > 0) params.set('exclude_tag_names', Array.from(excludeFilterTagNames).join(','))
      }
      filterDeviceIds.forEach(id => params.append('device_ids', id))
      const dateRange = resolveDatePreset(filterDatePreset, filterDateFrom, filterDateTo)
      if (dateRange) {
        params.set('date_field', filterDateField)
        if (dateRange.from) params.set('date_from', dateRange.from)
        if (dateRange.to) params.set('date_to', dateRange.to)
      }
      const endpoint = isUnassigned ? 'unassigned' : stageId
      const res = await fetch(`/api/leads/by-stage/${endpoint}?${params}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        const newLeads = data.leads || []
        if (isUnassigned) {
          setUnassignedData(prev => ({ ...prev, leads: [...prev.leads, ...newLeads], has_more: data.has_more }))
        } else {
          setStageData(prev => prev.map(s => s.id === stageId ? { ...s, leads: [...s.leads, ...newLeads], has_more: data.has_more } : s))
        }
      }
    } catch (err) {
      console.error('Failed to load more leads:', err)
    } finally {
      setLoadingMoreStages(prev => { const next = new Set(prev); next.delete(stageId); return next })
    }
  }, [loadingMoreStages, stageData, unassignedData, activePipeline, debouncedSearchTerm, filterTagNames, excludeFilterTagNames, tagFilterMode, filterDeviceIds, appliedFormulaType, appliedFormulaText, filterDateField, filterDatePreset, filterDateFrom, filterDateTo])

  // Helper: update a single lead across all stage data
  const updateLeadInStages = useCallback((leadId: string, updater: (lead: Lead) => Lead) => {
    setStageData(prev => prev.map(stage => ({
      ...stage,
      leads: stage.leads.map(l => l.id === leadId ? updater(l) : l)
    })))
    setUnassignedData(prev => ({
      ...prev,
      leads: prev.leads.map(l => l.id === leadId ? updater(l) : l)
    }))
    setListLeads(prev => prev.map(l => l.id === leadId ? updater(l) : l))
  }, [])

  // Helper: remove lead from all stage data
  const removeLeadFromStages = useCallback((leadId: string) => {
    setStageData(prev => prev.map(stage => ({
      ...stage,
      leads: stage.leads.filter(l => l.id !== leadId),
      total_count: stage.leads.some(l => l.id === leadId) ? stage.total_count - 1 : stage.total_count
    })))
    setUnassignedData(prev => ({
      ...prev,
      leads: prev.leads.filter(l => l.id !== leadId),
      total_count: prev.leads.some(l => l.id === leadId) ? prev.total_count - 1 : prev.total_count
    }))
    setListLeads(prev => prev.filter(l => l.id !== leadId))
  }, [])

  // All loaded leads from visible stages
  const allLoadedLeads = useMemo(() => {
    const all: Lead[] = []
    stageData.forEach(s => all.push(...(s.leads || [])))
    all.push(...(unassignedData.leads || []))
    return all
  }, [stageData, unassignedData])

  // Find lead by ID across all loaded data
  const findLeadById = useCallback((leadId: string): Lead | undefined => {
    for (const stage of stageData) {
      const found = (stage.leads || []).find(l => l.id === leadId)
      if (found) return found
    }
    const inUnassigned = (unassignedData.leads || []).find(l => l.id === leadId)
    if (inUnassigned) return inUnassigned
    return listLeads.find(l => l.id === leadId) || (detailLead?.id === leadId ? detailLead : undefined)
  }, [stageData, unassignedData, listLeads, detailLead])

  // Total count from server (all matching leads, not just loaded)
  const totalLeadCount = useMemo(() =>
    stageData.reduce((sum, s) => sum + s.total_count, 0) + unassignedData.total_count,
    [stageData, unassignedData]
  )

  const fetchDevices = useCallback(async () => {
    const result = await api<{ devices?: Device[] }>('/api/devices')
    if (result.success && result.data) {
      setDevices((result.data.devices || []).filter((d: Device) => d.status === 'connected'))
    }
  }, [])

  useEffect(() => {
    fetchPipelines()
    fetchDevices()
    // Check Google Contacts connection status
    fetch('/api/google/status').then(r => r.json()).then(d => setGoogleConnected(!!d.connected)).catch(() => {})
    // Fetch custom field definitions
    const token = localStorage.getItem('token')
    if (token) {
      fetch('/api/custom-fields', { headers: { Authorization: `Bearer ${token}` } })
        .then(r => r.json())
        .then(d => {
          if (d.success) {
            const defs: CustomFieldDefinition[] = d.definitions || []
            setCfDefs(defs)
            try {
              const saved = localStorage.getItem('cf_columns_leads')
              if (saved) {
                const ids: string[] = JSON.parse(saved)
                const validIds = ids.filter(id => defs.some(def => def.id === id))
                setCfVisibleIds(new Set(validIds))
              }
            } catch {}
          }
        })
        .catch(() => {})
    }
    // Load hidden stages from localStorage
    try {
      const saved = localStorage.getItem('hiddenStageIds')
      if (saved) setHiddenStageIds(new Set(JSON.parse(saved)))
    } catch {}
  }, [fetchPipelines])

  // Auto-open lead detail from URL params (e.g. ?lead_id=UUID&scroll=tasks)
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const leadId = params.get('lead_id')
    const scroll = params.get('scroll')
    if (!leadId) return

    // Clear URL params to avoid re-triggering
    window.history.replaceState({}, '', window.location.pathname)

    const fetchAndOpenLead = async () => {
      try {
        const token = localStorage.getItem('token')
        const res = await fetch(`/api/leads/${leadId}`, {
          headers: { Authorization: `Bearer ${token}` },
        })
        const data = await res.json()
        if (data.success && data.lead) {
          setDetailLead(data.lead)
          setShowDetailPanel(true)
          if (scroll === 'tasks') setScrollToTasks(true)
        }
      } catch { /* ignore */ }
    }
    fetchAndOpenLead()
  }, [])

  // Fetch paginated kanban data when pipelines loaded or pipeline/filters change
  useEffect(() => {
    if (pipelinesLoaded) {
      fetchLeadsPaginated()
      fetchLeadCounts()
    }
  }, [pipelinesLoaded, fetchLeadsPaginated, fetchLeadCounts])

  // Fetch list data when in list view (and when filters change)
  useEffect(() => {
    if (viewMode === 'list' && pipelinesLoaded) {
      fetchListLeads(true)
    }
  }, [viewMode, fetchListLeads, pipelinesLoaded])

  // Auto-switch to list view when search is active (cross-pipeline results work best in list)
  useEffect(() => {
    if (debouncedSearchTerm) {
      if (viewMode !== 'list') {
        prevViewModeRef.current = viewMode
        setViewMode('list')
      }
    } else {
      setViewMode(prevViewModeRef.current)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearchTerm])

  // WebSocket: listen for lead_update events — delta updates for paginated data
  useEffect(() => {
    const unsubscribe = subscribeWebSocket((data: unknown) => {
      const msg = data as { event?: string; action?: string; lead?: Lead; lead_id?: string; stage_id?: string }
      if (msg.event === 'lead_update') {
        if (msg.action === 'created' && msg.lead) {
          const lead = msg.lead!
          // Add to appropriate stage if it matches current pipeline
          if (lead.pipeline_id === activePipeline?.id) {
            if (lead.stage_id) {
              setStageData(prev => prev.map(s => s.id === lead.stage_id
                ? { ...s, leads: [lead, ...s.leads], total_count: s.total_count + 1 }
                : s
              ))
            } else {
              setUnassignedData(prev => ({
                ...prev,
                leads: [lead, ...prev.leads],
                total_count: prev.total_count + 1
              }))
            }
          } else if (!lead.pipeline_id) {
            setUnassignedData(prev => ({
              ...prev,
              leads: [lead, ...prev.leads],
              total_count: prev.total_count + 1
            }))
          }
        } else if (msg.action === 'updated' && msg.lead) {
          updateLeadInStages(msg.lead.id, l => ({ ...l, ...msg.lead! }))
          if (detailLead?.id === msg.lead.id) {
            setDetailLead(prev => prev ? { ...prev, ...msg.lead! } : prev)
          }
        } else if (msg.action === 'deleted' && msg.lead) {
          removeLeadFromStages(msg.lead.id)
          if (detailLead?.id === msg.lead.id) {
            setShowDetailPanel(false)
          }
        } else if (msg.action === 'stage_changed' && msg.lead_id && msg.stage_id) {
          const leadId = msg.lead_id!
          const newStageId = msg.stage_id!
          // Move lead between stages
          setStageData(prev => {
            let movedLead: Lead | undefined
            const afterRemove = prev.map(s => {
              if (s.id === newStageId && s.leads.some(l => l.id === leadId)) return s // already moved
              const idx = s.leads.findIndex(l => l.id === leadId)
              if (idx >= 0) {
                movedLead = { ...s.leads[idx], stage_id: newStageId }
                return { ...s, leads: s.leads.filter(l => l.id !== leadId), total_count: Math.max(0, s.total_count - 1) }
              }
              return s
            })
            if (movedLead) {
              return afterRemove.map(s => s.id === newStageId
                ? { ...s, leads: [movedLead!, ...s.leads], total_count: s.total_count + 1 }
                : s
              )
            }
            return prev
          })
          // Also check unassigned → stage move
          setUnassignedData(prev => {
            const idx = prev.leads.findIndex(l => l.id === leadId)
            if (idx >= 0) {
              const movedLead = { ...prev.leads[idx], stage_id: newStageId }
              setStageData(sd => sd.map(s => s.id === newStageId
                ? { ...s, leads: [movedLead, ...s.leads], total_count: s.total_count + 1 }
                : s
              ))
              return { ...prev, leads: prev.leads.filter(l => l.id !== leadId), total_count: Math.max(0, prev.total_count - 1) }
            }
            return prev
          })
        } else if (msg.action !== 'synced') {
          // Fallback: full re-fetch for unknown actions (skip background sync noise)
          fetchLeadsPaginated()
        }
      }
      // Handle interaction updates — invalidate observations cache so list view refreshes
      if (msg.event === 'interaction_update') {
        const leadId = (msg as Record<string, unknown>).lead_id as string | undefined
        if (leadId && viewMode === 'list') {
          setListObservations(prev => {
            const next = new Map(prev)
            next.delete(leadId)
            return next
          })
          setLoadingListObs(prev => {
            const next = new Set(prev)
            next.delete(leadId)
            return next
          })
          // Immediate refetch for the affected lead
          const tk = localStorage.getItem('token')
          if (tk) {
            fetch(`/api/leads/${leadId}/interactions?limit=5`, { headers: { Authorization: `Bearer ${tk}` } })
              .then(r => r.json()).then(d => {
                if (d.success) setListObservations(prev => new Map(prev).set(leadId, d.interactions || []))
              }).catch(() => {})
          }
        }
      }
      // Handle custom field definition updates
      if (msg.event === 'custom_field_def_update') {
        const tk = localStorage.getItem('token')
        if (tk) {
          fetch('/api/custom-fields', { headers: { Authorization: `Bearer ${tk}` } })
            .then(r => r.json())
            .then(d => { if (d.success) setCfDefs(d.definitions || []) })
            .catch(() => {})
        }
      }
    })
    return () => unsubscribe()
  }, [fetchLeadsPaginated, updateLeadInStages, removeLeadFromStages, detailLead, activePipeline, viewMode])

  // Custom field column toggle
  const toggleCfColumn = useCallback((fieldId: string) => {
    setCfVisibleIds(prev => {
      const next = new Set(prev)
      if (next.has(fieldId)) next.delete(fieldId)
      else next.add(fieldId)
      localStorage.setItem('cf_columns_leads', JSON.stringify(Array.from(next)))
      return next
    })
  }, [])

  // Format custom field value for list table cell
  const formatCfCell = useCallback((def: CustomFieldDefinition, lead: Lead) => {
    const vals: CustomFieldValue[] = (lead as any).custom_field_values || []
    const val = vals.find(v => v.field_id === def.id)
    if (!val) return <span className="text-slate-300">—</span>
    switch (def.field_type) {
      case 'text': case 'email': case 'phone': case 'url':
        return <span className="truncate">{val.value_text || '—'}</span>
      case 'number':
        return <span>{val.value_number != null ? val.value_number : '—'}</span>
      case 'currency': {
        if (val.value_number == null) return <span className="text-slate-300">—</span>
        const sym = def.config?.symbol || '$'
        const dec = def.config?.decimals ?? 2
        return <span>{sym} {val.value_number.toLocaleString('es-PE', { minimumFractionDigits: dec, maximumFractionDigits: dec })}</span>
      }
      case 'date':
        if (!val.value_date) return <span className="text-slate-300">—</span>
        try { return <span>{new Date(val.value_date).toLocaleDateString('es-PE', { year: 'numeric', month: 'short', day: 'numeric' })}</span> }
        catch { return <span>{val.value_date}</span> }
      case 'checkbox':
        return <span className={val.value_bool ? 'text-emerald-600' : 'text-slate-400'}>{val.value_bool ? 'Sí' : 'No'}</span>
      case 'select': {
        const opt = def.config?.options?.find(o => o.value === val.value_text)
        return <span>{opt?.label || val.value_text || '—'}</span>
      }
      case 'multi_select': {
        if (!val.value_json || val.value_json.length === 0) return <span className="text-slate-300">—</span>
        return <div className="flex flex-wrap gap-0.5">{val.value_json.slice(0, 2).map(v => {
          const o = def.config?.options?.find(opt => opt.value === v)
          return <span key={v} className="px-1.5 py-0.5 bg-slate-100 text-slate-600 text-[10px] rounded-full">{o?.label || v}</span>
        })}{val.value_json.length > 2 && <span className="text-[10px] text-slate-400">+{val.value_json.length - 2}</span>}</div>
      }
      default: return <span className="text-slate-300">—</span>
    }
  }, [])

  // Close cfColumnPicker on outside click
  useEffect(() => {
    if (!showCfColumnPicker) return
    const handler = (e: MouseEvent) => {
      if (cfColumnPickerRef.current && !cfColumnPickerRef.current.contains(e.target as Node)) {
        setShowCfColumnPicker(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showCfColumnPicker])

  // Debounce search term (500ms)
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearchTerm(searchTerm), 500)
    return () => clearTimeout(timer)
  }, [searchTerm])

  // Click outside to close filter dropdown + reset tag search
  useEffect(() => {
    if (!showFilterDropdown) {
      setTagSearchTerm('')
      return
    }
    const handleClickOutside = (e: MouseEvent) => {
      if (filterDropdownRef.current && !filterDropdownRef.current.contains(e.target as Node)) {
        setShowFilterDropdown(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [showFilterDropdown])

  // Sync horizontal scroll between top scrollbar and kanban
  const handleTopScroll = () => {
    if (syncingScroll.current) return
    syncingScroll.current = true
    if (kanbanRef.current && topScrollRef.current) {
      kanbanRef.current.scrollLeft = topScrollRef.current.scrollLeft
    }
    syncingScroll.current = false
  }
  const handleKanbanScroll = () => {
    if (syncingScroll.current) return
    syncingScroll.current = true
    if (kanbanRef.current && topScrollRef.current) {
      topScrollRef.current.scrollLeft = kanbanRef.current.scrollLeft
    }
    syncingScroll.current = false
  }

  const toggleStageVisibility = (stageId: string) => {
    setHiddenStageIds(prev => {
      const next = new Set(prev)
      if (next.has(stageId)) next.delete(stageId)
      else next.add(stageId)
      localStorage.setItem('hiddenStageIds', JSON.stringify(Array.from(next)))
      return next
    })
  }

  const handleReorderStages = async (fromIdx: number, toIdx: number) => {
    if (!activePipeline || fromIdx === toIdx) return
    const reordered = [...allStages]
    if (
      fromIdx < 0 ||
      toIdx < 0 ||
      fromIdx >= reordered.length ||
      toIdx >= reordered.length
    ) {
      console.warn('Ignoring invalid stage reorder', { fromIdx, toIdx, total: reordered.length })
      return
    }
    const [moved] = reordered.splice(fromIdx, 1)
    if (!moved) {
      console.warn('Ignoring stage reorder without source stage', { fromIdx, toIdx, total: reordered.length })
      return
    }
    reordered.splice(toIdx, 0, moved)
    // Optimistically update
    const updated = { ...activePipeline, stages: reordered.map((s, i) => ({ ...s, position: i })) }
    setActivePipeline(updated)
    setPipelines(prev => prev.map(p => p.id === updated.id ? updated : p))
    // API call
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${activePipeline.id}/stages/reorder`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ stage_ids: reordered.map(s => s.id) }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || 'No se pudo reordenar las etapas')
      }
    } catch (err) {
      console.error('Failed to reorder stages:', err)
      fetchPipelines()
    }
  }

  const allStages = activePipeline?.stages || []
  const stages = allStages.filter(s => !hiddenStageIds.has(s.id))

  const handleCreateLead = async () => {
    const token = localStorage.getItem('token')
    try {
      const stageId = formData.stage_id || undefined
      const res = await fetch('/api/leads', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name: formData.name,
          phone: formData.phone,
          email: formData.email,
          notes: formData.notes,
          dni: formData.dni || undefined,
          birth_date: formData.birth_date || undefined,
          tags: formData.tags.split(',').map(t => t.trim()).filter(Boolean),
          stage_id: stageId || undefined,
        }),
      })
      const data = await res.json()
      if (data.success) {
        setShowAddModal(false)
        setFormData({ name: '', phone: '', email: '', notes: '', tags: '', stage_id: '', dni: '', birth_date: '' })
        fetchLeadsPaginated()
      } else {
        alert(data.error || 'Error al crear lead')
      }
    } catch (err) {
      console.error('Failed to create lead:', err)
      alert('Error al crear lead')
    }
  }

  const handleUpdateLead = async () => {
    if (!selectedLead) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/leads/${selectedLead.id}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          ...formData,
          tags: formData.tags.split(',').map(t => t.trim()).filter(Boolean),
        }),
      })
      const data = await res.json()
      if (data.success) {
        setShowEditModal(false)
        setSelectedLead(null)
        fetchLeadsPaginated()
      } else {
        alert(data.error || 'Error al actualizar lead')
      }
    } catch (err) {
      console.error('Failed to update lead:', err)
      alert('Error al actualizar lead')
    }
  }

  const handleDeleteLead = async (leadId: string) => {
    if (!confirm('¿Eliminar este lead? No se eliminará el contacto ni el chat asociado.')) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/leads/${leadId}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        fetchLeadsPaginated()
      }
    } catch (err) {
      console.error('Failed to delete lead:', err)
    }
  }

  const handleCreateLeadsFromContacts = async (contacts: SelectedPerson[]) => {
    if (contacts.length === 0 || importingContacts) return
    const token = localStorage.getItem('token')
    setImportingContacts(true)
    try {
      const res = await fetch('/api/leads/from-contacts', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ contact_ids: contacts.map(c => c.id) }),
      })
      const data = await res.json()
      if (!res.ok || !data.success) {
        alert(data.error || 'Error al crear leads desde contactos')
        return
      }
      setShowContactImportModal(false)
      await Promise.all([
        fetchLeadsPaginated(),
        fetchPipelines(activePipelineIdRef.current),
      ])
      const created = data.created || 0
      const skipped = data.skipped || 0
      if (skipped > 0) {
        alert(`Se crearon ${created} lead(s). ${skipped} contacto(s) fueron omitidos porque ya tenían lead activo o no pudieron procesarse.`)
      }
    } catch (err) {
      console.error('Failed to create leads from contacts:', err)
      alert('Error al crear leads desde contactos')
    } finally {
      setImportingContacts(false)
    }
  }

  const handleDeleteSelected = async () => {
    if (selectedIds.size === 0) return
    if (!confirm(`¿Eliminar ${selectedIds.size} lead(s)? No se eliminarán contactos ni chats asociados.`)) return
    const token = localStorage.getItem('token')
    setDeleting(true)
    try {
      const res = await fetch('/api/leads/batch', {
        method: 'DELETE',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ ids: Array.from(selectedIds) }),
      })
      const data = await res.json()
      if (data.success) {
        setSelectedIds(new Set())
        setSelectionMode(false)
        fetchLeadsPaginated()
      }
    } catch (err) {
      console.error('Failed to delete leads:', err)
    } finally {
      setDeleting(false)
    }
  }

  const handleGoogleBatchSyncFromLeads = async () => {
    if (selectedIds.size === 0 || selectedIds.size > 30) return
    const token = localStorage.getItem('token')
    setGoogleSyncing(true)
    try {
      const res = await fetch('/api/google/contacts/batch/sync-from-leads', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ lead_ids: Array.from(selectedIds) }),
      })
      const data = await res.json()
      if (res.ok) {
        const synced = (data.results || []).filter((r: any) => r.success).length
        const errors = (data.results || []).filter((r: any) => !r.success).length
        alert(`Sincronizados: ${synced} contacto(s)${errors ? `, errores: ${errors}` : ''}`)
      } else {
        alert(data.error || 'Error al sincronizar')
      }
    } catch {
      alert('Error de conexión')
    } finally {
      setGoogleSyncing(false)
    }
  }

  const handleGoogleBatchDesyncFromLeads = async () => {
    if (selectedIds.size === 0 || selectedIds.size > 30) return
    if (!confirm(`¿Desincronizar los contactos de ${selectedIds.size} lead(s) de Google?`)) return
    const token = localStorage.getItem('token')
    setGoogleSyncing(true)
    try {
      const res = await fetch('/api/google/contacts/batch/desync-from-leads', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ lead_ids: Array.from(selectedIds) }),
      })
      const data = await res.json()
      if (res.ok) {
        const desynced = (data.results || []).filter((r: any) => r.success).length
        alert(`Desincronizados: ${desynced} contacto(s)`)
      } else {
        alert(data.error || 'Error al desincronizar')
      }
    } catch {
      alert('Error de conexión')
    } finally {
      setGoogleSyncing(false)
    }
  }

  const handleArchiveLead = async (leadId: string, archive: boolean, reason: string = '') => {
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/leads/${leadId}/archive`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ archive, reason }),
      })
      const data = await res.json()
      if (data.success) {
        fetchLeadsPaginated()
        fetchLeadCounts()
        if (viewMode === 'list') fetchListLeads(true)
      }
    } catch (err) {
      console.error('Failed to archive lead:', err)
    }
  }

  const handleBlockLead = async (leadId: string, block: boolean, reason: string = '') => {
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/leads/${leadId}/block`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ block, reason }),
      })
      const data = await res.json()
      if (data.success) {
        fetchLeadsPaginated()
        fetchLeadCounts()
        if (viewMode === 'list') fetchListLeads(true)
        if (showDetailPanel) setShowDetailPanel(false)
      }
    } catch (err) {
      console.error('Failed to block lead:', err)
    }
  }

  const handleArchiveSelectedBatch = async (archive: boolean, reason: string = '') => {
    if (selectedIds.size === 0) return
    const token = localStorage.getItem('token')
    setDeleting(true)
    try {
      const res = await fetch('/api/leads/batch/archive', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ ids: Array.from(selectedIds), archive, reason }),
      })
      const data = await res.json()
      if (data.success) {
        setSelectedIds(new Set())
        setSelectionMode(false)
        fetchLeadsPaginated()
        fetchLeadCounts()
        if (viewMode === 'list') fetchListLeads(true)
      }
    } catch (err) {
      console.error('Failed to archive leads batch:', err)
    } finally {
      setDeleting(false)
    }
  }

  const handleBlockSelectedBatch = async (block: boolean, reason: string = '') => {
    if (selectedIds.size === 0) return
    const token = localStorage.getItem('token')
    setDeleting(true)
    try {
      const res = await fetch('/api/leads/batch/block', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ ids: Array.from(selectedIds), block, reason }),
      })
      const data = await res.json()
      if (data.success) {
        setSelectedIds(new Set())
        setSelectionMode(false)
        fetchLeadsPaginated()
        fetchLeadCounts()
        if (viewMode === 'list') fetchListLeads(true)
      }
    } catch (err) {
      console.error('Failed to block leads batch:', err)
    } finally {
      setDeleting(false)
    }
  }

  // Block reason modal state
  const [showBlockModal, setShowBlockModal] = useState(false)
  const [blockReason, setBlockReason] = useState('')
  const [blockTargetId, setBlockTargetId] = useState<string | null>(null)
  const [blockBatchMode, setBlockBatchMode] = useState(false)

  // Archive reason modal state
  const [showArchiveModal, setShowArchiveModal] = useState(false)
  const [archiveReason, setArchiveReason] = useState('')
  const [archiveTargetId, setArchiveTargetId] = useState<string | null>(null)
  const [archiveBatchMode, setArchiveBatchMode] = useState(false)

  const openBlockModal = (leadId: string | null, batchMode: boolean = false) => {
    setBlockTargetId(leadId)
    setBlockBatchMode(batchMode)
    setBlockReason('')
    setShowBlockModal(true)
  }

  const openArchiveModal = (leadId: string | null, batchMode: boolean = false) => {
    setArchiveTargetId(leadId)
    setArchiveBatchMode(batchMode)
    setArchiveReason('')
    setShowArchiveModal(true)
  }

  const confirmArchive = () => {
    if (!archiveReason) return
    if (archiveBatchMode) {
      handleArchiveSelectedBatch(true, archiveReason)
    } else if (archiveTargetId) {
      handleArchiveLead(archiveTargetId, true, archiveReason)
      setShowDetailPanel(false)
      setShowInlineChat(false)
    }
    setShowArchiveModal(false)
  }

  const confirmBlock = () => {
    if (!blockReason) return
    if (blockBatchMode) {
      handleBlockSelectedBatch(true, blockReason)
    } else if (blockTargetId) {
      handleBlockLead(blockTargetId, true, blockReason)
    }
    setShowBlockModal(false)
  }

  const toggleSelection = (leadId: string) => {
    const newSelected = new Set(selectedIds)
    if (newSelected.has(leadId)) {
      newSelected.delete(leadId)
    } else {
      newSelected.add(leadId)
    }
    setSelectedIds(newSelected)
  }

  const selectAll = () => {
    const leads = viewMode === 'list' ? listLeads : allLoadedLeads
    setSelectedIds(new Set(leads.map(l => l.id)))
  }

  const openDetailPanel = (lead: Lead) => {
    setDetailLead(lead)
    setShowDetailPanel(true)
    setObsDisplayCount(5)
    setEditingField(null)
    setEditingNotes(false)
    setNotesValue(lead.notes || '')
  }

  const startEditing = (field: string, currentValue: string) => {
    setEditingField(field)
    setEditValues({ ...editValues, [field]: currentValue })
  }

  const cancelEditing = () => {
    setEditingField(null)
  }

  const saveLeadField = async (field: string) => {
    if (!detailLead?.id) return
    setSavingField(true)
    const token = localStorage.getItem('token')
    try {
      const payload: Record<string, string | number | null> = {}
      const val = editValues[field]?.trim() ?? ''
      if (field === 'age') {
        payload[field] = val ? parseInt(val, 10) : null
      } else {
        payload[field] = val || null
      }
      const res = await fetch(`/api/leads/${detailLead.id}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(payload),
      })
      const data = await res.json()
      if (data.success && data.lead) {
        const merged = { ...data.lead, structured_tags: data.lead.structured_tags || detailLead.structured_tags }
        setDetailLead(merged)
        updateLeadInStages(data.lead.id, () => merged)
      }
    } catch (err) {
      console.error('Failed to save lead field:', err)
    } finally {
      setSavingField(false)
      setEditingField(null)
    }
  }

  const handleFieldKeyDown = (e: React.KeyboardEvent, field: string) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      saveLeadField(field)
    } else if (e.key === 'Escape') {
      cancelEditing()
    }
  }

  const saveNotes = async () => {
    if (!detailLead) return
    setSavingNotes(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/leads/${detailLead.id}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ notes: notesValue }),
      })
      const data = await res.json()
      if (data.success && data.lead) {
        const merged = { ...data.lead, structured_tags: data.lead.structured_tags || detailLead.structured_tags }
        setDetailLead(merged)
        updateLeadInStages(data.lead.id, () => merged)
      }
      setEditingNotes(false)
    } catch (err) {
      console.error('Failed to save notes:', err)
    } finally {
      setSavingNotes(false)
    }
  }

  const handleUpdateLeadStage = async (leadId: string, stageId: string) => {
    const token = localStorage.getItem('token')

    let stage = stages.find(s => s.id === stageId)
    if (!stage) {
       for (const p of pipelines) {
         const found = p.stages?.find(s => s.id === stageId)
         if (found) { stage = found; break }
       }
    }

    const updatedProps = {
      stage_id: stageId,
      stage_name: stage?.name || null,
      stage_color: stage?.color || null,
      stage_position: stage?.position ?? null,
    }

    // Optimistic move between stages
    setStageData(prev => {
      let movedLead: Lead | undefined
      const afterRemove = prev.map(s => {
        const idx = s.leads.findIndex(l => l.id === leadId)
        if (idx >= 0) {
          movedLead = { ...s.leads[idx], ...updatedProps }
          return { ...s, leads: s.leads.filter(l => l.id !== leadId), total_count: Math.max(0, s.total_count - 1) }
        }
        return s
      })
      if (movedLead) {
        return afterRemove.map(s => s.id === stageId
          ? { ...s, leads: [movedLead!, ...s.leads], total_count: s.total_count + 1 }
          : s
        )
      }
      return afterRemove
    })
    // Handle unassigned → stage move
    setUnassignedData(prev => {
      const idx = prev.leads.findIndex(l => l.id === leadId)
      if (idx >= 0) {
        const movedLead = { ...prev.leads[idx], ...updatedProps }
        setStageData(sd => sd.map(s => s.id === stageId
          ? { ...s, leads: [movedLead, ...s.leads], total_count: s.total_count + 1 }
          : s
        ))
        return { ...prev, leads: prev.leads.filter(l => l.id !== leadId), total_count: Math.max(0, prev.total_count - 1) }
      }
      return prev
    })
    setListLeads(prev => prev.map(l => l.id === leadId ? { ...l, ...updatedProps } : l))

    if (detailLead?.id === leadId) {
      setDetailLead(prev => prev ? { ...prev, ...updatedProps } : null)
    }

    try {
      const res = await fetch(`/api/leads/${leadId}/stage`, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ stage_id: stageId }),
      })
      const data = await res.json()
      if (!data.success) {
        fetchLeadsPaginated() // Rollback on failure
      }
    } catch (err) {
      console.error('Failed to update stage:', err)
      fetchLeadsPaginated() // Rollback on error
    }
  }

  const handleUpdateLeadPipeline = async (leadId: string, pipelineId: string) => {
    const token = localStorage.getItem('token')
    // Find first stage of new pipeline
    const newPipeline = pipelines.find(p => p.id === pipelineId)
    // If selecting "Unassigned" (pipelineId is empty string), stage should be null
    const firstStageId = pipelineId ? (newPipeline?.stages?.[0]?.id || null) : null

    try {
      const res = await fetch(`/api/leads/${leadId}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          pipeline_id: pipelineId || null,
          stage_id: firstStageId
        }),
      })
      const data = await res.json()
      if (data.success && data.lead) {
        const merged = { ...data.lead, structured_tags: data.lead.structured_tags || detailLead?.structured_tags }
        setDetailLead(merged)
        fetchLeadsPaginated()
      }
    } catch (err) {
      console.error('Failed to update pipeline:', err)
    }
  }

  const fetchObservations = async (leadId: string) => {
    setLoadingObservations(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/leads/${leadId}/interactions?limit=100`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        setObservations(data.interactions || [])
      }
    } catch (err) {
      console.error('Failed to fetch observations:', err)
    } finally {
      setLoadingObservations(false)
    }
  }

  // Fetch observations for a single lead in list view (with cache) — used by detail panel
  const fetchListLeadObservations = async (leadId: string) => {
    if (listObservations.has(leadId) || loadingListObs.has(leadId)) return
    setLoadingListObs(prev => new Set(prev).add(leadId))
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/leads/${leadId}/interactions?limit=20`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        setListObservations(prev => new Map(prev).set(leadId, data.interactions || []))
      }
    } catch (err) {
      console.error('Failed to fetch list observations:', err)
    } finally {
      setLoadingListObs(prev => { const next = new Set(prev); next.delete(leadId); return next })
    }
  }

  // Batch fetch observations for multiple leads at once
  const fetchBatchObservations = useCallback(async (leadIds: string[]) => {
    const uncached = leadIds.filter(id => !listObservations.has(id) && !loadingListObs.has(id))
    if (uncached.length === 0) return
    setLoadingListObs(prev => {
      const next = new Set(prev)
      uncached.forEach(id => next.add(id))
      return next
    })
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/leads/observations/batch', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ lead_ids: uncached, limit: 5 }),
      })
      const data = await res.json()
      if (data.success && data.observations) {
        setListObservations(prev => {
          const next = new Map(prev)
          // Set results for leads that had observations
          for (const [leadId, obs] of Object.entries(data.observations)) {
            next.set(leadId, obs as Observation[])
          }
          // Set empty arrays for leads with no observations
          uncached.forEach(id => {
            if (!next.has(id)) next.set(id, [])
          })
          return next
        })
      }
    } catch (err) {
      console.error('Failed to batch fetch observations:', err)
    } finally {
      setLoadingListObs(prev => {
        const next = new Set(prev)
        uncached.forEach(id => next.delete(id))
        return next
      })
    }
  }, [listObservations, loadingListObs])

  const handleAddObservation = async () => {
    if (!detailLead || !newObservation.trim()) return
    setSavingObservation(true)
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/interactions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          lead_id: detailLead.id,
          type: newObservationType,
          notes: newObservation.trim(),
        }),
      })
      const data = await res.json()
      if (data.success) {
        setNewObservation('')
        fetchObservations(detailLead.id)
      }
    } catch (err) {
      console.error('Failed to add observation:', err)
    } finally {
      setSavingObservation(false)
    }
  }

  const handleDeleteObservation = async (obsId: string) => {
    if (!detailLead) return
    if (!confirm('¿Eliminar esta observación?')) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/interactions/${obsId}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        fetchObservations(detailLead.id)
      }
    } catch (err) {
      console.error('Failed to delete observation:', err)
    }
  }

  // Drag and drop (using stage_id)
  const handleDragStart = (e: React.DragEvent, leadId: string) => {
    setDraggedLeadId(leadId)
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', leadId)
    if (e.currentTarget instanceof HTMLElement) {
      e.currentTarget.style.opacity = '0.5'
    }
  }

  const handleDragEnd = (e: React.DragEvent) => {
    setDraggedLeadId(null)
    setDragOverColumn(null)
    if (e.currentTarget instanceof HTMLElement) {
      e.currentTarget.style.opacity = '1'
    }
  }

  const handleDragOver = (e: React.DragEvent, stageId: string) => {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    setDragOverColumn(stageId)
  }

  const handleDragLeave = () => {
    setDragOverColumn(null)
  }

  const handleDrop = (e: React.DragEvent, targetStageId: string) => {
    e.preventDefault()
    setDragOverColumn(null)
    const leadId = e.dataTransfer.getData('text/plain')
    if (leadId) {
      const lead = findLeadById(leadId)
      if (lead && lead.stage_id !== targetStageId) {
        handleUpdateLeadStage(leadId, targetStageId)
      }
    }
    setDraggedLeadId(null)
  }

  // Stage management
  const handleAddStage = async () => {
    if (!activePipeline || !newStageName.trim()) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${activePipeline.id}/stages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name: newStageName.trim(), color: newStageColor }),
      })
      const data = await res.json()
      if (data.success) {
        setNewStageName('')
        setNewStageColor('#6366f1')
        fetchPipelines()
      }
    } catch (err) {
      console.error('Failed to add stage:', err)
    }
  }

  const handleDeleteStage = async (stageId: string) => {
    if (!activePipeline) return
    if (!confirm('¿Eliminar esta etapa? Los leads en esta etapa quedarán sin etapa asignada.')) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${activePipeline.id}/stages/${stageId}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (data.success) {
        fetchPipelines()
        fetchLeadsPaginated()
      }
    } catch (err) {
      console.error('Failed to delete stage:', err)
    }
  }

  const handleUpdateStage = async (stageId: string) => {
    if (!activePipeline || !editStageName.trim()) return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch(`/api/pipelines/${activePipeline.id}/stages/${stageId}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name: editStageName.trim(), color: editStageColor }),
      })
      const data = await res.json()
      if (data.success) {
        setEditingStageId(null)
        fetchPipelines()
      }
    } catch (err) {
      console.error('Failed to update stage:', err)
    }
  }

  // Create event from current lead filters
  const handleCreateEventFromLeads = async () => {
    if (!createEventForm.name) return
    setCreatingEvent(true)
    try {
      const body: Record<string, unknown> = {
        name: createEventForm.name,
        description: createEventForm.description || undefined,
        event_date: createEventForm.event_date ? new Date(createEventForm.event_date).toISOString() : undefined,
        event_end: createEventForm.event_end ? new Date(createEventForm.event_end).toISOString() : undefined,
        location: createEventForm.location || undefined,
        color: createEventForm.color,
        // Lead filter criteria (current filters)
        lead_pipeline_id: activePipeline?.id || undefined,
        search: debouncedSearchTerm || undefined,
        tag_names: filterTagNames.size > 0 ? Array.from(filterTagNames) : undefined,
        stage_ids: filterStageIds.size > 0 ? Array.from(filterStageIds) : undefined,
        device_ids: filterDeviceIds.size > 0 ? Array.from(filterDeviceIds) : undefined,
      }
      const res = await fetch('/api/events/from-leads', {
        method: 'POST',
        headers: { Authorization: `Bearer ${localStorage.getItem('token') || ''}`, 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = await res.json()
      if (data.success) {
        setShowCreateEventModal(false)
        setCreateEventForm({ name: '', description: '', event_date: '', event_end: '', location: '', color: '#10b981' })
        // Navigate to the new event
        window.location.href = `/dashboard/events/${data.event.id}`
      } else {
        alert(data.error || 'Error al crear evento')
      }
    } catch (e) { console.error(e); alert('Error de conexión') }
    setCreatingEvent(false)
  }

  // WhatsApp internal chat — smart device selection
  const handleSendWhatsApp = async (phone: string) => {
    setWhatsappPhone(phone)
    try {
      const resolution = await resolveWhatsAppChat(phone)
      if (!resolution.success) {
        alert(resolution.error || 'Error al resolver conversación')
        return
      }
      setExistingChatForWA(resolution.chat || null)
      setWhatsappHistoricalPhone(resolution.historical_phone || '')

      if (resolution.mode === 'read_only' && resolution.chat) {
        setInlineChatId(resolution.chat.id)
        setInlineChat(resolution.chat)
        setInlineChatDeviceId(resolution.chat.device_id || '')
        setInlineChatReadOnly(true)
        setShowInlineChat(true)
        return
      }
      if (resolution.mode === 'open_direct' && resolution.devices[0]) {
        await handleDeviceSelected(resolution.devices[0] as Device, phone)
        return
      }
      if (resolution.mode === 'choose_device') {
        setAllDevicesForModal(resolution.devices as Device[])
        setDevices(resolution.devices as Device[])
        setShowDeviceSelector(true)
        return
      }
      alert('No hay dispositivos conectados para enviar')
    } catch {
      alert('Error de conexión')
    }
  }

  const handleDeviceSelected = async (device: Device, phoneOverride?: string) => {
    setShowDeviceSelector(false)
    setInlineChatReadOnly(false)
    try {
      const data = await createWhatsAppChat(device.id, phoneOverride || whatsappPhone)
      if (data.success && data.chat) {
        // Open inline chat instead of navigating away
        setInlineChatId(data.chat.id)
        setInlineChat(data.chat)
        setInlineChatDeviceId(device.id)
        setShowInlineChat(true)
      } else {
        alert(data.error || 'Error al crear conversación')
      }
    } catch {
      alert('Error de conexión')
    }
  }

  const handlePreviousDeviceSelected = () => {
    setShowDeviceSelector(false)
    if (existingChatForWA) {
      setInlineChatId(existingChatForWA.id)
      setInlineChat(existingChatForWA)
      setInlineChatDeviceId(existingChatForWA.device_id || '')
      setInlineChatReadOnly(true)
      setShowInlineChat(true)
    }
  }

  // Escape key closes modals/panels (topmost first)
  useEffect(() => {
    const handleEscapeKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (showDeviceSelector) { setShowDeviceSelector(false); return }
        if (showStageModal) { setShowStageModal(false); return }
        if (showAddModal) { setShowAddModal(false); return }
        if (showEditModal) { setShowEditModal(false); return }
        if (showFilterDropdown) { setShowFilterDropdown(false); return }
        if (showInlineChat) { setShowInlineChat(false); return }
        if (showDetailPanel) { setShowDetailPanel(false); return }
      }
    }
    window.addEventListener('keydown', handleEscapeKey)
    return () => window.removeEventListener('keydown', handleEscapeKey)
  }, [showDeviceSelector, showStageModal, showAddModal, showEditModal, showFilterDropdown, showInlineChat, showDetailPanel])

  // Tags for filter dropdown (from server response)
  const allUniqueTags = useMemo(() =>
    allTags.map(t => ({ id: t.name, account_id: '', name: t.name, color: t.color })).sort((a, b) => a.name.localeCompare(b.name)),
    [allTags]
  )

  // Count leads per tag (from server)
  const tagLeadCounts = useMemo(() => {
    const counts = new Map<string, number>()
    allTags.forEach(t => counts.set(t.name, t.count))
    return counts
  }, [allTags])

  // Filter tags by search term (% = wildcard like Kommo/SQL LIKE)
  const filteredTags = allUniqueTags.filter(tag => {
    if (!tagSearchTerm.trim()) return true
    const term = tagSearchTerm.trim()
    if (term.includes('%')) {
      const escaped = term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/%/g, '.*')
      try {
        return new RegExp(`^${escaped}$`, 'i').test(tag.name)
      } catch {
        return true
      }
    }
    return tag.name.toLowerCase().includes(term.toLowerCase())
  })

  const activeFilterCount = filterStageIds.size + filterTagNames.size + excludeFilterTagNames.size + (appliedFormulaType === 'advanced' && appliedFormulaText ? 1 : 0) + (filterDatePreset ? 1 : 0) + cfFilters.length

  // Export leads
  const handleExportLeads = async () => {
    setExporting(true)
    const token = localStorage.getItem('token')
    try {
      const params = new URLSearchParams()
      if (exportScope === 'filtered') {
        // Mirror EXACTLY the filters used by fetchListLeads so the export matches the visible list.
        params.set('status_filter', statusFilter)
        if (activePipeline && !debouncedSearchTerm) params.set('pipeline_id', activePipeline.id)
        if (debouncedSearchTerm) params.set('search', debouncedSearchTerm)
        if (appliedFormulaType === 'advanced' && appliedFormulaText) {
          params.set('tag_formula', appliedFormulaText)
        } else {
          if (filterTagNames.size > 0) params.set('tag_names', Array.from(filterTagNames).join(','))
          if (excludeFilterTagNames.size > 0) params.set('exclude_tag_names', Array.from(excludeFilterTagNames).join(','))
          if (filterTagNames.size > 0 || excludeFilterTagNames.size > 0) params.set('tag_mode', tagFilterMode)
        }
        if (filterStageIds.size > 0) params.set('stage_ids', Array.from(filterStageIds).join(','))
        filterDeviceIds.forEach(id => params.append('device_ids', id))
        const resolved = resolveDatePreset(filterDatePreset, filterDateFrom, filterDateTo)
        if (resolved) {
          params.set('date_field', filterDateField)
          if (resolved.from) params.set('date_from', resolved.from)
          if (resolved.to) params.set('date_to', resolved.to)
        }
        if (cfFilters.length > 0) params.set('cf_filter', JSON.stringify(cfFilters))
      } else {
        if (activePipeline) params.set('pipeline_id', activePipeline.id)
      }
      params.set('view', 'list')
      params.set('limit', '50000')
      params.set('offset', '0')

      const res = await fetch(`/api/leads/list-paginated?${params.toString()}`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (!data.success) return

      const allLeads: Lead[] = data.leads || []
      const { utils, writeFile } = await import('xlsx')
      const rows = allLeads.map(l => ({
        'Nombre': l.name || '',
        'Apellido': l.last_name || '',
        'Nombre corto': l.short_name || '',
        'Teléfono': l.phone || '',
        'Email': l.email || '',
        'Empresa': l.company || '',
        'Pipeline': activePipeline?.name || '',
        'Etapa': l.stage_name || '',
        'Etiquetas': (l.structured_tags || []).map((t: any) => t.name).join(', ') || (l.tags || []).join(', '),
        'Archivado': l.is_archived ? 'Sí' : 'No',
        'Bloqueado': l.is_blocked ? 'Sí' : 'No',
        'Creado': format(new Date(l.created_at), 'dd/MM/yyyy HH:mm', { locale: es }),
        'Actualizado': format(new Date(l.updated_at), 'dd/MM/yyyy HH:mm', { locale: es }),
      }))

      if (exportFormat === 'excel') {
        const wb = utils.book_new()
        const ws = utils.json_to_sheet(rows)
        utils.book_append_sheet(wb, ws, 'Leads')
        writeFile(wb, `leads_${format(new Date(), 'yyyy-MM-dd')}.xlsx`)
      } else {
        const ws = utils.json_to_sheet(rows)
        const csv = utils.sheet_to_csv(ws)
        const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `leads_${format(new Date(), 'yyyy-MM-dd')}.csv`
        a.click()
        URL.revokeObjectURL(url)
      }
      setShowExportModal(false)
    } catch (err) {
      console.error('Export failed:', err)
      alert('Error al exportar leads')
    } finally {
      setExporting(false)
    }
  }


  const handleCreateBroadcastFromLeads = async (formResult: CampaignFormResult) => {
    setSubmittingBroadcast(true)
    const token = localStorage.getItem('token')
    try {
      // 1. Create the campaign
      const res = await fetch('/api/campaigns', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({
          name: formResult.name,
          device_id: formResult.device_id,
          message_template: formResult.message_template,
          attachments: formResult.attachments,
          scheduled_at: formResult.scheduled_at || undefined,
          settings: formResult.settings,
        }),
      })
      const data = await res.json()
      if (!data.success) {
        alert(data.error || 'Error al crear campaña')
        return
      }

      const campaignId = data.campaign?.id
      if (!campaignId) {
        alert('Error: no se recibió el ID de la campaña')
        return
      }

      // 2. Schedule if needed
      if (formResult.scheduled_at) {
        await fetch(`/api/campaigns/${campaignId}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({ status: 'scheduled', scheduled_at: formResult.scheduled_at }),
        })
      }

      // 3. Add ALL matching leads as recipients server-side (not limited by client pagination)
      const filterParams = new URLSearchParams()
      if (activePipeline && !debouncedSearchTerm) filterParams.set('pipeline_id', activePipeline.id)
      if (debouncedSearchTerm) filterParams.set('search', debouncedSearchTerm)
      if (appliedFormulaType === 'advanced' && appliedFormulaText) {
        filterParams.set('tag_formula', appliedFormulaText)
      } else {
        if (filterTagNames.size > 0) filterParams.set('tag_names', Array.from(filterTagNames).join(','))
        if (filterTagNames.size > 0 || excludeFilterTagNames.size > 0) filterParams.set('tag_mode', tagFilterMode)
        if (excludeFilterTagNames.size > 0) filterParams.set('exclude_tag_names', Array.from(excludeFilterTagNames).join(','))
      }
      if (filterStageIds.size > 0) filterParams.set('stage_ids', Array.from(filterStageIds).join(','))
      filterDeviceIds.forEach(id => filterParams.append('device_ids', id))
      const dateRange = resolveDatePreset(filterDatePreset, filterDateFrom, filterDateTo)
      if (dateRange) {
        filterParams.set('date_field', filterDateField)
        if (dateRange.from) filterParams.set('date_from', dateRange.from)
        if (dateRange.to) filterParams.set('date_to', dateRange.to)
      }

      const recipRes = await fetch(`/api/campaigns/${campaignId}/recipients/from-leads?${filterParams}`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      const recipData = await recipRes.json()
      if (!recipData.success) {
        alert(recipData.error || 'Error al agregar destinatarios')
        return
      }

      // Add spreadsheet recipients if any
      if (formResult.recipients && formResult.recipients.length > 0) {
        const sheetRecipients = formResult.recipients.map(r => ({
          jid: r.phone + '@s.whatsapp.net',
          name: r.name || '',
          phone: r.phone,
          metadata: r.metadata || {},
        }))
        await fetch(`/api/campaigns/${campaignId}/recipients`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({ recipients: sheetRecipients }),
        })
      }

      setShowBroadcastModal(false)
      router.push('/dashboard/broadcasts')
    } catch (err) {
      alert('Error al crear campaña desde leads')
    } finally {
      setSubmittingBroadcast(false)
    }
  }

  // Visible stages (from server data, filtered by hiddenStageIds)
  const visibleStages = useMemo(() =>
    stageData.filter(s => !hiddenStageIds.has(s.id)),
    [stageData, hiddenStageIds]
  )

  // List virtualizer
  const listVirtualizer = useVirtualizer({
    count: listLeads.length,
    getScrollElement: () => listScrollRef.current,
    estimateSize: () => 80,
    overscan: 10,
  })

  // Batch-fetch observations for visible list rows
  useEffect(() => {
    if (viewMode !== 'list' || listLeads.length === 0) return
    const items = listVirtualizer.getVirtualItems()
    if (items.length === 0) return
    const visibleIds = items.map(item => listLeads[item.index]?.id).filter(Boolean)
    if (visibleIds.length > 0) {
      fetchBatchObservations(visibleIds)
    }
  }, [viewMode, listVirtualizer.getVirtualItems(), listLeads, fetchBatchObservations])

  // Infinite scroll for list view
  useEffect(() => {
    if (viewMode !== 'list' || !listHasMore || listLoading) return
    const el = listScrollRef.current
    if (!el) return
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      if (scrollHeight - scrollTop - clientHeight < 300) {
        fetchListLeads(false)
      }
    }
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [viewMode, listHasMore, listLoading, fetchListLeads])

  if (loading) {
    return (
      <div className="flex flex-col h-full min-h-0 animate-pulse">
        {/* Skeleton header */}
        <div className="flex items-center justify-between mb-3">
          <div>
            <div className="h-6 w-20 bg-slate-200 rounded" />
            <div className="h-4 w-32 bg-slate-100 rounded mt-1" />
          </div>
          <div className="flex gap-2">
            <div className="h-8 w-20 bg-slate-200 rounded-lg" />
            <div className="h-8 w-16 bg-slate-200 rounded-lg" />
            <div className="h-8 w-20 bg-emerald-200 rounded-lg" />
          </div>
        </div>
        {/* Skeleton search */}
        <div className="h-10 bg-slate-100 rounded-xl mb-3" />
        {/* Skeleton kanban columns */}
        <div className="flex-1 flex gap-3 overflow-hidden">
          {[1, 2, 3, 4].map(i => (
            <div key={i} className="w-[272px] flex-shrink-0">
              <div className="h-10 rounded-t-xl bg-slate-200 mb-2" />
              <div className="space-y-2 p-2">
                {[1, 2, 3, 4, 5].map(j => (
                  <div key={j} className="bg-white p-3 rounded-xl border border-slate-100">
                    <div className="flex items-center gap-2 mb-2">
                      <div className="w-7 h-7 bg-slate-200 rounded-full" />
                      <div className="h-4 w-24 bg-slate-200 rounded" />
                    </div>
                    <div className="h-3 w-32 bg-slate-100 rounded mt-1.5" />
                    <div className="flex gap-1 mt-2">
                      <div className="h-4 w-12 bg-slate-100 rounded-full" />
                      <div className="h-4 w-14 bg-slate-100 rounded-full" />
                    </div>
                    <div className="h-3 w-20 bg-slate-50 rounded mt-2" />
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Row 1: Title + View Toggle + Search + Más */}
      <div className="flex items-center gap-3 py-2 shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <h1 className="text-lg font-bold text-slate-900 whitespace-nowrap">Leads</h1>
          <span className="text-xs text-slate-400 font-medium tabular-nums bg-slate-100 px-2 py-0.5 rounded-full">{(viewMode === 'list' ? listTotal : totalLeadCount).toLocaleString()}</span>
        </div>

        {!selectionMode && (
          <div className="inline-flex items-center border border-slate-200 rounded-lg overflow-hidden">
            <button
              onClick={() => setViewMode('kanban')}
              className={`inline-flex items-center gap-1 px-2 py-1.5 text-xs font-medium transition ${
                viewMode === 'kanban' ? 'bg-emerald-50 text-emerald-700' : 'text-slate-500 hover:bg-slate-50'
              }`}
              title="Vista Kanban"
            >
              <LayoutGrid className="w-3.5 h-3.5" />
            </button>
            <button
              onClick={() => setViewMode('list')}
              className={`inline-flex items-center gap-1 px-2 py-1.5 text-xs font-medium transition ${
                viewMode === 'list' ? 'bg-emerald-50 text-emerald-700' : 'text-slate-500 hover:bg-slate-50'
              }`}
              title="Vista Lista"
            >
              <List className="w-3.5 h-3.5" />
            </button>
          </div>
        )}

        <div ref={filterDropdownRef} className="flex-1 max-w-sm relative">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 z-10" />
          <input
            type="text"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            onFocus={() => setShowFilterDropdown(true)}
            onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); setShowFilterDropdown(false) } }}
            placeholder="Buscar leads..."
            className={`w-full pl-8 pr-3 py-1.5 bg-white border rounded-lg focus:ring-2 focus:ring-emerald-500 focus:border-emerald-500 text-slate-800 placeholder:text-slate-400 text-sm ${activeFilterCount > 0 ? 'border-emerald-400 ring-1 ring-emerald-200' : 'border-slate-200'}`}
          />
          {activeFilterCount > 0 && !showFilterDropdown && (
            <span className="absolute right-2.5 top-1/2 -translate-y-1/2 w-5 h-5 bg-emerald-600 text-white text-[10px] font-bold rounded-full flex items-center justify-center">{activeFilterCount}</span>
          )}

          {/* Filter Dropdown — Two-Column Layout */}
          {showFilterDropdown && (
            <div className="absolute top-full left-0 mt-1 w-[min(600px,90vw)] bg-white border border-slate-200/80 rounded-2xl shadow-2xl shadow-slate-200/50 z-30 flex flex-col max-h-[70vh]">
              {/* ─── Header ─── */}
              <div className="px-4 py-3 border-b border-slate-100 flex items-center justify-between shrink-0">
                <div className="flex items-center gap-2.5">
                  <div className="w-1.5 h-4 bg-emerald-500 rounded-full" />
                  <span className="text-sm font-semibold text-slate-800">Filtros</span>
                  {activeFilterCount > 0 && (
                    <span className="text-[10px] font-medium bg-emerald-50 text-emerald-600 px-2 py-0.5 rounded-full">{activeFilterCount} activo{activeFilterCount > 1 ? 's' : ''}</span>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  {activeFilterCount > 0 && (
                    <button
                      onClick={() => { setFilterStageIds(new Set()); setFilterTagNames(new Set()); setExcludeFilterTagNames(new Set()); setTagFilterMode('OR'); setLeadFormulaType('simple'); setLeadFormulaText(''); setLeadFormulaIsValid(true); setAppliedFormulaType('simple'); setAppliedFormulaText(''); setFilterDateField('created_at'); setFilterDatePreset(''); setFilterDateFrom(''); setFilterDateTo(''); setCfFilters([]) }}
                      className="text-[11px] text-red-400 hover:text-red-600 font-medium transition-colors"
                    >
                      Limpiar todo
                    </button>
                  )}
                  <button onClick={() => setShowFilterDropdown(false)} className="p-1 hover:bg-slate-100 rounded-lg transition-colors">
                    <X className="w-4 h-4 text-slate-400" />
                  </button>
                </div>
              </div>

              {/* ─── Responsive Body: 2 cols when space, 1 col when narrow ─── */}
              <div className="flex flex-col sm:flex-row flex-1 min-h-0 overflow-hidden">

                {/* ══ Left Column — Selections ══ */}
                <div className="w-full sm:w-[240px] shrink-0 border-b sm:border-b-0 sm:border-r border-slate-100 overflow-y-auto p-3 space-y-4 bg-slate-50/30 max-h-[30vh] sm:max-h-none">

                  {/* Stage pills */}
                  {stages.length > 0 && (
                    <div>
                      <div className="flex items-center gap-2 mb-2.5">
                        <div className="w-1 h-3.5 bg-slate-300 rounded-full" />
                        <p className="text-[10px] font-bold text-slate-500 uppercase tracking-widest">Etapas</p>
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {stages.map(stage => {
                          const isActive = filterStageIds.has(stage.id)
                          return (
                            <button
                              key={stage.id}
                              onClick={() => {
                                const next = new Set(filterStageIds)
                                if (isActive) next.delete(stage.id); else next.add(stage.id)
                                setFilterStageIds(next)
                              }}
                              className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium transition-all border ${
                                isActive ? 'border-transparent text-white shadow-sm' : 'border-slate-200 text-slate-600 hover:bg-white hover:shadow-sm'
                              }`}
                              style={isActive ? { backgroundColor: stage.color } : {}}
                            >
                              <div className="w-2 h-2 rounded-full" style={{ backgroundColor: stage.color }} />
                              {stage.name}
                            </button>
                          )
                        })}
                      </div>
                    </div>
                  )}

                  {/* ── Date Filter ── */}
                  <div>
                    <div className="flex items-center gap-2 mb-2.5">
                      <div className="w-1 h-3.5 bg-blue-400 rounded-full" />
                      <p className="text-[10px] font-bold text-slate-500 uppercase tracking-widest">Fecha</p>
                    </div>
                    {/* Field toggle */}
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
                    {/* Preset buttons */}
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
                    {/* Custom date range inputs */}
                    {filterDatePreset === 'custom' && (
                      <div className="mt-2 space-y-1.5">
                        <div>
                          <label className="text-[9px] font-semibold text-slate-400 uppercase">Desde</label>
                          <input
                            type="date"
                            value={filterDateFrom}
                            onChange={e => setFilterDateFrom(e.target.value)}
                            className="w-full px-2 py-1.5 text-xs border border-slate-200 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-400 focus:border-blue-400"
                          />
                        </div>
                        <div>
                          <label className="text-[9px] font-semibold text-slate-400 uppercase">Hasta</label>
                          <input
                            type="date"
                            value={filterDateTo}
                            onChange={e => setFilterDateTo(e.target.value)}
                            className="w-full px-2 py-1.5 text-xs border border-slate-200 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-400 focus:border-blue-400"
                          />
                        </div>
                      </div>
                    )}
                    {/* Active date chip */}
                    {filterDatePreset && filterDatePreset !== 'custom' && (
                      <div className="mt-2 flex items-center gap-1">
                        <Clock className="w-3 h-3 text-blue-500" />
                        <span className="text-[10px] font-medium text-blue-600">{DATE_PRESETS.find(p => p.key === filterDatePreset)?.label}</span>
                        <button onClick={() => { setFilterDatePreset(''); setFilterDateFrom(''); setFilterDateTo('') }} className="ml-auto p-0.5 hover:bg-slate-100 rounded">
                          <X className="w-2.5 h-2.5 text-slate-400" />
                        </button>
                      </div>
                    )}
                  </div>

                  {/* Active tag selections */}
                  {allUniqueTags.length > 0 && (
                    <div>
                      <div className="flex items-center gap-2 mb-2.5">
                        <div className="w-1 h-3.5 bg-emerald-400 rounded-full" />
                        <p className="text-[10px] font-bold text-slate-500 uppercase tracking-widest">Selección</p>
                      </div>

                      {filterTagNames.size === 0 && excludeFilterTagNames.size === 0 ? (
                        <div className="border-2 border-dashed border-slate-200 rounded-xl p-4 text-center">
                          <Tag className="w-5 h-5 text-slate-300 mx-auto mb-1.5" />
                          <p className="text-[11px] text-slate-400">Haz click en las etiquetas para filtrar</p>
                        </div>
                      ) : (
                        <div className="space-y-3">
                          {/* Include chips */}
                          {filterTagNames.size > 0 && (
                            <div>
                              <div className="flex items-center gap-1.5 mb-1.5">
                                <CheckCircle2 className="w-3 h-3 text-emerald-500" />
                                <span className="text-[10px] font-semibold text-emerald-600 uppercase tracking-wide">Incluir</span>
                              </div>
                              <div className="flex flex-wrap gap-1">
                                {Array.from(filterTagNames).map(name => {
                                  const tag = allUniqueTags.find(t => t.name === name)
                                  return (
                                    <span
                                      key={name}
                                      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium text-white shadow-sm"
                                      style={{ backgroundColor: tag?.color || '#6b7280' }}
                                    >
                                      {name}
                                      <button onClick={() => { const next = new Set(filterTagNames); next.delete(name); setFilterTagNames(next) }} className="hover:opacity-75">
                                        <X className="w-2.5 h-2.5" />
                                      </button>
                                    </span>
                                  )
                                })}
                              </div>
                            </div>
                          )}
                          {/* Exclude chips */}
                          {excludeFilterTagNames.size > 0 && (
                            <div>
                              <div className="flex items-center gap-1.5 mb-1.5">
                                <XCircle className="w-3 h-3 text-red-400" />
                                <span className="text-[10px] font-semibold text-red-500 uppercase tracking-wide">Excluir</span>
                              </div>
                              <div className="flex flex-wrap gap-1">
                                {Array.from(excludeFilterTagNames).map(name => {
                                  const tag = allUniqueTags.find(t => t.name === name)
                                  return (
                                    <span
                                      key={name}
                                      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium text-white/90 line-through shadow-sm"
                                      style={{ backgroundColor: tag?.color || '#6b7280' }}
                                    >
                                      {name}
                                      <button onClick={() => { const next = new Set(excludeFilterTagNames); next.delete(name); setExcludeFilterTagNames(next) }} className="hover:opacity-75 no-underline">
                                        <X className="w-2.5 h-2.5" />
                                      </button>
                                    </span>
                                  )
                                })}
                              </div>
                            </div>
                          )}
                        </div>
                      )}

                      {/* Click instructions */}
                      <div className="mt-3 pt-3 border-t border-slate-100">
                        <div className="space-y-1">
                          <div className="flex items-center gap-2 text-[10px] text-slate-400">
                            <div className="w-3 h-3 rounded-full bg-emerald-500 flex items-center justify-center shrink-0"><CheckSquare className="w-2 h-2 text-white" /></div>
                            <span>Click = incluir</span>
                          </div>
                          <div className="flex items-center gap-2 text-[10px] text-slate-400">
                            <div className="w-3 h-3 rounded-full bg-red-500 flex items-center justify-center shrink-0"><X className="w-2 h-2 text-white" /></div>
                            <span>2do click = excluir</span>
                          </div>
                          <div className="flex items-center gap-2 text-[10px] text-slate-400">
                            <div className="w-3 h-3 rounded-full bg-slate-200 shrink-0" />
                            <span>3ro = quitar</span>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}
                </div>

                {/* ══ Center Column — Custom Fields ══ */}
                {cfDefs.length > 0 && (
                <div className="w-full sm:w-[220px] shrink-0 border-b sm:border-b-0 sm:border-r border-slate-100 overflow-y-auto p-3 space-y-3 max-h-[30vh] sm:max-h-none">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="w-1 h-3.5 bg-violet-400 rounded-full" />
                      <p className="text-[10px] font-bold text-slate-500 uppercase tracking-widest">Campos</p>
                    </div>
                    <button
                      onClick={() => setCfFilters(prev => [...prev, { field_id: cfDefs[0].id, operator: 'eq' as const, value: '' }])}
                      className="p-1 hover:bg-violet-50 rounded-lg transition-colors"
                    >
                      <Plus className="w-3.5 h-3.5 text-violet-500" />
                    </button>
                  </div>
                  {cfFilters.length === 0 && (
                    <p className="text-[10px] text-slate-400 text-center py-2">Sin filtros de campos</p>
                  )}
                  {cfFilters.map((cf, idx) => {
                    const def = cfDefs.find(d => d.id === cf.field_id)
                    const fieldType = def?.field_type || 'text'
                    const ops: { value: string; label: string }[] = (() => {
                      switch (fieldType) {
                        case 'number': case 'currency':
                          return [{ value: 'eq', label: '=' }, { value: 'neq', label: '≠' }, { value: 'gt', label: '>' }, { value: 'lt', label: '<' }, { value: 'gte', label: '≥' }, { value: 'lte', label: '≤' }, { value: 'is_empty', label: 'Vacío' }, { value: 'is_not_empty', label: 'No vacío' }]
                        case 'date':
                          return [{ value: 'eq', label: '=' }, { value: 'gt', label: 'Después' }, { value: 'lt', label: 'Antes' }, { value: 'is_empty', label: 'Vacío' }, { value: 'is_not_empty', label: 'No vacío' }]
                        case 'checkbox':
                          return [{ value: 'eq', label: '=' }]
                        case 'select':
                          return [{ value: 'eq', label: '=' }, { value: 'neq', label: '≠' }, { value: 'is_empty', label: 'Vacío' }, { value: 'is_not_empty', label: 'No vacío' }]
                        case 'multi_select':
                          return [{ value: 'contains_any', label: 'Contiene' }, { value: 'contains_all', label: 'Contiene todos' }, { value: 'is_empty', label: 'Vacío' }, { value: 'is_not_empty', label: 'No vacío' }]
                        default:
                          return [{ value: 'eq', label: '=' }, { value: 'neq', label: '≠' }, { value: 'contains', label: 'Contiene' }, { value: 'starts_with', label: 'Empieza' }, { value: 'is_empty', label: 'Vacío' }, { value: 'is_not_empty', label: 'No vacío' }]
                      }
                    })()
                    const needsValue = cf.operator !== 'is_empty' && cf.operator !== 'is_not_empty'
                    return (
                      <div key={idx} className="space-y-1 p-2 bg-slate-50 rounded-lg border border-slate-100">
                        <div className="flex items-center gap-1">
                          <select
                            value={cf.field_id}
                            onChange={(e) => {
                              const next = [...cfFilters]
                              next[idx] = { ...next[idx], field_id: e.target.value, value: '' }
                              setCfFilters(next)
                            }}
                            className="flex-1 min-w-0 px-1.5 py-1 bg-white border border-slate-200 rounded text-[10px] text-slate-700 focus:ring-1 focus:ring-violet-400"
                          >
                            {cfDefs.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                          </select>
                          <button onClick={() => setCfFilters(prev => prev.filter((_, i) => i !== idx))} className="p-0.5 hover:bg-red-50 rounded">
                            <X className="w-3 h-3 text-red-400" />
                          </button>
                        </div>
                        <select
                          value={cf.operator}
                          onChange={(e) => {
                            const next = [...cfFilters]
                            next[idx] = { ...next[idx], operator: e.target.value as CustomFieldFilter['operator'], value: '' }
                            setCfFilters(next)
                          }}
                          className="w-full px-1.5 py-1 bg-white border border-slate-200 rounded text-[10px] text-slate-700 focus:ring-1 focus:ring-violet-400"
                        >
                          {ops.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                        </select>
                        {needsValue && fieldType === 'checkbox' && (
                          <select
                            value={String(cf.value)}
                            onChange={(e) => {
                              const next = [...cfFilters]
                              next[idx] = { ...next[idx], value: e.target.value === 'true' }
                              setCfFilters(next)
                            }}
                            className="w-full px-1.5 py-1 bg-white border border-slate-200 rounded text-[10px] text-slate-700 focus:ring-1 focus:ring-violet-400"
                          >
                            <option value="true">Sí</option>
                            <option value="false">No</option>
                          </select>
                        )}
                        {needsValue && (fieldType === 'select' || fieldType === 'multi_select') && def?.config?.options && (
                          <select
                            value={String(cf.value)}
                            onChange={(e) => {
                              const next = [...cfFilters]
                              next[idx] = { ...next[idx], value: e.target.value }
                              setCfFilters(next)
                            }}
                            className="w-full px-1.5 py-1 bg-white border border-slate-200 rounded text-[10px] text-slate-700 focus:ring-1 focus:ring-violet-400"
                          >
                            <option value="">Seleccionar...</option>
                            {def.config.options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                          </select>
                        )}
                        {needsValue && fieldType === 'date' && (
                          <input
                            type="date"
                            value={String(cf.value || '')}
                            onChange={(e) => {
                              const next = [...cfFilters]
                              next[idx] = { ...next[idx], value: e.target.value }
                              setCfFilters(next)
                            }}
                            className="w-full px-1.5 py-1 bg-white border border-slate-200 rounded text-[10px] text-slate-700 focus:ring-1 focus:ring-violet-400"
                          />
                        )}
                        {needsValue && !['checkbox', 'select', 'multi_select', 'date'].includes(fieldType) && (
                          <input
                            type={fieldType === 'number' || fieldType === 'currency' ? 'number' : 'text'}
                            value={String(cf.value || '')}
                            onChange={(e) => {
                              const next = [...cfFilters]
                              next[idx] = { ...next[idx], value: fieldType === 'number' || fieldType === 'currency' ? (e.target.value ? Number(e.target.value) : '') : e.target.value }
                              setCfFilters(next)
                            }}
                            placeholder="Valor..."
                            className="w-full px-1.5 py-1 bg-white border border-slate-200 rounded text-[10px] text-slate-700 placeholder:text-slate-400 focus:ring-1 focus:ring-violet-400"
                          />
                        )}
                      </div>
                    )
                  })}
                </div>
                )}

                {/* ══ Right Column — Tag Browser ══ */}
                <div className="flex-1 flex flex-col min-w-0 min-h-0 w-full sm:w-auto">

                  {allUniqueTags.length > 0 && (
                    <>
                      {/* Top controls — shrink-0 */}
                      <div className="p-3 pb-0 shrink-0 space-y-2.5">
                        {/* Simple / Advanced tabs */}
                        <div className="flex rounded-xl border border-slate-200 bg-slate-50/50 overflow-hidden">
                          <button type="button"
                            onClick={() => setLeadFormulaType('simple')}
                            className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-[11px] font-semibold transition-all ${
                              leadFormulaType === 'simple'
                                ? 'bg-emerald-500 text-white shadow-sm'
                                : 'text-slate-500 hover:bg-white hover:text-slate-700'
                            }`}>
                            <FileText className="w-3.5 h-3.5" />
                            Simple
                          </button>
                          <button type="button"
                            onClick={() => setLeadFormulaType('advanced')}
                            className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-[11px] font-semibold transition-all ${
                              leadFormulaType === 'advanced'
                                ? 'bg-violet-500 text-white shadow-sm'
                                : 'text-slate-500 hover:bg-white hover:text-slate-700'
                            }`}>
                            <Code className="w-3.5 h-3.5" />
                            Avanzado
                          </button>
                        </div>

                        {/* ─── SIMPLE MODE controls ─── */}
                        {leadFormulaType === 'simple' && (
                          <>
                            <div className="flex items-center gap-3">
                              <div className="inline-flex rounded-lg border border-slate-200 overflow-hidden">
                                <button
                                  onClick={() => setTagFilterMode('OR')}
                                  className={`px-3 py-1 text-[10px] font-bold tracking-wide transition-all ${
                                    tagFilterMode === 'OR' ? 'bg-emerald-500 text-white' : 'bg-white text-slate-400 hover:bg-slate-50'
                                  }`}>
                                  OR
                                </button>
                                <button
                                  onClick={() => setTagFilterMode('AND')}
                                  className={`px-3 py-1 text-[10px] font-bold tracking-wide transition-all ${
                                    tagFilterMode === 'AND' ? 'bg-blue-500 text-white' : 'bg-white text-slate-400 hover:bg-slate-50'
                                  }`}>
                                  AND
                                </button>
                              </div>
                              <p className="text-[10px] text-slate-400 leading-tight">
                                {tagFilterMode === 'AND' ? 'Debe tener TODAS las incluidas' : 'Debe tener al menos UNA incluida'}
                                {excludeFilterTagNames.size > 0 ? ' y NINGUNA excluida' : ''}
                              </p>
                            </div>
                            <div className="relative">
                              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400" />
                              <input
                                type="text"
                                value={tagSearchTerm}
                                onChange={(e) => setTagSearchTerm(e.target.value)}
                                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); setShowFilterDropdown(false) } }}
                                placeholder="Buscar etiquetas... (% = comodín)"
                                className="w-full pl-9 pr-3 py-2 bg-white border border-slate-200 rounded-xl text-xs text-slate-800 placeholder:text-slate-400 focus:ring-2 focus:ring-emerald-500/30 focus:border-emerald-400 transition-all"
                              />
                            </div>
                          </>
                        )}
                      </div>

                      {/* ─── SIMPLE MODE — Tag list (scrollable, fills space) ─── */}
                      {leadFormulaType === 'simple' && (
                        <div className="flex-1 min-h-0 overflow-y-auto p-3 pt-2">
                          <div className="space-y-0.5">
                            {filteredTags.map(tag => {
                              const isIncluded = filterTagNames.has(tag.name)
                              const isExcluded = excludeFilterTagNames.has(tag.name)
                              const count = tagLeadCounts.get(tag.name) || 0
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
                                  className={`flex items-center gap-2.5 px-2.5 py-2 rounded-xl cursor-pointer select-none transition-all ${
                                    isIncluded
                                      ? 'bg-emerald-50 ring-1 ring-emerald-200'
                                      : isExcluded
                                        ? 'bg-red-50 ring-1 ring-red-200'
                                        : 'hover:bg-white hover:shadow-sm'
                                  }`}
                                >
                                  {isIncluded ? (
                                    <div className="w-5 h-5 rounded-full shrink-0 bg-emerald-500 flex items-center justify-center shadow-sm shadow-emerald-200">
                                      <CheckSquare className="w-3 h-3 text-white" />
                                    </div>
                                  ) : isExcluded ? (
                                    <div className="w-5 h-5 rounded-full shrink-0 bg-red-500 flex items-center justify-center shadow-sm shadow-red-200">
                                      <X className="w-3 h-3 text-white" />
                                    </div>
                                  ) : (
                                    <div className="w-3.5 h-3.5 rounded-full shrink-0 ring-2 ring-white shadow-sm" style={{ backgroundColor: tag.color }} />
                                  )}
                                  <span className={`flex-1 text-[12px] transition-colors ${
                                    isIncluded
                                      ? 'text-emerald-700 font-semibold'
                                      : isExcluded
                                        ? 'text-red-400 line-through'
                                        : 'text-slate-700'
                                  }`}>{tag.name}</span>
                                  <span className={`text-[10px] tabular-nums font-medium px-1.5 py-0.5 rounded-md ${
                                    isIncluded ? 'bg-emerald-100 text-emerald-600' : isExcluded ? 'bg-red-100 text-red-500' : 'bg-slate-100 text-slate-400'
                                  }`}>{count}</span>
                                </div>
                              )
                            })}
                            {filteredTags.length === 0 && tagSearchTerm.trim() && (
                              <div className="text-center py-6">
                                <Search className="w-5 h-5 text-slate-300 mx-auto mb-1.5" />
                                <p className="text-xs text-slate-400">Sin resultados para &quot;{tagSearchTerm}&quot;</p>
                              </div>
                            )}
                          </div>
                        </div>
                      )}

                      {/* ─── ADVANCED MODE ─── */}
                      {leadFormulaType === 'advanced' && (
                        <div className="flex-1 min-h-0 overflow-y-auto p-3 space-y-3">
                          <div className="p-2.5 bg-slate-50 rounded-xl border border-slate-100">
                            <div className="text-[9px] font-bold text-slate-400 uppercase tracking-widest mb-1.5">Sintaxis</div>
                            <div className="grid grid-cols-2 gap-1 text-[10px] text-slate-600">
                              <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">&quot;etiqueta&quot;</code> exacta</div>
                              <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">&quot;mar%&quot;</code> comodín</div>
                              <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">and</code> <code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">or</code> <code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">not</code></div>
                              <div><code className="text-violet-600 bg-violet-50 px-1 py-0.5 rounded">in ( )</code> agrupar lista</div>
                            </div>
                          </div>
                          <FormulaEditor
                            value={leadFormulaText}
                            onChange={setLeadFormulaText}
                            tags={allUniqueTags}
                            compact
                            rows={5}
                            onValidChange={setLeadFormulaIsValid}
                          />
                        </div>
                      )}
                    </>
                  )}

                  {allUniqueTags.length === 0 && (
                    <div className="flex-1 flex items-center justify-center p-6">
                      <div className="text-center">
                        <Tag className="w-6 h-6 text-slate-300 mx-auto mb-2" />
                        <p className="text-xs text-slate-400">No hay etiquetas disponibles</p>
                      </div>
                    </div>
                  )}
                </div>
              </div>

              {/* ─── Footer — Aplicar ─── */}
              <div className="px-4 py-3 border-t border-slate-100 shrink-0 bg-white rounded-b-2xl">
                <button
                  onClick={() => {
                    setAppliedFormulaType(leadFormulaType)
                    setAppliedFormulaText(leadFormulaType === 'advanced' ? leadFormulaText : '')
                    setShowFilterDropdown(false)
                  }}
                  disabled={leadFormulaType === 'advanced' && !leadFormulaIsValid}
                  className="w-full px-4 py-2.5 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 active:bg-emerald-800 disabled:opacity-50 disabled:cursor-not-allowed transition-all text-sm font-semibold shadow-sm shadow-emerald-200 hover:shadow-md hover:shadow-emerald-200"
                >
                  Aplicar
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Actions area */}
        <div className="flex items-center gap-2 shrink-0">
          {selectionMode ? (
            <>
              <span className="flex items-center px-2 py-1.5 text-xs text-slate-500 font-medium whitespace-nowrap">
                {selectedIds.size} sel.
              </span>
              <button onClick={selectAll} className="px-2.5 py-1.5 text-xs border border-slate-200 rounded-lg hover:bg-slate-50 text-slate-600 font-medium">
                Todos
              </button>
              {statusFilter === 'active' && (
                <button
                  onClick={() => openArchiveModal(null, true)}
                  disabled={selectedIds.size === 0 || deleting}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs bg-amber-500 text-white rounded-lg hover:bg-amber-600 disabled:opacity-50 font-medium"
                >
                  <Archive className="w-3 h-3" />
                  Archivar
                </button>
              )}
              {statusFilter === 'archived' && (
                <button
                  onClick={() => handleArchiveSelectedBatch(false)}
                  disabled={selectedIds.size === 0 || deleting}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 disabled:opacity-50 font-medium"
                >
                  <ArchiveRestore className="w-3 h-3" />
                  Restaurar
                </button>
              )}
              {statusFilter !== 'blocked' && (
                <button
                  onClick={() => openBlockModal(null, true)}
                  disabled={selectedIds.size === 0 || deleting}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs bg-red-600 text-white rounded-lg hover:bg-red-700 disabled:opacity-50 font-medium"
                >
                  <ShieldBan className="w-3 h-3" />
                  Bloquear
                </button>
              )}
              {statusFilter === 'blocked' && (
                <button
                  onClick={() => handleBlockSelectedBatch(false)}
                  disabled={selectedIds.size === 0 || deleting}
                  className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 disabled:opacity-50 font-medium"
                >
                  <ShieldOff className="w-3 h-3" />
                  Desbloquear
                </button>
              )}
              {googleConnected && (
                <>
                  <button
                    onClick={handleGoogleBatchSyncFromLeads}
                    disabled={selectedIds.size === 0 || selectedIds.size > 30 || googleSyncing}
                    className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 font-medium"
                  >
                    {googleSyncing ? <RefreshCw className="w-3 h-3 animate-spin" /> : <Upload className="w-3 h-3" />}
                    Sync
                  </button>
                  <button
                    onClick={handleGoogleBatchDesyncFromLeads}
                    disabled={selectedIds.size === 0 || selectedIds.size > 30 || googleSyncing}
                    className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs border border-slate-200 text-slate-600 rounded-lg hover:bg-slate-50 disabled:opacity-50 font-medium"
                  >
                    <XCircle className="w-3 h-3" />
                    Desync
                  </button>
                </>
              )}
              <button
                onClick={handleDeleteSelected}
                disabled={selectedIds.size === 0 || deleting}
                className="px-2.5 py-1.5 text-xs bg-red-800 text-white rounded-lg hover:bg-red-900 disabled:opacity-50 font-medium"
              >
                {deleting ? '...' : `Eliminar (${selectedIds.size})`}
              </button>
              <button
                onClick={() => { setSelectionMode(false); setSelectedIds(new Set()) }}
                className="w-8 h-8 flex items-center justify-center border border-slate-300 rounded-lg hover:bg-slate-50 text-slate-500 hover:text-slate-700 transition-colors"
                title="Cancelar selección"
              >
                <X className="w-4 h-4" />
              </button>
            </>
          ) : (
            <div ref={moreMenuRef} className="relative">
              <button
                onClick={() => setShowMoreMenu(v => !v)}
                className={`inline-flex items-center gap-1.5 px-3 py-1.5 border rounded-lg text-sm transition-colors ${
                  showMoreMenu ? 'border-slate-400 bg-slate-100 text-slate-700' : 'border-slate-300 hover:bg-slate-50 text-slate-600'
                }`}
                title="Más acciones"
              >
                <MoreHorizontal className="w-4 h-4" />
                <span className="hidden sm:inline">Más</span>
                <ChevronDown className={`w-3.5 h-3.5 transition-transform ${showMoreMenu ? 'rotate-180' : ''}`} />
              </button>
              {showMoreMenu && (
                <div className="absolute right-0 top-full mt-1.5 w-56 bg-white border border-slate-200 rounded-xl shadow-xl z-30 py-1 overflow-hidden">
                  <button
                    onClick={() => { setShowAddModal(true); setShowMoreMenu(false) }}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-emerald-700 font-medium hover:bg-emerald-50 transition-colors"
                  >
                    <Plus className="w-4 h-4 text-emerald-500" />
                    Nuevo lead
                  </button>
                  <button
                    onClick={() => { fetchDevices(); setShowBroadcastModal(true); setShowMoreMenu(false) }}
                    disabled={totalLeadCount === 0}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    <Radio className="w-4 h-4 text-slate-400" />
                    Masivo
                  </button>
                  <div className="my-1 border-t border-slate-100" />
                  <button
                    onClick={() => { setSelectionMode(true); setShowMoreMenu(false) }}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors"
                  >
                    <CheckSquare className="w-4 h-4 text-slate-400" />
                    Seleccionar
                  </button>
                  <button
                    onClick={() => { setShowStageModal(true); setShowMoreMenu(false) }}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors"
                  >
                    <Settings className="w-4 h-4 text-slate-400" />
                    Etapas
                  </button>
                  <div className="my-1 border-t border-slate-100" />
                  <button
                    onClick={() => { setShowImportModal(true); setShowMoreMenu(false) }}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors"
                  >
                    <Upload className="w-4 h-4 text-slate-400" />
                    Importar Excel
                  </button>
                  <button
                    onClick={() => { setShowContactImportModal(true); setShowMoreMenu(false) }}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors"
                  >
                    <UserPlus className="w-4 h-4 text-slate-400" />
                    Crear desde contactos
                  </button>
                  <button
                    onClick={() => { setShowExportModal(true); setShowMoreMenu(false) }}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors"
                  >
                    <Download className="w-4 h-4 text-slate-400" />
                    Exportar leads
                  </button>
                  <button
                    onClick={() => { setShowBulkDocModal(true); setShowMoreMenu(false) }}
                    disabled={(viewMode === 'list' ? listLeads : allLoadedLeads).length === 0}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-slate-700 hover:bg-slate-50 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    <FileText className="w-4 h-4 text-slate-400" />
                    Generar Documentos
                  </button>
                  <div className="my-1 border-t border-slate-100" />
                  <button
                    onClick={() => { setShowCreateEventModal(true); setShowMoreMenu(false) }}
                    className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-emerald-700 hover:bg-emerald-50 transition-colors"
                  >
                    <Calendar className="w-4 h-4 text-emerald-500" />
                    Crear Evento desde Leads
                  </button>
                  {devices.length > 0 && (
                    <>
                      <div className="my-1 border-t border-slate-100" />
                      <div className="px-4 py-2">
                        <p className="text-[10px] font-semibold text-slate-400 uppercase tracking-wider mb-1.5">Filtrar por dispositivo</p>
                        {devices.map(d => (
                          <label key={d.id} className="flex items-center gap-2 py-1 cursor-pointer text-sm">
                            <input
                              type="checkbox"
                              checked={filterDeviceIds.has(d.id)}
                              onChange={() => {
                                setFilterDeviceIds(prev => {
                                  const next = new Set(prev)
                                  if (next.has(d.id)) next.delete(d.id)
                                  else next.add(d.id)
                                  return next
                                })
                              }}
                              className="w-3.5 h-3.5 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                            />
                            <span className="text-slate-700 text-xs">{d.name || d.phone || 'Dispositivo'}</span>
                          </label>
                        ))}
                        {filterDeviceIds.size > 0 && (
                          <button
                            onClick={() => setFilterDeviceIds(new Set())}
                            className="w-full mt-1 text-xs text-slate-500 hover:text-slate-700 py-0.5"
                          >
                            Limpiar filtro
                          </button>
                        )}
                      </div>
                    </>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Row 2: Status tabs + Pipeline selector */}
      <div className="flex items-center gap-1 mb-2 shrink-0">
        <button
          onClick={() => setStatusFilter('active')}
          className={`inline-flex items-center gap-1 px-2 py-1 text-[11px] font-medium rounded-lg transition ${
            statusFilter === 'active'
              ? 'bg-emerald-50 text-emerald-700 ring-1 ring-emerald-200'
              : 'text-slate-500 hover:bg-slate-50'
          }`}
        >
          Activos
          <span className={`px-1.5 py-0.5 rounded-full text-[10px] font-semibold ${
            statusFilter === 'active' ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-500'
          }`}>{leadCounts.active}</span>
        </button>
        <button
          onClick={() => setStatusFilter('archived')}
          className={`inline-flex items-center gap-1 px-2 py-1 text-[11px] font-medium rounded-lg transition ${
            statusFilter === 'archived'
              ? 'bg-amber-50 text-amber-700 ring-1 ring-amber-200'
              : 'text-slate-500 hover:bg-slate-50'
          }`}
        >
          <Archive className="w-3 h-3" />
          Archivados
          {leadCounts.archived > 0 && (
            <span className={`px-1.5 py-0.5 rounded-full text-[10px] font-semibold ${
              statusFilter === 'archived' ? 'bg-amber-100 text-amber-700' : 'bg-slate-100 text-slate-500'
            }`}>{leadCounts.archived}</span>
          )}
        </button>
        <button
          onClick={() => setStatusFilter('blocked')}
          className={`inline-flex items-center gap-1 px-2 py-1 text-[11px] font-medium rounded-lg transition ${
            statusFilter === 'blocked'
              ? 'bg-red-50 text-red-700 ring-1 ring-red-200'
              : 'text-slate-500 hover:bg-slate-50'
          }`}
        >
          <ShieldBan className="w-3 h-3" />
          Bloqueados
          {leadCounts.blocked > 0 && (
            <span className={`px-1.5 py-0.5 rounded-full text-[10px] font-semibold ${
              statusFilter === 'blocked' ? 'bg-red-100 text-red-700' : 'bg-slate-100 text-slate-500'
            }`}>{leadCounts.blocked}</span>
          )}
        </button>
        {pipelines.length > 0 && (
          <div className="relative ml-auto">
            <select
              value={activePipeline?.id || ''}
              onChange={(e) => {
                const val = e.target.value
                if (val === '__no_pipeline__') {
                  setActivePipeline({ id: '__no_pipeline__', name: 'Sin pipeline', is_default: false, stages: [] })
                } else {
                  const p = pipelines.find(p => p.id === val)
                  if (p) setActivePipeline(p)
                }
              }}
              className="pl-2 pr-6 py-1 bg-white border border-slate-200 rounded-lg focus:ring-1 focus:ring-emerald-500 appearance-none cursor-pointer text-xs text-slate-700"
            >
              {pipelines.map(p => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
              <option value="__no_pipeline__" className="text-slate-400 italic">── Sin pipeline ──</option>
            </select>
            <ChevronDown className="absolute right-1.5 top-1/2 -translate-y-1/2 w-3 h-3 text-slate-400 pointer-events-none" />
          </div>
        )}
      </div>

      {/* Hidden leads banner */}
      {hiddenByStatus > 0 && (
        <div className="flex items-center gap-2 px-3 py-1.5 mb-2 bg-amber-50 border border-amber-200 rounded-lg text-xs text-amber-700">
          <EyeOff className="w-3.5 h-3.5 shrink-0" />
          <span>
            {hiddenByStatus} lead{hiddenByStatus !== 1 ? 's' : ''} {statusFilter === 'active' ? 'archivado' + (hiddenByStatus !== 1 ? 's' : '') + '/bloqueado' + (hiddenByStatus !== 1 ? 's' : '') : statusFilter === 'archived' ? 'activo' + (hiddenByStatus !== 1 ? 's' : '') + '/bloqueado' + (hiddenByStatus !== 1 ? 's' : '') : 'activo' + (hiddenByStatus !== 1 ? 's' : '') + '/archivado' + (hiddenByStatus !== 1 ? 's' : '')} coinciden con este filtro.
          </span>
        </div>
      )}

      {/* Pipeline Kanban — Virtualized */}
      {viewMode === 'kanban' && (
      <div className="flex-1 min-h-0 flex flex-col">
      {/* Top synced scrollbar */}
      <div
        ref={topScrollRef}
        onScroll={handleTopScroll}
        className="overflow-x-auto kanban-scroll-top flex-shrink-0"
        style={{ height: 12 }}
      >
        <div style={{ width: `${(visibleStages.length + (unassignedData.total_count > 0 ? 1 : 0)) * 288}px`, height: 1 }} />
      </div>
      <div
        ref={kanbanRef}
        onScroll={handleKanbanScroll}
        className="overflow-x-auto flex-1 min-h-0 kanban-scroll"
      >
        <div className="flex gap-3 h-full" style={{ minWidth: `${(visibleStages.length + (unassignedData.total_count > 0 ? 1 : 0)) * 288}px` }}>
          {visibleStages.map((stageItem) => (
            <VirtualKanbanColumn
              key={stageItem.id}
              column={stageItem}
              totalCount={stageItem.total_count}
              hasMore={stageItem.has_more}
              loadingMore={loadingMoreStages.has(stageItem.id)}
              onLoadMore={() => loadMoreForStage(stageItem.id)}
              selectedIds={selectedIds}
              detailLeadId={detailLead?.id || null}
              draggedLeadId={draggedLeadId}
              dragOverColumn={dragOverColumn}
              selectionMode={selectionMode}
                    onToggleSelection={toggleSelection}
              onOpenDetail={openDetailPanel}
              onDelete={handleDeleteLead}
              onDragStart={handleDragStart}
              onDragEnd={handleDragEnd}
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              onDrop={handleDrop}
            />
          ))}
          {/* Unassigned column */}
          {unassignedData.total_count > 0 && (
            <VirtualKanbanColumn
              key="__unassigned__"
              column={{
                id: '__unassigned__',
                name: 'Sin etapa',
                color: '#64748b',
                leads: unassignedData.leads,
              }}
              totalCount={unassignedData.total_count}
              hasMore={unassignedData.has_more}
              loadingMore={loadingMoreStages.has('__unassigned__')}
              onLoadMore={() => loadMoreForStage('__unassigned__')}
              selectedIds={selectedIds}
              detailLeadId={detailLead?.id || null}
              draggedLeadId={draggedLeadId}
              dragOverColumn={dragOverColumn}
              selectionMode={selectionMode}
              onToggleSelection={toggleSelection}
              onOpenDetail={openDetailPanel}
              onDelete={handleDeleteLead}
              onDragStart={handleDragStart}
              onDragEnd={handleDragEnd}
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              onDrop={handleDrop}
            />
          )}
        </div>
      </div>
      </div>
      )}

      {/* List View — Virtualized */}
      {viewMode === 'list' && (
        <div className="flex-1 min-h-0 flex flex-col">
          {/* Cross-pipeline search indicator */}
          {debouncedSearchTerm && (
            <div className="flex-shrink-0 bg-emerald-50 border-b border-emerald-100 px-4 py-1.5 flex items-center gap-2 text-xs text-emerald-700">
              <Search className="w-3 h-3" />
              <span>Buscando en todos los pipelines · <strong>{listTotal}</strong> resultado{listTotal !== 1 ? 's' : ''}</span>
            </div>
          )}
          {/* Sticky header */}
          <div className="bg-slate-50 border-b-2 border-slate-200 flex-shrink-0">
            <div className="flex">
              {selectionMode && (
                <div className="px-2 py-2.5 w-[36px] flex items-center justify-center">
                  <button
                    onClick={() => {
                      if (selectedIds.size === listLeads.length) {
                        setSelectedIds(new Set())
                      } else {
                        setSelectedIds(new Set(listLeads.map(l => l.id)))
                      }
                    }}
                    className="p-0.5"
                    title={selectedIds.size === listLeads.length ? 'Deseleccionar todos' : 'Seleccionar todos'}
                  >
                    {selectedIds.size > 0 && selectedIds.size === listLeads.length ? (
                      <CheckSquare className="w-4 h-4 text-emerald-600" />
                    ) : selectedIds.size > 0 ? (
                      <MinusSquare className="w-4 h-4 text-emerald-400" />
                    ) : (
                      <Square className="w-4 h-4 text-slate-300" />
                    )}
                  </button>
                </div>
              )}
              <div className="px-3 py-2.5 text-[11px] font-semibold text-slate-500 uppercase tracking-wider w-[220px]">Lead</div>
              <div className="px-3 py-2.5 text-[11px] font-semibold text-slate-500 uppercase tracking-wider w-[110px]">Etapa</div>
              <div className="px-3 py-2.5 text-[11px] font-semibold text-slate-500 uppercase tracking-wider w-[180px]">Etiquetas</div>
              {cfDefs.filter(d => cfVisibleIds.has(d.id)).sort((a, b) => a.sort_order - b.sort_order).map(def => (
                <div key={def.id} className="px-3 py-2.5 text-[11px] font-semibold text-slate-500 uppercase tracking-wider w-[140px] truncate">{def.name}</div>
              ))}
              <div className="px-3 py-2.5 text-[11px] font-semibold text-slate-500 uppercase tracking-wider flex-1">Últimas observaciones</div>
              {!selectionMode && (
                <div className="px-3 py-2.5 w-[40px] relative" ref={cfColumnPickerRef}>
                  {cfDefs.length > 0 && (
                    <button
                      onClick={(e) => { e.stopPropagation(); setShowCfColumnPicker(!showCfColumnPicker) }}
                      className={`p-1 rounded hover:bg-slate-100 transition ${showCfColumnPicker || cfVisibleIds.size > 0 ? 'text-emerald-600' : 'text-slate-400'}`}
                      title="Columnas personalizadas"
                    >
                      <Settings className="w-3.5 h-3.5" />
                    </button>
                  )}
                  {showCfColumnPicker && (
                    <div className="absolute right-0 top-full mt-1 w-56 bg-white border border-slate-200 rounded-xl shadow-xl z-30 py-2 max-h-64 overflow-y-auto">
                      <div className="px-3 py-1.5 text-[10px] font-semibold text-slate-400 uppercase tracking-wider">Campos personalizados</div>
                      {cfDefs.sort((a, b) => a.sort_order - b.sort_order).map(def => (
                        <label key={def.id} className="flex items-center gap-2 px-3 py-1.5 hover:bg-slate-50 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={cfVisibleIds.has(def.id)}
                            onChange={() => toggleCfColumn(def.id)}
                            className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                          />
                          <span className="text-sm text-slate-700 truncate">{def.name}</span>
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
          {/* Virtualized rows */}
          <div ref={listScrollRef} className="flex-1 min-h-0 overflow-auto">
            {listLeads.length > 0 ? (
              <div style={{ height: listVirtualizer.getTotalSize(), position: 'relative', width: '100%' }}>
                {listVirtualizer.getVirtualItems().map((virtualRow) => {
                  const lead = listLeads[virtualRow.index]
                  const stageName = lead.stage_name || stages.find(s => s.id === lead.stage_id)?.name
                  const stageColor = lead.stage_color || stages.find(s => s.id === lead.stage_id)?.color || '#94a3b8'
                  const obs = listObservations.get(lead.id)
                  const isExpanded = expandedListLeadId === lead.id

                  return (
                    <div
                      key={lead.id}
                      ref={listVirtualizer.measureElement}
                      data-index={virtualRow.index}
                      style={{
                        position: 'absolute',
                        top: 0,
                        left: 0,
                        width: '100%',
                        transform: `translateY(${virtualRow.start}px)`,
                      }}
                    >
                      <div
                        className={`flex items-start group border-b border-slate-200/80 hover:bg-emerald-50/40 hover:shadow-sm transition-all duration-150 cursor-pointer ${
                          selectionMode && selectedIds.has(lead.id) ? 'bg-emerald-50 border-l-[3px] border-l-emerald-500' :
                          detailLead?.id === lead.id ? 'bg-emerald-100 border-l-[3px] border-l-emerald-500 shadow-sm ring-1 ring-emerald-200/60' : 'border-l-[3px] border-l-transparent'
                        }`}
                        onClick={() => selectionMode ? toggleSelection(lead.id) : openDetailPanel(lead)}
                      >
                        {/* Selection checkbox */}
                        {selectionMode && (
                          <div className="px-2 py-2.5 w-[36px] flex items-center justify-center shrink-0">
                            <button onClick={(e) => { e.stopPropagation(); toggleSelection(lead.id) }} className="p-0.5">
                              {selectedIds.has(lead.id) ? <CheckSquare className="w-4 h-4 text-emerald-600" /> : <Square className="w-4 h-4 text-slate-300" />}
                            </button>
                          </div>
                        )}
                        {/* Lead info */}
                        <div className="px-3 py-2.5 w-[220px]">
                          <div className="flex items-center gap-2.5">
                            <div className="w-8 h-8 bg-emerald-50 rounded-full flex items-center justify-center shrink-0">
                              <span className="text-emerald-700 text-xs font-semibold">
                                {(lead.name || '?').charAt(0).toUpperCase()}
                              </span>
                            </div>
                            <div className="min-w-0">
                              <p className="text-[13px] font-medium text-slate-900 truncate">{lead.name || 'Sin nombre'}</p>
                              {lead.phone && (
                                <p className="text-[11px] text-slate-500 mt-0.5">{lead.phone}</p>
                              )}
                            </div>
                          </div>
                        </div>

                        {/* Stage */}
                        <div className="px-3 py-2.5 w-[110px]">
                          {stageName ? (
                            <span
                              className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-semibold text-white"
                              style={{ backgroundColor: stageColor }}
                            >
                              {stageName}
                            </span>
                          ) : (
                            <span className="text-[10px] text-slate-400 italic">Sin etapa</span>
                          )}
                        </div>

                        {/* Tags */}
                        <div className="px-3 py-2.5 w-[180px]">
                          {lead.structured_tags && lead.structured_tags.length > 0 ? (
                            <div className="flex flex-wrap gap-1">
                              {lead.structured_tags.slice(0, 3).map(tag => (
                                <span
                                  key={tag.id}
                                  className="px-1.5 py-0.5 text-[10px] rounded-full text-white font-medium"
                                  style={{ backgroundColor: tag.color || '#6b7280' }}
                                >
                                  {tag.name}
                                </span>
                              ))}
                              {lead.structured_tags.length > 3 && (
                                <span className="text-[10px] text-slate-400">+{lead.structured_tags.length - 3}</span>
                              )}
                            </div>
                          ) : (
                            <span className="text-[10px] text-slate-300">—</span>
                          )}
                        </div>

                        {/* Custom field columns */}
                        {cfDefs.filter(d => cfVisibleIds.has(d.id)).sort((a, b) => a.sort_order - b.sort_order).map(def => (
                          <div key={def.id} className="px-3 py-2.5 w-[140px] text-[11px] text-slate-600 truncate">
                            {formatCfCell(def, lead)}
                          </div>
                        ))}

                        {/* Observations preview */}
                        <div
                          className="px-3 py-2.5 flex-1 cursor-pointer hover:bg-slate-50 rounded-lg transition-colors"
                          onClick={(e) => {
                            e.stopPropagation()
                            if (obs && obs.length > 0) {
                              setListHistoryLead(lead)
                            }
                          }}
                        >
                          {loadingListObs.has(lead.id) ? (
                            <div className="flex items-center gap-2">
                              <div className="animate-spin rounded-full h-3 w-3 border border-slate-200 border-t-emerald-500" />
                              <span className="text-[10px] text-slate-400">Cargando...</span>
                            </div>
                          ) : obs && obs.length > 0 ? (
                            <div className="space-y-1">
                              {obs.slice(0, isExpanded ? 10 : 2).map(o => (
                                <div key={o.id} className="flex items-start gap-1.5">
                                  <span className="shrink-0 mt-0.5 text-[10px]">
                                    {o.type === 'call' ? '📞' : o.type === 'note' ? '📝' : '↕'}
                                  </span>
                                  <p className="text-[11px] text-slate-600 leading-tight">
                                    {(o.notes || '').replace(/^\(sinc\)\s*/i, '')}
                                  </p>
                                  <span className="shrink-0 text-[9px] text-slate-400 mt-0.5 whitespace-nowrap">
                                    {formatDistanceToNow(new Date(o.created_at), { locale: es, addSuffix: false })}
                                  </span>
                                </div>
                              ))}
                              {obs.length > 2 && (
                                <span className="text-[10px] text-emerald-600 font-medium inline-flex items-center gap-0.5">
                                  <Maximize2 className="w-3 h-3" /> Ver {obs.length} observaciones
                                </span>
                              )}
                            </div>
                          ) : (
                            <span className="text-[10px] text-slate-300 italic">Sin observaciones</span>
                          )}
                        </div>

                        {/* Actions */}
                        {!selectionMode && (
                        <div className="px-3 py-2.5 w-[40px]">
                          <button
                            onClick={(e) => { e.stopPropagation(); handleDeleteLead(lead.id) }}
                            className="p-1 text-slate-300 hover:text-red-500 opacity-0 group-hover:opacity-100 transition-opacity"
                            title="Eliminar"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </div>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className="flex flex-col items-center justify-center py-16 text-slate-400">
                <FileText className="w-10 h-10 mb-2 text-slate-300" />
                <p className="text-sm">No se encontraron leads</p>
              </div>
            )}
            {listLoading && (
              <div className="flex items-center justify-center py-3">
                <div className="animate-spin rounded-full h-5 w-5 border-2 border-slate-200 border-t-emerald-500" />
              </div>
            )}
          </div>
        </div>
      )}

      {/* List View — Historial Completo Modal */}
      {listHistoryLead && (
        <ObservationHistoryModal
          isOpen={true}
          onClose={() => setListHistoryLead(null)}
          leadId={listHistoryLead.id}
          name={listHistoryLead.name || 'Sin nombre'}
          observations={listObservations.get(listHistoryLead.id) || []}
          onObservationChange={() => {
            // Invalidate cache so it refetches
            setListObservations(prev => { const next = new Map(prev); next.delete(listHistoryLead.id); return next })
            fetchBatchObservations([listHistoryLead.id])
          }}
        />
      )}

      {/* Add Lead Modal */}
      {showAddModal && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-md border border-slate-100">
            <h2 className="text-lg font-semibold text-slate-900 mb-4">Nuevo Lead</h2>
            <div className="space-y-3">
              {/* Pipeline & Stage selector */}
              {activePipeline && activePipeline.stages && activePipeline.stages.length > 0 && (
                <div>
                  <label className="block text-xs font-medium text-slate-600 mb-1">Pipeline / Etapa</label>
                  <div className="flex gap-2">
                    <div className="flex-1 px-3 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm text-slate-700 truncate">
                      {activePipeline.name}
                    </div>
                    <select
                      value={formData.stage_id || ''}
                      onChange={(e) => setFormData({ ...formData, stage_id: e.target.value })}
                      className="flex-1 px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 bg-white"
                    >
                      <option value="">Automático (configuración de cuenta)</option>
                      {activePipeline.stages.map((st) => (
                        <option key={st.id} value={st.id}>{st.name}</option>
                      ))}
                    </select>
                  </div>
                </div>
              )}
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">Nombre *</label>
                <input
                  type="text"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400"
                  placeholder="Nombre del lead"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">Teléfono</label>
                <input
                  type="tel"
                  value={formData.phone}
                  onChange={(e) => setFormData({ ...formData, phone: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400"
                  placeholder="+51 999 888 777"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">Email</label>
                <input
                  type="email"
                  value={formData.email}
                  onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400"
                  placeholder="correo@ejemplo.com"
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-slate-600 mb-1">DNI</label>
                  <input
                    type="text"
                    value={formData.dni}
                    onChange={(e) => setFormData({ ...formData, dni: e.target.value })}
                    className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400"
                    placeholder="12345678"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-slate-600 mb-1">Fecha de nacimiento</label>
                  <input
                    type="date"
                    value={formData.birth_date}
                    onChange={(e) => setFormData({ ...formData, birth_date: e.target.value })}
                    className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400"
                  />
                </div>
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">Etiquetas</label>
                <input
                  type="text"
                  value={formData.tags}
                  onChange={(e) => setFormData({ ...formData, tags: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400"
                  placeholder="ventas, premium (separadas por coma)"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">Notas</label>
                <textarea
                  value={formData.notes}
                  onChange={(e) => setFormData({ ...formData, notes: e.target.value })}
                  rows={3}
                  className="w-full px-3 py-2 border border-slate-200 rounded-xl focus:ring-2 focus:ring-emerald-500 text-sm text-slate-900 placeholder:text-slate-400 resize-none"
                  placeholder="Notas adicionales..."
                />
              </div>
            </div>
            <div className="flex gap-3 mt-5">
              <button
                onClick={() => { setShowAddModal(false); setFormData({ name: '', phone: '', email: '', notes: '', tags: '', stage_id: '', dni: '', birth_date: '' }) }}
                className="flex-1 px-4 py-2 border border-slate-200 text-slate-600 rounded-xl hover:bg-slate-50 text-sm"
              >
                Cancelar
              </button>
              <button
                onClick={handleCreateLead}
                disabled={!formData.name}
                className="flex-1 px-4 py-2 bg-emerald-600 text-white rounded-xl hover:bg-emerald-700 disabled:opacity-50 text-sm font-medium shadow-sm"
              >
                Crear Lead
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Stage Management Modal */}
      {showStageModal && activePipeline && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-md max-h-[85vh] overflow-y-auto border border-gray-100">
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-lg font-semibold text-gray-900">Gestionar Etapas</h2>
                <p className="text-sm text-gray-500 mt-0.5">{activePipeline.name}</p>
              </div>
              <button onClick={() => setShowStageModal(false)} className="p-1.5 hover:bg-gray-100 rounded-lg transition">
                <X className="w-5 h-5 text-gray-400" />
              </button>
            </div>

            {/* Current stages with drag reorder */}
            <div className="space-y-1.5 mb-5">
              {allStages.map((stage, idx) => (
                <div
                  key={stage.id}
                  draggable={editingStageId !== stage.id}
                  onDragStart={() => setDragSrcIdx(idx)}
                  onDragOver={(e) => { e.preventDefault(); setDragOverIdx(idx) }}
                  onDragEnd={() => { if (dragSrcIdx !== null && dragOverIdx !== null) handleReorderStages(dragSrcIdx, dragOverIdx); setDragSrcIdx(null); setDragOverIdx(null) }}
                  className={`p-2.5 rounded-xl transition-all ${
                    dragOverIdx === idx ? 'bg-green-50 ring-2 ring-green-300' : 'bg-gray-50 hover:bg-gray-100'
                  } ${hiddenStageIds.has(stage.id) ? 'opacity-50' : ''}`}
                >
                  {editingStageId === stage.id ? (
                    <div className="flex items-center gap-2">
                      <input
                        type="color"
                        value={editStageColor}
                        onChange={(e) => setEditStageColor(e.target.value)}
                        className="w-8 h-8 rounded border border-gray-300 cursor-pointer shrink-0"
                      />
                      <input
                        type="text"
                        value={editStageName}
                        onChange={(e) => setEditStageName(e.target.value)}
                        className="flex-1 px-2 py-1 border border-gray-300 rounded-lg text-sm text-gray-900 focus:ring-2 focus:ring-green-500"
                        onKeyDown={(e) => e.key === 'Enter' && handleUpdateStage(stage.id)}
                      />
                      <button onClick={() => handleUpdateStage(stage.id)} className="px-2.5 py-1 bg-green-600 text-white rounded-lg text-xs hover:bg-green-700">
                        Guardar
                      </button>
                      <button onClick={() => setEditingStageId(null)} className="p-1 text-gray-400 hover:text-gray-600">
                        <X className="w-4 h-4" />
                      </button>
                    </div>
                  ) : (
                    <div className="flex items-center gap-2">
                      <GripVertical className="w-4 h-4 text-gray-300 cursor-grab shrink-0" />
                      <div className="w-3.5 h-3.5 rounded-full shrink-0" style={{ backgroundColor: stage.color }} />
                      <span className="flex-1 text-sm font-medium text-gray-800 truncate">{stage.name}</span>
                      <span className="text-xs text-gray-400 shrink-0">{stage.lead_count}</span>
                      <button
                        onClick={() => toggleStageVisibility(stage.id)}
                        className="p-1 text-gray-400 hover:text-gray-600"
                        title={hiddenStageIds.has(stage.id) ? 'Mostrar etapa' : 'Ocultar etapa'}
                      >
                        {hiddenStageIds.has(stage.id) ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                      </button>
                      <button
                        onClick={() => { setEditingStageId(stage.id); setEditStageName(stage.name); setEditStageColor(stage.color) }}
                        className="p-1 text-gray-400 hover:text-blue-500"
                        title="Editar"
                      >
                        <Pencil className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => handleDeleteStage(stage.id)}
                        className="p-1 text-gray-400 hover:text-red-500"
                        title="Eliminar"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </div>

            {/* Add new stage */}
            <div className="border-t border-gray-200 pt-4">
              <h4 className="text-sm font-semibold text-gray-700 mb-3">Agregar nueva etapa</h4>
              <div className="flex gap-2">
                <input
                  type="color"
                  value={newStageColor}
                  onChange={(e) => setNewStageColor(e.target.value)}
                  className="w-10 h-10 rounded-lg border border-gray-300 cursor-pointer"
                />
                <input
                  type="text"
                  value={newStageName}
                  onChange={(e) => setNewStageName(e.target.value)}
                  placeholder="Nombre de la etapa"
                  className="flex-1 px-3 py-2 border border-gray-200 rounded-xl text-sm text-gray-900 placeholder:text-gray-400 focus:ring-2 focus:ring-green-500"
                  onKeyDown={(e) => e.key === 'Enter' && handleAddStage()}
                />
                <button
                  onClick={handleAddStage}
                  disabled={!newStageName.trim()}
                  className="px-4 py-2 bg-green-600 text-white rounded-xl hover:bg-green-700 disabled:opacity-50 text-sm font-medium"
                >
                  Agregar
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Lead Detail Panel (Slide-over) with Inline Chat */}
      {(showDetailPanel || showInlineChat) && detailLead && (
        <div className="fixed inset-0 z-50 flex justify-end overflow-hidden">
          <div
            className="absolute inset-0 bg-black/30 backdrop-blur-[2px]"
            onClick={() => { setShowDetailPanel(false); setShowInlineChat(false); setInlineChatReadOnly(false); setNewObservation(''); setEditingField(null); setEditingNotes(false) }}
          />
          <div className={`relative h-full bg-white shadow-2xl flex transition-all duration-300 border-l border-slate-200 ${showInlineChat ? 'w-[85vw] max-w-6xl' : 'w-full max-w-md'}`}>

            {/* Chat Panel - Left Side */}
            {showInlineChat && inlineChatId && (
              <div className="flex-1 min-w-0 border-r border-slate-200 flex flex-col h-full bg-slate-50/50">
                <ChatPanel
                  chatId={inlineChatId}
                  deviceId={inlineChatDeviceId}
                  initialChat={inlineChat || undefined}
                  readOnly={inlineChatReadOnly}
                  onClose={() => { setShowInlineChat(false); setInlineChatReadOnly(false) }}
                  className="h-full"
                />
              </div>
            )}

            {/* Lead Details - Right Side */}
            <div className={`${showInlineChat ? 'w-[360px] shrink-0' : 'w-full'} flex flex-col h-full bg-white`}>
              <LeadDetailPanel
                lead={detailLead}
                scrollToTasks={scrollToTasks}
                onLeadChange={(updatedLead: Lead) => {
                  setDetailLead(updatedLead as any)
                  updateLeadInStages(updatedLead.id, () => updatedLead as any)
                }}
                onClose={() => { setShowDetailPanel(false); setShowInlineChat(false); setScrollToTasks(false) }}
                onSendWhatsApp={(phone: string) => handleSendWhatsApp(phone)}
                onObservationChange={(leadId: string) => {
                  if (viewMode === 'list') {
                    setListObservations(prev => { const next = new Map(prev); next.delete(leadId); return next })
                    setLoadingListObs(prev => { const next = new Set(prev); next.delete(leadId); return next })
                    fetchBatchObservations([leadId])
                  }
                }}
                onDelete={(leadId: string) => {
                  removeLeadFromStages(leadId)
                  setShowDetailPanel(false)
                  setShowInlineChat(false)
                }}
                hideWhatsApp={showInlineChat}
                onArchive={(leadId: string, archive: boolean) => {
                  if (archive) {
                    openArchiveModal(leadId, false)
                  } else {
                    handleArchiveLead(leadId, false)
                    setShowDetailPanel(false)
                    setShowInlineChat(false)
                  }
                }}
                onBlock={(leadId: string) => {
                  openBlockModal(leadId, false)
                }}
                onUnblock={(leadId: string) => {
                  handleBlockLead(leadId, false)
                  setShowDetailPanel(false)
                  setShowInlineChat(false)
                }}
              />

            </div>
          </div>
        </div>
      )}

      {/* Device Selector Modal for WhatsApp */}
      {showDeviceSelector && (
        <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-[60] p-4">
          <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-sm border border-slate-100">
	            <h2 className="text-sm font-semibold text-slate-900 mb-3">Seleccionar dispositivo</h2>
	            <p className="text-xs text-slate-500 mb-4">Elige el dispositivo para enviar el mensaje a {whatsappPhone}</p>
	            {existingChatForWA && (
	              <p className="text-xs text-amber-700 bg-amber-50 border border-amber-100 rounded-lg px-3 py-2 mb-3">
	                Ya existe historial{whatsappHistoricalPhone ? ` con el numero ${whatsappHistoricalPhone}` : ' con numero historico desconocido'}.
	              </p>
	            )}
            {devices.length === 0 ? (
              <p className="text-xs text-slate-400 text-center py-4">No hay dispositivos conectados</p>
            ) : (
              <div className="space-y-2">
                {/* Connected devices — sort chat owner first */}
                {[...devices].sort((a, b) => {
                  if (existingChatForWA?.device_id === a.id) return -1
                  if (existingChatForWA?.device_id === b.id) return 1
                  return 0
	                }).map((device) => {
	                  const isChatOwner = device.matches_historical || existingChatForWA?.device_id === device.id
	                  return (
                    <button
                      key={device.id}
                      onClick={() => handleDeviceSelected(device)}
                      className={`w-full flex items-center gap-3 p-3 border rounded-xl transition text-left ${isChatOwner ? 'border-emerald-200 bg-emerald-50/50 hover:bg-emerald-50' : 'border-slate-100 hover:bg-emerald-50 hover:border-emerald-200'}`}
                    >
                      <div className={`w-9 h-9 rounded-full flex items-center justify-center ${isChatOwner ? 'bg-emerald-100' : 'bg-emerald-50'}`}>
                        <Phone className="w-4 h-4 text-emerald-600" />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <p className="text-sm font-medium text-slate-900">{device.name || 'Dispositivo'}</p>
	                          {isChatOwner && (
	                            <span className="text-[10px] font-medium bg-emerald-100 text-emerald-700 px-1.5 py-0.5 rounded-full">Chat activo</span>
	                          )}
	                          <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded-full ${relationClassName(device)}`}>{relationLabel(device)}</span>
	                        </div>
	                        <p className="text-xs text-slate-500">{deviceDisplayPhone(device)}</p>
	                      </div>
                    </button>
                  )
                })}

                {/* Previous device option (disconnected) — read-only mode */}
                {existingChatForWA && existingChatForWA.device_id && !devices.find(d => d.id === existingChatForWA.device_id) && (
                  <div className="pt-2 mt-2 border-t border-slate-100">
                    <button
                      onClick={handlePreviousDeviceSelected}
                      className="w-full flex items-center gap-3 p-3 border border-amber-200 bg-amber-50/50 rounded-xl hover:bg-amber-50 transition text-left"
                    >
                      <div className="w-9 h-9 bg-amber-100 rounded-full flex items-center justify-center">
                        <Eye className="w-4 h-4 text-amber-600" />
                      </div>
                      <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium text-amber-800">Dispositivo anterior</p>
                        <p className="text-xs text-amber-600">Solo lectura · {existingChatForWA.device_name || 'Desconectado'}</p>
                      </div>
                    </button>
                  </div>
                )}
              </div>
            )}
            <button onClick={() => setShowDeviceSelector(false)} className="w-full mt-4 px-4 py-2 border border-slate-200 text-slate-600 rounded-xl hover:bg-slate-50 text-sm">
              Cancelar
            </button>
          </div>
        </div>
      )}



      <ImportCSVModal
        open={showImportModal}
        onClose={() => setShowImportModal(false)}
        onSuccess={() => { fetchLeadsPaginated(); fetchPipelines(activePipelineIdRef.current) }}
        defaultType="leads"
      />

      <ContactSelector
        open={showContactImportModal}
        onClose={() => {
          if (!importingContacts) setShowContactImportModal(false)
        }}
        onConfirm={handleCreateLeadsFromContacts}
        title="Crear leads desde contactos"
        subtitle="Selecciona contactos existentes que todavía no tienen un lead activo"
        confirmLabel={importingContacts ? 'Creando...' : 'Crear leads'}
        sourceFilter="contact"
        advancedFilters
        withoutActiveLead
      />

      {/* Broadcast from Leads Modal */}
      <CreateCampaignModal
        open={showBroadcastModal}
        onClose={() => setShowBroadcastModal(false)}
        onSubmit={handleCreateBroadcastFromLeads}
        devices={devices}
        submitting={submittingBroadcast}
        title="Envío Masivo desde Leads"
        subtitle={`Se incluirán todos los ${totalLeadCount} leads con teléfono (filtro aplicado en servidor)`}
        submitLabel={submittingBroadcast ? 'Creando...' : 'Crear y agregar destinatarios'}
        initialName={`Leads - ${new Date().toLocaleDateString('es-PE', { day: 'numeric', month: 'short' })}`}
        infoPanel={
          <div className="bg-emerald-50 border border-emerald-100 rounded-xl p-3 text-xs text-emerald-800">
            <div className="flex items-center gap-2 mb-1">
              <Radio className="w-3.5 h-3.5 text-emerald-600" />
              <span className="font-medium">Destinatarios desde Leads</span>
            </div>
            <p className="text-emerald-600">
              Se agregarán automáticamente <strong>todos los leads con teléfono</strong> que coincidan con los filtros actuales
              {filterStageIds.size > 0 || filterTagNames.size > 0 || debouncedSearchTerm || filterDatePreset
                ? ' (filtrados)' : ''} como destinatarios.
            </p>
            <p className="text-slate-500 mt-1">
              Total de leads en filtro actual: <strong>{totalLeadCount}</strong> (los sin teléfono se excluyen automáticamente).
            </p>
          </div>
        }
      />

      {/* Create Event from Leads Modal */}
      {showCreateEventModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
          <div className="bg-white rounded-2xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden">
            <div className="px-6 py-4 border-b border-slate-200 flex items-center justify-between">
              <div>
                <h3 className="text-lg font-bold text-slate-900">Crear Evento desde Leads</h3>
                <p className="text-xs text-slate-500 mt-0.5">Se agregarán los leads del filtro actual como participantes</p>
              </div>
              <button onClick={() => setShowCreateEventModal(false)} className="p-1.5 text-slate-400 hover:text-slate-600 rounded-lg hover:bg-slate-100">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="px-6 py-4 space-y-4">
              {/* Active filters summary */}
              {(debouncedSearchTerm || filterTagNames.size > 0 || filterStageIds.size > 0 || filterDeviceIds.size > 0) && (
                <div className="bg-emerald-50 border border-emerald-100 rounded-xl p-3 text-xs text-emerald-800">
                  <p className="font-medium mb-1">Filtros activos:</p>
                  <div className="flex flex-wrap gap-1.5">
                    {debouncedSearchTerm && <span className="bg-emerald-100 px-2 py-0.5 rounded-full">Búsqueda: &quot;{debouncedSearchTerm}&quot;</span>}
                    {filterTagNames.size > 0 && <span className="bg-emerald-100 px-2 py-0.5 rounded-full">{filterTagNames.size} etiqueta(s)</span>}
                    {filterStageIds.size > 0 && <span className="bg-emerald-100 px-2 py-0.5 rounded-full">{filterStageIds.size} etapa(s)</span>}
                    {filterDeviceIds.size > 0 && <span className="bg-emerald-100 px-2 py-0.5 rounded-full">{filterDeviceIds.size} dispositivo(s)</span>}
                  </div>
                </div>
              )}
              <div>
                <label className="text-sm font-medium text-slate-700">Nombre del evento *</label>
                <input
                  value={createEventForm.name}
                  onChange={e => setCreateEventForm(f => ({ ...f, name: e.target.value }))}
                  placeholder="Ej: Webinar Febrero 2025"
                  className="mt-1 w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900"
                />
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Descripción</label>
                <textarea
                  value={createEventForm.description}
                  onChange={e => setCreateEventForm(f => ({ ...f, description: e.target.value }))}
                  rows={2}
                  className="mt-1 w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900"
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-sm font-medium text-slate-700">Fecha inicio</label>
                  <input
                    type="datetime-local"
                    value={createEventForm.event_date}
                    onChange={e => setCreateEventForm(f => ({ ...f, event_date: e.target.value }))}
                    className="mt-1 w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium text-slate-700">Fecha fin</label>
                  <input
                    type="datetime-local"
                    value={createEventForm.event_end}
                    onChange={e => setCreateEventForm(f => ({ ...f, event_end: e.target.value }))}
                    className="mt-1 w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900"
                  />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-sm font-medium text-slate-700">Ubicación</label>
                  <input
                    value={createEventForm.location}
                    onChange={e => setCreateEventForm(f => ({ ...f, location: e.target.value }))}
                    placeholder="Ej: Sala de conferencias"
                    className="mt-1 w-full px-3 py-2 border border-slate-300 rounded-lg text-sm focus:ring-2 focus:ring-emerald-500 focus:border-transparent text-slate-900"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium text-slate-700">Color</label>
                  <div className="mt-1 flex gap-2 flex-wrap">
                    {['#10b981', '#3b82f6', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#6366f1'].map(c => (
                      <button
                        key={c}
                        onClick={() => setCreateEventForm(f => ({ ...f, color: c }))}
                        className={`w-8 h-8 rounded-full border-2 transition-all ${createEventForm.color === c ? 'border-slate-800 scale-110' : 'border-transparent hover:scale-105'}`}
                        style={{ backgroundColor: c }}
                      />
                    ))}
                  </div>
                </div>
              </div>
            </div>
            <div className="px-6 py-4 border-t border-slate-200 flex justify-end gap-3">
              <button
                onClick={() => setShowCreateEventModal(false)}
                className="px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 rounded-lg transition"
              >
                Cancelar
              </button>
              <button
                onClick={handleCreateEventFromLeads}
                disabled={creatingEvent || !createEventForm.name}
                className="px-4 py-2 bg-emerald-600 text-white text-sm font-medium rounded-lg hover:bg-emerald-700 disabled:opacity-50 transition"
              >
                {creatingEvent ? 'Creando...' : 'Crear Evento'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Block Reason Modal */}
      {showBlockModal && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
          <div className="bg-white rounded-2xl shadow-2xl w-[420px] max-w-[95vw]">
            <div className="px-6 py-4 border-b border-slate-200">
              <h3 className="text-lg font-semibold text-slate-900 flex items-center gap-2">
                <ShieldBan className="w-5 h-5 text-red-500" />
                Bloquear {blockBatchMode ? `${selectedIds.size} lead(s)` : 'lead'}
              </h3>
              <p className="text-sm text-slate-500 mt-1">Selecciona el motivo del bloqueo. Los leads bloqueados no recibirán mensajes ni participarán en campañas o eventos.</p>
            </div>
            <div className="px-6 py-4 space-y-2">
              {[
                'No está interesado',
                'Solicita no ser contactado',
                'Agresivo o abusivo',
                'Número equivocado',
                'Spam o fraude',
              ].map(reason => (
                <button
                  key={reason}
                  onClick={() => setBlockReason(reason)}
                  className={`w-full text-left px-4 py-2.5 rounded-lg text-sm transition ${
                    blockReason === reason
                      ? 'bg-red-50 text-red-700 ring-1 ring-red-200 font-medium'
                      : 'text-slate-700 hover:bg-slate-50'
                  }`}
                >
                  {reason}
                </button>
              ))}
              <div className="pt-2">
                <input
                  type="text"
                  placeholder="Otro motivo..."
                  value={!['No está interesado', 'Solicita no ser contactado', 'Agresivo o abusivo', 'Número equivocado', 'Spam o fraude'].includes(blockReason) ? blockReason : ''}
                  onChange={(e) => setBlockReason(e.target.value)}
                  onFocus={() => { if (['No está interesado', 'Solicita no ser contactado', 'Agresivo o abusivo', 'Número equivocado', 'Spam o fraude'].includes(blockReason)) setBlockReason('') }}
                  className="w-full px-4 py-2.5 border border-slate-200 rounded-lg text-sm focus:ring-2 focus:ring-red-500 focus:border-red-500"
                />
              </div>
            </div>
            <div className="px-6 py-4 border-t border-slate-200 flex justify-end gap-3">
              <button
                onClick={() => setShowBlockModal(false)}
                className="px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 rounded-lg transition"
              >
                Cancelar
              </button>
              <button
                onClick={confirmBlock}
                disabled={!blockReason}
                className="px-4 py-2 bg-red-600 text-white text-sm font-medium rounded-lg hover:bg-red-700 disabled:opacity-50 transition"
              >
                Bloquear
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Archive Reason Modal */}
      {showArchiveModal && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
          <div className="bg-white rounded-2xl shadow-2xl w-[420px] max-w-[95vw]">
            <div className="px-6 py-4 border-b border-slate-200">
              <h3 className="text-lg font-semibold text-slate-900 flex items-center gap-2">
                <Archive className="w-5 h-5 text-amber-500" />
                Archivar {archiveBatchMode ? `${selectedIds.size} lead(s)` : 'lead'}
              </h3>
              <p className="text-sm text-slate-500 mt-1">Selecciona el motivo del archivado. Los leads archivados no aparecerán en la vista principal ni participarán en eventos.</p>
            </div>
            <div className="px-6 py-4 space-y-2">
              {[
                'Ya no aplica al programa',
                'Proceso finalizado',
                'Lead duplicado',
                'Datos incorrectos',
                'No responde',
              ].map(reason => (
                <button
                  key={reason}
                  onClick={() => setArchiveReason(reason)}
                  className={`w-full text-left px-4 py-2.5 rounded-lg text-sm transition ${
                    archiveReason === reason
                      ? 'bg-amber-50 text-amber-700 ring-1 ring-amber-200 font-medium'
                      : 'text-slate-700 hover:bg-slate-50'
                  }`}
                >
                  {reason}
                </button>
              ))}
              <div className="pt-2">
                <input
                  type="text"
                  placeholder="Otro motivo..."
                  value={!['Ya no aplica al programa', 'Proceso finalizado', 'Lead duplicado', 'Datos incorrectos', 'No responde'].includes(archiveReason) ? archiveReason : ''}
                  onChange={(e) => setArchiveReason(e.target.value)}
                  onFocus={() => { if (['Ya no aplica al programa', 'Proceso finalizado', 'Lead duplicado', 'Datos incorrectos', 'No responde'].includes(archiveReason)) setArchiveReason('') }}
                  className="w-full px-4 py-2.5 border border-slate-200 rounded-lg text-sm focus:ring-2 focus:ring-amber-500 focus:border-amber-500"
                />
              </div>
            </div>
            <div className="px-6 py-4 border-t border-slate-200 flex justify-end gap-3">
              <button
                onClick={() => setShowArchiveModal(false)}
                className="px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 rounded-lg transition"
              >
                Cancelar
              </button>
              <button
                onClick={confirmArchive}
                disabled={!archiveReason}
                className="px-4 py-2 bg-amber-600 text-white text-sm font-medium rounded-lg hover:bg-amber-700 disabled:opacity-50 transition"
              >
                Archivar
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Export Modal */}

      {/* Bulk Document Generation Modal */}
      {showBulkDocModal && (
        <BulkGenerateDocumentModal
          leads={viewMode === 'list' ? listLeads : allLoadedLeads}
          onClose={() => setShowBulkDocModal(false)}
        />
      )}

      {showExportModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-white rounded-xl p-6 w-full max-w-sm">
            <div className="flex items-center gap-3 mb-4">
              <div className="w-10 h-10 bg-emerald-100 rounded-full flex items-center justify-center">
                <Download className="w-5 h-5 text-emerald-600" />
              </div>
              <div>
                <h3 className="text-lg font-bold text-slate-900">Exportar Leads</h3>
                <p className="text-sm text-slate-500">{activePipeline?.name || 'Todos'}</p>
              </div>
            </div>

            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-2">Formato</label>
                <div className="flex rounded-lg border border-slate-200 overflow-hidden">
                  <button onClick={() => setExportFormat('excel')}
                    className={`flex-1 px-3 py-2 text-sm font-medium transition ${exportFormat === 'excel' ? 'bg-emerald-600 text-white' : 'bg-white text-slate-600 hover:bg-slate-50'}`}>
                    Excel (.xlsx)
                  </button>
                  <button onClick={() => setExportFormat('csv')}
                    className={`flex-1 px-3 py-2 text-sm font-medium transition ${exportFormat === 'csv' ? 'bg-emerald-600 text-white' : 'bg-white text-slate-600 hover:bg-slate-50'}`}>
                    CSV
                  </button>
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-slate-700 mb-2">Alcance</label>
                <div className="space-y-2">
                  <label className="flex items-center gap-3 p-3 border border-slate-200 rounded-lg cursor-pointer hover:bg-slate-50">
                    <input type="radio" checked={exportScope === 'all'} onChange={() => setExportScope('all')} className="text-emerald-600 focus:ring-emerald-500" />
                    <div>
                      <p className="text-sm font-medium text-slate-700">Todos los leads del pipeline</p>
                    </div>
                  </label>
                  <label className={`flex items-center gap-3 p-3 border rounded-lg cursor-pointer hover:bg-slate-50 ${activeFilterCount > 0 ? 'border-emerald-300 bg-emerald-50/50' : 'border-slate-200'}`}>
                    <input type="radio" checked={exportScope === 'filtered'} onChange={() => setExportScope('filtered')} className="text-emerald-600 focus:ring-emerald-500" />
                    <div>
                      <p className="text-sm font-medium text-slate-700">Solo filtrados</p>
                      {activeFilterCount > 0 && <p className="text-xs text-emerald-600">{activeFilterCount} filtro{activeFilterCount > 1 ? 's' : ''} activo{activeFilterCount > 1 ? 's' : ''}</p>}
                    </div>
                  </label>
                </div>
              </div>
            </div>

            <div className="flex gap-3 mt-6">
              <button onClick={() => setShowExportModal(false)} className="flex-1 px-4 py-2 border border-slate-300 text-slate-700 rounded-lg hover:bg-slate-50 text-sm">
                Cancelar
              </button>
              <button onClick={handleExportLeads} disabled={exporting}
                className="flex-1 px-4 py-2 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 text-sm disabled:opacity-50 flex items-center justify-center gap-2">
                {exporting ? <><div className="animate-spin rounded-full h-4 w-4 border-b-2 border-white" /> Exportando...</> : <><Download className="w-4 h-4" /> Exportar</>}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
