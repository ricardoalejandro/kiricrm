# Copilot Instructions — Clarin CRM

## Perfil de Desarrollo

Actúa como un ingeniero de software senior con más de 15 años de experiencia en Go, TypeScript/React y sistemas distribuidos. Sé extremadamente exigente, minucioso y riguroso con cada línea de código. No toleres errores, código redundante ni soluciones a medias.

---

## Regla #1: Auto-Verificación Obligatoria

**Después de CADA cambio de código, SIEMPRE:**

1. **Backend (Go/Fiber):** Ejecutar `cd /root/proyect/clarin && docker compose build backend` y verificar que compila sin errores.
2. **Frontend (Next.js/React):** Ejecutar `cd /root/proyect/clarin && docker compose build frontend` y verificar que compila sin errores.
3. **Si hay errores:** Identificarlos, analizarlos y corregirlos ANTES de informar al usuario. Repetir hasta que el build sea exitoso.
4. **Después del build exitoso:** Ejecutar `docker compose up -d` y verificar logs con `docker compose logs --tail=30 backend` o `docker compose logs --tail=30 frontend`.
5. **Nunca presentar código al usuario sin haber verificado que compila y se despliega correctamente.**

> **IMPORTANTE:** No hay compilador Go instalado localmente. TODOS los builds de Go se hacen via Docker. No usar `go build` directamente.

---

## Stack Tecnológico

| Capa | Tecnología | Versión |
|------|-----------|---------|
| Frontend | Next.js (App Router) | 14.2 |
| UI | React + TypeScript + Tailwind CSS | 18.3 / 5.4 / 3.4 |
| Backend | Go + Fiber | 1.25.0 / 2.52 |
| Base de datos | PostgreSQL | 16 |
| Cache | Redis (pkg/cache) | 7 |
| Almacenamiento | MinIO (S3-compatible) | imagen definida en docker-compose |
| WhatsApp | whatsmeow | v0.0.0-20260604205742-c6a4b703e48f |
| CRM externo | API de Kommo v4 dormida | — |
| Contenedores | Docker Compose | — |
| Deploy | Dokploy | clarin.naperu.cloud |

---

## API de Kommo Dormida

La estructura Kommo se conserva para compatibilidad futura: columnas `kommo_id`,
metadata de importación Excel/CSV interna, repositorios, tipos y helpers como
`kommo.NormalizePhone()`. Sin embargo, la comunicación por API con Kommo está
intencionalmente desactivada.

- No iniciar `kommo.Manager`, outbox, reconciliación, poller de eventos ni auto-registro de webhooks.
- No exponer acciones frontend para sincronizar, forzar pull, crear/editar integraciones Kommo o borrar/mover leads en Kommo.
- No reactivar rutas o workers que llamen a Kommo sin una decisión explícita de producto y una revisión de seguridad de datos.
- La importación Excel de Kommo sí puede seguir funcionando porque es un flujo local y no llama a la API de Kommo.
- La UI de importación acepta sólo `.xlsx/.xls`; internamente puede convertir el Excel a CSV para reutilizar endpoints existentes.

## Seguridad de Acceso

- Clarín no permite registro público. No habilitar `/signup` ni `POST /api/auth/register`; los usuarios y cuentas se crean sólo desde el panel administrador.
- El login en producción debe usar Cloudflare Turnstile con `TURNSTILE_SITE_KEY` y `TURNSTILE_SECRET_KEY` configurados como variables de entorno. Nunca commitear la clave secreta.
- La autenticación objetivo vive en cookies httpOnly (`auth-token` y `refresh-token`). El frontend puede conservar compatibilidad temporal con `Authorization` desde `localStorage`, pero no debe ampliarse ese patrón.
- Las contraseñas nuevas deben cumplir la política fuerte del backend: mínimo 10 caracteres, mayúscula, minúscula, número y símbolo.
- Nunca imprimir ni commitear `.env`, tokens, cookies, JWT, contraseñas, secretos de MinIO/JWT/Turnstile ni respuestas completas de login.

---

## Arquitectura del Proyecto

```
backend/
  cmd/server/main.go              → Punto de entrada, inicialización de servicios
  internal/
    api/                           → Handlers HTTP (Fiber), rutas REST + WebSocket
      server.go                    → Handlers principales + setupRoutes()
      program_handler.go           → Handlers de programas (CRUD + sesiones + asistencia)
      program_handler_filter.go    → Filtros avanzados para programas
    domain/
      entities.go                  → Todas las entidades del dominio (38 structs)
    formula/                       → Motor de fórmulas para etiquetas
      parser.go                    → Tokenizer + parser recursivo → AST
      sql_builder.go               → AST → SQL parametrizado (PostgreSQL)
      evaluator.go                 → AST + lista de tags → evaluación booleana
    kommo/                         → Integración con Kommo CRM
      client.go                    → Cliente HTTP para API de Kommo v4
      sync.go                      → Código de sync dormido; el flujo activo es importación Excel local
    repository/                    → Capa de datos (pgx/PostgreSQL)
      repository.go                → Repositorio principal (leads, contacts, chats, events, etc.)
      program_repository.go        → Repositorio de programas
    service/                       → Lógica de negocio
      service.go                   → Servicio principal
      program_service.go           → Servicio de programas
    storage/
      storage.go                   → Almacenamiento de archivos (MinIO/S3)
    whatsapp/
      device_pool.go               → Pool de dispositivos WhatsApp (whatsmeow)
    ws/
      hub.go                       → Hub WebSocket para comunicación en tiempo real
  pkg/
    cache/
      cache.go                     → Cache Redis (Get/Set/Del/DelPattern con TTL)
    config/
      config.go                    → Variables de entorno
    database/
      database.go                  → Conexión DB + migraciones en InitDB()
  migrations/                      → SQL de migraciones (legacy, ahora en database.go)

frontend/
  src/
    app/                           → Next.js App Router (pages + layouts)
      dashboard/
        layout.tsx                 → Layout con sidebar de navegación
        page.tsx                   → Dashboard home
        admin/page.tsx             → Administración de cuentas/usuarios
        broadcasts/page.tsx        → Campañas de mensajes masivos WhatsApp
        chats/page.tsx             → Chat en tiempo real con WhatsApp
        contacts/page.tsx          → Gestión de contactos WhatsApp
        devices/page.tsx           → Dispositivos WhatsApp (QR, estado)
        events/page.tsx            → Eventos con fórmulas de etiquetas
        events/[id]/page.tsx       → Detalle de evento (participantes, interacciones)
        leads/page.tsx             → CRM Kanban/Lista con filtros avanzados
        programs/page.tsx          → Programas educativos
        programs/[id]/page.tsx     → Detalle de programa (sesiones, asistencia)
        settings/page.tsx          → Configuración (Kommo, pipelines, quick replies)
        tags/page.tsx              → Gestión de etiquetas
    components/                    → Componentes React reutilizables
      FormulaEditor.tsx            → Editor de fórmulas con tokenizer, autocomplete y validación en tiempo real
      ContactSelector.tsx          → Selector de contactos
      CreateCampaignModal.tsx      → Modal de creación de campañas
      ImportCSVModal.tsx           → Modal de importación Excel de Kommo/Clarín
      LeadDetailPanel.tsx          → Panel lateral de detalle de lead
      NotificationProvider.tsx     → Proveedor de notificaciones toast
      TagInput.tsx                 → Input de etiquetas con autocompletado
      WhatsAppTextInput.tsx        → Input de texto con formato WhatsApp
      chat/                        → Componentes específicos del chat
        ChatPanel.tsx              → Panel principal de conversación
        ContactPanel.tsx           → Panel de info del contacto
        DeviceSelector.tsx         → Selector de dispositivo activo
        EmojiPicker.tsx            → Picker de emojis
        FileUploader.tsx           → Upload de archivos/imágenes
        ImageViewer.tsx            → Visor de imágenes ampliadas
        MessageBubble.tsx          → Burbuja de mensaje individual
        NewChatModal.tsx           → Modal para nuevo chat
        PollModal.tsx              → Modal para crear encuestas
        QuickReplyPicker.tsx       → Picker de respuestas rápidas
        StickerPicker.tsx          → Picker de stickers
        TagSelector.tsx            → Selector de etiquetas para chats
    lib/
      api.ts                       → Cliente HTTP (fetch) + WebSocket factory
      utils.ts                     → Utilidades compartidas (formateo, helpers)
      notificationSounds.ts        → Sonidos de notificación
      whatsappFormat.tsx           → Renderizado de formato WhatsApp (bold, italic, etc.)
    types/                         → TypeScript interfaces compartidas entre páginas
      chat.ts                      → Tipos de Chat, Message, etc.
      program.ts                   → Tipos de Program, Session, Attendance
    utils/                         → Funciones utilitarias puras (sin side-effects)
      chat.ts                      → Utilidades de chat (formateo de mensajes)
      eventExport.ts               → Exportación de eventos a Excel/CSV
      eventWordReport.ts           → Generación de reportes Word para eventos
      format.ts                    → Formateo de números, fechas, moneda
      imageCompression.ts          → Compresión de imágenes antes de upload
```

---

## Entidades del Dominio (domain/entities.go)

### Autenticación y Cuentas
- `Account` — Cuenta multi-tenant (nombre, configuración Kommo)
- `User` — Usuario del sistema (email, password hash)
- `Role` — Rol de usuario (admin, user)
- `UserAccount` — Relación usuario↔cuenta

### WhatsApp
- `Device` — Dispositivo WhatsApp conectado (JID, estado, nombre)
- `Contact` — Contacto de WhatsApp (JID, nombre, teléfono)
- `Chat` — Conversación WhatsApp (último mensaje, unread count)
- `Message` — Mensaje individual (tipo, contenido, media, estado)
- `MessageReaction` — Reacción emoji a mensaje
- `PollOption`, `PollVote` — Encuestas WhatsApp

### CRM (Leads)
- `Lead` — Lead/prospecto (nombre, teléfono, email, pipeline stage, tags, custom fields)
- `Pipeline` — Pipeline de ventas (nombre, etapas)
- `PipelineStage` — Etapa del pipeline (nombre, color, orden)
- `Tag` — Etiqueta (nombre, color, account_id)
- `Person` — Persona vinculada a lead (de Kommo)

### Campañas (Broadcasts)
- `Campaign` — Campaña de mensajes masivos (nombre, template, estado)
- `CampaignAttachment` — Archivo adjunto de campaña
- `CampaignRecipient` — Destinatario con estado de envío

### Eventos
- `Event` — Evento con fórmula de etiquetas (tag_formula, tag_formula_type)
- `EventPipeline`, `EventPipelineStage` — Pipeline específico de eventos
- `EventFolder` — Carpeta organizativa de eventos
- `EventParticipant` — Participante vinculado por fórmula de tags
- `Interaction` — Interacción registrada en evento

### Programas
- `Program` — Programa educativo (nombre, descripción, estado)
- `ProgramParticipant` — Participante de programa
- `ProgramSession` — Sesión programada
- `ProgramAttendance` — Registro de asistencia

### Otros
- `QuickReply` — Respuesta rápida predefinida (shortcut, contenido)
- `WhatsAppCheckResult` — Resultado de verificación de número WhatsApp

---

## Motor de Fórmulas (internal/formula/)

Sistema de expresiones para filtrar leads/participantes por etiquetas. Soporta:

- **Literales:** `"etiqueta"` — coincidencia exacta (case-insensitive)
- **Comodines:** `"mar%"` — LIKE de SQL, `%` = cualquier texto
- **Operadores:** `and`, `or`, `not`
- **Agrupación:** `( )`
- **Ejemplo:** `("04-mar" or "07-mar") and "iquitos" and not "elimi%"`

### Flujo:
1. `parser.go` → Tokeniza + parsea a AST (recursive descent)
2. `sql_builder.go` → Convierte AST a SQL parametrizado con JOINs a `lead_tags`/`event_tags`
3. `evaluator.go` → Evalúa AST contra lista de tags de un lead (usado en sync en memoria)

### Uso:
- **Eventos:** Campo `tag_formula` en Event. Sync automático de participantes matching.
- **Leads:** Filtro avanzado en la UI. Se envía como query param `tag_formula`.
- **Frontend:** `FormulaEditor` componente con tokenizer client-side, autocomplete de tags, y validación en tiempo real.

---

## Eventos WebSocket (ws/hub.go)

| Evento | Constante | Cuándo se emite |
|--------|-----------|----------------|
| `new_message` | `EventNewMessage` | Llega mensaje entrante de WhatsApp |
| `message_sent` | `EventMessageSent` | Mensaje enviado exitosamente |
| `message_status` | `EventMessageStatus` | Cambio de estado (delivered, read) |
| `device_status` | `EventDeviceStatus` | Dispositivo conecta/desconecta |
| `qr_code` | `EventQRCode` | Nuevo QR para vincular dispositivo |
| `chat_update` | `EventChatUpdate` | Chat creado/actualizado |
| `presence` | `EventPresence` | Contacto online/offline |
| `typing` | `EventTyping` | Contacto escribiendo |
| `lead_update` | `EventLeadUpdate` | Lead creado/actualizado/eliminado |
| `notification` | `EventNotification` | Notificación push al usuario |
| `message_reaction` | `EventMessageReaction` | Reacción emoji a mensaje |
| `poll_update` | `EventPollUpdate` | Voto en encuesta |
| `interaction_update` | `EventInteractionUpdate` | Interacción registrada en evento |
| `message_revoked` | `EventMessageRevoked` | Mensaje eliminado/revocado |
| `message_edited` | `EventMessageEdited` | Mensaje editado |
| `event_participant_update` | `EventEventParticipantUpdate` | Participante añadido/removido de evento |

**Broadcast:** `s.hub.BroadcastToAccount(accountID, ws.EventXxx, data)`

---

## Convenciones de Código

### Go (Backend)

- **Manejo de errores:** Siempre verificar `err != nil`. Nunca ignorar errores silenciosamente.
- **Logging:** Usar `log.Printf` con prefijos descriptivos: `[SYNC]`, `[WS]`, `[API]`, `[WHATSAPP]`, `[STORAGE]`, `[CACHE]`.
- **SQL:** Usar queries parametrizadas con `$1, $2...` (pgx). NUNCA concatenar strings en queries SQL.
- **Contexto:** Pasar `context.Context` en operaciones de DB y HTTP.
- **Nombrado:** camelCase para variables locales, PascalCase para exportados. Nombres descriptivos.
- **Imports:** Agrupar stdlib, terceros, internos — separados por línea en blanco.
- **Fiber handlers:** Patrón `func (s *Server) handleXxx(c *fiber.Ctx) error`.
- **Repository:** Métodos en el struct `Repository`. Queries SQL como constantes o inline limpias.
- **Phone normalization:** Siempre usar `kommo.NormalizePhone()` para números telefónicos. Perú (51) es el único país soportado. Números de 9 dígitos que empiezan con 9 reciben prefijo automático "51".
- **Database migrations:** Las migraciones están en `database.go` como SQL ejecutado en `InitDB()`. Para cambios de esquema, agregar `ALTER TABLE` o `CREATE TABLE IF NOT EXISTS` al final de la función.
- **Módulos multi-archivo:** Features grandes dividen handler/repository/service en archivos con prefijo (ej: `program_handler.go`, `program_repository.go`, `program_service.go`) junto a los archivos principales.
- **Cache:** Usar `pkg/cache` para datos frecuentes. Invalidar con `DelPattern()` cuando datos cambian.
- **Storage:** Usar `internal/storage` para archivos. Métodos principales: `UploadFile()` y `DeleteFile()`. URLs públicas via MinIO.
- **Jerarquía de datos:** `Contact` es padre; `Lead` y `Chat` son hijos paralelos. Crear lead/chat exige `contact_id`. Eliminar contacto borra leads, chats y mensajes; eliminar lead no borra chat; eliminar chat no borra lead.
- **Storage seguro:** En MinIO, borrar automáticamente sólo objetos bajo prefijos `account_id/` que ya no existen en `accounts`. Objetos sin referencia dentro de cuentas activas requieren dry-run, antigüedad mínima o confirmación explícita.

### TypeScript/React (Frontend)

- **Componentes:** Functional components con hooks. No class components.
- **Estado:** `useState` para local. No hay store global actualmente (zustand disponible pero no en uso).
- **Estilos:** Tailwind CSS exclusivamente. No CSS modules ni styled-components.
- **Sistema de diseño:** Paleta `emerald` (primario) y `slate` (neutro). Usar variantes como `emerald-500`, `emerald-600`, `slate-700`, `slate-800`.
- **API calls:** Usar funciones de `@/lib/api.ts`. Manejar errores con try/catch.
- **WebSocket:** Usar `createWebSocket(onMessage)` de `@/lib/api.ts`.
- **Imports:** Usar alias `@/` para imports desde `src/`.
- **TypeScript:** Strict mode habilitado. Definir interfaces/types para props y respuestas de API.
- **Rutas API:** El frontend hace proxy a través de Next.js rewrites (`/api/:path*` → backend:8080).
- **Types compartidos:** Interfaces usadas entre páginas van en `src/types/` (ej: `chat.ts`, `program.ts`).
- **Utilidades puras:** Funciones sin side-effects van en `src/utils/` (formateo, exportación, compresión). Diferente de `lib/` que tiene side-effects (HTTP, WebSocket).
- **Organización de componentes:** Componentes compartidos al root de `components/`. Features complejas en subdirectorios (ej: `components/chat/` con 13 componentes).
- **Componentes ricos:** Para UX tipo IDE (ej: `FormulaEditor`), usar tokenizer client-side, autocomplete, y validación en tiempo real. `onMouseDown` con `e.preventDefault()` + `e.stopPropagation()` para evitar event bubbling en dropdowns anidados.

---

## Flujo de Trabajo de Desarrollo

### Para CADA cambio:

```
1. Leer y entender el código existente antes de modificar
2. Hacer el cambio mínimo necesario (no sobre-ingeniería)
3. Verificar build: docker compose build [backend|frontend]
4. Si hay errores → analizar → corregir → volver al paso 3
5. Deploy: docker compose up -d
6. Verificar logs: docker compose logs --tail=30 [servicio]
7. Verificar `/health`; reportar `whatsapp.devices_connected/devices_total` si baja después del deploy
8. Si hay errores en runtime → analizar → corregir → volver al paso 3
9. Confirmar al usuario que todo está funcionando
```

### Para cambios de base de datos:

```
1. Agregar migración en database.go (InitDB)
2. Usar CREATE TABLE IF NOT EXISTS o ALTER TABLE con manejo de "already exists"
3. Build y deploy
4. Verificar que la migración se ejecutó en logs del backend
```

### Para nuevos endpoints API:

```
1. Definir entidad en domain/entities.go si es necesaria
2. Agregar método en repository/repository.go (o módulo_repository.go para features grandes)
3. Agregar lógica en service/service.go (o módulo_service.go)
4. Agregar handler en api/server.go (o módulo_handler.go)
5. Registrar ruta en setupRoutes()
6. Build, deploy, verificar
```

### Para features grandes (nuevo módulo):

```
1. Crear archivos separados: módulo_handler.go, módulo_repository.go, módulo_service.go
2. Agregar entidades en domain/entities.go
3. Agregar tipos TypeScript en src/types/módulo.ts si se usan en múltiples páginas
4. Crear página en src/app/dashboard/módulo/page.tsx
5. Agregar link en sidebar (src/app/dashboard/layout.tsx)
6. Si tiene subpáginas dinámicas: src/app/dashboard/módulo/[id]/page.tsx
7. Build, deploy, verificar
```

---

## Reglas Críticas

1. **NUNCA uses `go build` localmente** — siempre `docker compose build backend`.
2. **NUNCA concatenes strings en queries SQL** — siempre queries parametrizadas.
3. **NUNCA ignores un error de compilación** — corrígelo antes de continuar.
4. **NUNCA modifiques código sin leerlo primero** — entiende el contexto completo.
5. **NUNCA hagas cambios masivos innecesarios** — cambios mínimos y enfocados.
6. **NUNCA presentes código sin verificar** — build exitoso = requisito mínimo.
7. **SIEMPRE verifica que no hay errores de runtime** después del deploy (revisar logs).
8. **SIEMPRE normaliza teléfonos** con `kommo.NormalizePhone()` al crear/editar leads/chats.
9. **SIEMPRE usa Tailwind con emerald/slate** para estilos del frontend.
10. **SIEMPRE broadcast por WebSocket** cuando datos cambian que el frontend necesita ver en tiempo real.
11. **SIEMPRE usa `e.stopPropagation()`** en componentes con dropdowns/autocomplete dentro de otros dropdowns para evitar event bubbling.
12. **SIEMPRE invalida cache Redis** cuando modificas datos que pueden estar cacheados.

---

## Patrones Comunes

### Agregar campo a entidad existente:

```go
// 1. domain/entities.go — agregar campo al struct
type Lead struct {
    // ... campos existentes
    NuevoCampo string `json:"nuevo_campo"`
}

// 2. database.go — migración
_, _ = db.Exec(ctx, `ALTER TABLE leads ADD COLUMN IF NOT EXISTS nuevo_campo TEXT DEFAULT ''`)

// 3. repository.go — actualizar queries INSERT/UPDATE/SELECT
// 4. server.go — actualizar handlers si el campo viene del frontend
```

### Agregar nueva página al dashboard:

```tsx
// 1. Crear src/app/dashboard/nueva-pagina/page.tsx
// 2. Agregar link en src/app/dashboard/layout.tsx (sidebar)
// 3. Usar misma estructura: "use client", emerald/slate, Tailwind
// 4. Si tiene subpáginas: src/app/dashboard/nueva-pagina/[id]/page.tsx
```

### Broadcast WebSocket:

```go
// Después de una operación que cambia datos visibles en el frontend:
if s.hub != nil {
    s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, map[string]interface{}{
        "action": "updated",
    })
}
// Eventos disponibles: ver tabla completa en "Eventos WebSocket" arriba
```

### Componente con autocomplete en dropdown anidado:

```tsx
// SIEMPRE usar stopPropagation en componentes dentro de otros dropdowns
<button onMouseDown={(e) => {
  e.preventDefault()      // evita blur del textarea
  e.stopPropagation()     // evita cerrar dropdown padre
  handleSelect(item)
}}>
```

### Upload de archivos:

```go
// Usar storage.UploadFile() para subir a MinIO
url, err := s.storage.UploadFile(ctx, accountID, "carpeta", "nombre.ext", data, contentType)
// URL pública disponible inmediatamente
```

---

## Checklist de Calidad (Aplicar en CADA cambio)

- [ ] ¿El código compila sin errores? (`docker compose build`)
- [ ] ¿Los logs muestran arranque limpio? (`docker compose logs --tail=30`)
- [ ] ¿Se manejan todos los errores posibles?
- [ ] ¿Las queries SQL están parametrizadas?
- [ ] ¿Los tipos TypeScript están correctos?
- [ ] ¿Se usa la paleta emerald/slate?
- [ ] ¿El cambio es mínimo y enfocado?
- [ ] ¿Se ha leído el código existente antes de modificar?
- [ ] ¿Se necesita broadcast WebSocket para este cambio?
- [ ] ¿Se normaliza el teléfono si aplica?
- [ ] ¿Se invalida cache si aplica?
- [ ] ¿Los eventos de componentes usan stopPropagation donde es necesario?

## Active Technologies
- Frontend: Next.js 14.2, React 18.3, TypeScript 5.4, Tailwind CSS 3.4
- Backend: Go 1.25.0, toolchain go1.25.11, Fiber 2.52, pgx v5
- WhatsApp: whatsmeow v0.0.0-20260604205742-c6a4b703e48f
- Storage: MinIO/S3-compatible
