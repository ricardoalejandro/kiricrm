---
name: database-changes
description: Make database schema changes for Clarin CRM. Use when adding tables, columns, indexes, or modifying the PostgreSQL schema. Migrations live in database.go InitDB() function.
---

# Database Changes — Clarin CRM

## How Migrations Work

Migrations are SQL statements inside `backend/pkg/database/database.go` in the `InitDB()` function. They run on every backend startup, so they MUST be idempotent.

## Adding a New Column

```go
// In InitDB() at the end:
_, _ = db.Exec(ctx, `ALTER TABLE leads ADD COLUMN IF NOT EXISTS priority TEXT DEFAULT 'normal'`)
```

## Adding a New Table

```go
_, _ = db.Exec(ctx, `
    CREATE TABLE IF NOT EXISTS campaigns (
        id BIGSERIAL PRIMARY KEY,
        account_id BIGINT NOT NULL REFERENCES accounts(id),
        name TEXT NOT NULL,
        status TEXT DEFAULT 'draft',
        created_at TIMESTAMPTZ DEFAULT NOW(),
        updated_at TIMESTAMPTZ DEFAULT NOW()
    )
`)
```

## Adding an Index

```go
_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_leads_phone ON leads(phone)`)
```

## Adding a Junction Table

```go
_, _ = db.Exec(ctx, `
    CREATE TABLE IF NOT EXISTS lead_tags (
        lead_id BIGINT NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
        tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
        PRIMARY KEY (lead_id, tag_id)
    )
`)
```

## Rules

1. **ALWAYS use `IF NOT EXISTS`** or `ADD COLUMN IF NOT EXISTS` — migrations run on every startup.
2. **ALWAYS use parameterized queries** in repository code — never concatenate values.
3. After adding a migration, **update the corresponding struct** in `domain/entities.go`.
4. Update **repository queries** (SELECT, INSERT, UPDATE) in `repository/repository.go`.
5. Protect `account_id` boundaries in every FK repair, delete and backfill query.
6. Build and deploy to verify: `docker compose build backend && docker compose up -d`
7. Check logs to confirm migration ran: `docker compose logs --tail=30 backend`

## Contact / Lead / Chat Integrity

- `contacts` is the parent table. `leads` and `chats` should have valid `contact_id`.
- Do not create new flows that allow leads or chats without a contact.
- Prefer explicit backend transactions for contact deletion: delete chats/messages and leads first, then contact.
- Avoid changing core FKs to broad `ON DELETE CASCADE` unless the account-scope implications are reviewed.
- Before making `contact_id` stricter, repair or delete orphan leads/chats by same-account normalized phone.
- Backfills must never connect records across accounts.

## Existing Key Tables

- `accounts` — Multi-tenant accounts
- `users` — Users per account
- `contacts` — Parent CRM/WhatsApp contacts
- `leads` — CRM leads linked to contacts
- `chats` — WhatsApp conversations
- `messages` — Chat messages
- `tags` — Tags with `UNIQUE(account_id, name)` and `kommo_id`
- `lead_tags`, `contact_tags`, `chat_tags` — Junction tables
- `devices` — WhatsApp devices
- `pipelines`, `pipeline_stages` — Kanban pipelines
