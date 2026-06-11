---
name: build-and-deploy
description: Build, deploy, and verify changes for Clarin CRM. Use this skill after making any code change to compile, deploy, and validate that everything works correctly. Covers Docker-based builds for Go backend and Next.js frontend.
---

# Build and Deploy — Clarin CRM

## IMPORTANT: No Go compiler installed locally. All builds MUST use Docker.

## Build Commands

### Backend (Go/Fiber)
```bash
cd /root/proyect/clarin && docker compose build backend
```

### Frontend (Next.js/React)
```bash
cd /root/proyect/clarin && docker compose build frontend
```

### Both services
```bash
cd /root/proyect/clarin && docker compose build backend frontend
```

## Deploy Commands

After a successful build:
```bash
cd /root/proyect/clarin && docker compose up -d
```

## Verify Commands

Always check logs after deploy:
```bash
# Healthcheck
docker compose exec -T backend wget -qO- http://127.0.0.1:8080/health

# Backend logs
docker compose logs --tail=30 backend

# Frontend logs
docker compose logs --tail=30 frontend

# All services
docker compose logs --tail=30
```

## Mandatory Workflow

After EVERY code change, follow this exact sequence:

1. **Build** the affected service(s) — `docker compose build backend` and/or `docker compose build frontend`
2. **If build fails**: Read the error, fix it, rebuild. Repeat until clean.
3. **Deploy** — `docker compose up -d`
4. **Check logs** — `docker compose logs --tail=30 backend` or `frontend`
5. **Check health** — verify `/health` returns healthy dependencies.
6. **Check WhatsApp count** — compare `whatsapp.devices_connected/devices_total`; report any drop after restart.
7. **If runtime errors**: Read the logs, fix the issue, rebuild and redeploy.
8. **NEVER present code to the user without a successful build, healthcheck and clean relevant logs.**

## Common Build Errors

- **Go import errors**: Check `backend/go.mod` and ensure all imports match actual package paths.
- **Go undefined errors**: Usually a missing function or wrong package reference. Check the file where it's defined.
- **TypeScript type errors**: Check interface definitions in the component or `@/lib/api.ts`.
- **Next.js build errors**: Often missing dependencies — check `frontend/package.json`.

## Docker Architecture

- Backend: Multi-stage build using `golang:1.25-alpine` with CGO_ENABLED=1 (required for whatsmeow/sqlite)
- Frontend: Multi-stage build using `node:20-alpine` with standalone Next.js output
- Dockerfiles are in `deploy/Dockerfile.backend` and `deploy/Dockerfile.frontend`
