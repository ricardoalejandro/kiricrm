---
name: backend-development
description: Build and maintain the current Kiri CRM API using Node.js, TypeScript, NestJS, Fastify, Prisma, PostgreSQL and Redis. The old Go backend is legacy reference only.
---

# Backend Development — Kiri CRM

## Current Backend

The active backend target is:

```text
apps/api/
  prisma/schema.prisma
  src/main.ts
  src/modules/
    auth/
    accounts/
    contacts/
    leads/
    campaigns/
    health/
    realtime/
```

`backend/` is legacy Clarin/Go code. Do not add new production behavior there unless explicitly requested.

## Stack

- Node.js 20
- TypeScript strict mode
- NestJS 10
- Fastify 4 via `@nestjs/platform-fastify`
- Prisma Client
- PostgreSQL 16
- Redis 7 for cache/rate limits/future queues

## Rules

- Keep API stateless.
- Keep every business entity scoped by `account_id`.
- Validate request payloads with Zod or Nest pipes before writing.
- Never trust account IDs from client body when authenticated user context already has account.
- Avoid N+1 queries. Use Prisma includes/selects intentionally.
- Return stable response shapes that the frontend can render safely.
- Never return raw database errors or secrets to the client.

## Auth

- Login route: `/api/auth/login`
- Session user route: `/api/me`
- Logout route: `/api/auth/logout`
- Public security config: `/api/public/security-config`
- Prefer httpOnly cookies.
- Do not enable public signup without explicit approval.

## WhatsApp

WhatsApp is not active in this phase.

- `/api/devices` and `/api/chats` may return empty `coming_soon` responses.
- Do not add whatsmeow, QR or WhatsApp Web behavior.
- Future implementation should use WhatsApp Cloud API official webhooks and workers.

## Prisma

After schema changes:

```bash
npm --workspace @kiricrm/api run db:generate
npm --workspace @kiricrm/api run typecheck
```

Do not run destructive migrations or database resets against production without explicit approval.

## Build

```bash
npm --workspace @kiricrm/api run typecheck
npm --workspace @kiricrm/api run build
docker build -t kiricrm-api:latest -f deploy/Dockerfile.api .
```

## Common Runtime Failures

- `FST_ERR_PLUGIN_VERSION_MISMATCH`: Fastify plugin version does not match Nest Fastify version. NestJS 10 uses Fastify 4.
- Prisma `P1001`: API cannot reach Postgres. Check service DNS and network.
- Prisma `P1000`: credentials mismatch. Do not rotate password without explicit approval.
