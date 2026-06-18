package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/repository"
	"github.com/naperu/clarin/internal/whatsapp"
	"github.com/naperu/clarin/internal/ws"
	"github.com/naperu/clarin/pkg/cache"
)

const (
	autoWorkerCount  = 50  // global goroutine pool size
	autoQueueBuffer  = 500 // buffered job channel
	autoRateLimit    = 500 // max executions per hour per account
	autoRateTTL      = time.Hour
	autoDelay        = 30 * time.Second // scheduler tick for paused executions
	autoLogRetention = 30               // days to keep execution logs
)

// AutomationJob is the unit of work dispatched to a worker goroutine.
type AutomationJob struct {
	AutomationID uuid.UUID
	ExecutionID  uuid.UUID
	AccountID    uuid.UUID
}

// AutomationService runs the automation engine: triggers, worker pool, delay scheduler.
type AutomationService struct {
	repos  *repository.Repositories
	pool   *whatsapp.DevicePool
	hub    *ws.Hub
	cache  *cache.Cache
	jobs   chan AutomationJob
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewAutomationService creates the service and spawns the worker pool.
// Call Start() to begin processing.
func NewAutomationService(repos *repository.Repositories, pool *whatsapp.DevicePool, hub *ws.Hub, c *cache.Cache) *AutomationService {
	return &AutomationService{
		repos:  repos,
		pool:   pool,
		hub:    hub,
		cache:  c,
		jobs:   make(chan AutomationJob, autoQueueBuffer),
		stopCh: make(chan struct{}),
	}
}

// SetCache injects the Redis cache (called after NewServices when cache is available).
func (s *AutomationService) SetCache(c *cache.Cache) {
	s.cache = c
}

// Start launches all background goroutines. Call once from main.
func (s *AutomationService) Start() {
	// Worker pool
	for i := 0; i < autoWorkerCount; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	// Delay scheduler
	s.wg.Add(1)
	go s.delayScheduler()
	// Daily log purge
	s.wg.Add(1)
	go s.logPurger()
	log.Printf("[AUTO] ⚙️ Automation engine started: %d workers, scheduler tick %s", autoWorkerCount, autoDelay)
}

// Stop gracefully shuts down the engine.
func (s *AutomationService) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

// ─── Trigger entry points (called from API handlers) ─────────────────────────

// TriggerLeadCreated fires automations with trigger=lead_created for the given lead.
func (s *AutomationService) TriggerLeadCreated(ctx context.Context, accountID, leadID uuid.UUID) {
	s.fireTrigger(ctx, accountID, leadID, domain.AutoTriggerLeadCreated, nil)
}

// TriggerLeadStageChanged fires automations with trigger=lead_stage_changed.
// triggerConfig filter: {stage_id} — if set, only fire if stageID matches.
func (s *AutomationService) TriggerLeadStageChanged(ctx context.Context, accountID, leadID, stageID uuid.UUID) {
	s.fireTrigger(ctx, accountID, leadID, domain.AutoTriggerLeadStageChanged, map[string]interface{}{
		"stage_id": stageID.String(),
	})
}

// TriggerTagAssigned fires automations with trigger=tag_assigned.
func (s *AutomationService) TriggerTagAssigned(ctx context.Context, accountID, leadID, tagID uuid.UUID) {
	s.fireTrigger(ctx, accountID, leadID, domain.AutoTriggerTagAssigned, map[string]interface{}{
		"tag_id": tagID.String(),
	})
}

// TriggerTagRemoved fires automations with trigger=tag_removed.
func (s *AutomationService) TriggerTagRemoved(ctx context.Context, accountID, leadID, tagID uuid.UUID) {
	s.fireTrigger(ctx, accountID, leadID, domain.AutoTriggerTagRemoved, map[string]interface{}{
		"tag_id": tagID.String(),
	})
}

// TriggerMessageReceived fires automations with trigger=message_received if a keyword matches.
func (s *AutomationService) TriggerMessageReceived(ctx context.Context, accountID uuid.UUID, leadID *uuid.UUID, messageText string) {
	automations, err := s.repos.Automation.GetByTrigger(ctx, accountID, domain.AutoTriggerMessageReceived)
	if err != nil || len(automations) == 0 {
		return
	}
	for _, a := range automations {
		keyword, _ := a.TriggerConfig["keyword"].(string)
		if keyword != "" && !strings.Contains(strings.ToLower(messageText), strings.ToLower(keyword)) {
			continue
		}
		s.enqueueExecution(ctx, a, leadID, map[string]interface{}{"message": messageText})
	}
}

// TriggerManual fires an automation manually for a specific lead (or no lead).
func (s *AutomationService) TriggerManual(ctx context.Context, automationID, accountID uuid.UUID, leadID *uuid.UUID) error {
	a, err := s.repos.Automation.GetByID(ctx, automationID, accountID)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("automation not found")
	}
	s.enqueueExecution(ctx, a, leadID, map[string]interface{}{"triggered_by": "manual"})
	return nil
}

// ─── Internal trigger fan-out ─────────────────────────────────────────────────

func (s *AutomationService) fireTrigger(
	ctx context.Context,
	accountID, leadID uuid.UUID,
	triggerType string,
	eventData map[string]interface{},
) {
	// Skip automations for blocked leads
	lead, err := s.repos.Lead.GetByID(ctx, leadID)
	if err == nil && lead != nil && lead.IsBlocked {
		log.Printf("[AUTOMATION] Skipping trigger %s for blocked lead %s", triggerType, leadID)
		return
	}

	automations, err := s.repos.Automation.GetByTrigger(ctx, accountID, triggerType)
	if err != nil || len(automations) == 0 {
		return
	}
	for _, a := range automations {
		// Filter: stage_id or tag_id match if configured
		if !triggerMatchesConfig(a.TriggerConfig, eventData) {
			continue
		}
		lid := leadID
		s.enqueueExecution(ctx, a, &lid, eventData)
	}
}

// triggerMatchesConfig returns false if the automation has a filter that doesn't match the event.
func triggerMatchesConfig(config, eventData map[string]interface{}) bool {
	if eventData == nil {
		return true
	}
	// If automation requires a specific stage_id / tag_id, verify it matches
	for _, key := range []string{"stage_id", "tag_id"} {
		requiredVal, hasRequired := config[key].(string)
		if !hasRequired || requiredVal == "" {
			continue
		}
		eventVal, _ := eventData[key].(string)
		if eventVal != requiredVal {
			return false
		}
	}
	return true
}

// enqueueExecution validates rate limit, dedup, persists execution, and pushes to job queue.
func (s *AutomationService) enqueueExecution(
	ctx context.Context,
	a *domain.Automation,
	leadID *uuid.UUID,
	contextData map[string]interface{},
) {
	// Rate limit check via Redis (or in-memory fallback)
	if !s.checkRateLimit(ctx, a.AccountID) {
		log.Printf("[AUTO] Rate limit reached for account %s — skipping automation %s", a.AccountID, a.Name)
		return
	}

	// Deduplication: skip if identical execution is running/pending within 5 min
	if dup, err := s.repos.Automation.HasActiveExecutionRecent(ctx, a.ID, leadID); err == nil && dup {
		return
	}

	// Snapshot the config so Delay nodes aren't affected by future edits
	snapshot := a.Config

	exec := &domain.AutomationExecution{
		AutomationID:   a.ID,
		AccountID:      a.AccountID,
		LeadID:         leadID,
		Status:         domain.AutoExecPending,
		CurrentNodeID:  "",
		ConfigSnapshot: &snapshot,
		ContextData:    contextData,
	}
	if contextData == nil {
		exec.ContextData = map[string]interface{}{}
	}

	if err := s.repos.Automation.CreateExecution(ctx, exec); err != nil {
		log.Printf("[AUTO] Failed to create execution for automation %s: %v", a.ID, err)
		return
	}

	_ = s.repos.Automation.IncrementExecutionCount(ctx, a.ID)

	// Non-blocking enqueue (drop if queue full — won't happen at 100 accounts)
	select {
	case s.jobs <- AutomationJob{AutomationID: a.ID, ExecutionID: exec.ID, AccountID: a.AccountID}:
	default:
		log.Printf("[AUTO] ⚠️ Job queue full, dropped execution %s", exec.ID)
	}
}

// ─── Rate limiter (Redis INCR/EXPIRE per account per hour) ───────────────────

func (s *AutomationService) checkRateLimit(ctx context.Context, accountID uuid.UUID) bool {
	if s.cache == nil {
		return true // no Redis — allow all
	}
	key := fmt.Sprintf("rate:auto:%s", accountID.String())
	raw, err := s.cache.Get(ctx, key)
	if err != nil {
		return true // Redis error — allow
	}
	var count int
	if raw != nil {
		fmt.Sscanf(string(raw), "%d", &count)
	}
	if count >= autoRateLimit {
		return false
	}
	// Increment
	newCount := count + 1
	_ = s.cache.Set(ctx, key, []byte(fmt.Sprintf("%d", newCount)), autoRateTTL)
	return true
}

// ─── Worker pool ─────────────────────────────────────────────────────────────

func (s *AutomationService) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case job := <-s.jobs:
			s.processExecution(context.Background(), job.ExecutionID)
		}
	}
}

// processExecution loads the execution and walks the graph node by node.
func (s *AutomationService) processExecution(ctx context.Context, execID uuid.UUID) {
	exec, err := s.repos.Automation.GetExecutionByID(ctx, execID)
	if err != nil || exec == nil {
		log.Printf("[AUTO] Cannot load execution %s: %v", execID, err)
		return
	}
	if exec.Status != domain.AutoExecPending && exec.Status != domain.AutoExecRunning {
		return // already done/paused
	}

	exec.Status = domain.AutoExecRunning
	_ = s.repos.Automation.UpdateExecution(ctx, exec)

	graph := exec.ConfigSnapshot
	if graph == nil {
		s.failExecution(ctx, exec, "no config snapshot")
		return
	}

	// Find start node (trigger node has no incoming edges)
	startNodeID := exec.CurrentNodeID
	if startNodeID == "" {
		startNodeID = findStartNode(graph)
		if startNodeID == "" {
			s.failExecution(ctx, exec, "no start node found")
			return
		}
	}

	// Walk graph
	s.walkGraph(ctx, exec, graph, startNodeID)
}

// walkGraph traverses the automation graph starting from nodeID.
func (s *AutomationService) walkGraph(ctx context.Context, exec *domain.AutomationExecution, graph *domain.AutomationGraph, nodeID string) {
	visited := make(map[string]bool)
	current := nodeID

	for current != "" {
		if visited[current] {
			break // cycle guard
		}
		visited[current] = true

		node := findNode(graph, current)
		if node == nil {
			break
		}

		exec.CurrentNodeID = current
		_ = s.repos.Automation.UpdateExecution(ctx, exec)

		nodeType := getNodeDataString(node, "nodeType")

		// Skip trigger node (it's just the entry point)
		if node.Type == "trigger" || nodeType == "" {
			current = nextNode(graph, current, "")
			continue
		}

		start := time.Now()
		outHandle, execErr := s.executeNode(ctx, exec, node)
		elapsed := int(time.Since(start).Milliseconds())

		logEntry := &domain.AutomationExecutionLog{
			ExecutionID: exec.ID,
			NodeID:      node.ID,
			NodeType:    nodeType,
			DurationMs:  elapsed,
		}

		if execErr != nil {
			logEntry.Status = "failed"
			logEntry.Error = execErr.Error()
			_ = s.repos.Automation.AppendLog(ctx, logEntry)
			s.failExecution(ctx, exec, execErr.Error())
			return
		}

		// Delay node: pause execution
		if nodeType == domain.AutoNodeDelay {
			logEntry.Status = "success"
			_ = s.repos.Automation.AppendLog(ctx, logEntry)
			// exec is already paused inside executeNode
			return
		}

		logEntry.Status = "success"
		_ = s.repos.Automation.AppendLog(ctx, logEntry)

		// Advance to next node (condition uses outHandle "true"/"false")
		current = nextNode(graph, current, outHandle)
	}

	// All nodes executed — mark complete
	now := time.Now()
	exec.Status = domain.AutoExecCompleted
	exec.CompletedAt = &now
	exec.CurrentNodeID = ""
	_ = s.repos.Automation.UpdateExecution(ctx, exec)

	s.broadcastUpdate(exec.AccountID, exec.ID, domain.AutoExecCompleted)
}

// executeNode dispatches to the correct action handler.
// Returns outHandle ("true"/"false" for condition, "" for others) and any error.
func (s *AutomationService) executeNode(ctx context.Context, exec *domain.AutomationExecution, node *domain.AutomationNode) (string, error) {
	nodeType := getNodeDataString(node, "nodeType")
	cfg := getNodeDataMap(node, "config")

	switch nodeType {
	case domain.AutoNodeSendWhatsApp:
		return "", s.execSendWhatsApp(ctx, exec, cfg)

	case domain.AutoNodeChangeStage:
		return "", s.execChangeStage(ctx, exec, cfg)

	case domain.AutoNodeAssignTag:
		return "", s.execAssignTag(ctx, exec, cfg, true)

	case domain.AutoNodeRemoveTag:
		return "", s.execAssignTag(ctx, exec, cfg, false)

	case domain.AutoNodeDelay:
		return "", s.execDelay(ctx, exec, node, cfg)

	case domain.AutoNodeCondition:
		result, err := s.execCondition(ctx, exec, cfg)
		if err != nil {
			return "", err
		}
		if result {
			return "true", nil
		}
		return "false", nil

	default:
		return "", fmt.Errorf("unknown node type: %s", nodeType)
	}
}

// ─── Node executors ──────────────────────────────────────────────────────────

func (s *AutomationService) execSendWhatsApp(ctx context.Context, exec *domain.AutomationExecution, cfg map[string]interface{}) error {
	if s.pool == nil {
		return fmt.Errorf("whatsapp pool not available")
	}
	if exec.LeadID == nil {
		return fmt.Errorf("no lead_id for send_whatsapp action")
	}

	deviceIDStr, _ := cfg["device_id"].(string)
	message, _ := cfg["message"].(string)
	if message == "" {
		return fmt.Errorf("send_whatsapp: empty message")
	}

	// Get lead phone
	lead, err := s.repos.Lead.GetByID(ctx, *exec.LeadID)
	if err != nil || lead == nil {
		return fmt.Errorf("lead not found")
	}
	if lead.AccountID != exec.AccountID {
		return fmt.Errorf("lead does not belong to automation account")
	}
	if lead.JID == "" || strings.HasPrefix(lead.JID, "manual_") {
		return fmt.Errorf("lead has no phone number for whatsapp")
	}

	// Resolve device
	deviceID, err := uuid.Parse(deviceIDStr)
	if err != nil {
		return fmt.Errorf("invalid device_id")
	}
	device, err := s.repos.Device.GetByID(ctx, deviceID)
	if err != nil {
		return err
	}
	if device == nil || device.AccountID != exec.AccountID {
		return fmt.Errorf("device not found for automation account")
	}
	if device.Provider != nil && *device.Provider == domain.DeviceProviderWhatsAppCloudAPI {
		return fmt.Errorf("cloud api device cannot send automation whatsapp yet")
	}

	// Send message via device pool
	_, err = s.pool.SendMessage(ctx, deviceID, lead.JID, message)
	return err
}

func (s *AutomationService) execChangeStage(ctx context.Context, exec *domain.AutomationExecution, cfg map[string]interface{}) error {
	if exec.LeadID == nil {
		return fmt.Errorf("no lead_id for change_stage action")
	}
	stageIDStr, _ := cfg["stage_id"].(string)
	stageID, err := uuid.Parse(stageIDStr)
	if err != nil {
		return fmt.Errorf("invalid stage_id")
	}
	if err := s.repos.Lead.UpdateStage(ctx, *exec.LeadID, stageID); err != nil {
		return err
	}
	if s.hub != nil {
		s.hub.BroadcastToAccount(exec.AccountID, ws.EventLeadUpdate, map[string]interface{}{
			"action":   "stage_changed",
			"lead_id":  exec.LeadID.String(),
			"stage_id": stageID.String(),
		})
	}
	return nil
}

func (s *AutomationService) execAssignTag(ctx context.Context, exec *domain.AutomationExecution, cfg map[string]interface{}, assign bool) error {
	if exec.LeadID == nil {
		return fmt.Errorf("no lead_id for tag action")
	}
	tagIDStr, _ := cfg["tag_id"].(string)
	tagID, err := uuid.Parse(tagIDStr)
	if err != nil {
		return fmt.Errorf("invalid tag_id")
	}
	if assign {
		return s.repos.Tag.AssignToLead(ctx, *exec.LeadID, tagID)
	}
	return s.repos.Tag.RemoveFromLead(ctx, *exec.LeadID, tagID)
}

func (s *AutomationService) execDelay(ctx context.Context, exec *domain.AutomationExecution, node *domain.AutomationNode, cfg map[string]interface{}) error {
	minutes := 0
	if v, ok := cfg["duration_minutes"].(float64); ok {
		minutes = int(v)
	}
	if minutes <= 0 {
		minutes = 1
	}
	// Find next node to resume from after the delay
	nextNodeID := nextNode(exec.ConfigSnapshot, node.ID, "")

	resumeAt := time.Now().Add(time.Duration(minutes) * time.Minute)
	exec.Status = domain.AutoExecPaused
	exec.NextNodeID = nextNodeID
	exec.ResumeAt = &resumeAt
	_ = s.repos.Automation.UpdateExecution(ctx, exec)
	log.Printf("[AUTO] ⏸️ Execution %s paused, resuming in %d min at %s", exec.ID, minutes, resumeAt.Format(time.RFC3339))
	return nil
}

func (s *AutomationService) execCondition(ctx context.Context, exec *domain.AutomationExecution, cfg map[string]interface{}) (bool, error) {
	if exec.LeadID == nil {
		return false, fmt.Errorf("no lead_id for condition")
	}
	field, _ := cfg["field"].(string)
	operator, _ := cfg["operator"].(string)
	value, _ := cfg["value"].(string)

	lead, err := s.repos.Lead.GetByID(ctx, *exec.LeadID)
	if err != nil || lead == nil {
		return false, fmt.Errorf("lead not found")
	}

	return evaluateCondition(lead, field, operator, value), nil
}

// evaluateCondition checks a lead field against operator+value.
func evaluateCondition(lead *domain.Lead, field, operator, value string) bool {
	var fieldVal string
	switch field {
	case "stage_id":
		if lead.StageID != nil {
			fieldVal = lead.StageID.String()
		}
	case "pipeline_id":
		if lead.PipelineID != nil {
			fieldVal = lead.PipelineID.String()
		}
	case "phone":
		if lead.Phone != nil {
			fieldVal = *lead.Phone
		}
	case "email":
		if lead.Email != nil {
			fieldVal = *lead.Email
		}
	case "name":
		if lead.Name != nil {
			fieldVal = *lead.Name
		}
	case "source":
		if lead.Source != nil {
			fieldVal = *lead.Source
		}
	default:
		// Check custom fields
		if lead.CustomFields != nil {
			if v, ok := lead.CustomFields[field]; ok {
				fieldVal = fmt.Sprintf("%v", v)
			}
		}
	}

	switch operator {
	case "eq", "equals":
		return strings.EqualFold(fieldVal, value)
	case "neq", "not_equals":
		return !strings.EqualFold(fieldVal, value)
	case "contains":
		return strings.Contains(strings.ToLower(fieldVal), strings.ToLower(value))
	case "starts_with":
		return strings.HasPrefix(strings.ToLower(fieldVal), strings.ToLower(value))
	case "empty":
		return fieldVal == ""
	case "not_empty":
		return fieldVal != ""
	default:
		return false
	}
}

// ─── Delay scheduler ─────────────────────────────────────────────────────────

func (s *AutomationService) delayScheduler() {
	defer s.wg.Done()
	ticker := time.NewTicker(autoDelay)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.resumePausedExecutions()
		}
	}
}

func (s *AutomationService) resumePausedExecutions() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	paused, err := s.repos.Automation.GetPausedDue(ctx)
	if err != nil || len(paused) == 0 {
		return
	}
	log.Printf("[AUTO] Scheduler resuming %d paused executions", len(paused))

	for _, exec := range paused {
		// Mark as pending again and set current_node_id to next_node_id
		exec.Status = domain.AutoExecPending
		exec.CurrentNodeID = exec.NextNodeID
		exec.NextNodeID = ""
		exec.ResumeAt = nil
		_ = s.repos.Automation.UpdateExecution(ctx, exec)

		select {
		case s.jobs <- AutomationJob{AutomationID: exec.AutomationID, ExecutionID: exec.ID, AccountID: exec.AccountID}:
		default:
			log.Printf("[AUTO] ⚠️ Queue full, delay resume dropped for %s", exec.ID)
		}
	}
}

// ─── Log purger ──────────────────────────────────────────────────────────────

func (s *AutomationService) logPurger() {
	defer s.wg.Done()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = s.repos.Automation.PurgeOldLogs(ctx, autoLogRetention)
			cancel()
		}
	}
}

// ─── Helper: broadcast execution state via WebSocket ─────────────────────────

func (s *AutomationService) broadcastUpdate(accountID, execID uuid.UUID, status string) {
	if s.hub == nil {
		return
	}
	s.hub.BroadcastToAccount(accountID, "automation_execution_update", map[string]interface{}{
		"execution_id": execID.String(),
		"status":       status,
	})
}

func (s *AutomationService) failExecution(ctx context.Context, exec *domain.AutomationExecution, reason string) {
	now := time.Now()
	exec.Status = domain.AutoExecFailed
	exec.ErrorMessage = reason
	exec.CompletedAt = &now
	_ = s.repos.Automation.UpdateExecution(ctx, exec)
	s.broadcastUpdate(exec.AccountID, exec.ID, domain.AutoExecFailed)
	log.Printf("[AUTO] ❌ Execution %s failed: %s", exec.ID, reason)
}

// ─── Graph traversal helpers ─────────────────────────────────────────────────

func findStartNode(graph *domain.AutomationGraph) string {
	// The trigger node has no incoming edges
	hasIncoming := make(map[string]bool)
	for _, e := range graph.Edges {
		hasIncoming[e.Target] = true
	}
	for _, n := range graph.Nodes {
		if !hasIncoming[n.ID] {
			return n.ID
		}
	}
	return ""
}

func findNode(graph *domain.AutomationGraph, nodeID string) *domain.AutomationNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == nodeID {
			return &graph.Nodes[i]
		}
	}
	return nil
}

// nextNode finds the target node following the edge from sourceID with optional sourceHandle.
func nextNode(graph *domain.AutomationGraph, sourceID, sourceHandle string) string {
	for _, e := range graph.Edges {
		if e.Source != sourceID {
			continue
		}
		if sourceHandle == "" || e.SourceHandle == sourceHandle || e.SourceHandle == "" {
			return e.Target
		}
	}
	return ""
}

func getNodeDataString(node *domain.AutomationNode, key string) string {
	if node.Data == nil {
		return ""
	}
	v, _ := node.Data[key].(string)
	return v
}

func getNodeDataMap(node *domain.AutomationNode, key string) map[string]interface{} {
	if node.Data == nil {
		return nil
	}
	v, _ := node.Data[key].(map[string]interface{})
	return v
}
