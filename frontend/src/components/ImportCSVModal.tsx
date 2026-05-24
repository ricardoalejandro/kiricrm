'use client'

import { useState, useRef, useEffect } from 'react'
import { Upload, FileText, X, AlertTriangle, CheckCircle2, Download, Loader2, ShieldCheck, Search } from 'lucide-react'

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
  rows?: ImportPreviewRow[]
  errors: string[]
}

export default function ImportCSVModal({ open, onClose, onSuccess, defaultType = 'leads' }: ImportCSVModalProps) {
  const [file, setFile] = useState<File | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [preview, setPreview] = useState<ImportSummary | null>(null)
  const [result, setResult] = useState<ImportSummary | null>(null)
  const [error, setError] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', h)
    return () => document.removeEventListener('keydown', h)
  }, [open, onClose])

  if (!open) return null

  const title = defaultType === 'contacts' ? 'Importar Contactos' : defaultType === 'both' ? 'Importar Leads y Contactos' : 'Importar Leads'

  const buildFormData = () => {
    if (!file) return null
    const formData = new FormData()
    formData.append('file', file)
    formData.append('import_type', defaultType)
    return formData
  }

  const handleFileChange = (nextFile: File | null) => {
    setFile(nextFile)
    setPreview(null)
    setResult(null)
    setError('')
  }

  const handlePreview = async () => {
    const formData = buildFormData()
    if (!formData) return
    setPreviewing(true)
    setError('')
    setPreview(null)
    setResult(null)

    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/import/csv/preview', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
        body: formData,
      })
      const data = await res.json()
      if (data.success) {
        setPreview(data.preview)
      } else {
        setError(data.error || 'No se pudo analizar el CSV')
      }
    } catch {
      setError('Error de conexión')
    } finally {
      setPreviewing(false)
    }
  }

  const handleUpload = async () => {
    const formData = buildFormData()
    if (!formData) return
    setUploading(true)
    setError('')

    const token = localStorage.getItem('token')
    try {
      const res = await fetch('/api/import/csv', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
        body: formData,
      })
      const data = await res.json()
      if (data.success) {
        setResult(data.summary)
        setPreview(null)
        onSuccess()
      } else {
        setError(data.error || 'Error desconocido')
      }
    } catch {
      setError('Error de conexión')
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

  return (
    <div className="fixed inset-0 bg-black/40 backdrop-blur-sm flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl shadow-2xl p-6 w-full max-w-2xl border border-gray-100 max-h-[90vh] overflow-y-auto">
        <div className="flex items-start justify-between mb-5">
          <div>
            <h2 className="text-lg font-semibold text-gray-900">{title}</h2>
            <p className="text-sm text-gray-500 mt-0.5">Modo seguro para cargas recurrentes desde CSV o Kommo</p>
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
              </div>
            </div>
            <SummaryStats data={result} final />
            {result.errors.length > 0 && (
              <div className="bg-amber-50 border border-amber-100 rounded-xl p-3 max-h-32 overflow-y-auto">
                <p className="text-xs font-medium text-amber-700 mb-1">Errores ({result.errors.length}):</p>
                {result.errors.slice(0, 10).map((item, i) => (
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
                <p className="text-emerald-700">Los leads existentes no se moverán de etapa ni perderán etiquetas, notas, tareas u observaciones.</p>
                {preview.incoming_destination && (
                  <p className="text-emerald-700 mt-1">Los nuevos leads irán a: <span className="font-medium">{preview.incoming_destination}</span>.</p>
                )}
              </div>
            </div>

            <SummaryStats data={preview} />

            <div className="rounded-xl border border-slate-200 overflow-hidden">
              <div className="px-3 py-2 bg-slate-50 border-b border-slate-200 flex items-center justify-between">
                <p className="text-xs font-semibold text-slate-500 uppercase">Vista previa</p>
                <p className="text-xs text-slate-400">{preview.total_rows} filas detectadas</p>
              </div>
              <div className="max-h-56 overflow-y-auto divide-y divide-slate-100">
                {(preview.rows || []).map((row) => (
                  <div key={`${row.row}-${row.action}-${row.phone}`} className="px-3 py-2 flex items-start gap-3">
                    <span className="text-xs text-slate-400 w-10 shrink-0">#{row.row}</span>
                    <span className={`text-[11px] px-2 py-0.5 rounded-full border shrink-0 ${actionClass(row.action)}`}>{actionLabel(row.action)}</span>
                    <div className="min-w-0 flex-1">
                      <p className="text-sm text-slate-800 truncate">{row.name || row.phone || 'Sin nombre'}</p>
                      <p className="text-xs text-slate-500 truncate">{row.reason}</p>
                    </div>
                    {row.kommo_id && <span className="text-[11px] text-slate-400 shrink-0">Kommo {row.kommo_id}</span>}
                  </div>
                ))}
              </div>
            </div>

            {preview.errors.length > 0 && (
              <div className="bg-amber-50 border border-amber-100 rounded-xl p-3 max-h-32 overflow-y-auto">
                <p className="text-xs font-medium text-amber-700 mb-1">Advertencias ({preview.errors.length}):</p>
                {preview.errors.slice(0, 10).map((item, i) => (
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
                accept=".csv,text/csv"
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
                    <p className="text-xs text-gray-400 mt-1">CSV exportado desde Kommo o plantilla de Clarín</p>
                  </>
                )}
              </button>
            </div>

            <div className="bg-gray-50 rounded-xl p-3.5 text-xs text-gray-600 space-y-1">
              <div className="flex items-center justify-between mb-1">
                <p className="font-medium text-gray-700">Columnas reconocidas:</p>
                <button
                  type="button"
                  onClick={() => {
                    const csv = 'telefono,nombre,apellido,email,empresa,notas,dni,fecha_nacimiento,tags\n987654321,Juan,Pérez,juan@ejemplo.com,Empresa SA,Nota de ejemplo,12345678,1990-05-15,"cliente, vip"'
                    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
                    const url = URL.createObjectURL(blob)
                    const a = document.createElement('a')
                    a.href = url
                    a.download = 'plantilla_contactos.csv'
                    a.click()
                    URL.revokeObjectURL(url)
                  }}
                  className="flex items-center gap-1 text-green-600 hover:text-green-700 font-medium transition-colors"
                >
                  <Download className="w-3 h-3" />
                  Descargar plantilla
                </button>
              </div>
              <p><span className="text-green-600 font-medium">Requerida:</span> phone / telefono / celular, o columna telefónica de Kommo</p>
              <p><span className="text-gray-500 font-medium">Kommo:</span> ID, Nombre completo, Correo, Etiquetas, Estatus del lead, Embudo de ventas</p>
              <p><span className="text-gray-500 font-medium">Seguro:</span> si reimportas, no se sobrescribe el trabajo hecho en Clarín</p>
            </div>

            <div className="flex gap-3 mt-5">
              <button onClick={handleClose} className="flex-1 px-4 py-2.5 border border-gray-200 text-gray-600 rounded-xl hover:bg-gray-50 font-medium text-sm transition">
                Cancelar
              </button>
              <button onClick={handlePreview} disabled={!file || previewing} className="flex-1 px-4 py-2.5 bg-green-600 text-white rounded-xl hover:bg-green-700 disabled:opacity-50 font-medium text-sm transition">
                {previewing ? (
                  <span className="flex items-center justify-center gap-2"><Loader2 className="w-4 h-4 animate-spin" />Analizando...</span>
                ) : (
                  <span className="flex items-center justify-center gap-2"><Search className="w-4 h-4" />Analizar CSV</span>
                )}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
