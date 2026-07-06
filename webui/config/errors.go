package config

import "errors"

var (
	// Configuration validation errors
	ErrInvalidPort          = errors.New("port must be between 1 and 65535")
	ErrInvalidMode          = errors.New("mode must be debug, release, or test")
	ErrInvalidReadTimeout   = errors.New("read timeout must be non-negative")
	ErrInvalidWriteTimeout  = errors.New("write timeout must be non-negative")
	ErrInvalidMinApprovals  = errors.New("minimum approvals must be non-negative")
	ErrInvalidPageSize      = errors.New("page size must be at least 1")
	ErrInvalidMaxTables     = errors.New("max tables per diagram must be at least 1")
	ErrInvalidRateLimit     = errors.New("rate limit requests must be at least 1")
)
