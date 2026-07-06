package constants

import "time"

// API versioning
const (
	APIVersion    = "v1"
	APIBasePrefix = "/api/" + APIVersion
)

// Web UI routes
const (
	WebUIPrefix        = "/admin"
	StaticAssetsPrefix = "/static"
	WebSocketPrefix    = "/ws"
)

// API endpoints
const (
	// Migration endpoints
	EndpointMigrations         = APIBasePrefix + "/migrations"
	EndpointMigrationStatus    = EndpointMigrations + "/status"
	EndpointMigrationExecute   = EndpointMigrations + "/execute"
	EndpointMigrationRollback  = EndpointMigrations + "/rollback"
	EndpointMigrationValidate  = EndpointMigrations + "/validate"
	EndpointMigrationHistory   = EndpointMigrations + "/history"
	EndpointMigrationCatalog   = EndpointMigrations + "/catalog"
	EndpointMigrationSearch    = EndpointMigrations + "/search"
	EndpointMigrationDesigner  = EndpointMigrations + "/designer"
	EndpointMigrationTemplate  = EndpointMigrations + "/template"
	EndpointMigrationDiff      = EndpointMigrations + "/diff"
	EndpointMigrationDryRun    = EndpointMigrations + "/dry-run"

	// Collaboration endpoints
	EndpointReviews            = APIBasePrefix + "/reviews"
	EndpointReviewCreate       = EndpointReviews + "/create"
	EndpointReviewApprove      = EndpointReviews + "/:id/approve"
	EndpointReviewReject       = EndpointReviews + "/:id/reject"
	EndpointReviewComment      = EndpointReviews + "/:id/comment"
	EndpointReviewList         = EndpointReviews + "/list"

	// Team collaboration endpoints
	EndpointTeams              = APIBasePrefix + "/teams"
	EndpointTeamMembers        = EndpointTeams + "/:id/members"
	EndpointTeamPermissions    = EndpointTeams + "/:id/permissions"

	// Workflow endpoints
	EndpointWorkflows          = APIBasePrefix + "/workflows"
	EndpointWorkflowCreate     = EndpointWorkflows + "/create"
	EndpointWorkflowExecute    = EndpointWorkflows + "/:id/execute"
	EndpointWorkflowStatus     = EndpointWorkflows + "/:id/status"
)

// WebSocket channels
const (
	WSChannelMigrationStatus   = "migration:status"
	WSChannelMigrationLogs     = "migration:logs"
	WSChannelReviewUpdates     = "review:updates"
	WSChannelTeamNotifications = "team:notifications"
	WSChannelRollbackProgress  = "rollback:progress"
	WSChannelRollbackAnalysis  = "rollback:analysis"
	WSChannelRollbackTest      = "rollback:test"
)

// Review statuses
type ReviewStatus string

const (
	ReviewStatusPending   ReviewStatus = "pending"
	ReviewStatusApproved  ReviewStatus = "approved"
	ReviewStatusRejected  ReviewStatus = "rejected"
	ReviewStatusChanges   ReviewStatus = "changes_requested"
)

// Review priorities
type ReviewPriority string

const (
	ReviewPriorityLow      ReviewPriority = "low"
	ReviewPriorityMedium   ReviewPriority = "medium"
	ReviewPriorityHigh     ReviewPriority = "high"
	ReviewPriorityCritical ReviewPriority = "critical"
)

// Migration catalog categories
type MigrationCategory string

const (
	CategorySchema      MigrationCategory = "schema"
	CategoryData        MigrationCategory = "data"
	CategorySecurity    MigrationCategory = "security"
	CategoryPerformance MigrationCategory = "performance"
	CategoryRefactor    MigrationCategory = "refactor"
)

// Designer element types
type DesignerElementType string

const (
	ElementTypeTable       DesignerElementType = "table"
	ElementTypeColumn      DesignerElementType = "column"
	ElementTypeIndex       DesignerElementType = "index"
	ElementTypeConstraint  DesignerElementType = "constraint"
	ElementTypeRelation    DesignerElementType = "relation"
	ElementTypeTrigger     DesignerElementType = "trigger"
	ElementTypeView        DesignerElementType = "view"
	ElementTypeProcedure   DesignerElementType = "procedure"
)

// Notification types
type NotificationType string

const (
	NotificationMigrationStarted   NotificationType = "migration_started"
	NotificationMigrationCompleted NotificationType = "migration_completed"
	NotificationMigrationFailed    NotificationType = "migration_failed"
	NotificationReviewRequested    NotificationType = "review_requested"
	NotificationReviewApproved     NotificationType = "review_approved"
	NotificationReviewRejected     NotificationType = "review_rejected"
	NotificationCommentAdded       NotificationType = "comment_added"
)

// Default values
const (
	DefaultWebUIPort           = 8080
	DefaultWebUIHost           = "0.0.0.0"
	DefaultReadTimeout         = 30 * time.Second
	DefaultWriteTimeout        = 30 * time.Second
	DefaultShutdownTimeout     = 10 * time.Second
	DefaultMaxRequestSize      = 10 * 1024 * 1024 // 10 MB
	DefaultWSPingInterval      = 30 * time.Second
	DefaultWSPongWait          = 60 * time.Second
	DefaultWSWriteWait         = 10 * time.Second
	DefaultWSMaxMessageSize    = 512 * 1024 // 512 KB
	DefaultCatalogPageSize     = 50
	DefaultSearchLimit         = 100
	DefaultReviewTimeout       = 24 * time.Hour
	DefaultSessionTimeout      = 8 * time.Hour
	DefaultMaxConcurrent       = 5
)

// HTTP headers
const (
	HeaderContentType   = "Content-Type"
	HeaderAuthorization = "Authorization"
	HeaderRequestID     = "X-Request-ID"
	HeaderUserID        = "X-User-ID"
	HeaderTeamID        = "X-Team-ID"
	HeaderAccept        = "Accept"
)

// Content types
const (
	ContentTypeJSON       = "application/json"
	ContentTypeHTML       = "text/html"
	ContentTypeText       = "text/plain"
	ContentTypeEventStream = "text/event-stream"
	ContentTypeSQL        = "application/sql"
	ContentTypeXML        = "application/xml"
	ContentTypeYAML       = "application/x-yaml"
)

// Error codes
const (
	ErrCodeInvalidRequest       = "INVALID_REQUEST"
	ErrCodeUnauthorized         = "UNAUTHORIZED"
	ErrCodeForbidden            = "FORBIDDEN"
	ErrCodeNotFound             = "NOT_FOUND"
	ErrCodeConflict             = "CONFLICT"
	ErrCodeValidationFailed     = "VALIDATION_FAILED"
	ErrCodeMigrationFailed      = "MIGRATION_FAILED"
	ErrCodeRollbackFailed       = "ROLLBACK_FAILED"
	ErrCodeReviewRequired       = "REVIEW_REQUIRED"
	ErrCodeInsufficientApprovals = "INSUFFICIENT_APPROVALS"
	ErrCodeInternalError        = "INTERNAL_ERROR"
)

// Error messages
const (
	MsgInvalidRequest       = "Invalid request format"
	MsgUnauthorized         = "Authentication required"
	MsgForbidden            = "Access denied"
	MsgNotFound             = "Resource not found"
	MsgConflict             = "Resource conflict"
	MsgValidationFailed     = "Validation failed"
	MsgMigrationFailed      = "Migration execution failed"
	MsgRollbackFailed       = "Rollback execution failed"
	MsgReviewRequired       = "Migration requires review approval"
	MsgInsufficientApprovals = "Insufficient approvals for migration"
	MsgInternalError        = "Internal server error"
)

// Metrics names
const (
	MetricWebUIRequests        = "webui_requests_total"
	MetricWebUIRequestDuration = "webui_request_duration_seconds"
	MetricWebUIErrors          = "webui_errors_total"
	MetricWSConnections        = "webui_ws_connections_active"
	MetricWSMessages           = "webui_ws_messages_total"
	MetricReviewsCreated       = "webui_reviews_created_total"
	MetricReviewsApproved      = "webui_reviews_approved_total"
	MetricReviewsRejected      = "webui_reviews_rejected_total"
	MetricCatalogSearches      = "webui_catalog_searches_total"
)

// Cache keys
const (
	CacheKeyMigrationStatus   = "migration:status:%s"
	CacheKeyMigrationHistory  = "migration:history:%s"
	CacheKeyCatalog           = "migration:catalog:%s"
	CacheKeyReview            = "review:%s"
	CacheKeyTeamMembers       = "team:members:%s"
	CacheKeyUserPermissions   = "user:permissions:%s"
)

// Cache TTLs
const (
	CacheTTLMigrationStatus  = 10 * time.Second
	CacheTTLMigrationHistory = 5 * time.Minute
	CacheTTLCatalog          = 15 * time.Minute
	CacheTTLReview           = 1 * time.Minute
	CacheTTLTeamMembers      = 10 * time.Minute
	CacheTTLUserPermissions  = 5 * time.Minute
)

// File paths
const (
	StaticAssetsDir  = "web/static"
	TemplatesDir     = "web/templates"
	MigrationsDir    = "migrations"
	DesignsDir       = "designs"
)

// Permissions
type Permission string

const (
	PermissionViewMigrations    Permission = "migrations:view"
	PermissionExecuteMigrations Permission = "migrations:execute"
	PermissionRollback          Permission = "migrations:rollback"
	PermissionCreateMigrations  Permission = "migrations:create"
	PermissionDeleteMigrations  Permission = "migrations:delete"
	PermissionReviewMigrations  Permission = "migrations:review"
	PermissionApproveMigrations Permission = "migrations:approve"
	PermissionManageTeams       Permission = "teams:manage"
	PermissionManageWorkflows   Permission = "workflows:manage"
)

// Workflow states
type WorkflowState string

const (
	WorkflowStatePending   WorkflowState = "pending"
	WorkflowStateRunning   WorkflowState = "running"
	WorkflowStateCompleted WorkflowState = "completed"
	WorkflowStateFailed    WorkflowState = "failed"
	WorkflowStateCancelled WorkflowState = "cancelled"
)
