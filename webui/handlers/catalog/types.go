package catalog

import (
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

// CatalogEntry represents a migration in the catalog
type CatalogEntry struct {
	ID           string                       `json:"id"`
	Version      string                       `json:"version"`
	Title        string                       `json:"title"`
	Description  string                       `json:"description"`
	Author       string                       `json:"author"`
	AuthorEmail  string                       `json:"authorEmail"`
	Category     constants.MigrationCategory  `json:"category"`
	Tags         []string                     `json:"tags"`
	Type         string                       `json:"type"`
	Status       string                       `json:"status"`
	FilePath     string                       `json:"filePath"`
	FileSize     int64                        `json:"fileSize"`
	Checksum     string                       `json:"checksum"`
	Dependencies []string                     `json:"dependencies"`
	CreatedAt    time.Time                    `json:"createdAt"`
	UpdatedAt    time.Time                    `json:"updatedAt"`
	AppliedAt    *time.Time                   `json:"appliedAt,omitempty"`
	Metadata     map[string]interface{}       `json:"metadata,omitempty"`
}

// SearchRequest represents catalog search request
type SearchRequest struct {
	Query       string                      `json:"query,omitempty"`
	Category    constants.MigrationCategory `json:"category,omitempty"`
	Tags        []string                    `json:"tags,omitempty"`
	Author      string                      `json:"author,omitempty"`
	Status      string                      `json:"status,omitempty"`
	Type        string                      `json:"type,omitempty"`
	From        time.Time                   `json:"from,omitempty"`
	To          time.Time                   `json:"to,omitempty"`
	Limit       int                         `json:"limit,omitempty"`
	Offset      int                         `json:"offset,omitempty"`
	SortBy      string                      `json:"sortBy,omitempty"`
	SortOrder   string                      `json:"sortOrder,omitempty"`
}

// SearchResponse represents catalog search response
type SearchResponse struct {
	Entries   []CatalogEntry `json:"entries"`
	Total     int            `json:"total"`
	Limit     int            `json:"limit"`
	Offset    int            `json:"offset"`
	Query     string         `json:"query,omitempty"`
	Facets    Facets         `json:"facets,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// Facets represents search facets for filtering
type Facets struct {
	Categories map[string]int `json:"categories"`
	Tags       map[string]int `json:"tags"`
	Authors    map[string]int `json:"authors"`
	Types      map[string]int `json:"types"`
	Statuses   map[string]int `json:"statuses"`
}

// BrowseRequest represents catalog browse request
type BrowseRequest struct {
	Category  constants.MigrationCategory `json:"category,omitempty"`
	Limit     int                         `json:"limit,omitempty"`
	Offset    int                         `json:"offset,omitempty"`
	SortBy    string                      `json:"sortBy,omitempty"`
	SortOrder string                      `json:"sortOrder,omitempty"`
}

// BrowseResponse represents catalog browse response
type BrowseResponse struct {
	Entries   []CatalogEntry `json:"entries"`
	Total     int            `json:"total"`
	Limit     int            `json:"limit"`
	Offset    int            `json:"offset"`
	Timestamp time.Time      `json:"timestamp"`
}

// GetEntryResponse represents get catalog entry response
type GetEntryResponse struct {
	Entry      CatalogEntry       `json:"entry"`
	Content    string             `json:"content,omitempty"`
	Related    []CatalogEntry     `json:"related,omitempty"`
	Timestamp  time.Time          `json:"timestamp"`
}

// TagsResponse represents tags response
type TagsResponse struct {
	Tags      []TagInfo `json:"tags"`
	Total     int       `json:"total"`
	Timestamp time.Time `json:"timestamp"`
}

// TagInfo represents tag information
type TagInfo struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// CategoriesResponse represents categories response
type CategoriesResponse struct {
	Categories []CategoryInfo `json:"categories"`
	Timestamp  time.Time      `json:"timestamp"`
}

// CategoryInfo represents category information
type CategoryInfo struct {
	Name        constants.MigrationCategory `json:"name"`
	Count       int                         `json:"count"`
	Description string                      `json:"description"`
}

// StatsResponse represents catalog statistics response
type StatsResponse struct {
	Total            int                        `json:"total"`
	ByCategory       map[string]int             `json:"byCategory"`
	ByStatus         map[string]int             `json:"byStatus"`
	ByType           map[string]int             `json:"byType"`
	ByAuthor         map[string]int             `json:"byAuthor"`
	RecentlyAdded    []CatalogEntry             `json:"recentlyAdded"`
	RecentlyApplied  []CatalogEntry             `json:"recentlyApplied"`
	MostPopularTags  []TagInfo                  `json:"mostPopularTags"`
	Timestamp        time.Time                  `json:"timestamp"`
}

// ErrorResponse represents error response
type ErrorResponse struct {
	Error     string    `json:"error"`
	Code      string    `json:"code"`
	Details   string    `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
