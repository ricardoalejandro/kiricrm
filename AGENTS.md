# Kiri CRM - Codex Instructions

## Current Goal

Kiri CRM is being restarted from zero. The old project was archived in `_old/`
and must be treated only as reference material.

## Work Order

- Build backend first. Do not build or modify frontend unless the owner asks.
- Do not expose Kiri CRM publicly until the owner explicitly approves it.
- Do not route new services through `kiricrm.com` yet.
- Prefer local/SSH development from VSCode and test backend endpoints locally.
- Use `_old/` only as a reference. If frontend behavior is needed, consult
  `_old/` only after the owner asks or when it is necessary to preserve a known
  contract.

## Backend Stack

- Node.js 20
- pnpm 10.34.3 via Corepack
- TypeScript
- NestJS 11
- Fastify via `@nestjs/platform-fastify`
- Prisma
- PostgreSQL 16
- Redis 7
- Docker for local/service packaging later

## Backend Rules

- Use pnpm only. Never use `npm install`, `npm ci`, Yarn, or Bun for this repo.
- Keep dependency versions exact and review recently published packages before
  adding or upgrading dependencies.
- Preserve pnpm security settings: `minimumReleaseAge` must stay at 7 days and
  dependency build scripts must stay denied unless explicitly approved.
- Keep `trustPolicy: no-downgrade` and `blockExoticSubdeps: true` enabled in
  `pnpm-workspace.yaml`.
- Keep the `undici-types` override pinned unless a newer version has equivalent
  provenance/attestation evidence and passes pnpm trust checks.
- Avoid dev tools that pull native installer scripts unless they are necessary.
  Prefer the smallest dependency tree that supports the current backend slice.
- Keep the API stateless.
- Keep tenant-owned data scoped by `account_id`.
- Validate request payloads before writes.
- Prefer stable JSON responses that are easy to test from Postman.
- Use httpOnly cookies when auth is added.
- Do not enable public signup without explicit approval.
- Do not print secrets, `.env`, JWTs, cookies, database passwords, or access
  tokens.

## Out of Scope For Now

- Frontend implementation.
- WhatsApp integrations.
- Kommo integrations.
- Public production deployment.
- Public access through `kiricrm.com`.

## Legacy Reference

- `_old/backend` contains the old Go backend reference.
- `_old/frontend` contains the old Next/React frontend reference.
- `_old/.github/copilot-instructions.md` and `_old/.github/skills` contain the
  previous instruction set and workflow notes.

## Codex Notes

- Codex reads this `AGENTS.md` automatically when launched from this project.
- Repo skills live in `.agents/skills/<skill-name>/SKILL.md`.
- Current Kiri repo skills:
  - `.agents/skills/backend-development/SKILL.md`
  - `.agents/skills/database-changes/SKILL.md`
- Do not install Kiri-specific skills globally under `$HOME/.agents/skills`.
- Restart the Codex session after changing instructions or skills.
