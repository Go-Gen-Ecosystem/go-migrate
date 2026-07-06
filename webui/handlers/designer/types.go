package designer

import (
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

// Design represents a visual migration design
type Design struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Author      string                 `json:"author"`
	Version     string                 `json:"version"`
	Elements    []Element              `json:"elements"`
	Connections []Connection           `json:"connections"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
}

// Element represents a design element
type Element struct {
	ID         string                         `json:"id"`
	Type       constants.DesignerElementType  `json:"type"`
	Name       string                         `json:"name"`
	Properties map[string]interface{}         `json:"properties"`
	Position   Position                       `json:"position"`
	Size       Size                           `json:"size,omitempty"`
	Style      map[string]string              `json:"style,omitempty"`
	Metadata   map[string]interface{}         `json:"metadata,omitempty"`
}

// Position represents element position
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Size represents element size
type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Connection represents a connection between elements
type Connection struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Style      map[string]string      `json:"style,omitempty"`
}

// CreateDesignRequest represents create design request
type CreateDesignRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Elements    []Element              `json:"elements"`
	Connections []Connection           `json:"connections"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// UpdateDesignRequest represents update design request
type UpdateDesignRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Elements    []Element              `json:"elements"`
	Connections []Connection           `json:"connections"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// GenerateMigrationRequest represents generate migration request
type GenerateMigrationRequest struct {
	DesignID string `json:"designId" binding:"required"`
	Format   string `json:"format"`
	Options  map[string]interface{} `json:"options"`
}

// GenerateMigrationResponse represents generate migration response
type GenerateMigrationResponse struct {
	MigrationID string    `json:"migrationId"`
	Content     string    `json:"content"`
	FilePath    string    `json:"filePath"`
	Format      string    `json:"format"`
	Timestamp   time.Time `json:"timestamp"`
}

// ValidateDesignRequest represents validate design request
type ValidateDesignRequest struct {
	Elements    []Element    `json:"elements" binding:"required"`
	Connections []Connection `json:"connections"`
}

// ValidateDesignResponse represents validate design response
type ValidateDesignResponse struct {
	Valid     bool              `json:"valid"`
	Errors    []ValidationError `json:"errors,omitempty"`
	Warnings  []ValidationError `json:"warnings,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// ValidationError represents a validation error
type ValidationError struct {
	ElementID string `json:"elementId,omitempty"`
	Field     string `json:"field,omitempty"`
	Message   string `json:"message"`
	Severity  string `json:"severity"`
}

// ReverseEngineerRequest represents reverse engineer request
type ReverseEngineerRequest struct {
	ConnectionString string   `json:"connectionString" binding:"required"`
	Schema           string   `json:"schema,omitempty"`
	Tables           []string `json:"tables,omitempty"`
	IncludeData      bool     `json:"includeData"`
}

// ReverseEngineerResponse represents reverse engineer response
type ReverseEngineerResponse struct {
	Design    Design    `json:"design"`
	Timestamp time.Time `json:"timestamp"`
}

// TemplateResponse represents template response
type TemplateResponse struct {
	Templates []Template `json:"templates"`
	Timestamp time.Time  `json:"timestamp"`
}

// Template represents a design template
type Template struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Elements    []Element `json:"elements"`
	Preview     string    `json:"preview,omitempty"`
}

// ErrorResponse represents error response
type ErrorResponse struct {
	Error     string    `json:"error"`
	Code      string    `json:"code"`
	Details   string    `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
