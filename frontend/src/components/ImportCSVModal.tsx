'use client'

import { useState, useRef, useEffect, useCallback } from 'react'
import { Upload, FileText, X, AlertTriangle, CheckCircle2, Download, Loader2, ShieldCheck, Search, Plus, Check } from 'lucide-react'

interface ImportCSVModalProps {
  open: boolean
  onClose: () => void
  onSuccess: () => void
  defaultType?: 'leads' | 'contacts' | 'both'
}

interface ImportPreviewRow {
  row: number
  action: 'create' | 'update_existing' | 'skip' | string
  reason?: string
  name?: string
  phone?: string
  kommo_id?: number
  existing_lead_id?: string
}

interface ImportSummary {
  import_type: string
  source: string
  file_name: string
  import_tag?: string
  total_rows: number
  new: number
  existing: number
  created: number
  updated: number
  skipped: number
  duplicates: number
  error_count: number
  new_contacts: number
  safe_mode: boolean
  incoming_destination?: string
  rows?: ImportPreviewRow[] | null
  errors: string[] | null
}

interface ImportTagOption {
  id: string
  name: string
  color?: string
}

const EXCEL_ACCEPT = '.xlsx,.xls,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-excel'
const EXCEL_EXTENSIONS = /\.(xlsx|xls)$/i
const EXCEL_MIME_TYPES = new Set([
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  'application/vnd.ms-excel',
])
const TAG_PRESET_COLORS = [
  '#ef4444', '#f97316', '#eab308', '#22c55e', '#06b6d4',
  '#3b82f6', '#6366f1', '#8b5cf6', '#ec4899', '#6b7280',
]

const isExcelFile = (file: File) => EXCEL_EXTENSIONS.test(file.name) || EXCEL_MIME_TYPES.has(file.type)

const csvFileNameFromExcel = (name: string) => {
  const base = name.replace(EXCEL_EXTENSIONS, '').trim() || 'importacion_excel'
  return `${base}.csv`
}

const buildExcelTemplate = async (defaultType: 'leads' | 'contacts' | 'both') => {
  const { utils, writeFile } = await import('xlsx')
  const row = defaultType === 'contacts'
    ? {
        telefono: '987654321',
        nombre: 'Juan Perez',
        apellido: 'Gomez',
        email: 'juan@ejemplo.com',
        empresa: 'Empresa SA',
        notas: 'Nota de ejemplo',
        dni: '12345678',
        fecha_nacimiento: '1990-05-15',
      }
    : {
        telefono: '987654321',
        nombre: 'Juan Perez',
        apellido: 'Gomez',
        email: 'juan@ejemplo.com',
        empresa: 'Empresa SA',
        notas: 'Nota de ejemplo',
        dni: '12345678',
        fecha_nacimiento: '1990-05-15',
        tags: 'cliente, vip',
      }
  const ws = utils.json_to_sheet([row])
  const wb = utils.book_new()
  utils.book_append_sheet(wb, ws, defaultType === 'contacts' ? 'Contactos' : 'Leads')
  writeFile(wb, defaultType === 'contacts' ? 'plantilla_contactos.xlsx' : 'plantilla_leads.xlsx')
}

const normalizeImportSummary = (summary?: Partial<ImportSummary> | null): ImportSummary => ({
  import_type: summary?.import_type || 'leads',
  source: summary?.source || '',
  file_name: summary?.file_name || '',
  import_tag: summary?.import_tag,
  total_rows: summary?.total_rows || 0,
  new: summary?.new || 0,
  existing: summary?.existing || 0,
  created: summary?.created || 0,
  updated: summary?.updated || 0,
  skipped: summary?.skipped || 0,
  duplicates: summary?.duplicates || 0,
  error_count: summary?.error_count || 0,
  new_contacts: summary?.new_contacts || 0,
  safe_mode: summary?.safe_mode !== false,
  incoming_destination: summary?.incoming_destination,
  rows: Array.isArray(summary?.rows) ? summary.rows : [],
  errors: Array.isArray(summary?.errors) ? summary.errors : [],
})


export default function ImportCSVModal({ open, onClose, onSuccess, defaultType = 'leads' }: ImportCSVModalProps) {
  const [file, setFile] = useState<File | null>(null)
  const [importTagMode, setImportTagMode] = useState<'none' | 'custom'>('none')
  const [importTag, setImportTag] = useState('')
  const [tagSearch, setTagSearch] = useState('')
  const [tagOptions, setTagOptions] = useState<ImportTagOption[]>([])
  const [creatingTag, setCreatingTag] = useState(false)
  const [newTagColor, setNewTagColor] = useState(TAG_PRESET_COLORS[6])
  const [previewing, setPreviewing] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [preview, setPreview] = useState<ImportSummary | null>(null)
  const [result, setResult] = useState<ImportSummary | null>(null)
  const [error, setError] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setImportTagMode('none')
    setImportTag('')
    setTagSearch('')
    setNewTagColor(TAG_PRESET_COLORS[6])
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [open, onClose, defaultType])

  const fetchTagOptions = useCallback(async () => {
    if (defaultType === 'contacts') return
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/tags', { headers: token ? { Authorization: `Bearer ${token}` } : {} })
      const data = await res.json()
      if (data.success && Array.isArray(data.tags)) {
        setTagOptions(data.tags.map((tag: any) => ({
          id: tag.id,
          name: tag.name,
          color: tag.color,
        })))
      }
    } catch {
      setTagOptions([])
    }
  }, [defaultType])

  useEffect(() => {
    if (!open || defaultType === 'contacts') return
    fetchTagOptions()
  }, [open, defaultType, fetchTagOptions])

  const handleSelectImportTag = (tag: ImportTagOption) => {
    setImportTagMode('custom')
    setImportTag(tag.name)
    setTagSearch('')
    setPreview(null)
    setResult(null)
  }

  const handleCreateImportTag = async () => {
    const name = tagSearch.trim()
    if (!name || creatingTag) return
    setCreatingTag(true)
    setError('')
    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/tags', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name, color: newTagColor }),
      })
      const data = await res.json()
      if (data.success && data.tag) {
        const created = { id: data.tag.id, name: data.tag.name, color: data.tag.color }
        setTagOptions(prev => {
          const withoutDuplicate = prev.filter(tag => tag.name.toLowerCase() !== created.name.toLowerCase())
          return [...withoutDuplicate, created].sort((a, b) => a.name.localeCompare(b.name))
        })
        handleSelectImportTag(created)
        setNewTagColor(TAG_PRESET_COLORS[Math.floor(Math.random() * TAG_PRESET_COLORS.length)])
      } else {
        setError(data.error || 'No se pudo crear la etiqueta')
      }
    } catch {
      setError('No se pudo crear la etiqueta')
    } finally {
      setCreatingTag(false)
    }
  }

  if (!open) return null

  const title = defaultType === 'contacts' ? 'Importar Contactos' : defaultType === 'both' ? 'Importar Leads y Contactos' : 'Importar Leads'

  const excelToCSVFile = async (sourceFile: File) => {
    if (!isExcelFile(sourceFile)) {
      throw new Error('Selecciona un archivo Excel válido (.xlsx o .xls).')
    }
    const XLSX = await import('xlsx')
    const buffer = await sourceFile.arrayBuffer()
    const workbook = XLSX.read(buffer, { type: 'array', cellDates: false })
    const sheetName = workbook.SheetNames[0]
    if (!sheetName) {
      throw new Error('El Excel no contiene hojas para importar.')
    }
    const worksheet = workbook.Sheets[sheetName]
    const csv = XLSX.utils.sheet_to_csv(worksheet, { blankrows: false })
    if (!csv.trim() || csv.trim().split(/\r?\n/).length < 2) {
      throw new Error('El Excel debe tener encabezados y al menos una fila de datos.')
    }
    return new File([csv], csvFileNameFromExcel(sourceFile.name), { type: 'text/csv;charset=utf-8' })
  }

  const buildFormData = async () => {
    if (!file) return null
    const csvFile = await excelToCSVFile(file)
    const formData = new FormData()
    formData.append('file', csvFile)
    formData.append('import_type', defaultType)
    if (defaultType !== 'contacts' && importTagMode === 'custom' && importTag.trim()) {
      formData.append('import_tag', importTag.trim())
    }
    return formData
  }

  const handleFileChange = (nextFile: File | null) => {
    setPreview(null)
    setResult(null)
    setError('')
    if (nextFile && !isExcelFile(nextFile)) {
      setFile(null)
      setError('Esta importación acepta solamente archivos Excel (.xlsx o .xls).')
      if (fileInputRef.current) fileInputRef.current.value = ''
      return
    }
    setFile(nextFile)
  }

  const handlePreview = async () => {
    if (!file) return
    setPreviewing(true)
    setError('')
    setPreview(null)
    setResult(null)

    const token = localStorage.getItem('token')
    try {
      const formData = await buildFormData()
      if (!formData) return
      const res = await fetch('/api/import/csv/preview', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
        body: formData,
      })
      const data = await res.json()
      if (data.success) {
        setPreview(normalizeImportSummary(data.preview))
      } else {
        setError(data.error || 'No se pudo analizar el Excel')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo leer el Excel')
    } finally {
      setPreviewing(false)
    }
  }

  const handleUpload = async () => {
    if (!file) return
    setUploading(true)
    setError('')

    const token = localStorage.getItem('token')
    try {
      const formData = await buildFormData()
      if (!formData) return
      const res = await fetch('/api/import/csv', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
        body: formData,
      })
      const data = await res.json()
      if (data.success) {
        setResult(normalizeImportSummary(data.summary))
        setPreview(null)
        onSuccess()
      } else {
        setError(data.error || 'Error desconocido')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Error de conexión')
    } finally {
      setUploading(false)
    }
  }

  const handleClose = () => {
    setFile(null)
    setPreview(null)
    setResult(null)
    setError('')
    setPreviewing(false)
    setUploading(false)
    setImportTagMode('none')
    setImportTag('')
    setTagSearch('')
    setNewTagColor(TAG_PRESET_COLORS[6])
    onClose()
  }

  const actionLabel = (action: string) => {
    if (action === 'create') return 'Nuevo'
    if (action === 'update_existing') return 'Existente'
    return 'Omitido'
  }

  const actionClass = (action: string) => {
    if (action === 'create') return 'bg-emerald-50 text-emerald-700 border-emerald-100'
    if (action === 'update_existing') return 'bg-blue-50 text-blue-700 border-blue-100'
    return 'bg-amber-50 text-amber-700 border-amber-100'
  }

  const selectedImportTag = importTag
    ? tagOptions.find(tag => tag.name.toLowerCase() === importTag.toLowerCase())
    : null
  const normalizedTagSearch = tagSearch.trim().toLowerCase()
  const filteredTagOptions = normalizedTagSearch
    ? tagOptions.filter(tag => tag.name.toLowerCase().includes(normalizedTagSearch))
    : tagOptions
  const exactTagMatch = normalizedTagSearch
    ? tagOptions.find(tag => tag.name.toLowerCase() === normalizedTagSearch)
    : null
  const canCreateImportTag = Boolean(tagSearch.trim()) && !exactTagMatch
  const needsImportTagSelection = defaultType !== 'contacts' && importTagMode === 'custom' && !importTag.trim()
  const canPreview = Boolean(file) && !previewing && !needsImportTagSelection

  const SummaryStats = ({ data, final = false }: { data: ImportSummary; final?: boolean }) => (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
      <div className="rounded-lg border border-slate-200 p-3">
        <p className="text-[11px] uppercase font-semibold text-slate-400">{final ? 'Creados' : 'Nuevos'}</p>
        <p className="text-xl font-semibold text-slate-900">{final ? data.created : data.new}</p>
      </div>
      <div className="rounded-lg border border-slate-200 p-3">
        <p className="text-[11px] uppercase font-semibold text-slate-400">{final ? 'Actualizados' : 'Existentes'}</p>
        <p className="text-xl font-semibold text-slate-900">{final ? data.updated : data.existing}</p>
      </div>
      <div className="rounded-lg border border-slate-200 p-3">
        <p className="text-[11px] uppercase font-semibold text-slate-400">Omitidos</p>
        <p className="text-xl font-semibold text-slate-900">{data.skipped}</p>
      </div>
      <div className="rounded-lg border border-slate-200 p-3">
        <p className="text-[11px] uppercase font-semibold text-slate-400">Duplicados</p>
        <p className="text-xl font-semibold text-slate-900">{data.duplicates}</p>
      </div>
    </div>
  )

  const resultErrors = Array.isArray(result?.errors) ? result.errors : []
  const previewErrors = Array.isArray(preview?.errors) ? preview.errors : []
  const previewRows = Array.isArray(preview?.rows) ? preview.rows : []

  return (
    <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-2xl border border-gray-100 max-h-[90vh] overflow-y-auto">
        <div className="flex items-start justify-between mb-5">
          <div>
            <h2 className="text-lg font-semibold text-gray-900">{title}</h2>
            <p className="text-sm text-gray-500 mt-0.5">Modo seguro para cargas recurrentes desde Excel de Kommo</p>
          </div>
          <button onClick={handleClose} className="p-1.5 hover:bg-gray-100 rounded-lg transition">
            <X className="w-5 h-5 text-gray-400" />
          </button>
        </div>

        {error && (
          <div className="mb-4 flex items-start gap-2 rounded-xl border border-red-100 bg-red-50 p-3 text-sm text-red-700">
            <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {result ? (
          <div className="space-y-4">
            <div className="flex items-center gap-3 p-4 bg-green-50 border border-green-100 rounded-xl">
              <div className="w-10 h-10 bg-green-100 rounded-xl flex items-center justify-center shrink-0">
                <CheckCircle2 className="w-5 h-5 text-green-600" />
              </div>
              <div>
                <p className="font-medium text-green-800">Importación segura completada</p>
                <p className="text-sm text-green-600">{result.created} creados, {result.updated} existentes procesados, {result.skipped} omitidos</p>
                {resultErrors.length === 0 && (
                  <p className="text-xs text-green-700 mt-0.5">Finalizó sin errores reportados.</p>
                )}
              </div>
            </div>
            <SummaryStats data={result} final />
            {resultErrors.length > 0 && (
              <div className="bg-amber-50 border border-amber-100 rounded-xl p-3 max-h-32 overflow-y-auto">
                <p className="text-xs font-medium text-amber-700 mb-1">Errores ({resultErrors.length}):</p>
                {resultErrors.slice(0, 10).map((item, i) => (
                  <p key={i} className="text-xs text-amber-600">{item}</p>
                ))}
              </div>
            )}
            <button onClick={handleClose} className="w-full px-4 py-2.5 bg-green-600 text-white rounded-xl hover:bg-green-700 font-medium text-sm transition">
              Cerrar
            </button>
          </div>
        ) : preview ? (
          <div className="space-y-4">
            <div className="rounded-xl border border-emerald-100 bg-emerald-50 p-3 text-sm text-emerald-800 flex items-start gap-2">
              <ShieldCheck className="w-4 h-4 mt-0.5 shrink-0" />
              <div>
                <p className="font-medium">Modo seguro activo</p>
                <p className="text-emerald-700">Los leads existentes no se moverán de etapa ni perderán notas, tareas u observaciones.</p>
                <p className="text-emerald-700">Los leads nuevos se crearán aunque superen 24h desde Kommo.</p>
                <p className="text-emerald-700">La ventana de 24h sólo limita reimportaciones sobre leads existentes; si aplica, sincroniza estado Kommo y fecha.</p>
                {preview.incoming_destination && (
                  <p className="text-emerald-700 mt-1">Los nuevos leads irán a: <span className="font-medium">{preview.incoming_destination}</span>.</p>
                )}
                {defaultType !== 'contacts' && preview.import_tag && (
                  <p className="text-emerald-700 mt-1">Etiqueta para leads nuevos: <span className="font-medium">{preview.import_tag}</span>.</p>
                )}
                {defaultType !== 'contacts' && !preview.import_tag && (
                  <p className="text-emerald-700 mt-1">No se agregará etiqueta adicional a los leads nuevos.</p>
                )}
              </div>
            </div>

            <SummaryStats data={preview} />

            <div className="rounded-xl border border-slate-200 overflow-hidden">
              <div className="px-3 py-2 bg-slate-50 border-b border-slate-200 flex items-center justify-between">
                <p className="text-xs font-semibold text-slate-500 uppercase">Vista previa</p>
                <p className="text-xs text-slate-400">{preview.total_rows} filas detectadas</p>
              </div>
              <div className="max-h-80 overflow-y-auto divide-y divide-slate-100">
                {previewRows.map((row) => (
                  <div key={`${row.row}-${row.action}-${row.phone}`} className="px-3 py-2.5 flex items-start gap-3">
                    <span className="text-xs text-slate-400 w-10 shrink-0">#{row.row}</span>
                    <span className={`text-[11px] px-2 py-0.5 rounded-full border shrink-0 ${actionClass(row.action)}`}>{actionLabel(row.action)}</span>
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <p className="text-sm font-medium text-slate-800 break-words">{row.name || row.phone || 'Sin nombre'}</p>
                        {row.kommo_id && <span className="text-[11px] text-slate-400 shrink-0">Kommo {row.kommo_id}</span>}
                      </div>
                      {row.reason && (
                        <p className="text-xs text-slate-500 whitespace-pre-line break-words leading-relaxed" title={row.reason}>
                          {row.reason}
                        </p>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {previewErrors.length > 0 && (
              <div className="bg-amber-50 border border-amber-100 rounded-xl p-3 max-h-32 overflow-y-auto">
                <p className="text-xs font-medium text-amber-700 mb-1">Advertencias ({previewErrors.length}):</p>
                {previewErrors.slice(0, 10).map((item, i) => (
                  <p key={i} className="text-xs text-amber-600">{item}</p>
                ))}
              </div>
            )}

            <div className="flex gap-3 pt-1">
              <button onClick={() => setPreview(null)} className="flex-1 px-4 py-2.5 border border-gray-200 text-gray-600 rounded-xl hover:bg-gray-50 font-medium text-sm transition">
                Volver
              </button>
              <button onClick={handleUpload} disabled={uploading || preview.total_rows === 0} className="flex-1 px-4 py-2.5 bg-green-600 text-white rounded-xl hover:bg-green-700 disabled:opacity-50 font-medium text-sm transition">
                {uploading ? (
                  <span className="flex items-center justify-center gap-2"><Loader2 className="w-4 h-4 animate-spin" />Importando...</span>
                ) : (
                  'Importar en modo seguro'
                )}
              </button>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <div>
              <input
                ref={fileInputRef}
                type="file"
                accept={EXCEL_ACCEPT}
                className="hidden"
                onChange={e => handleFileChange(e.target.files?.[0] || null)}
              />
              <button
                onClick={() => fileInputRef.current?.click()}
                className="w-full border-2 border-dashed border-gray-200 rounded-xl p-8 text-center hover:border-green-400 hover:bg-green-50/30 transition group"
              >
                {file ? (
                  <div className="flex items-center justify-center gap-3">
                    <div className="w-10 h-10 bg-green-100 rounded-lg flex items-center justify-center">
                      <FileText className="w-5 h-5 text-green-600" />
                    </div>
                    <div className="text-left">
                      <p className="text-sm font-medium text-gray-900">{file.name}</p>
                      <p className="text-xs text-gray-400">{(file.size / 1024).toFixed(1)} KB</p>
                    </div>
                  </div>
                ) : (
                  <>
                    <div className="w-12 h-12 bg-gray-100 rounded-xl flex items-center justify-center mx-auto mb-3 group-hover:bg-green-100 transition">
                      <Upload className="w-6 h-6 text-gray-400 group-hover:text-green-600 transition" />
                    </div>
                    <p className="text-sm font-medium text-gray-700">Haz clic para seleccionar un archivo</p>
                    <p className="text-xs text-gray-400 mt-1">Excel exportado desde Kommo o plantilla de Clarín</p>
                  </>
                )}
              </button>
            </div>

            <div className="bg-gray-50 rounded-xl p-3.5 text-xs text-gray-600 space-y-1">
              <div className="flex items-center justify-between mb-1">
                <p className="font-medium text-gray-700">Columnas reconocidas:</p>
                <button
                  type="button"
                  onClick={() => buildExcelTemplate(defaultType).catch(() => setError('No se pudo generar la plantilla Excel'))}
                  className="flex items-center gap-1 text-green-600 hover:text-green-700 font-medium transition-colors"
                >
                  <Download className="w-3 h-3" />
                  Descargar plantilla
                </button>
              </div>
              <p><span className="text-green-600 font-medium">Requerida:</span> phone / telefono / celular, o columna telefónica de Kommo</p>
              <p><span className="text-gray-500 font-medium">Kommo:</span> ID, Contacto principal, Correo, Etiquetas del lead, Estatus del lead, Embudo de ventas</p>
              <p><span className="text-gray-500 font-medium">Seguro:</span> los nuevos se crean siempre; al reimportar, los existentes sólo pueden cambiar o quitar etiquetas de estado Kommo y fecha dentro de 24h</p>
            </div>

            {defaultType !== 'contacts' && (
              <div className="rounded-xl border border-slate-200 p-3.5">
                <label className="text-xs font-semibold text-slate-500 uppercase">Etiqueta para nuevos leads importados</label>
                <div className="mt-2 grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => { setImportTagMode('none'); setImportTag(''); setTagSearch('') }}
                    className={`rounded-lg border px-3 py-2 text-sm font-medium transition ${
                      importTagMode === 'none'
                        ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                        : 'border-slate-200 text-slate-600 hover:bg-slate-50'
                    }`}
                  >
                    Sin etiqueta
                  </button>
                  <button
                    type="button"
                    onClick={() => setImportTagMode('custom')}
                    className={`rounded-lg border px-3 py-2 text-sm font-medium transition ${
                      importTagMode === 'custom'
                        ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                        : 'border-slate-200 text-slate-600 hover:bg-slate-50'
                    }`}
                  >
                    Usar etiqueta
                  </button>
                </div>
                {importTagMode === 'custom' && (
                  <div className="mt-3 space-y-3">
                    {importTag && (
                      <div className="inline-flex items-center gap-2 rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-700">
                        <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: selectedImportTag?.color || '#10b981' }} />
                        {importTag}
                        <button
                          type="button"
                          onClick={() => setImportTag('')}
                          className="rounded-full p-0.5 text-emerald-500 hover:bg-emerald-100 hover:text-emerald-700"
                          aria-label="Quitar etiqueta seleccionada"
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </div>
                    )}

                    <div className="relative">
                      <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
                      <input
                        value={tagSearch}
                        onChange={e => {
                          setTagSearch(e.target.value)
                          setPreview(null)
                          setResult(null)
                        }}
                        onKeyDown={e => {
                          if (e.key !== 'Enter') return
                          e.preventDefault()
                          if (exactTagMatch) {
                            handleSelectImportTag(exactTagMatch)
                          } else if (canCreateImportTag) {
                            handleCreateImportTag()
                          } else if (normalizedTagSearch && filteredTagOptions[0]) {
                            handleSelectImportTag(filteredTagOptions[0])
                          }
                        }}
                        placeholder="Buscar etiqueta existente o crear una nueva"
                        className="w-full rounded-lg border border-slate-200 py-2 pl-9 pr-3 text-sm text-slate-800 outline-none focus:border-green-500 focus:ring-2 focus:ring-green-100"
                      />
                    </div>

                    {filteredTagOptions.length > 0 && (
                      <div className="max-h-24 overflow-y-auto rounded-lg border border-slate-100 bg-slate-50 p-2">
                        <div className="flex flex-wrap gap-1.5">
                          {filteredTagOptions.slice(0, 10).map(tag => {
                            const selected = importTag.toLowerCase() === tag.name.toLowerCase()
                            return (
                              <button
                                key={tag.id}
                                type="button"
                                onClick={() => handleSelectImportTag(tag)}
                                className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition ${
                                  selected
                                    ? 'border-emerald-200 bg-white text-emerald-700'
                                    : 'border-slate-200 bg-white text-slate-600 hover:border-emerald-200 hover:text-emerald-700'
                                }`}
                              >
                                <span className="h-2 w-2 rounded-full" style={{ backgroundColor: tag.color || '#64748b' }} />
                                {tag.name}
                                {selected && <Check className="h-3 w-3" />}
                              </button>
                            )
                          })}
                        </div>
                      </div>
                    )}

                    {canCreateImportTag && (
                      <div className="rounded-lg border border-dashed border-emerald-200 bg-emerald-50/60 p-3">
                        <div className="flex items-center justify-between gap-3">
                          <div className="min-w-0">
                            <p className="truncate text-xs font-medium text-emerald-800">Crear etiqueta "{tagSearch.trim()}"</p>
                            <p className="text-[11px] text-emerald-600">Se creará antes de analizar el Excel.</p>
                          </div>
                          <button
                            type="button"
                            onClick={handleCreateImportTag}
                            disabled={creatingTag}
                            className="inline-flex shrink-0 items-center gap-1.5 rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-700 disabled:opacity-50"
                          >
                            {creatingTag ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                            Crear
                          </button>
                        </div>
                        <div className="mt-2 flex flex-wrap gap-1.5">
                          {TAG_PRESET_COLORS.map(color => (
                            <button
                              key={color}
                              type="button"
                              onClick={() => setNewTagColor(color)}
                              className={`h-6 w-6 rounded-full border-2 ${newTagColor === color ? 'border-slate-800' : 'border-white'}`}
                              style={{ backgroundColor: color }}
                              aria-label={`Color ${color}`}
                            />
                          ))}
                        </div>
                      </div>
                    )}

                    {needsImportTagSelection && (
                      <p className="text-xs text-amber-600">Selecciona una etiqueta existente o crea una nueva. También puedes volver a "Sin etiqueta".</p>
                    )}
                  </div>
                )}
                <p className="mt-2 text-xs text-slate-500">
                  Se aplicará sólo a leads nuevos. En existentes, sólo pueden sincronizarse estado Kommo y fecha dentro de 24h.
                </p>
              </div>
            )}

            <div className="flex gap-3 mt-5">
              <button onClick={handleClose} className="flex-1 px-4 py-2.5 border border-gray-200 text-gray-600 rounded-xl hover:bg-gray-50 font-medium text-sm transition">
                Cancelar
              </button>
              <button onClick={handlePreview} disabled={!canPreview} className="flex-1 px-4 py-2.5 bg-green-600 text-white rounded-xl hover:bg-green-700 disabled:opacity-50 font-medium text-sm transition">
                {previewing ? (
                  <span className="flex items-center justify-center gap-2"><Loader2 className="w-4 h-4 animate-spin" />Analizando...</span>
                ) : (
                  <span className="flex items-center justify-center gap-2"><Search className="w-4 h-4" />Analizar Excel</span>
                )}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
