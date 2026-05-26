'use client'

import { useEffect, useMemo, useState } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { ArrowRight, Check, Eye, EyeOff, Lock, Sparkles } from 'lucide-react'
import { markAuthActivity } from '@/lib/api'

interface PublicPlan {
  code: string
  name: string
  description: string
  trial_days: number
  is_public: boolean
  sort_order: number
}

const fallbackPlans: PublicPlan[] = [
  { code: 'starter', name: 'Starter', description: 'Para equipos pequeños que empiezan con WhatsApp CRM.', trial_days: 14, is_public: true, sort_order: 30 },
  { code: 'pro', name: 'Pro', description: 'Para equipos comerciales con más volumen y campañas.', trial_days: 14, is_public: true, sort_order: 40 },
  { code: 'business', name: 'Business', description: 'Para operaciones con automatizaciones y más capacidad.', trial_days: 14, is_public: true, sort_order: 50 },
]

const planDetails: Record<string, { price: string; badge?: string; features: string[] }> = {
  starter: { price: 'S/ 149', features: ['3 dispositivos WhatsApp', '5 usuarios', '10 mil contactos', 'Kommo y Google Contacts'] },
  pro: { price: 'S/ 299', badge: 'Más elegido', features: ['8 dispositivos WhatsApp', '12 usuarios', '50 mil contactos', 'Campañas de difusión'] },
  business: { price: 'S/ 599', features: ['20 dispositivos WhatsApp', '30 usuarios', '150 mil contactos', 'Automatizaciones avanzadas'] },
}

export default function SignupPage() {
  const router = useRouter()
  const [plans, setPlans] = useState<PublicPlan[]>([])
  const [selectedPlan, setSelectedPlan] = useState('pro')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [form, setForm] = useState({ account_name: '', display_name: '', email: '', password: '' })

  const visiblePlans = useMemo(() => {
    const commercial = plans.filter((p) => ['starter', 'pro', 'business'].includes(p.code))
    return commercial.length > 0 ? commercial : fallbackPlans
  }, [plans])

  useEffect(() => {
    fetch('/api/public/plans')
      .then((r) => r.json())
      .then((data) => {
        if (data.success && Array.isArray(data.plans)) setPlans(data.plans)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    if (!visiblePlans.some((p) => p.code === selectedPlan)) {
      setSelectedPlan(visiblePlans[0]?.code || 'starter')
    }
  }, [selectedPlan, visiblePlans])

  const handleSignup = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...form, plan_code: selectedPlan }),
        credentials: 'include',
      })
      const data = await res.json()
      if (!data.success) {
        setError(data.error || 'No se pudo crear la cuenta')
        return
      }
      if (data.token) {
        localStorage.setItem('token', data.token)
        markAuthActivity(true)
        router.push('/dashboard')
        router.refresh()
        return
      }
      router.push('/login')
    } catch {
      setError('Error de conexión')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-white flex flex-col">
      <div className="flex-1 flex items-start justify-center px-4 py-12">
        <div className="w-full max-w-lg">
          <div className="bg-white border border-slate-200 rounded-2xl shadow-sm overflow-hidden">
            <div className="p-8">
              <div className="mb-6">
                <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full border border-emerald-200 bg-emerald-50 text-emerald-700 text-xs font-medium mb-4">
                  <Sparkles className="w-3.5 h-3.5" />
                  14 días de prueba gratis
                </div>
                <h1 className="text-2xl font-bold text-slate-900">Crea tu cuenta SaaS</h1>
                <p className="text-sm text-slate-500 mt-1">Prueba gratis y configura tu operación en minutos.</p>
              </div>

              {error && (
                <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-xl text-sm mb-5">
                  {error}
                </div>
              )}

              <form onSubmit={handleSignup} className="space-y-4">
                <div className="grid sm:grid-cols-2 gap-3">
                  <Field label="Empresa" value={form.account_name} onChange={(v) => setForm((f) => ({ ...f, account_name: v }))} placeholder="Mi empresa" />
                  <Field label="Tu nombre" value={form.display_name} onChange={(v) => setForm((f) => ({ ...f, display_name: v }))} placeholder="Nombre completo" />
                </div>
                <Field label="Correo" type="email" value={form.email} onChange={(v) => setForm((f) => ({ ...f, email: v }))} placeholder="ventas@empresa.com" />

                <div>
                  <label className="block text-xs font-medium text-slate-500 uppercase tracking-wider mb-1.5">Contraseña</label>
                  <div className="relative">
                    <Lock className="absolute left-3.5 top-1/2 -translate-y-1/2 w-[18px] h-[18px] text-slate-400" />
                    <input
                      type={showPassword ? 'text' : 'password'}
                      value={form.password}
                      onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
                      placeholder="mínimo 8 caracteres"
                      className="w-full pl-11 pr-11 py-3 bg-white border border-slate-300 text-slate-900 placeholder:text-slate-400 rounded-xl focus:ring-2 focus:ring-emerald-500/30 focus:border-emerald-500 outline-none transition-all text-sm"
                      minLength={8}
                      required
                      disabled={loading}
                    />
                    <button type="button" onClick={() => setShowPassword((v) => !v)} className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 transition-colors">
                      {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                    </button>
                  </div>
                </div>

                <div className="space-y-2 pt-1">
                  <label className="block text-xs font-medium text-slate-500 uppercase tracking-wider">Plan de prueba</label>
                  <div className="grid gap-2">
                    {visiblePlans.map((plan) => {
                      const details = planDetails[plan.code] || { price: 'A medida', features: ['Configuración personalizada'] }
                      const active = selectedPlan === plan.code
                      return (
                        <button
                          key={plan.code}
                          type="button"
                          onClick={() => setSelectedPlan(plan.code)}
                          className={`text-left border rounded-xl p-3 transition-all duration-200 ${
                            active ? 'border-emerald-500 bg-emerald-50/50 ring-1 ring-emerald-500/20' : 'border-slate-200 bg-white hover:border-slate-300'
                          }`}
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div>
                              <div className="flex items-center gap-2">
                                <span className="font-semibold text-slate-900">{plan.name}</span>
                                {details.badge && (
                                  <span className="text-[10px] px-2 py-0.5 rounded-full bg-emerald-100 text-emerald-700 font-medium">{details.badge}</span>
                                )}
                              </div>
                              <p className="text-xs text-slate-500 mt-1">{plan.description}</p>
                            </div>
                            <div className="text-right shrink-0">
                              <p className="text-sm font-bold text-slate-900">{details.price}</p>
                              <p className="text-[11px] text-slate-400">mensual</p>
                            </div>
                          </div>
                        </button>
                      )
                    })}
                  </div>
                </div>

                <button
                  type="submit"
                  className="w-full bg-emerald-600 hover:bg-emerald-700 text-white py-3 rounded-xl font-bold transition-colors disabled:opacity-50 flex items-center justify-center gap-2 shadow-sm"
                  disabled={loading}
                >
                  {loading ? (
                    <div className="animate-spin rounded-full h-5 w-5 border-2 border-white/30 border-t-white" />
                  ) : (
                    <>
                      Crear cuenta y empezar <ArrowRight className="w-4 h-4" />
                    </>
                  )}
                </button>
              </form>

              <div className="mt-6 text-center">
                <p className="text-sm text-slate-500">
                  ¿Ya tienes cuenta?{' '}
                  <Link href="/login" className="text-emerald-600 hover:text-emerald-700 font-semibold">
                    Inicia sesión
                  </Link>
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function Field({ label, value, onChange, placeholder, type = 'text' }: { label: string; value: string; onChange: (value: string) => void; placeholder: string; type?: string }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-500 uppercase tracking-wider mb-1.5">{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full px-3.5 py-3 bg-white border border-slate-300 text-slate-900 placeholder:text-slate-400 rounded-xl focus:ring-2 focus:ring-emerald-500/30 focus:border-emerald-500 outline-none transition-all text-sm"
        required
      />
    </div>
  )
}
