'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import Script from 'next/script'
import { ArrowRight, Eye, EyeOff, Lock, MessageSquare, User } from 'lucide-react'
import { markAuthSession } from '@/lib/api'

type TurnstileWidgetID = string | number

declare global {
  interface Window {
    turnstile?: {
      render: (
        container: string | HTMLElement,
        options: {
          sitekey: string
          theme?: 'light' | 'dark' | 'auto'
          callback?: (token: string) => void
          'expired-callback'?: () => void
          'error-callback'?: () => void
        }
      ) => TurnstileWidgetID
      reset: (widgetId?: TurnstileWidgetID) => void
      remove: (widgetId: TurnstileWidgetID) => void
    }
  }
}

export default function LoginScreen() {
  const router = useRouter()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('')
  const [turnstileRequired, setTurnstileRequired] = useState(false)
  const [loginEnabled, setLoginEnabled] = useState(true)
  const [turnstileReady, setTurnstileReady] = useState(false)
  const [turnstileToken, setTurnstileToken] = useState('')
  const turnstileRef = useRef<HTMLDivElement | null>(null)
  const widgetIdRef = useRef<TurnstileWidgetID | null>(null)

  const renderTurnstile = useCallback(() => {
    const turnstile = window.turnstile
    if (!turnstileSiteKey || !turnstileReady || !turnstile || !turnstileRef.current || widgetIdRef.current !== null) return
    widgetIdRef.current = turnstile.render(turnstileRef.current, {
      sitekey: turnstileSiteKey,
      theme: 'light',
      callback: (token: string) => {
        setTurnstileToken(token)
        setError('')
      },
      'expired-callback': () => setTurnstileToken(''),
      'error-callback': () => {
        setTurnstileToken('')
        setError('No se pudo completar la verificación de seguridad. Intenta nuevamente.')
      },
    })
  }, [turnstileReady, turnstileSiteKey])

  const resetTurnstile = useCallback(() => {
    setTurnstileToken('')
    if (window.turnstile && widgetIdRef.current !== null) {
      window.turnstile.reset(widgetIdRef.current)
    }
  }, [])

  useEffect(() => {
    fetch('/api/public/security-config')
      .then((r) => r.json())
      .then((data) => {
        if (!data.success) return
        setTurnstileSiteKey(typeof data.turnstile_site_key === 'string' ? data.turnstile_site_key : '')
        setTurnstileRequired(Boolean(data.login_turnstile_required))
        setLoginEnabled(Boolean(data.login_enabled ?? true))
      })
      .catch(() => {
        setTurnstileRequired(false)
        setLoginEnabled(true)
      })
  }, [])

  useEffect(() => {
    if (window.turnstile) setTurnstileReady(true)
  }, [turnstileSiteKey])

  useEffect(() => {
    renderTurnstile()
    return () => {
      if (window.turnstile && widgetIdRef.current !== null) {
        window.turnstile.remove(widgetIdRef.current)
        widgetIdRef.current = null
      }
    }
  }, [renderTurnstile])

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!loginEnabled) {
      setError('Inicio de sesión temporalmente no disponible.')
      return
    }
    if (turnstileRequired && !turnstileToken) {
      setError('Completa la verificación de seguridad para iniciar sesión.')
      return
    }
    setLoading(true)
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password, turnstile_token: turnstileToken }),
        credentials: 'include',
      })
      const data = await res.json()
      if (!data.success) {
        setError(data.error || 'Error al iniciar sesión')
        resetTurnstile()
        return
      }
      markAuthSession()
      router.push('/dashboard')
      router.refresh()
    } catch {
      setError('Error de conexión')
      resetTurnstile()
    } finally {
      setLoading(false)
    }
  }

  return (
    <main className="min-h-screen bg-slate-50 flex items-center justify-center px-4 py-10">
      {turnstileSiteKey && (
        <Script
          src="https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit"
          strategy="afterInteractive"
          onLoad={() => setTurnstileReady(true)}
        />
      )}
      <section className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center text-center">
          <div className="w-12 h-12 bg-emerald-600 rounded-xl flex items-center justify-center shadow-sm">
            <MessageSquare className="w-6 h-6 text-white" />
          </div>
          <h1 className="mt-4 text-2xl font-bold text-slate-900">Clarin</h1>
          <p className="mt-1 text-sm text-slate-500">Ingresa a tu dashboard</p>
        </div>

        <div className="bg-white border border-slate-200 rounded-xl shadow-sm p-6">
          {error && (
            <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded-lg text-sm mb-5">
              {error}
            </div>
          )}

          <form onSubmit={handleLogin} className="space-y-5">
            <div>
              <label className="block text-xs font-medium text-slate-500 uppercase tracking-wider mb-1.5">
                Usuario
              </label>
              <div className="relative">
                <User className="absolute left-3.5 top-1/2 -translate-y-1/2 w-[18px] h-[18px] text-slate-400" />
                <input
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="usuario o correo"
                  className="w-full pl-11 pr-4 py-3 bg-white border border-slate-300 text-slate-900 placeholder:text-slate-400 rounded-lg focus:ring-2 focus:ring-emerald-500/30 focus:border-emerald-500 outline-none transition-all text-sm"
                  required
                  disabled={loading}
                />
              </div>
            </div>

            <div>
              <label className="block text-xs font-medium text-slate-500 uppercase tracking-wider mb-1.5">
                Contraseña
              </label>
              <div className="relative">
                <Lock className="absolute left-3.5 top-1/2 -translate-y-1/2 w-[18px] h-[18px] text-slate-400" />
                <input
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="tu contraseña"
                  className="w-full pl-11 pr-11 py-3 bg-white border border-slate-300 text-slate-900 placeholder:text-slate-400 rounded-lg focus:ring-2 focus:ring-emerald-500/30 focus:border-emerald-500 outline-none transition-all text-sm"
                  required
                  disabled={loading}
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((v) => !v)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 transition-colors"
                  aria-label={showPassword ? 'Ocultar contraseña' : 'Mostrar contraseña'}
                  disabled={loading}
                >
                  {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
            </div>

            {turnstileRequired && (
              <div className="min-h-[70px] flex items-center justify-center">
                {turnstileSiteKey && loginEnabled ? (
                  <div ref={turnstileRef} />
                ) : (
                  <div className="w-full rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
                    Estamos activando el inicio de sesión seguro.
                  </div>
                )}
              </div>
            )}

            <button
              type="submit"
              className="w-full bg-emerald-600 hover:bg-emerald-700 text-white py-3 rounded-lg font-semibold transition-colors disabled:opacity-50 flex items-center justify-center gap-2 shadow-sm"
              disabled={loading || !loginEnabled || (turnstileRequired && !turnstileSiteKey)}
            >
              {loading ? (
                <span className="animate-spin rounded-full h-5 w-5 border-2 border-white/30 border-t-white" />
              ) : (
                <>
                  Iniciar sesión <ArrowRight className="w-4 h-4" />
                </>
              )}
            </button>
          </form>
        </div>
      </section>
    </main>
  )
}
