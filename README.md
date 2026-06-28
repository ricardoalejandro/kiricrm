# Kiri CRM

Kiri CRM is being restarted from zero.

The previous codebase is archived in `_old/` and should be used only as a
reference. The first active implementation phase will be the backend.

## Current Status

- Public access through `kiricrm.com` is intentionally disabled.
- Development is local/SSH-first from VSCode.
- Backend stack target: Node.js, TypeScript, NestJS/Fastify, Prisma,
  PostgreSQL and Redis.
- Frontend work is paused until explicitly requested.

## Backend

The first backend slice is a local-only NestJS/Fastify API.

```bash
corepack enable
corepack prepare pnpm@10.34.3 --activate
pnpm install
pnpm run build
pnpm run start:api:local
```

Local endpoints:

```text
GET http://127.0.0.1:18080/health
GET http://127.0.0.1:18080/api/hello
```

`start:api:local` uses port `18080` because port `8080` may already be used by
other services on the server.

## Package Security

Kiri CRM uses pnpm only. Do not use `npm install`, `npm ci`, Yarn, or Bun to
install dependencies.

Security defaults:

- pnpm is pinned with `packageManager`.
- dependency versions are exact.
- `minimumReleaseAge` is set to 7 days.
- `trustPolicy` is set to `no-downgrade`.
- `undici-types` is pinned through pnpm overrides because newer compatible
  patches lacked provenance evidence during the initial rebuild.
- exotic dependency protocols are blocked.
- dependency build scripts are denied unless explicitly approved.
- use `pnpm audit --prod` before shipping backend changes.

## Reference

- Old backend: `_old/backend`
- Old frontend: `_old/frontend`
- Old project instructions: `_old/.github/copilot-instructions.md`
- Old skill notes: `_old/.github/skills`

## Codex

Codex should load `AGENTS.md` automatically when started from this project
directory. Repo-local skills live in:

```text
.agents/skills/<skill-name>/SKILL.md
```

Current skills:

- `.agents/skills/backend-development/SKILL.md`
- `.agents/skills/database-changes/SKILL.md`

Kiri-specific skills should stay repo-local and should not be installed in
`$HOME/.agents/skills`, so they do not affect other projects.
