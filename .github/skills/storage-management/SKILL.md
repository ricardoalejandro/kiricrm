---
name: storage-management
description: Diagnose, preview, and safely clean Clarin MinIO/S3 storage. Use when working with storage usage, media objects, storage_objects, media_assets, orphan files, account purge, or storage cleanup UI/API. Enforces dry-runs, account-prefix safety, and conservative active-account deletion rules.
---

# Storage Management — Clarin CRM

## Storage Model

- Media lives in MinIO under the `clarin-media` bucket.
- Object keys are prefixed by account: `account_id/...`.
- `storage_objects` is an inventory/audit ledger.
- `media_assets` tracks active media assets and object keys.
- Live usage should be validated against MinIO, not only database rows.

## Orphan Classes

- **Deleted-account orphan:** object prefix `account_id/` does not exist in `accounts`. Safe to delete after dry-run confirmation.
- **Active-account orphan:** object belongs to an existing account but has no known DB reference. Treat as candidate only.

Known references include messages, contact avatars, campaigns, campaign attachments,
document thumbnails, dynamic media, quick replies, saved stickers, survey uploads and
active `media_assets`.

## Safety Rules

- Always run a dry-run/count before deleting storage.
- Never delete active-account candidates without explicit confirmation plus an age rule.
- Never infer safety from filename alone; use account prefix and reference scan.
- Account purge may delete the full account prefix after the account deletion is confirmed.
- Do not print MinIO secrets, `.env`, signed URLs, cookies or JWTs.
- Prefer MinIO APIs/client commands over direct filesystem deletion.

## Verification

After cleanup:

1. Recount total objects and bytes.
2. Confirm deleted-account orphan count is `0` for the targeted prefixes.
3. Confirm active accounts still load storage pages.
4. Check backend/frontend logs and `/health`.
5. Report exactly how many objects and bytes were removed.
