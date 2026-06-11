---
name: kommo-integration
description: Work with Kommo data in Clarin through local Excel imports, phone normalization, status/date tags, observations, and compatibility metadata. Use when modifying Kommo import logic or normalized Kommo fields. Do not use to reactivate API sync with Kommo without an explicit product decision.
---

# Kommo Local Import — Clarin CRM

## Current Rule

API communication with Kommo is intentionally dormant. Keep client structs, `kommo_id`
metadata and helpers for compatibility, but do not restart API pollers, webhooks,
outbox jobs or frontend sync actions unless product explicitly asks for it.

The supported Kommo flow is local Excel import from the leads UI.

## Excel Import Behavior

- UI accepts only `.xlsx/.xls`; it may convert the workbook to CSV internally for backend reuse.
- Use `kommo.NormalizePhone()` for phone matching and creation.
- Leads nuevos always get created when basic validation passes, even if Kommo `Fecha de Creación` is older than 24h.
- The 24h window only controls modifications to existing Clarín leads/contacts.
- Existing leads outside the 24h window must not be touched.
- New leads may receive manual import tags, Excel tags, Kommo status tags, `✅ Fecha` tags and a `Komo: ...` observation.
- Existing leads inside the 24h window may sync only Kommo status/date tags; do not move pipeline/stage or rewrite notes/tasks.

## Status Tags

Closed Kommo status tag set:

`CONFIRMADO`, `FLUJO INCOMPLETO`, `OTRAS CONSULTAS`, `REVIVIÓ`, `NO RESPONDE`

When a new lead or eligible existing lead brings one of these statuses, keep only
one status tag from this set on the contact. Compare case-insensitively and
tolerate extra spaces. Do not remove unrelated tags.

## Observations

For new leads only, create a note when values exist:

`Komo: Status: <Estatus del lead>; Campaña: <Campaña>`

Accept campaign headers normalized from variants such as `✅ Campaña`. Missing
columns or empty values must not break the import.

## Safety

- Never print imported personal data, raw workbook rows, tokens or secrets in logs.
- Reimports must be idempotent: existing leads should not lose tags, observations,
  tasks or stage unless the explicit 24h status/date rule applies.
- After backend changes, build/deploy with Docker and verify logs plus `/health`.
