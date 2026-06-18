# Changelog — Clarin CRM

## 2026-03-27

### Build 1 — Eros AI Revamp
- ✨ Eros ahora usa exclusivamente OpenAI (se eliminó soporte Gemini)
- ✨ Selección de modelo AI después de validar API key (GPT-4o, GPT-4.1, etc.)
- ✨ Pantalla de configuración personalizada: rol, persona e instrucciones custom
- ✨ Atajo Ctrl+I / Cmd+I para abrir/cerrar Eros desde cualquier página
- 🔧 Nuevos campos en usuario: eros_model, eros_role, eros_instructions
- 🔧 Nuevo endpoint POST /api/ai/models para listar modelos disponibles
- 🔧 buildSystemPrompt acepta rol e instrucciones personalizadas

## 2026-03-26

### Build 3 — Sistema de Versionamiento
- ✨ Sistema de versionamiento con detección automática de actualizaciones
- ✨ Banner no intrusivo cuando hay nueva versión disponible
- ✨ Modal de changelog accesible desde el sidebar
- ✨ Endpoint `/api/version` con changelog embebido
- 🔧 Header `X-Clarin-Version` en todas las respuestas API

### Build 2 — Archivo y Bloqueo desde Eventos
- ✨ Archivar/bloquear leads desde la página de eventos
- ✨ Modal de razón de archivo con opciones predefinidas
- ✨ Observaciones automáticas al archivar/bloquear
- 💄 Mejora de estilos de selección en listas

### Build 1 — Mejoras de UX
- 🐛 Fix Ctrl+Enter para enviar mensajes
- 💄 Mejora de estilos de selección en listas
- ✨ Sincronización de contactos Google
- ✨ Auto-desync Google al archivar/bloquear
