package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/naperu/clarin/internal/domain"
	"github.com/naperu/clarin/internal/kommo"
	"github.com/naperu/clarin/internal/ws"
)

// ─── Protected Dynamic Handlers ──────────────────────────────────────────────

func (s *Server) handleListDynamics(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	// Redis cache — 30s TTL
	dynamicsCacheKey := ""
	if s.cache != nil {
		dynamicsCacheKey = fmt.Sprintf("dynamics:%s:all", accountID.String())
		if cached, err := s.cache.Get(c.Context(), dynamicsCacheKey); err == nil && cached != nil {
			c.Set("Content-Type", "application/json")
			return c.Send(cached)
		}
	}

	dynamics, err := s.repos.Dynamic.List(c.Context(), accountID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if dynamics == nil {
		dynamics = []*domain.Dynamic{}
	}

	if dynamicsCacheKey != "" && s.cache != nil {
		if data, err := json.Marshal(dynamics); err == nil {
			_ = s.cache.Set(c.Context(), dynamicsCacheKey, data, 30*time.Second)
		}
	}

	return c.JSON(dynamics)
}

func (s *Server) handleCreateDynamic(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)

	var req struct {
		Name        string               `json:"name"`
		Type        string               `json:"type"`
		Slug        string               `json:"slug"`
		Description string               `json:"description"`
		Config      domain.DynamicConfig `json:"config"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if req.Type == "" {
		req.Type = "scratch_card"
	}

	d := &domain.Dynamic{
		AccountID:   accountID,
		Type:        req.Type,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Config:      req.Config,
	}

	if err := s.repos.Dynamic.Create(c.Context(), d); err != nil {
		if strings.Contains(err.Error(), "uq_dynamics_slug") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El slug ya está en uso"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	s.invalidateDynamicsCache(accountID)
	return c.Status(fiber.StatusCreated).JSON(d)
}

func (s *Server) handleGetDynamic(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}

	d, err := s.repos.Dynamic.GetByID(c.Context(), id, accountID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}
	return c.JSON(d)
}

func (s *Server) handleUpdateDynamic(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}

	var req struct {
		Name        string               `json:"name"`
		Slug        string               `json:"slug"`
		Description string               `json:"description"`
		Config      domain.DynamicConfig `json:"config"`
		IsActive    bool                 `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	d := &domain.Dynamic{
		ID:          id,
		AccountID:   accountID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Config:      req.Config,
		IsActive:    req.IsActive,
	}

	if err := s.repos.Dynamic.Update(c.Context(), d); err != nil {
		if strings.Contains(err.Error(), "uq_dynamics_slug") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El slug ya está en uso"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	s.invalidateDynamicsCache(accountID)
	return c.JSON(d)
}

func (s *Server) handleDeleteDynamic(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}

	if err := s.repos.Dynamic.Delete(c.Context(), id, accountID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	s.invalidateDynamicsCache(accountID)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleSetDynamicActive(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}

	var req struct {
		IsActive bool `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := s.repos.Dynamic.SetActive(c.Context(), id, accountID, req.IsActive); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	s.invalidateDynamicsCache(accountID)
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleCheckDynamicSlug(c *fiber.Ctx) error {
	var req struct {
		Slug      string     `json:"slug"`
		ExcludeID *uuid.UUID `json:"exclude_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	exists, err := s.repos.Dynamic.SlugExists(c.Context(), req.Slug, req.ExcludeID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"exists": exists})
}

// ─── Dynamic Items ───────────────────────────────────────────────────────────

func (s *Server) handleListDynamicItems(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	items, err := s.repos.Dynamic.ListItems(c.Context(), dynamicID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if items == nil {
		items = []*domain.DynamicItem{}
	}
	return c.JSON(items)
}

func (s *Server) handleCreateDynamicItem(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}

	// Verify ownership
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	// Handle multipart file upload
	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Image file is required"})
	}

	// Validate file type
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	if !allowedExts[ext] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Only image files are allowed (jpg, png, gif, webp)"})
	}

	// Upload to MinIO
	f, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open file"})
	}
	defer f.Close()

	folder := fmt.Sprintf("dynamics/%s", dynamicID.String())
	fileName := fmt.Sprintf("%s%s", uuid.New().String(), ext)
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	_, err = s.storage.UploadReader(c.Context(), accountID, folder, fileName, f, file.Size, contentType)
	if err != nil {
		log.Printf("[DYNAMIC] Failed to upload image: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to upload image"})
	}

	// Use proxy URL so images load through the backend media proxy
	proxyURL := fmt.Sprintf("/api/media/file/%s/dynamics/%s/%s", accountID.String(), dynamicID.String(), fileName)

	thoughtText := c.FormValue("thought_text", "")
	author := c.FormValue("author", "")
	tipo := c.FormValue("tipo", "")

	item := &domain.DynamicItem{
		DynamicID:   dynamicID,
		ImageURL:    proxyURL,
		ThoughtText: thoughtText,
		Author:      author,
		Tipo:        tipo,
		FileSize:    file.Size,
		SortOrder:   0,
		IsActive:    true,
		OptionIDs:   []uuid.UUID{},
	}

	if err := s.repos.Dynamic.CreateItem(c.Context(), item); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

func (s *Server) handleUpdateDynamicItem(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	itemID, err := uuid.Parse(c.Params("itemId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid item ID"})
	}

	// Verify ownership
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		ThoughtText string `json:"thought_text"`
		Author      string `json:"author"`
		Tipo        string `json:"tipo"`
		IsActive    bool   `json:"is_active"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	item := &domain.DynamicItem{
		ID:          itemID,
		DynamicID:   dynamicID,
		ThoughtText: req.ThoughtText,
		Author:      req.Author,
		Tipo:        req.Tipo,
		IsActive:    req.IsActive,
	}

	// Keep existing image_url — only update text fields and is_active
	existing, err := s.repos.Dynamic.ListItems(c.Context(), dynamicID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	for _, e := range existing {
		if e.ID == itemID {
			item.ImageURL = e.ImageURL
			item.SortOrder = e.SortOrder
			break
		}
	}

	if err := s.repos.Dynamic.UpdateItem(c.Context(), item); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(item)
}

func (s *Server) handleDeleteDynamicItem(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	itemID, err := uuid.Parse(c.Params("itemId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid item ID"})
	}

	// Verify ownership
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	if err := s.repos.Dynamic.DeleteItem(c.Context(), itemID, dynamicID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleBulkDeleteDynamicItems(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}

	// Verify ownership
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var body struct {
		ItemIDs []uuid.UUID `json:"item_ids"`
	}
	if err := c.BodyParser(&body); err != nil || len(body.ItemIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "item_ids is required"})
	}

	if err := s.repos.Dynamic.DeleteItems(c.Context(), dynamicID, body.ItemIDs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleReorderDynamicItems(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}

	// Verify ownership
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		ItemIDs []uuid.UUID `json:"item_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := s.repos.Dynamic.ReorderItems(c.Context(), dynamicID, req.ItemIDs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ─── Item ↔ Options (many-to-many) ──────────────────────────────────────────

func (s *Server) handleSetItemOptions(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	itemID, err := uuid.Parse(c.Params("itemId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid item ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		OptionIDs []uuid.UUID `json:"option_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if req.OptionIDs == nil {
		req.OptionIDs = []uuid.UUID{}
	}

	if err := s.repos.Dynamic.SetItemOptions(c.Context(), itemID, req.OptionIDs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (s *Server) handleBulkAssignOption(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		ItemIDs  []uuid.UUID `json:"item_ids"`
		OptionID uuid.UUID   `json:"option_id"`
		Action   string      `json:"action"` // "add" or "remove"
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if len(req.ItemIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "item_ids is required"})
	}

	add := req.Action != "remove"
	if err := s.repos.Dynamic.BulkAssignOption(c.Context(), req.ItemIDs, req.OptionID, add); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ─── Public Dynamic Handler ──────────────────────────────────────────────────

func (s *Server) handleGetPublicDynamic(c *fiber.Ctx) error {
	slug := c.Params("slug")

	// Try resolving via dynamic_links first
	link, d, err := s.repos.Dynamic.GetLinkBySlug(c.Context(), slug)
	if err != nil {
		// Fallback: try legacy dynamics.slug
		d2, err2 := s.repos.Dynamic.GetBySlug(c.Context(), slug)
		if err2 != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
		}
		d = d2
		link = nil
	}

	items, err := s.repos.Dynamic.ListActiveItems(c.Context(), d.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if items == nil {
		items = []*domain.DynamicItem{}
	}

	options, err := s.repos.Dynamic.ListOptions(c.Context(), d.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if options == nil {
		options = []*domain.DynamicOption{}
	}

	result := fiber.Map{
		"dynamic": d,
		"items":   items,
		"options": options,
	}
	if link != nil {
		result["link"] = link
	}
	return c.JSON(result)
}

// ─── Dynamic Options Handlers ────────────────────────────────────────────────

func (s *Server) handleListDynamicOptions(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	options, err := s.repos.Dynamic.ListOptions(c.Context(), dynamicID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if options == nil {
		options = []*domain.DynamicOption{}
	}
	return c.JSON(options)
}

func (s *Server) handleCreateDynamicOption(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	opt := &domain.DynamicOption{
		DynamicID: dynamicID,
		Name:      req.Name,
		Emoji:     req.Emoji,
	}
	if err := s.repos.Dynamic.CreateOption(c.Context(), opt); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(opt)
}

func (s *Server) handleUpdateDynamicOption(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	optionID, err := uuid.Parse(c.Params("optionId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid option ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	opt := &domain.DynamicOption{
		ID:        optionID,
		DynamicID: dynamicID,
		Name:      req.Name,
		Emoji:     req.Emoji,
	}
	if err := s.repos.Dynamic.UpdateOption(c.Context(), opt); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(opt)
}

func (s *Server) handleDeleteDynamicOption(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	optionID, err := uuid.Parse(c.Params("optionId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid option ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	if err := s.repos.Dynamic.DeleteOption(c.Context(), optionID, dynamicID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleReorderDynamicOptions(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		OptionIDs []uuid.UUID `json:"option_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := s.repos.Dynamic.ReorderOptions(c.Context(), dynamicID, req.OptionIDs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ─── Dynamic Links Handlers ──────────────────────────────────────────────────

func (s *Server) handleListDynamicLinks(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}
	links, err := s.repos.Dynamic.ListLinks(c.Context(), dynamicID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if links == nil {
		links = []*domain.DynamicLink{}
	}
	return c.JSON(links)
}

func (s *Server) handleCreateDynamicLink(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		Slug             string  `json:"slug"`
		WhatsAppEnabled  bool    `json:"whatsapp_enabled"`
		WhatsAppMessage  string  `json:"whatsapp_message"`
		ExtraMessageText string  `json:"extra_message_text"`
		StartsAt         *string `json:"starts_at"`
		EndsAt           *string `json:"ends_at"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	link := &domain.DynamicLink{
		DynamicID:        dynamicID,
		Slug:             req.Slug,
		WhatsAppEnabled:  req.WhatsAppEnabled,
		WhatsAppMessage:  req.WhatsAppMessage,
		ExtraMessageText: req.ExtraMessageText,
		IsActive:         true,
	}
	if req.StartsAt != nil && *req.StartsAt != "" {
		if t, err := time.Parse(time.RFC3339, *req.StartsAt); err == nil {
			link.StartsAt = &t
		}
	}
	if req.EndsAt != nil && *req.EndsAt != "" {
		if t, err := time.Parse(time.RFC3339, *req.EndsAt); err == nil {
			link.EndsAt = &t
		}
	}
	if err := s.repos.Dynamic.CreateLink(c.Context(), link); err != nil {
		if strings.Contains(err.Error(), "uq_dynamic_links_slug") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El slug ya está en uso"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(link)
}

func (s *Server) handleUpdateDynamicLink(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	var req struct {
		Slug             string  `json:"slug"`
		WhatsAppEnabled  bool    `json:"whatsapp_enabled"`
		WhatsAppMessage  string  `json:"whatsapp_message"`
		ExtraMessageText string  `json:"extra_message_text"`
		IsActive         bool    `json:"is_active"`
		StartsAt         *string `json:"starts_at"`
		EndsAt           *string `json:"ends_at"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// Get current link to preserve media fields
	existing, _, err := s.repos.Dynamic.GetLinkByID(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Link not found"})
	}

	link := &domain.DynamicLink{
		ID:                    linkID,
		DynamicID:             dynamicID,
		Slug:                  req.Slug,
		WhatsAppEnabled:       req.WhatsAppEnabled,
		WhatsAppMessage:       req.WhatsAppMessage,
		ExtraMessageText:      req.ExtraMessageText,
		ExtraMessageMediaURL:  existing.ExtraMessageMediaURL,
		ExtraMessageMediaType: existing.ExtraMessageMediaType,
		IsActive:              req.IsActive,
	}
	if req.StartsAt != nil && *req.StartsAt != "" {
		if t, err := time.Parse(time.RFC3339, *req.StartsAt); err == nil {
			link.StartsAt = &t
		}
	}
	if req.EndsAt != nil && *req.EndsAt != "" {
		if t, err := time.Parse(time.RFC3339, *req.EndsAt); err == nil {
			link.EndsAt = &t
		}
	}
	if err := s.repos.Dynamic.UpdateLink(c.Context(), link); err != nil {
		if strings.Contains(err.Error(), "uq_dynamic_links_slug") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "El slug ya está en uso"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(link)
}

func (s *Server) handleDeleteDynamicLink(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	// Don't allow deleting the last link
	count, err := s.repos.Dynamic.CountLinks(c.Context(), dynamicID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if count <= 1 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No se puede eliminar el último link"})
	}

	if err := s.repos.Dynamic.DeleteLink(c.Context(), linkID, dynamicID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleUploadLinkExtraMedia(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	file, err := c.FormFile("media")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Media file is required"})
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string]string{
		".jpg": "image", ".jpeg": "image", ".png": "image", ".gif": "image", ".webp": "image",
		".mp4": "video",
	}
	mediaType, ok := allowedExts[ext]
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Solo se permiten imágenes (jpg, png, gif, webp) o videos (mp4)"})
	}

	// Video max 3MB
	if mediaType == "video" && file.Size > 3*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El video no debe superar los 3MB"})
	}

	f, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open file"})
	}
	defer f.Close()

	folder := fmt.Sprintf("dynamics/%s/extra", dynamicID.String())
	fileName := fmt.Sprintf("%s%s", uuid.New().String(), ext)
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		if mediaType == "video" {
			contentType = "video/mp4"
		} else {
			contentType = "image/jpeg"
		}
	}

	_, err = s.storage.UploadReader(c.Context(), accountID, folder, fileName, f, file.Size, contentType)
	if err != nil {
		log.Printf("[DYNAMIC] Failed to upload extra media: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to upload media"})
	}

	proxyURL := fmt.Sprintf("/api/media/file/%s/dynamics/%s/extra/%s", accountID.String(), dynamicID.String(), fileName)

	// Update the link
	link, _, err := s.repos.Dynamic.GetLinkByID(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Link not found"})
	}
	link.ExtraMessageMediaURL = proxyURL
	link.ExtraMessageMediaType = mediaType
	if err := s.repos.Dynamic.UpdateLink(c.Context(), link); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"url": proxyURL, "media_type": mediaType})
}

func (s *Server) handleDeleteLinkExtraMedia(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	link, _, err := s.repos.Dynamic.GetLinkByID(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Link not found"})
	}
	link.ExtraMessageMediaURL = ""
	link.ExtraMessageMediaType = ""
	if err := s.repos.Dynamic.UpdateLink(c.Context(), link); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ─── Link Extra Media (multi, up to 10 per link) ─────────────────────────────

const maxExtraMediaPerLink = 10

// handleListLinkMedia returns the list of extras for a given link (admin).
func (s *Server) handleListLinkMedia(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}
	items, err := s.repos.Dynamic.ListExtraMedia(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"media": items})
}

// handleCreateLinkMedia uploads one media file and appends it as a new extra.
// Accepts multipart form fields: media (file), caption (string, optional).
func (s *Server) handleCreateLinkMedia(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	// Enforce max count
	count, err := s.repos.Dynamic.CountExtraMedia(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if count >= maxExtraMediaPerLink {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("Máximo %d medios por link", maxExtraMediaPerLink)})
	}

	file, err := c.FormFile("media")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Archivo requerido"})
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowed := map[string]string{
		".jpg": "image", ".jpeg": "image", ".png": "image", ".gif": "image", ".webp": "image",
		".mp4": "video",
	}
	mediaType, ok := allowed[ext]
	if !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Formato no soportado (jpg, png, gif, webp, mp4)"})
	}
	if mediaType == "video" && file.Size > 3*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El video no debe superar los 3MB"})
	}

	f, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "No se pudo abrir el archivo"})
	}
	defer f.Close()

	folder := fmt.Sprintf("dynamics/%s/extra", dynamicID.String())
	fileName := fmt.Sprintf("%s%s", uuid.New().String(), ext)
	contentType := file.Header.Get("Content-Type")
	if contentType == "" {
		if mediaType == "video" {
			contentType = "video/mp4"
		} else {
			contentType = "image/jpeg"
		}
	}
	if _, err := s.storage.UploadReader(c.Context(), accountID, folder, fileName, f, file.Size, contentType); err != nil {
		log.Printf("[DYNAMIC] Failed to upload media: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "No se pudo subir el archivo"})
	}
	proxyURL := fmt.Sprintf("/api/media/file/%s/dynamics/%s/extra/%s", accountID.String(), dynamicID.String(), fileName)

	m := &domain.DynamicLinkExtraMedia{
		LinkID:    linkID,
		URL:       proxyURL,
		MediaType: mediaType,
		Caption:   c.FormValue("caption"),
		SortOrder: count,
	}
	if err := s.repos.Dynamic.CreateExtraMedia(c.Context(), m); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(m)
}

// handleUpdateLinkMediaCaption updates only the caption of an extra.
func (s *Server) handleUpdateLinkMediaCaption(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	mediaID, err := uuid.Parse(c.Params("mediaId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid media ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}
	var req struct {
		Caption string `json:"caption"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := s.repos.Dynamic.UpdateExtraMediaCaption(c.Context(), mediaID, linkID, req.Caption); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleDeleteLinkMedia removes one extra from the link.
func (s *Server) handleDeleteLinkMedia(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	mediaID, err := uuid.Parse(c.Params("mediaId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid media ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}
	if err := s.repos.Dynamic.DeleteExtraMedia(c.Context(), mediaID, linkID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleReorderLinkMedia accepts { ids: [uuid,...] } and updates sort_order.
func (s *Server) handleReorderLinkMedia(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}
	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, idStr := range req.IDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
		}
		ids = append(ids, id)
	}
	if err := s.repos.Dynamic.ReorderExtraMedia(c.Context(), linkID, ids); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleCheckDynamicLinkSlug(c *fiber.Ctx) error {
	var req struct {
		Slug      string     `json:"slug"`
		ExcludeID *uuid.UUID `json:"exclude_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	exists, err := s.repos.Dynamic.LinkSlugExists(c.Context(), req.Slug, req.ExcludeID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"exists": exists})
}

// ─── Public WhatsApp Send Handler ────────────────────────────────────────────

func (s *Server) handleSendDynamicWhatsApp(c *fiber.Ctx) error {
	var req struct {
		LinkID string `json:"link_id"`
		Phone  string `json:"phone"`
		ItemID string `json:"item_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	linkID, err := uuid.Parse(req.LinkID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Link inválido"})
	}
	itemID, err := uuid.Parse(req.ItemID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Item inválido"})
	}

	// Normalize phone
	phone := kommo.NormalizePhone(req.Phone)
	if len(phone) < 11 || !strings.HasPrefix(phone, "51") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Número de teléfono inválido"})
	}

	// Validate link exists and has WhatsApp enabled
	link, d, err := s.repos.Dynamic.GetLinkByID(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Link no encontrado"})
	}
	if !link.WhatsAppEnabled {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "WhatsApp no está habilitado para este link"})
	}

	// Get the item to validate it exists and get image URL
	item, err := s.repos.Dynamic.GetItemByID(c.Context(), itemID)
	if err != nil || item.DynamicID != d.ID {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Item no encontrado"})
	}

	caption := link.WhatsAppMessage
	if caption == "" {
		caption = "¡Aquí tienes tu pensamiento del día! 🌟"
	}

	q := &domain.DynamicWhatsAppQueue{
		DynamicID:      d.ID,
		AccountID:      d.AccountID,
		LinkID:         link.ID,
		Phone:          phone,
		ItemID:         item.ID,
		ImageURL:       item.ImageURL,
		Caption:        caption,
		ExtraText:      link.ExtraMessageText,
		ExtraMediaURL:  link.ExtraMessageMediaURL,
		ExtraMediaType: link.ExtraMessageMediaType,
	}
	if err := s.repos.Dynamic.EnqueueWhatsApp(c.Context(), q); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al encolar mensaje"})
	}

	return c.JSON(fiber.Map{"success": true, "message": "¡En breve recibirás tu imagen por WhatsApp!"})
}

// ─── Registration Handlers ───────────────────────────────────────────────────

// sendRegistrationWhatsApp performs the synchronous WhatsApp delivery for a
// registration. The main (scratched image) message is blocking — if it fails
// the registration is marked failed. Extra media is sent sequentially and
// errors on extras only log a warning without aborting the flow.
//
// Returns (status, errorMessage). status ∈ {"sent","failed","skipped"}.
// captionAppend (optional) is appended to the main caption — used to inject a
// personalized link when sharing so the recipient can land directly in the game.
func (s *Server) sendRegistrationWhatsApp(ctx context.Context, link *domain.DynamicLink, d *domain.Dynamic, item *domain.DynamicItem, phone string, captionAppend string) (string, string) {
	if !link.WhatsAppEnabled {
		return "skipped", ""
	}
	if s.pool == nil {
		return "failed", "WhatsApp no disponible"
	}

	deviceID, err := s.pool.GetFirstConnectedDeviceForAccount(d.AccountID)
	if err != nil {
		return "failed", "No hay dispositivo WhatsApp conectado"
	}

	to := phone + "@s.whatsapp.net"
	caption := strings.TrimSpace(link.WhatsAppMessage)
	if caption == "" {
		caption = "¡Aquí tienes tu pensamiento del día! 🌟"
	}
	if captionAppend != "" {
		caption = caption + "\n\n" + captionAppend
	}

	if item == nil || item.ImageURL == "" {
		return "failed", "No hay imagen disponible"
	}

	if _, err := s.pool.SendMediaMessage(ctx, deviceID, to, caption, item.ImageURL, "image"); err != nil {
		log.Printf("[DYNAMIC] ❌ Main WA send failed to %s: %v", phone, err)
		return "failed", err.Error()
	}
	log.Printf("[DYNAMIC] ✅ Main WA sent to %s", phone)

	// Legacy single-slot extra (kept for compatibility until UI fully removes it)
	if link.ExtraMessageMediaURL != "" {
		time.Sleep(700 * time.Millisecond)
		if _, err := s.pool.SendMediaMessage(ctx, deviceID, to, link.ExtraMessageText, link.ExtraMessageMediaURL, link.ExtraMessageMediaType); err != nil {
			log.Printf("[DYNAMIC] ⚠️ Legacy extra media send failed to %s: %v", phone, err)
		}
	} else if strings.TrimSpace(link.ExtraMessageText) != "" {
		time.Sleep(700 * time.Millisecond)
		if _, err := s.pool.SendMessage(ctx, deviceID, to, link.ExtraMessageText); err != nil {
			log.Printf("[DYNAMIC] ⚠️ Legacy extra text send failed to %s: %v", phone, err)
		}
	}

	// New multi-media extras (up to 10)
	for _, m := range link.ExtraMedia {
		if ctx.Err() != nil {
			log.Printf("[DYNAMIC] ⚠️ Context done before sending all extras to %s", phone)
			break
		}
		time.Sleep(700 * time.Millisecond)
		if m.URL == "" {
			if strings.TrimSpace(m.Caption) != "" {
				if _, err := s.pool.SendMessage(ctx, deviceID, to, m.Caption); err != nil {
					log.Printf("[DYNAMIC] ⚠️ Extra text send failed to %s: %v", phone, err)
				}
			}
			continue
		}
		mediaType := m.MediaType
		if mediaType == "" {
			mediaType = "image"
		}
		if _, err := s.pool.SendMediaMessage(ctx, deviceID, to, m.Caption, m.URL, mediaType); err != nil {
			log.Printf("[DYNAMIC] ⚠️ Extra media send failed to %s: %v", phone, err)
		}
	}

	return "sent", ""
}

// ensureLeadForRegistration makes sure there is a Contact + Lead for the given
// phone under accountID. It also merges the dynamic tag. Returns contactID, leadID.
func (s *Server) ensureLeadForRegistration(ctx context.Context, accountID uuid.UUID, jid, phone, fullName, dynamicTag string) (*uuid.UUID, *uuid.UUID, error) {
	contact, err := s.repos.Contact.GetOrCreate(ctx, accountID, nil, jid, phone, fullName, "", false)
	if err != nil {
		return nil, nil, err
	}

	// Update contact name if empty
	if contact != nil && (contact.CustomName == nil || *contact.CustomName == "") && fullName != "" {
		contact.CustomName = &fullName
		_ = s.repos.Contact.Update(ctx, contact)
	}

	// Try to find existing lead by JID
	existingLead, _ := s.services.Lead.GetByJID(ctx, accountID, jid)
	if existingLead != nil {
		if dynamicTag != "" {
			_ = s.repos.Tag.SyncLeadTagsByNames(ctx, accountID, existingLead.ID, []string{dynamicTag})
		}
		return &contact.ID, &existingLead.ID, nil
	}

	// Create lead with the account incoming pipeline/stage.
	lead := &domain.Lead{
		AccountID: accountID,
		JID:       jid,
		Name:      strPtr(fullName),
		Phone:     strPtr(phone),
		Source:    strPtr("dinamica"),
		Status:    strPtr(domain.LeadStatusNew),
		ContactID: &contact.ID,
	}
	if pipelineID, stageID, err := s.repos.Pipeline.ResolveIncomingLeadDestination(ctx, accountID); err == nil {
		lead.PipelineID = pipelineID
		lead.StageID = stageID
	}
	if err := s.services.Lead.Create(ctx, lead); err != nil {
		return &contact.ID, nil, err
	}
	if dynamicTag != "" {
		_ = s.repos.Tag.SyncLeadTagsByNames(ctx, accountID, lead.ID, []string{dynamicTag})
	}
	return &contact.ID, &lead.ID, nil
}

// handleRegisterOnLink — public endpoint: register on a link and synchronously
// deliver the WhatsApp messages. Returns a session token that the client stores
// in localStorage so the form is skipped on return visits.
func (s *Server) handleRegisterOnLink(c *fiber.Ctx) error {
	var req struct {
		LinkID   string `json:"link_id"`
		FullName string `json:"full_name"`
		Phone    string `json:"phone"`
		ItemID   string `json:"item_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	linkID, err := uuid.Parse(req.LinkID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Link inválido"})
	}
	itemID, err := uuid.Parse(req.ItemID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Item inválido"})
	}

	fullName := strings.TrimSpace(req.FullName)
	if fullName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El nombre es obligatorio"})
	}

	phone := kommo.NormalizePhone(req.Phone)
	if len(phone) < 11 || !strings.HasPrefix(phone, "51") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Número de teléfono inválido"})
	}

	link, d, err := s.repos.Dynamic.GetLinkByID(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Link no encontrado"})
	}

	now := time.Now()
	if link.StartsAt != nil && now.Before(*link.StartsAt) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Este evento aún no ha comenzado"})
	}
	if link.EndsAt != nil && now.After(*link.EndsAt) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Este evento ya finalizó"})
	}

	// Validate item belongs to dynamic
	item, err := s.repos.Dynamic.GetItemByID(c.Context(), itemID)
	if err != nil || item.DynamicID != d.ID {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Ítem inválido"})
	}

	// If already registered on this link with this phone, return existing token
	if existing, _ := s.repos.Dynamic.GetRegistrationByPhone(c.Context(), linkID, phone); existing != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error":           "Ya registraste tus datos con este número",
			"already":         true,
			"session_token":   existing.SessionToken,
			"whatsapp_status": existing.WhatsAppStatus,
			"registration":    existing,
		})
	}

	// If the phone was previously registered via a share (shared_by_registration_id IS NOT NULL)
	// we skip the WhatsApp send — they already received the message — and just create a
	// self-registration so they can enter the game directly on subsequent visits.
	alreadySharedRecipient, _ := s.repos.Dynamic.RegistrationExistsByPhone(c.Context(), linkID, phone)

	jid := phone + "@s.whatsapp.net"
	dynamicTag := "dinamica:" + d.Slug

	// Ensure contact + lead globals (idempotent)
	contactID, leadID, err := s.ensureLeadForRegistration(c.Context(), d.AccountID, jid, phone, fullName, dynamicTag)
	if err != nil {
		log.Printf("[DYNAMIC] Error ensuring lead for registration: %v", err)
	}

	// Send WhatsApp synchronously with 30s cap — unless this phone already received the
	// dynamic via a share, in which case we consider the message already delivered.
	waStatus := "skipped"
	waError := ""
	if link.WhatsAppEnabled && !alreadySharedRecipient {
		sendCtx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
		waStatus, waError = s.sendRegistrationWhatsApp(sendCtx, link, d, item, phone, "")
		cancel()
	} else if alreadySharedRecipient {
		waStatus = "already_delivered"
	}

	// If WhatsApp was required but failed, return an error without persisting
	// the registration — this lets the user retry the form.
	if link.WhatsAppEnabled && !alreadySharedRecipient && waStatus != "sent" {
		msg := waError
		if msg == "" {
			msg = "No pudimos entregar tu mensaje por WhatsApp. Intenta nuevamente."
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error":           msg,
			"whatsapp_status": waStatus,
		})
	}

	// Create registration row
	sessionToken := uuid.New().String()
	reg := &domain.DynamicLinkRegistration{
		LinkID:         linkID,
		FullName:       fullName,
		Phone:          phone,
		ContactID:      contactID,
		LeadID:         leadID,
		WhatsAppStatus: waStatus,
		WhatsAppError:  waError,
		SessionToken:   sessionToken,
	}
	if err := s.repos.Dynamic.CreateRegistration(c.Context(), reg); err != nil {
		if strings.Contains(err.Error(), "uq_link_phone") {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Ya registraste tus datos con este número"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al registrar"})
	}

	if s.hub != nil {
		s.hub.BroadcastToAccount(d.AccountID, ws.EventDynamicRegistration, map[string]interface{}{
			"action":       "created",
			"link_id":      link.ID,
			"registration": reg,
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success":         true,
		"registration":    reg,
		"session_token":   sessionToken,
		"whatsapp_status": waStatus,
	})
}

// handleShareOnLink — public endpoint: a registered user shares the dynamic
// with another person. Sends the same WhatsApp payload (main message + extras)
// to the recipient and persists a registration row linked to the sharer via
// shared_by_registration_id.
func (s *Server) handleShareOnLink(c *fiber.Ctx) error {
	var req struct {
		LinkID       string `json:"link_id"`
		ItemID       string `json:"item_id"`
		FullName     string `json:"full_name"`
		Phone        string `json:"phone"`
		SessionToken string `json:"session_token"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	linkID, err := uuid.Parse(req.LinkID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Link inválido"})
	}
	itemID, err := uuid.Parse(req.ItemID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Ítem inválido"})
	}

	token := strings.TrimSpace(req.SessionToken)
	if token == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Debes registrarte antes de compartir"})
	}
	sharerReg, err := s.repos.Dynamic.GetRegistrationBySessionToken(c.Context(), token)
	if err != nil || sharerReg == nil || sharerReg.LinkID != linkID {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Sesión inválida, recarga la página"})
	}

	fullName := strings.TrimSpace(req.FullName)
	if fullName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El nombre es obligatorio"})
	}

	phone := kommo.NormalizePhone(req.Phone)
	if len(phone) < 11 || !strings.HasPrefix(phone, "51") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Número de teléfono inválido"})
	}

	link, d, err := s.repos.Dynamic.GetLinkByID(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Link no encontrado"})
	}

	now := time.Now()
	if link.StartsAt != nil && now.Before(*link.StartsAt) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Este evento aún no ha comenzado"})
	}
	if link.EndsAt != nil && now.After(*link.EndsAt) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Este evento ya finalizó"})
	}

	item, err := s.repos.Dynamic.GetItemByID(c.Context(), itemID)
	if err != nil || item.DynamicID != d.ID {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Ítem inválido"})
	}

	jid := phone + "@s.whatsapp.net"
	dynamicTag := "dinamica:" + d.Slug

	contactID, leadID, err := s.ensureLeadForRegistration(c.Context(), d.AccountID, jid, phone, fullName, dynamicTag)
	if err != nil {
		log.Printf("[DYNAMIC] Error ensuring lead for shared registration: %v", err)
	}

	// Share requires WhatsApp to be meaningful. If the link hasn't got WA
	// enabled, there's nothing to deliver — refuse.
	if !link.WhatsAppEnabled {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Esta dinámica no tiene WhatsApp activo"})
	}

	// Generate the recipient's session_token UPFRONT so we can embed it in the
	// shared link. The recipient will land in the game without seeing the
	// registration form — the token authenticates them automatically.
	recipientToken := uuid.New().String()
	personalizedLink := ""
	if base := strings.TrimRight(s.cfg.PublicURL, "/"); base != "" {
		personalizedLink = fmt.Sprintf("👉 Juega tú también: %s/d/%s?t=%s", base, d.Slug, recipientToken)
	}

	sendCtx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	waStatus, waError := s.sendRegistrationWhatsApp(sendCtx, link, d, item, phone, personalizedLink)
	cancel()

	if waStatus != "sent" {
		msg := waError
		if msg == "" {
			msg = "No pudimos entregar el mensaje por WhatsApp. Intenta nuevamente."
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error":           msg,
			"whatsapp_status": waStatus,
		})
	}

	sharerID := sharerReg.ID
	reg := &domain.DynamicLinkRegistration{
		LinkID:                 linkID,
		FullName:               fullName,
		Phone:                  phone,
		ContactID:              contactID,
		LeadID:                 leadID,
		WhatsAppStatus:         waStatus,
		WhatsAppError:          waError,
		SessionToken:           recipientToken,
		SharedByRegistrationID: &sharerID,
	}
	if err := s.repos.Dynamic.CreateRegistration(c.Context(), reg); err != nil {
		log.Printf("[DYNAMIC] Error persisting shared registration (WA already sent): %v", err)
		// The WA was already delivered; don't fail the request.
	}

	if s.hub != nil {
		s.hub.BroadcastToAccount(d.AccountID, ws.EventDynamicRegistration, map[string]interface{}{
			"action":       "shared",
			"link_id":      link.ID,
			"registration": reg,
			"shared_by":    sharerReg.ID,
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success":         true,
		"whatsapp_status": waStatus,
		"recipient": fiber.Map{
			"full_name": fullName,
			"phone":     phone,
		},
	})
}

// handleCheckRegistration — public: verify if a session token matches an
// existing registration on a link. Used by the frontend to skip the form on
// return visits (token stored in localStorage).
func (s *Server) handleCheckRegistration(c *fiber.Ctx) error {
	linkID, err := uuid.Parse(c.Query("link_id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Link inválido"})
	}
	token := strings.TrimSpace(c.Query("session_token"))
	if token != "" {
		reg, err := s.repos.Dynamic.GetRegistrationBySessionToken(c.Context(), token)
		if err == nil && reg != nil && reg.LinkID == linkID {
			return c.JSON(fiber.Map{"registered": true, "registration": reg})
		}
		return c.JSON(fiber.Map{"registered": false})
	}
	// Fallback: phone-based check (kept for backwards compat)
	phone := kommo.NormalizePhone(c.Query("phone"))
	if phone == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token o teléfono requerido"})
	}
	if reg, err := s.repos.Dynamic.GetRegistrationByPhone(c.Context(), linkID, phone); err == nil && reg != nil {
		return c.JSON(fiber.Map{"registered": true, "registration": reg, "session_token": reg.SessionToken})
	}
	return c.JSON(fiber.Map{"registered": false})
}

// handleListLinkRegistrations — admin: list registrations for a link
func (s *Server) handleListLinkRegistrations(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	regs, err := s.repos.Dynamic.ListRegistrationsByLink(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	count, _ := s.repos.Dynamic.CountRegistrationsByLink(c.Context(), linkID)
	return c.JSON(fiber.Map{"registrations": regs, "total": count})
}

// handleDeleteLinkRegistration — admin: delete a registration
func (s *Server) handleDeleteLinkRegistration(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	regID, err := uuid.Parse(c.Params("regId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid registration ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}
	if err := s.repos.Dynamic.DeleteRegistration(c.Context(), regID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	linkID, _ := uuid.Parse(c.Params("linkId"))
	if s.hub != nil {
		s.hub.BroadcastToAccount(accountID, ws.EventDynamicRegistration, map[string]interface{}{
			"action":  "deleted",
			"link_id": linkID,
			"reg_id":  regID,
		})
	}

	return c.JSON(fiber.Map{"success": true})
}

// handleExportLinkRegistrations — admin: export registrations as CSV
func (s *Server) handleExportLinkRegistrations(c *fiber.Ctx) error {
	accountID := c.Locals("account_id").(uuid.UUID)
	dynamicID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid dynamic ID"})
	}
	linkID, err := uuid.Parse(c.Params("linkId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid link ID"})
	}
	if _, err := s.repos.Dynamic.GetByID(c.Context(), dynamicID, accountID); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Dynamic not found"})
	}

	regs, err := s.repos.Dynamic.ListRegistrationsByLink(c.Context(), linkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	loc, _ := time.LoadLocation("America/Lima")

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"Nombre", "Teléfono", "Edad", "Fecha de registro"})
	for _, r := range regs {
		ageStr := ""
		if r.Age != nil {
			ageStr = fmt.Sprintf("%d", *r.Age)
		}
		w.Write([]string{
			r.FullName,
			r.Phone,
			ageStr,
			r.CreatedAt.In(loc).Format("02/01/2006 15:04"),
		})
	}
	w.Flush()

	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", "attachment; filename=registros.csv")
	return c.Send(buf.Bytes())
}
