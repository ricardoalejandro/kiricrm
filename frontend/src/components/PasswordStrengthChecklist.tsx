'use client'

import { CheckCircle2, Circle } from 'lucide-react'

export interface PasswordCheck {
  key: string
  label: string
  passed: boolean
}

export function getPasswordChecks(password: string, confirmPassword?: string): PasswordCheck[] {
  const checks: PasswordCheck[] = [
    { key: 'length', label: '10 caracteres como mínimo', passed: password.length >= 10 },
    { key: 'upper', label: 'Una letra mayúscula', passed: /[A-Z]/.test(password) },
    { key: 'lower', label: 'Una letra minúscula', passed: /[a-z]/.test(password) },
    { key: 'number', label: 'Un número', passed: /\d/.test(password) },
    { key: 'symbol', label: 'Un símbolo', passed: /[^A-Za-z0-9]/.test(password) },
  ]
  if (confirmPassword !== undefined) {
    checks.push({
      key: 'match',
      label: 'Las contraseñas coinciden',
      passed: password.length > 0 && password === confirmPassword,
    })
  }
  return checks
}

export function getPasswordIssues(password: string, confirmPassword?: string) {
  return getPasswordChecks(password, confirmPassword)
    .filter(check => !check.passed)
    .map(check => check.label.toLowerCase())
}

export function isStrongPassword(password: string) {
  return getPasswordChecks(password).every(check => check.passed)
}

export default function PasswordStrengthChecklist({
  password,
  confirmPassword,
  compact = false,
}: {
  password: string
  confirmPassword?: string
  compact?: boolean
}) {
  const checks = getPasswordChecks(password, confirmPassword)
  const passedCount = checks.filter(check => check.passed).length
  const complete = passedCount === checks.length
  const progress = Math.round((passedCount / checks.length) * 100)

  return (
    <div className={`rounded-lg border ${complete ? 'border-emerald-200 bg-emerald-50/70' : 'border-slate-200 bg-slate-50'} ${compact ? 'p-3' : 'p-4'}`}>
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wider text-slate-600">Seguridad de contraseña</p>
          <p className={`mt-0.5 text-xs ${complete ? 'text-emerald-700' : 'text-slate-500'}`}>
            {complete ? 'Lista para usar.' : `${passedCount} de ${checks.length} condiciones cumplidas.`}
          </p>
        </div>
        <span className={`rounded-full px-2.5 py-1 text-xs font-semibold ${complete ? 'bg-emerald-100 text-emerald-700' : 'bg-white text-slate-500 border border-slate-200'}`}>
          {complete ? 'Fuerte' : `${progress}%`}
        </span>
      </div>
      <div className="mt-3 h-1.5 rounded-full bg-white border border-slate-100 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-300 ${complete ? 'bg-emerald-500' : 'bg-amber-400'}`}
          style={{ width: `${progress}%` }}
        />
      </div>
      <div className={`mt-3 grid ${compact ? 'gap-1.5' : 'sm:grid-cols-2 gap-2'}`}>
        {checks.map(check => (
          <div key={check.key} className={`flex items-center gap-2 text-xs ${check.passed ? 'text-emerald-700' : 'text-slate-500'}`}>
            {check.passed ? <CheckCircle2 className="w-4 h-4 shrink-0" /> : <Circle className="w-4 h-4 shrink-0 text-slate-300" />}
            <span>{check.label}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
