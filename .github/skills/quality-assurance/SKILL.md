---
name: quality-assurance
description: Enforce code quality, self-testing, and verification standards for Clarin CRM. Use this skill to ensure every change is compiled, deployed, and validated before presenting to the user. Acts as a senior engineer code review checklist.
user-invokable: false
---

# Quality Assurance — Clarin CRM

## Senior Engineer Mindset

Act as a senior software engineer with 15+ years of experience. Be extremely demanding, thorough, and rigorous with every line of code. Never tolerate errors, redundant code, or half-baked solutions.

## Self-Testing Protocol — MANDATORY after every change

### Step 1: Build
```bash
# Backend changes
cd /root/proyect/clarin && docker compose build backend

# Frontend changes
cd /root/proyect/clarin && docker compose build frontend
```

### Step 2: Fix Build Errors
If the build fails:
1. Read the error output carefully
2. Identify the root cause (not just the symptom)
3. Fix the code
4. Rebuild — repeat until successful

### Step 3: Deploy
```bash
cd /root/proyect/clarin && docker compose up -d
```

### Step 4: Verify Logs
```bash
docker compose exec -T backend wget -qO- http://127.0.0.1:8080/health
docker compose logs --tail=30 backend
docker compose logs --tail=30 frontend
```

### Step 5: Fix Runtime Errors
If logs show errors:
1. Analyze the error
2. Fix the code
3. Rebuild, redeploy, recheck logs

### Step 6: Confirm to User
Only after ALL steps pass with zero errors, confirm to the user that everything is working.

## Code Review Checklist

Before presenting any change, verify:

- [ ] Code compiles without errors
- [ ] Logs show clean relevant startup (no panics, no new runtime errors)
- [ ] `/health` is healthy after deploy
- [ ] WhatsApp `devices_connected/devices_total` was checked and any drop was reported
- [ ] All errors are handled (no ignored `err` in Go)
- [ ] SQL queries are parameterized ($1, $2...) — NO string concatenation
- [ ] TypeScript types are correct and strict
- [ ] UI matches the existing page style and uses emerald/slate accents consistently
- [ ] Change is minimal and focused — no over-engineering
- [ ] Existing code was read and understood before modifying
- [ ] WebSocket broadcast added if data changes affect frontend
- [ ] Phone numbers normalized with `kommo.NormalizePhone()` if applicable

## Common Mistakes to Catch

1. **Missing import** in Go — will fail build
2. **Missing closing brace** — often happens in large file edits
3. **Leftover code fragments** — from copy-paste edits
4. **Wrong variable type** in TypeScript — breaks build
5. **Hardcoded values** that should come from config/env
6. **Deleted helper functions** — accidentally removed during large replacements
7. **Missing route registration** — handler exists but route not added
8. **Silent WhatsApp regression** — backend is healthy but devices dropped into QR/timeout after restart
9. **Secret leakage** — `.env`, tokens, cookies, JWTs or login responses printed in logs/output
