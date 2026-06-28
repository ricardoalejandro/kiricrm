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
