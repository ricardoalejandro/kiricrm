---
name: database-changes
description: Make PostgreSQL schema changes for the current Kiri CRM Prisma-based API. Use for Prisma schema changes, migrations, indexes and seed data.
---

# Database Changes — Kiri CRM

## Current Model

The active schema lives in:

```text
apps/api/prisma/schema.prisma
```

The old Go `backend/pkg/database/database.go` migration style is legacy reference only.

## Rules

- PostgreSQL is the source of truth.
- Prisma is the schema/client layer for the current API.
- Every tenant-owned table must include `account_id`.
- Add indexes for common tenant queries: `account_id`, status, updated time, phone/email, foreign keys.
- Do not reset, drop, truncate or destructive-migrate production without explicit approval.
- Do not rotate database credentials without explicit approval.

## Commands

```bash
npm --workspace @kiricrm/api run db:generate
npm --workspace @kiricrm/api run typecheck
npm --workspace @kiricrm/api run build
```

For production migrations, require an explicit migration plan and backup/rollback note before execution.

## Seed

Seed script:

```text
apps/api/prisma/seed.ts
```

Do not print seeded passwords or `.env` values.

## Credential Reality

The Docker volume may preserve the original Postgres user password even if `.env` changes later. If Prisma reports `P1000`, choose one of these only with owner approval:

- update deployment env to the real existing password, or
- rotate the database user's password to the new `.env` value.
