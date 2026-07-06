package collaboration

import (
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

// Review represents a migration review
type Review struct {
	ID             string                    `json:"id"`
	MigrationID    string                    `json:"migrationId"`
	Title          string                    `json:"title"`
	Description    string                    `json:"description"`
	Author         string                    `json:"author"`
	AuthorEmail    string                    `json:"authorEmail"`
	Status         constants.ReviewStatus    `json:"status"`
	Priority       constants.ReviewPriority  `json:"priority"`
	Reviewers      []string                  `json:"reviewers"`
	Approvals      []Approval                `json:"approvals"`
	Comments       []Comment                 `json:"comments"`
	RequiredApprovals int                    `json:"requiredApprovals"`
	CreatedAt      time.Time                 `json:"createdAt"`
	UpdatedAt      time.Time                 `json:"updatedAt"`
	ClosedAt       *time.Time                `json:"closedAt,omitempty"`
	DueDate        *time.Time                `json:"dueDate,omitempty"`
	Tags           []string                  `json:"tags,omitempty"`
	Metadata       map[string]interface{}    `json:"metadata,omitempty"`
}

// Approval represents a review approval
type Approval struct {
	ID         string    `json:"id"`
	ReviewID   string    `json:"reviewId"`
	Reviewer   string    `json:"reviewer"`
	Email      string    `json:"email"`
	Approved   bool      `json:"approved"`
	Comment    string    `json:"comment,omitempty"`
	ApprovedAt time.Time `json:"approvedAt"`
}

// Comment represents a review comment
type Comment struct {
	ID        string    `json:"id"`
	ReviewID  string    `json:"reviewId"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	Content   string    `json:"content"`
	LineStart int       `json:"lineStart,omitempty"`
	LineEnd   int       `json:"lineEnd,omitempty"`
	Resolved  bool      `json:"resolved"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateReviewRequest represents create review request
type CreateReviewRequest struct {
	MigrationID       string                   `json:"migrationId" binding:"required"`
	Title             string                   `json:"title" binding:"required"`
	Description       string                   `json:"description"`
	Reviewers         []string                 `json:"reviewers" binding:"required"`
	RequiredApprovals int                      `json:"requiredApprovals"`
	Priority          constants.ReviewPriority `json:"priority"`
	DueDate           *time.Time               `json:"dueDate,omitempty"`
	Tags              []string                 `json:"tags,omitempty"`
}

// CreateReviewResponse represents create review response
type CreateReviewResponse struct {
	Review    Review    `json:"review"`
	Timestamp time.Time `json:"timestamp"`
}

// ApproveReviewRequest represents approve review request
type ApproveReviewRequest struct {
	Comment string `json:"comment,omitempty"`
}

// ApproveReviewResponse represents approve review response
type ApproveReviewResponse struct {
	Success   bool      `json:"success"`
	Review    Review    `json:"review"`
	Timestamp time.Time `json:"timestamp"`
}

// RejectReviewRequest represents reject review request
type RejectReviewRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// RejectReviewResponse represents reject review response
type RejectReviewResponse struct {
	Success   bool      `json:"success"`
	Review    Review    `json:"review"`
	Timestamp time.Time `json:"timestamp"`
}

// AddCommentRequest represents add comment request
type AddCommentRequest struct {
	Content   string `json:"content" binding:"required"`
	LineStart int    `json:"lineStart,omitempty"`
	LineEnd   int    `json:"lineEnd,omitempty"`
}

// AddCommentResponse represents add comment response
type AddCommentResponse struct {
	Comment   Comment   `json:"comment"`
	Timestamp time.Time `json:"timestamp"`
}

// ListReviewsRequest represents list reviews request
type ListReviewsRequest struct {
	Status       constants.ReviewStatus   `json:"status,omitempty"`
	Priority     constants.ReviewPriority `json:"priority,omitempty"`
	Author       string                   `json:"author,omitempty"`
	Reviewer     string                   `json:"reviewer,omitempty"`
	MigrationID  string                   `json:"migrationId,omitempty"`
	Limit        int                      `json:"limit,omitempty"`
	Offset       int                      `json:"offset,omitempty"`
}

// ListReviewsResponse represents list reviews response
type ListReviewsResponse struct {
	Reviews   []Review  `json:"reviews"`
	Total     int       `json:"total"`
	Limit     int       `json:"limit"`
	Offset    int       `json:"offset"`
	Timestamp time.Time `json:"timestamp"`
}

// Team represents a collaboration team
type Team struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Members     []TeamMember           `json:"members"`
	Permissions []string               `json:"permissions"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// TeamMember represents a team member
type TeamMember struct {
	UserID      string                 `json:"userId"`
	Email       string                 `json:"email"`
	Name        string                 `json:"name"`
	Role        string                 `json:"role"`
	Permissions []constants.Permission `json:"permissions"`
	JoinedAt    time.Time              `json:"joinedAt"`
}

// Workflow represents an approval workflow
type Workflow struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Steps       []WorkflowStep         `json:"steps"`
	Status      constants.WorkflowState `json:"status"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	StartedAt   *time.Time             `json:"startedAt,omitempty"`
	CompletedAt *time.Time             `json:"completedAt,omitempty"`
}

// WorkflowStep represents a workflow step
type WorkflowStep struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Type         string                 `json:"type"`
	Status       constants.WorkflowState `json:"status"`
	Approvers    []string               `json:"approvers,omitempty"`
	MinApprovals int                    `json:"minApprovals,omitempty"`
	Actions      []string               `json:"actions,omitempty"`
	StartedAt    *time.Time             `json:"startedAt,omitempty"`
	CompletedAt  *time.Time             `json:"completedAt,omitempty"`
}

// Notification represents a user notification
type Notification struct {
	ID        string                      `json:"id"`
	Type      constants.NotificationType  `json:"type"`
	Title     string                      `json:"title"`
	Message   string                      `json:"message"`
	UserID    string                      `json:"userId"`
	Read      bool                        `json:"read"`
	Data      map[string]interface{}      `json:"data,omitempty"`
	CreatedAt time.Time                   `json:"createdAt"`
}

// ErrorResponse represents error response
type ErrorResponse struct {
	Error     string    `json:"error"`
	Code      string    `json:"code"`
	Details   string    `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
