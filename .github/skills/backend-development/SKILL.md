---
name: backend-development
description: Build high-performance, optimized Go/Fiber backend services for Clarin CRM. Use when modifying API handlers, repository queries, services, domain entities, database migrations, local Kommo Excel import, or WhatsApp integration. Enforces performance-first patterns, efficient SQL, contact/lead/chat integrity, caching, and zero-waste architecture.
---

# Backend Development — Clarin CRM

## Philosophy: Performance First

Every endpoint must be fast. Think sub-50ms for reads, sub-200ms for writes. Never accept slow queries, N+1 problems, or wasteful memory allocations. Profile mentally before coding.

## Stack
- Go 1.25, Fiber v2.52, pgx v5 (PostgreSQL), Redis v9
- No local Go compiler — build via `docker compose build backend`

## Data Integrity Rules

- `Contact` is the parent entity. `Lead` and `Chat` are parallel children.
- Creating a lead or chat must ensure a valid contact in the same account first.
- Deleting a contact deletes its leads, chats and chat messages in an explicit backend transaction.
- Deleting a lead must not delete its contact or chat.
- Deleting a chat must not delete its contact or lead, but it does delete its messages.
- Always validate `account_id` before reads, writes, batch deletes and repair jobs.
- If repairing orphan leads/chats, reconnect by normalized phone only within the same account; delete only when no matching contact exists.

## Project Layout

```
backend/
  cmd/server/main.go              → Entry point, service initialization
  internal/
    api/server.go                  → HTTP handlers (Fiber), REST routes + WebSocket (~4300 lines)
    domain/entities.go             → Domain structs (Lead, Contact, Chat, Tag, etc.)
    repository/repository.go       → Data access layer (pgx/PostgreSQL)
    service/service.go             → Business logic
    kommo/
      client.go                    → Dormant API client kept for Kommo compatibility
      sync.go                      → Dormant sync code; local Excel import is the active flow
    whatsapp/device_pool.go        → WhatsApp multi-device pool (whatsmeow)
    ws/hub.go                      → WebSocket hub for real-time broadcasts
  pkg/
    config/config.go               → Environment variables
    database/database.go           → DB connection + migrations (InitDB)
```

---

## Performance Patterns

### 1. SQL Queries — Efficient and Indexed

```go
// ALWAYS parameterized — NEVER concatenate strings
row := db.QueryRow(ctx, `SELECT id, name FROM leads WHERE phone = $1 AND account_id = $2`, phone, accountID)

// SELECT only the columns you need — NEVER SELECT *
// BAD:
rows, _ := db.Query(ctx, `SELECT * FROM leads WHERE account_id = $1`, accountID)
// GOOD:
rows, _ := db.Query(ctx, `SELECT id, name, phone, status FROM leads WHERE account_id = $1`, accountID)

// Use LIMIT for paginated queries
rows, _ := db.Query(ctx, `SELECT id, name FROM leads WHERE account_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, accountID, limit, offset)

// Batch operations with single queries instead of loops
// BAD — N queries in a loop:
for _, tag := range tags {
    db.Exec(ctx, `INSERT INTO lead_tags ...`, leadID, tag.ID)
}
// GOOD — single batch with unnest or VALUES:
db.Exec(ctx, `
    INSERT INTO lead_tags (lead_id, tag_id)
    SELECT $1, unnest($2::bigint[])
    ON CONFLICT DO NOTHING
`, leadID, tagIDs)
```

### 2. Avoid N+1 Queries

```go
// BAD — N+1: one query per lead to get tags
leads, _ := repo.GetLeads(ctx, accountID)
for _, lead := range leads {
    lead.Tags, _ = repo.GetLeadTags(ctx, lead.ID) // N extra queries!
}

// GOOD — JOIN or subquery in a single query
rows, _ := db.Query(ctx, `
    SELECT l.id, l.name, l.phone,
           COALESCE(array_agg(t.name) FILTER (WHERE t.name IS NOT NULL), '{}') as tags
    FROM leads l
    LEFT JOIN lead_tags lt ON lt.lead_id = l.id
    LEFT JOIN tags t ON t.id = lt.tag_id
    WHERE l.account_id = $1
    GROUP BY l.id
    ORDER BY l.created_at DESC
`, accountID)

// ALTERNATIVE — batch load with IN clause
tagsByLead, _ := repo.GetTagsForLeadIDs(ctx, leadIDs) // single query with WHERE lead_id = ANY($1)
```

### 3. Database Indexing — Always think about it

```go
// When adding migrations, always consider indexes for:
// - Columns used in WHERE clauses
// - Columns used in JOIN conditions
// - Columns used in ORDER BY with large tables
// - Foreign keys

// In InitDB():
_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_leads_account_id ON leads(account_id)`)
_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_leads_phone ON leads(phone)`)
_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_messages_chat_id_created ON messages(chat_id, created_at DESC)`)
_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_chats_account_id ON chats(account_id)`)

// Composite indexes for common query patterns:
_, _ = db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_leads_account_status ON leads(account_id, status)`)
```

### 4. Redis Caching

```go
// Cache expensive or frequently-accessed data
// Pattern: Check cache → miss → query DB → set cache

func (s *Service) GetLeadCached(ctx context.Context, accountID, leadID int64) (*domain.Lead, error) {
    key := fmt.Sprintf("lead:%d:%d", accountID, leadID)

    // Try cache first
    cached, err := s.redis.Get(ctx, key).Result()
    if err == nil {
        var lead domain.Lead
        json.Unmarshal([]byte(cached), &lead)
        return &lead, nil
    }

    // Cache miss — query DB
    lead, err := s.repo.GetLead(ctx, accountID, leadID)
    if err != nil {
        return nil, err
    }

    // Set cache with TTL
    data, _ := json.Marshal(lead)
    s.redis.Set(ctx, key, data, 5*time.Minute)
    return lead, nil
}

// ALWAYS invalidate cache on write:
func (s *Service) UpdateLead(ctx context.Context, lead *domain.Lead) error {
    err := s.repo.UpdateLead(ctx, lead)
    if err != nil {
        return err
    }
    s.redis.Del(ctx, fmt.Sprintf("lead:%d:%d", lead.AccountID, lead.ID))
    return nil
}
```

### 5. Concurrency — Goroutines for Independent I/O

```go
// When you need multiple independent queries, run them concurrently
var (
    leads    []domain.Lead
    contacts []domain.Contact
    errG     errgroup.Group
)

errG.Go(func() error {
    var err error
    leads, err = repo.GetLeads(ctx, accountID)
    return err
})
errG.Go(func() error {
    var err error
    contacts, err = repo.GetContacts(ctx, accountID)
    return err
})

if err := errG.Wait(); err != nil {
    return c.Status(500).JSON(fiber.Map{"error": "internal error"})
}
```

### 6. Response Optimization

```go
// Use pagination for all list endpoints
func (s *Server) handleGetLeads(c *fiber.Ctx) error {
    accountID := c.Locals("account_id").(int64)
    page := c.QueryInt("page", 1)
    limit := c.QueryInt("limit", 50)
    if limit > 200 {
        limit = 200 // Cap maximum
    }
    offset := (page - 1) * limit

    leads, total, err := s.repo.GetLeadsPaginated(ctx, accountID, limit, offset)
    if err != nil {
        return c.Status(500).JSON(fiber.Map{"error": "internal error"})
    }

    return c.JSON(fiber.Map{
        "data":  leads,
        "total": total,
        "page":  page,
        "limit": limit,
    })
}

// Omit empty/null fields with omitempty
type Lead struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Phone string `json:"phone,omitempty"`
    Notes string `json:"notes,omitempty"`
}
```

### 7. Connection Pool — Already configured, respect it

```go
// pgx pool is configured in database.go — don't create new connections
// Use the pool's context-aware methods:
db.QueryRow(ctx, ...)  // single row
db.Query(ctx, ...)     // multiple rows
db.Exec(ctx, ...)      // commands (INSERT, UPDATE, DELETE)

// ALWAYS use context with timeout for external calls:
ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
defer cancel()
```

### 8. Memory Efficiency

```go
// Pre-allocate slices when you know the size
leads := make([]domain.Lead, 0, expectedCount)

// Use strings.Builder for string concatenation
var b strings.Builder
for _, tag := range tags {
    b.WriteString(tag.Name)
    b.WriteString(",")
}

// Close database rows ALWAYS
rows, err := db.Query(ctx, query, args...)
if err != nil {
    return err
}
defer rows.Close()

// Scan directly into struct fields — no intermediate maps
for rows.Next() {
    var lead domain.Lead
    err := rows.Scan(&lead.ID, &lead.Name, &lead.Phone)
    // ...
}
```

---

## Code Conventions

### Error Handling — ALWAYS check, ALWAYS log
```go
result, err := repo.GetLead(ctx, id)
if err != nil {
    log.Printf("[API] Error getting lead %d: %v", id, err)
    return c.Status(500).JSON(fiber.Map{"error": "internal error"})
}
```

### Logging with Prefixes
```go
log.Printf("[API] Creating lead for account %d", accountID)
log.Printf("[IMPORT] Processing %d Kommo Excel rows", len(rows))
log.Printf("[WHATSAPP] Device %s connected", deviceID)
log.Printf("[WS] Broadcasting to account %d", accountID)
```

### Fiber Handler Pattern
```go
func (s *Server) handleGetLeads(c *fiber.Ctx) error {
    accountID := c.Locals("account_id").(int64)
    leads, err := s.repo.GetLeads(ctx, accountID)
    if err != nil {
        return c.Status(500).JSON(fiber.Map{"error": "internal error"})
    }
    return c.JSON(leads)
}
```

### Import Grouping
```go
import (
    // stdlib
    "context"
    "fmt"
    "log"

    // third-party
    "github.com/gofiber/fiber/v2"
    "github.com/jackc/pgx/v5/pgxpool"

    // internal
    "clarin/internal/domain"
    "clarin/internal/repository"
)
```

### Phone Normalization
```go
// ALWAYS use kommo.NormalizePhone() for phone numbers
normalized := kommo.NormalizePhone(rawPhone)
```

---

## Checklist for Every Backend Change

1. **Is the SQL efficient?** No SELECT *, no N+1, proper JOINs, indexes considered.
2. **Is it parameterized?** NEVER concatenate user input into SQL.
3. **Are errors handled?** Every `err` checked and logged.
4. **Is context propagated?** `context.Context` passed to all DB/HTTP operations.
5. **Should this be cached?** Frequently accessed, expensive queries → Redis.
6. **Is pagination added?** List endpoints must support limit/offset.
7. **Should WebSocket broadcast?** If data changed that frontend displays → broadcast.

## Adding a New API Endpoint

1. Define entity in `domain/entities.go` if needed
2. Add repository method in `repository/repository.go` — efficient query, proper indexes
3. Add business logic in `service/service.go` if needed — consider caching
4. Add handler in `api/server.go` — pagination, error handling, validation
5. Register route in `setupRoutes()`
6. Build and verify: `docker compose build backend && docker compose up -d`

## WebSocket Broadcasting

After any operation that changes data visible to the frontend:
```go
if s.hub != nil {
    s.hub.BroadcastToAccount(accountID, ws.EventLeadUpdate, map[string]interface{}{
        "action": "updated",
    })
}
```
