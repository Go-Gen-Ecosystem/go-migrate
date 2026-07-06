package catalog

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

// Handler handles catalog-related HTTP requests
type Handler struct {
	logger logging.Logger
	// TODO: Add catalog service/repository
}

// NewHandler creates a new catalog handler
func NewHandler(logger logging.Logger) *Handler {
	return &Handler{
		logger: logger,
	}
}

// RegisterRoutes registers catalog routes
func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	catalog := router.Group("/migrations/catalog")
	{
		catalog.POST("/search", h.Search)
		catalog.GET("", h.Browse)
		catalog.GET("/:id", h.GetEntry)
		catalog.GET("/tags", h.GetTags)
		catalog.GET("/categories", h.GetCategories)
		catalog.GET("/stats", h.GetStats)
	}
}

// Search handles catalog search requests
func (h *Handler) Search(c *gin.Context) {
	ctx := c.Request.Context()

	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Set defaults
	if req.Limit == 0 {
		req.Limit = constants.DefaultCatalogPageSize
	}
	if req.SortBy == "" {
		req.SortBy = "createdAt"
	}
	if req.SortOrder == "" {
		req.SortOrder = "desc"
	}

	h.logger.Info(ctx, "Searching catalog",
		"query", req.Query,
		"category", req.Category,
		"tags", len(req.Tags),
		"limit", req.Limit,
		"offset", req.Offset)

	// TODO: Implement full-text search
	entries := h.searchEntries(&req)
	facets := h.computeFacets(entries)

	response := SearchResponse{
		Entries:   entries,
		Total:     len(entries),
		Limit:     req.Limit,
		Offset:    req.Offset,
		Query:     req.Query,
		Facets:    facets,
		Timestamp: time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// Browse handles catalog browse requests
func (h *Handler) Browse(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse query parameters
	category := c.Query("category")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(constants.DefaultCatalogPageSize)))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	sortBy := c.DefaultQuery("sortBy", "createdAt")
	sortOrder := c.DefaultQuery("sortOrder", "desc")

	h.logger.Info(ctx, "Browsing catalog",
		"category", category,
		"limit", limit,
		"offset", offset,
		"sortBy", sortBy,
		"sortOrder", sortOrder)

	// TODO: Implement catalog browsing
	entries := []CatalogEntry{}

	response := BrowseResponse{
		Entries:   entries,
		Total:     len(entries),
		Limit:     limit,
		Offset:    offset,
		Timestamp: time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// GetEntry handles get catalog entry requests
func (h *Handler) GetEntry(c *gin.Context) {
	ctx := c.Request.Context()
	entryID := c.Param("id")

	h.logger.Info(ctx, "Getting catalog entry", "entryId", entryID)

	// TODO: Get entry from catalog
	// For now, return not found
	c.JSON(http.StatusNotFound, ErrorResponse{
		Error:     constants.MsgNotFound,
		Code:      constants.ErrCodeNotFound,
		Timestamp: time.Now(),
	})
}

// GetTags handles get tags requests
func (h *Handler) GetTags(c *gin.Context) {
	ctx := c.Request.Context()

	h.logger.Info(ctx, "Getting catalog tags")

	// TODO: Get tags from catalog
	tags := []TagInfo{}

	response := TagsResponse{
		Tags:      tags,
		Total:     len(tags),
		Timestamp: time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// GetCategories handles get categories requests
func (h *Handler) GetCategories(c *gin.Context) {
	ctx := c.Request.Context()

	h.logger.Info(ctx, "Getting catalog categories")

	categories := []CategoryInfo{
		{Name: constants.CategorySchema, Count: 0, Description: "Schema changes (tables, columns, indexes)"},
		{Name: constants.CategoryData, Count: 0, Description: "Data migrations and transformations"},
		{Name: constants.CategorySecurity, Count: 0, Description: "Security-related changes"},
		{Name: constants.CategoryPerformance, Count: 0, Description: "Performance optimizations"},
		{Name: constants.CategoryRefactor, Count: 0, Description: "Code refactoring migrations"},
	}

	response := CategoriesResponse{
		Categories: categories,
		Timestamp:  time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// GetStats handles get statistics requests
func (h *Handler) GetStats(c *gin.Context) {
	ctx := c.Request.Context()

	h.logger.Info(ctx, "Getting catalog statistics")

	// TODO: Compute real statistics
	stats := StatsResponse{
		Total:            0,
		ByCategory:       make(map[string]int),
		ByStatus:         make(map[string]int),
		ByType:           make(map[string]int),
		ByAuthor:         make(map[string]int),
		RecentlyAdded:    []CatalogEntry{},
		RecentlyApplied:  []CatalogEntry{},
		MostPopularTags:  []TagInfo{},
		Timestamp:        time.Now(),
	}

	c.JSON(http.StatusOK, stats)
}

// Helper functions

func (h *Handler) searchEntries(req *SearchRequest) []CatalogEntry {
	// TODO: Implement real search logic
	// This is a placeholder that returns empty results
	entries := []CatalogEntry{}

	// Filter by query
	if req.Query != "" {
		entries = h.filterByQuery(entries, req.Query)
	}

	// Filter by category
	if req.Category != "" {
		entries = h.filterByCategory(entries, req.Category)
	}

	// Filter by tags
	if len(req.Tags) > 0 {
		entries = h.filterByTags(entries, req.Tags)
	}

	// Filter by author
	if req.Author != "" {
		entries = h.filterByAuthor(entries, req.Author)
	}

	// Filter by status
	if req.Status != "" {
		entries = h.filterByStatus(entries, req.Status)
	}

	// Filter by type
	if req.Type != "" {
		entries = h.filterByType(entries, req.Type)
	}

	// Sort entries
	entries = h.sortEntries(entries, req.SortBy, req.SortOrder)

	// Apply pagination
	start := req.Offset
	end := req.Offset + req.Limit
	if start >= len(entries) {
		return []CatalogEntry{}
	}
	if end > len(entries) {
		end = len(entries)
	}

	return entries[start:end]
}

func (h *Handler) filterByQuery(entries []CatalogEntry, query string) []CatalogEntry {
	query = strings.ToLower(query)
	filtered := []CatalogEntry{}

	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Title), query) ||
			strings.Contains(strings.ToLower(entry.Description), query) ||
			strings.Contains(strings.ToLower(entry.Version), query) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

func (h *Handler) filterByCategory(entries []CatalogEntry, category constants.MigrationCategory) []CatalogEntry {
	filtered := []CatalogEntry{}

	for _, entry := range entries {
		if entry.Category == category {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

func (h *Handler) filterByTags(entries []CatalogEntry, tags []string) []CatalogEntry {
	filtered := []CatalogEntry{}

	for _, entry := range entries {
		hasAllTags := true
		for _, tag := range tags {
			found := false
			for _, entryTag := range entry.Tags {
				if entryTag == tag {
					found = true
					break
				}
			}
			if !found {
				hasAllTags = false
				break
			}
		}
		if hasAllTags {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

func (h *Handler) filterByAuthor(entries []CatalogEntry, author string) []CatalogEntry {
	filtered := []CatalogEntry{}

	for _, entry := range entries {
		if entry.Author == author {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

func (h *Handler) filterByStatus(entries []CatalogEntry, status string) []CatalogEntry {
	filtered := []CatalogEntry{}

	for _, entry := range entries {
		if entry.Status == status {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

func (h *Handler) filterByType(entries []CatalogEntry, migType string) []CatalogEntry {
	filtered := []CatalogEntry{}

	for _, entry := range entries {
		ext := filepath.Ext(entry.FilePath)
		if strings.TrimPrefix(ext, ".") == migType {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

func (h *Handler) sortEntries(entries []CatalogEntry, sortBy, sortOrder string) []CatalogEntry {
	// TODO: Implement proper sorting
	// For now, return as-is
	return entries
}

func (h *Handler) computeFacets(entries []CatalogEntry) Facets {
	facets := Facets{
		Categories: make(map[string]int),
		Tags:       make(map[string]int),
		Authors:    make(map[string]int),
		Types:      make(map[string]int),
		Statuses:   make(map[string]int),
	}

	for _, entry := range entries {
		facets.Categories[string(entry.Category)]++
		facets.Authors[entry.Author]++
		facets.Statuses[entry.Status]++

		ext := filepath.Ext(entry.FilePath)
		facets.Types[strings.TrimPrefix(ext, ".")]++

		for _, tag := range entry.Tags {
			facets.Tags[tag]++
		}
	}

	return facets
}
