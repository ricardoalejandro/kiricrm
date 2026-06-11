---
name: frontend-development
description: Create polished, consistent, and highly usable Next.js/React/TypeScript interfaces for Clarin CRM. Use when creating or modifying dashboard pages, components, API calls, WebSocket listeners, or UI styling. Enforces consistency with the current screen, micro-interactions, accessibility, and safe API usage.
---

# Frontend Development — Clarin CRM

## Philosophy: Beauty + Usability

Every UI must feel polished, clear and operational. Match the current screen first;
do not force an unrelated color mode or a new design language into an existing light dashboard.

## Stack
- Next.js 14.2 (App Router), React 18.3, TypeScript 5.4 (strict mode)
- Tailwind CSS 3.4, lucide-react icons, date-fns, zustand (global state), xlsx
- Build via `docker compose build frontend`

## Project Layout

```
frontend/src/
  app/
    layout.tsx                    → Root layout
    page.tsx                      → Login page
    globals.css                   → Global styles + custom scrollbar classes
    dashboard/
      layout.tsx                  → Dashboard layout with sidebar navigation
      page.tsx                    → Dashboard home
      chats/page.tsx              → Chat management (WhatsApp)
      devices/page.tsx            → WhatsApp device management
      leads/page.tsx              → Leads kanban board
      settings/page.tsx           → Account settings
  components/
    CreateCampaignModal.tsx       → Campaign creation modal
    ImportCSVModal.tsx            → Excel import modal for Kommo/Clarín data
    NotificationProvider.tsx      → Toast notifications
    TagInput.tsx                  → Tag input component
    WhatsAppTextInput.tsx         → WhatsApp text formatting input
    chat/                         → Chat-related components
  lib/
    api.ts                        → API client + WebSocket factory
    utils.ts                      → Shared utilities
```

---

## Design System — Match Existing UI

The current dashboard is mostly light: white/slate surfaces, subtle borders,
rounded panels and emerald accents. Before editing, inspect the target page and
reuse its local classes, spacing, density, button shapes and icon style.

Use emerald for primary actions, slate/gray for neutral UI, red for destructive
actions and amber for caution. Do not introduce a one-off palette, oversized
marketing composition or dark-only pattern unless the existing page already uses it.

---

## Visual Excellence Rules

### 1. Transitions & Micro-interactions — ALWAYS
```tsx
// EVERY interactive element needs transitions
<button className="... transition-all duration-200 hover:scale-[1.02] active:scale-[0.98]">

// Smooth hover on cards
<div className="... transition-all duration-200 hover:border-slate-600 hover:shadow-lg">

// Fade-in loading states
<div className={`transition-opacity duration-300 ${loading ? 'opacity-50' : 'opacity-100'}`}>
```

### 2. Loading States — NEVER leave blank screens
```tsx
// Skeleton loading — not spinners for content areas
{loading ? (
  <div className="space-y-3">
    {[...Array(5)].map((_, i) => (
      <div key={i} className="h-16 bg-slate-700/50 rounded-lg animate-pulse" />
    ))}
  </div>
) : (
  <ActualContent />
)}

// Use spinners only for actions (saving, submitting)
{saving && <Loader2 className="w-4 h-4 animate-spin" />}
```

### 3. Empty States — ALWAYS beautiful and helpful
```tsx
// Never show just "No data" — make it inviting
<div className="flex flex-col items-center justify-center py-16 text-center">
  <div className="w-16 h-16 bg-slate-800 rounded-2xl flex items-center justify-center mb-4">
    <Users className="w-8 h-8 text-slate-500" />
  </div>
  <h3 className="text-lg font-semibold text-slate-300 mb-1">No hay leads todavía</h3>
  <p className="text-sm text-slate-500 mb-6 max-w-sm">
    Los leads aparecerán aquí cuando se sincronicen desde Kommo o se creen manualmente.
  </p>
  <button className="bg-emerald-600 hover:bg-emerald-700 text-white px-4 py-2 rounded-lg text-sm font-medium transition-all duration-200 shadow-lg shadow-emerald-600/20">
    <Plus className="w-4 h-4 mr-2 inline" />
    Crear lead
  </button>
</div>
```

### 4. Spacing & Typography Hierarchy
```
Page title:      text-2xl font-bold text-white tracking-tight
Section title:   text-lg font-semibold text-slate-100
Card title:      text-base font-medium text-white
Body text:       text-sm text-slate-300
Caption/label:   text-xs font-medium text-slate-400 uppercase tracking-wider
Numbers/stats:   text-3xl font-bold tracking-tight tabular-nums

Page padding:    p-6 (desktop), p-4 (mobile)
Card padding:    p-5 or p-6
Section gaps:    space-y-6
Element gaps:    space-y-3 or gap-3
```

### 5. Icons — ALWAYS use lucide-react
```tsx
import { Search, Plus, X, ChevronDown, MoreHorizontal, Settings, Loader2 } from "lucide-react"

// Icons in buttons: w-4 h-4
<button><Plus className="w-4 h-4 mr-2" /> Crear</button>

// Icons standalone: w-5 h-5
<Search className="w-5 h-5 text-slate-400" />

// Icons in empty states: w-8 h-8
<Users className="w-8 h-8 text-slate-500" />
```

### 6. Responsive & Overflow
```tsx
// Tables and lists: ALWAYS handle overflow text
<span className="truncate max-w-[200px]">{name}</span>

// Long content: scroll containers
<div className="overflow-y-auto max-h-[calc(100vh-200px)] scrollbar-thin scrollbar-thumb-slate-700">

// Grid layouts: responsive
<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
```

### 7. Modals — Polished with backdrop
```tsx
// Overlay
<div className="fixed inset-0 bg-black/60 backdrop-blur-sm z-50 flex items-center justify-center p-4">
  // Modal
  <div className="bg-slate-800 border border-slate-700/50 rounded-2xl shadow-2xl w-full max-w-lg max-h-[90vh] overflow-y-auto">
    // Header
    <div className="flex items-center justify-between p-6 border-b border-slate-700/50">
      <h2 className="text-lg font-semibold text-white">Título</h2>
      <button className="text-slate-400 hover:text-white transition-colors p-1 rounded-lg hover:bg-slate-700">
        <X className="w-5 h-5" />
      </button>
    </div>
    // Body
    <div className="p-6">...</div>
    // Footer
    <div className="flex justify-end gap-3 p-6 border-t border-slate-700/50">
      <button className="px-4 py-2 text-sm text-slate-300 hover:text-white transition-colors">Cancelar</button>
      <button className="px-4 py-2 text-sm bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg font-medium transition-all shadow-lg shadow-emerald-600/20">Guardar</button>
    </div>
  </div>
</div>
```

### 8. Tables — Clean and scannable
```tsx
<table className="w-full">
  <thead>
    <tr className="border-b border-slate-700/50">
      <th className="text-left text-xs font-medium text-slate-400 uppercase tracking-wider py-3 px-4">Nombre</th>
    </tr>
  </thead>
  <tbody className="divide-y divide-slate-700/30">
    <tr className="hover:bg-slate-700/30 transition-colors cursor-pointer">
      <td className="py-3 px-4 text-sm text-slate-200">...</td>
    </tr>
  </tbody>
</table>
```

### 9. Form Inputs — Consistent and accessible
```tsx
<label className="block text-xs font-medium text-slate-400 uppercase tracking-wider mb-1.5">
  Nombre
</label>
<input
  type="text"
  className="w-full bg-slate-800/50 border border-slate-600/50 text-white rounded-lg px-3 py-2.5 text-sm placeholder-slate-500 focus:ring-2 focus:ring-emerald-500/40 focus:border-emerald-500 outline-none transition-all"
  placeholder="Ingresa el nombre..."
/>
// Error state
<p className="text-xs text-red-400 mt-1">Este campo es requerido</p>
```

### 10. Notifications & Toasts
```tsx
// Success: emerald accent
<div className="bg-emerald-500/10 border border-emerald-500/20 text-emerald-300 px-4 py-3 rounded-lg text-sm">
  <CheckCircle className="w-4 h-4 inline mr-2" /> Guardado correctamente
</div>

// Error: red accent
<div className="bg-red-500/10 border border-red-500/20 text-red-300 px-4 py-3 rounded-lg text-sm">
  <AlertCircle className="w-4 h-4 inline mr-2" /> Error al guardar
</div>
```

---

## Usability Rules (UX)

1. **Click targets**: Minimum 40px height for buttons and interactive elements. 44px on mobile.
2. **Keyboard navigation**: All interactive elements must be focusable. Use `tabIndex`, `onKeyDown` for custom widgets.
3. **Feedback immediato**: Every click must produce visible feedback within 100ms (hover state, loading indicator, optimistic update).
4. **No dead-ends**: Every empty state has a CTA. Every error has a retry option.
5. **Confirmación destructiva**: Delete actions always show a confirmation dialog with clear consequences.
6. **Search/filter first**: Lists with >10 items must have search. Lists with categories must have filters.
7. **Truncation**: Long text truncates with `...` and shows full text on hover (title attribute or tooltip).
8. **Consistent actions**: Primary action always on the right. Cancel always on the left.
9. **Mobile-ready**: All layouts must work on tablet/mobile. Use responsive grid and flex.
10. **Disable during save**: Buttons disable and show spinner while async operations are in progress.

---

## Code Conventions

### Component Structure
```tsx
"use client"

import { useState, useEffect, useCallback } from "react"
import { Search, Plus, Loader2 } from "lucide-react"
import { apiGet, createWebSocket } from "@/lib/api"

interface Lead {
  id: number
  name: string
  phone: string
}

export default function LeadsPage() {
  const [leads, setLeads] = useState<Lead[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState("")

  const fetchLeads = useCallback(async () => {
    try {
      const data = await apiGet("/api/leads")
      setLeads(data)
    } catch (err) {
      console.error("Error fetching leads:", err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchLeads() }, [fetchLeads])

  // WebSocket for real-time
  useEffect(() => {
    const ws = createWebSocket((msg) => {
      const data = JSON.parse(msg.data)
      if (data.type === "lead_update") fetchLeads()
    })
    return () => ws?.close()
  }, [fetchLeads])

  const filtered = leads.filter(l =>
    l.name.toLowerCase().includes(search.toLowerCase())
  )

  return (
    <div className="p-6 bg-slate-900 min-h-screen">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-white tracking-tight">Leads</h1>
          <p className="text-sm text-slate-400 mt-1">{leads.length} leads en total</p>
        </div>
        <button className="bg-emerald-600 hover:bg-emerald-700 text-white px-4 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 shadow-lg shadow-emerald-600/20 flex items-center gap-2">
          <Plus className="w-4 h-4" /> Nuevo lead
        </button>
      </div>

      {/* Search */}
      <div className="relative mb-4">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500" />
        <input
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="w-full bg-slate-800/50 border border-slate-700/50 text-white rounded-lg pl-10 pr-4 py-2.5 text-sm placeholder-slate-500 focus:ring-2 focus:ring-emerald-500/40 focus:border-emerald-500 outline-none transition-all"
          placeholder="Buscar leads..."
        />
      </div>

      {/* Content with loading/empty states */}
      {loading ? (
        <div className="space-y-3">
          {[...Array(5)].map((_, i) => (
            <div key={i} className="h-16 bg-slate-800/50 rounded-lg animate-pulse" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState />
      ) : (
        <LeadsList leads={filtered} />
      )}
    </div>
  )
}
```

### API & WebSocket — use @/lib/api.ts
```tsx
import { apiGet, apiPost, apiPut, apiDelete, createWebSocket } from "@/lib/api"
```

### Rules
- All components are functional with hooks. No class components.
- Use `@/` alias for all imports from `src/`.
- Define TypeScript interfaces for all props and API responses.
- Tailwind only — no CSS modules, no styled-components.
- API routes proxy through Next.js rewrites: `/api/:path*` → backend:8080.
- `useCallback` for functions passed as deps or to children.
- Skeleton loaders for content areas. Spinners for actions only.
- Every list has search if >10 items.

## Adding a New Dashboard Page

1. Create `src/app/dashboard/new-page/page.tsx`
2. Add navigation link in `src/app/dashboard/layout.tsx` sidebar
3. Follow the component structure above with `"use client"`, emerald/slate palette
4. Include: page header, search bar, loading skeletons, empty state, content
5. Build and verify: `docker compose build frontend && docker compose up -d`
