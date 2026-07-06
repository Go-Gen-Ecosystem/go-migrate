/**
 * @file executor.go
 * @description Redis migration executor implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/nosql/redis/executor.go
 */

package redis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	Database int
	SSL      bool
}

// DatabaseInfo holds database information
type DatabaseInfo struct {
	Type         string
	Version      string
	Host         string
	Port         int
	Database     int
	Size         int64
	IsCluster    bool
	IsReplicated bool
}

// RedisMigrationExecutor implements MigrationExecutor for Redis
type RedisMigrationExecutor struct {
	client *redis.Client
	config *RedisConfig
}

// NewRedisMigrationExecutor creates a new Redis migration executor
func NewRedisMigrationExecutor(config *RedisConfig) *RedisMigrationExecutor {
	return &RedisMigrationExecutor{
		config: config,
	}
}

// Connect establishes connection to Redis
func (e *RedisMigrationExecutor) Connect(ctx context.Context) error {
	// Build Redis options
	opts := &redis.Options{
		Addr:     fmt.Sprintf("%s:%d", e.config.Host, e.config.Port),
		Password: e.config.Password,
		DB:       e.config.Database,
	}

	// SSL/TLS configuration
	if e.config.SSL {
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	e.client = redis.NewClient(opts)

	// Test connection
	_, err := e.client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return nil
}

// Disconnect closes the Redis connection
func (e *RedisMigrationExecutor) Disconnect(ctx context.Context) error {
	if e.client != nil {
		err := e.client.Close()
		e.client = nil
		return err
	}
	return nil
}

// Ping checks the connection to Redis
func (e *RedisMigrationExecutor) Ping(ctx context.Context) error {
	if e.client == nil {
		return fmt.Errorf("not connected to Redis")
	}
	
	_, err := e.client.Ping(ctx).Result()
	return err
}

// GetDatabaseInfo returns information about the Redis database
func (e *RedisMigrationExecutor) GetDatabaseInfo(ctx context.Context) (*DatabaseInfo, error) {
	if e.client == nil {
		return nil, fmt.Errorf("not connected to Redis")
	}

	// Get Redis server info
	infoResult, err := e.client.Info(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis server info: %w", err)
	}

	// Parse server info
	infoMap := parseRedisInfo(infoResult)
	
	version := infoMap["redis_version"]
	host := e.config.Host

	// Get database size
	dbSize, err := e.client.DBSize(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get database size: %w", err)
	}

	// Check if Redis is in cluster mode
	isCluster := false
	clusterInfo, err := e.client.ClusterInfo(ctx).Result()
	if err == nil && strings.Contains(clusterInfo, "cluster_state:ok") {
		isCluster = true
	}

	// Check replication info
	replicationInfo := infoMap["role"]
	isReplicated := replicationInfo == "master" || replicationInfo == "slave"

	info := &DatabaseInfo{
		Type:         "redis",
		Version:      version,
		Host:         host,
		Port:         e.config.Port,
		Database:     e.config.Database,
		Size:         dbSize,
		IsCluster:    isCluster,
		IsReplicated: isReplicated,
	}

	return info, nil
}

// Execute executes a Redis migration
func (e *RedisMigrationExecutor) Execute(ctx context.Context, migration *types.Migration) error {
	if e.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	startTime := time.Now()

	// Parse and execute Redis commands
	commands, err := e.parseRedisCommands(migration.Script)
	if err != nil {
		migration.State = "failed"
		migration.ErrorMessage = fmt.Sprintf("failed to parse migration script: %v", err)
		return fmt.Errorf("failed to parse Redis commands: %w", err)
	}

	// Execute commands in a pipeline for better performance
	pipe := e.client.Pipeline()
	
	for _, cmd := range commands {
		if err := e.addCommandToPipeline(pipe, cmd); err != nil {
			migration.State = "failed"
			migration.ErrorMessage = fmt.Sprintf("failed to add command to pipeline: %v", err)
			return fmt.Errorf("failed to add command to pipeline: %w", err)
		}
	}

	// Execute the pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		migration.State = "failed"
		migration.ErrorMessage = fmt.Sprintf("failed to execute pipeline: %v", err)
		return fmt.Errorf("failed to execute Redis pipeline: %w", err)
	}

	// Update migration state
	executedAt := time.Now()
	migration.State = "applied"
	migration.ExecutedAt = &executedAt
	migration.ExecutionTime = time.Since(startTime)
	migration.UpdatedAt = time.Now()

	return nil
}

// ExecuteDryRun simulates migration execution without applying changes
func (e *RedisMigrationExecutor) ExecuteDryRun(ctx context.Context, migration *types.Migration) (*types.ExecutionPlan, error) {
	commands, err := e.parseRedisCommands(migration.Script)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis commands: %w", err)
	}

	plan := &types.ExecutionPlan{
		Migrations:    []*types.Migration{migration},
		TotalCount:    1,
		EstimatedTime: time.Duration(len(commands)) * 10 * time.Millisecond, // Rough estimate
		Warnings:      []string{},
		Errors:        []string{},
	}

	// Validate each command
	for i, cmd := range commands {
		if err := e.validateCommand(cmd); err != nil {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Command %d: %v", i+1, err))
		}

		// Check for potentially dangerous commands
		if e.isDangerousCommand(cmd) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Command %d contains potentially dangerous operation: %s", i+1, cmd.Name))
		}
	}

	return plan, nil
}

// Rollback executes rollback for a Redis migration
func (e *RedisMigrationExecutor) Rollback(ctx context.Context, migration *types.Migration) error {
	if e.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	if migration.RollbackSQL == "" {
		return fmt.Errorf("no rollback script available for migration %s", migration.ID)
	}

	startTime := time.Now()

	// Parse and execute rollback commands
	commands, err := e.parseRedisCommands(migration.RollbackSQL)
	if err != nil {
		return fmt.Errorf("failed to parse rollback commands: %w", err)
	}

	// Execute rollback commands in a pipeline
	pipe := e.client.Pipeline()
	
	for _, cmd := range commands {
		if err := e.addCommandToPipeline(pipe, cmd); err != nil {
			return fmt.Errorf("failed to add rollback command to pipeline: %w", err)
		}
	}

	// Execute the pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute rollback pipeline: %w", err)
	}

	// Update migration state
	rolledBackAt := time.Now()
	migration.State = "rolled_back"
	migration.RolledBackAt = &rolledBackAt
	migration.ExecutionTime = time.Since(startTime)
	migration.UpdatedAt = time.Now()

	return nil
}

// ValidateRollback validates a rollback before execution
func (e *RedisMigrationExecutor) ValidateRollback(ctx context.Context, migration *types.Migration) error {
	if strings.TrimSpace(migration.RollbackSQL) == "" {
		return fmt.Errorf("migration %s has empty rollback script", migration.ID)
	}

	commands, err := e.parseRedisCommands(migration.RollbackSQL)
	if err != nil {
		return fmt.Errorf("invalid Redis commands in rollback script: %w", err)
	}

	for i, cmd := range commands {
		if err := e.validateCommand(cmd); err != nil {
			return fmt.Errorf("invalid command %d in rollback script: %w", i+1, err)
		}
	}

	return nil
}

// ValidateMigration validates a migration before execution
func (e *RedisMigrationExecutor) ValidateMigration(ctx context.Context, migration *types.Migration) error {
	if strings.TrimSpace(migration.Script) == "" {
		return fmt.Errorf("migration %s has empty script", migration.ID)
	}

	commands, err := e.parseRedisCommands(migration.Script)
	if err != nil {
		return fmt.Errorf("invalid Redis commands in migration script: %w", err)
	}

	for i, cmd := range commands {
		if err := e.validateCommand(cmd); err != nil {
			return fmt.Errorf("invalid command %d in migration script: %w", i+1, err)
		}
	}

	return nil
}

// GetExecutionPlan returns execution plan for multiple migrations
func (e *RedisMigrationExecutor) GetExecutionPlan(migrations []*types.Migration) ([]string, error) {
	var plan []string

	for _, migration := range migrations {
		commands, err := e.parseRedisCommands(migration.Script)
		if err != nil {
			return nil, fmt.Errorf("failed to parse migration %s: %w", migration.ID, err)
		}

		for _, cmd := range commands {
			plan = append(plan, fmt.Sprintf("// Migration: %s\n// Command: %s\n%s",
				migration.ID, cmd.Name, cmd.Script))
		}
	}

	return plan, nil
}

// RedisCommand represents a Redis command
type RedisCommand struct {
	Name   string                 `json:"name"`
	Args   []interface{}          `json:"args"`
	Script string                 `json:"script"`
	Type   string                 `json:"type"` // SET, GET, HSET, LPUSH, etc.
}

// parseRedisCommands parses Redis migration script into commands
func (e *RedisMigrationExecutor) parseRedisCommands(script string) ([]RedisCommand, error) {
	var commands []RedisCommand

	// Split script by lines and parse each command
	lines := strings.Split(script, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		cmd, err := e.parseCommand(line)
		if err != nil {
			return nil, fmt.Errorf("failed to parse command '%s': %w", line, err)
		}
		commands = append(commands, cmd)
	}

	return commands, nil
}

// parseCommand parses a single Redis command
func (e *RedisMigrationExecutor) parseCommand(script string) (RedisCommand, error) {
	script = strings.TrimSpace(script)

	// Try to parse as JSON first
	var jsonCmd RedisCommand
	if err := json.Unmarshal([]byte(script), &jsonCmd); err == nil {
		jsonCmd.Script = script
		return jsonCmd, nil
	}

	// Parse Redis CLI-style commands
	parts := strings.Fields(script)
	if len(parts) == 0 {
		return RedisCommand{}, fmt.Errorf("empty command")
	}

	cmd := RedisCommand{
		Name:   strings.ToUpper(parts[0]),
		Script: script,
		Type:   strings.ToUpper(parts[0]),
	}

	// Convert string arguments to interface{}
	for i := 1; i < len(parts); i++ {
		// Try to parse as number first
		if val, err := strconv.ParseInt(parts[i], 10, 64); err == nil {
			cmd.Args = append(cmd.Args, val)
		} else if val, err := strconv.ParseFloat(parts[i], 64); err == nil {
			cmd.Args = append(cmd.Args, val)
		} else {
			// Remove quotes if present
			arg := strings.Trim(parts[i], "\"'")
			cmd.Args = append(cmd.Args, arg)
		}
	}

	return cmd, nil
}

// addCommandToPipeline adds a Redis command to the pipeline
func (e *RedisMigrationExecutor) addCommandToPipeline(pipe redis.Pipeliner, cmd RedisCommand) error {
	switch cmd.Name {
	case "SET":
		if len(cmd.Args) < 2 {
			return fmt.Errorf("SET command requires at least 2 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		value := fmt.Sprintf("%v", cmd.Args[1])
		pipe.Set(context.Background(), key, value, 0)
	case "GET":
		if len(cmd.Args) < 1 {
			return fmt.Errorf("GET command requires at least 1 argument")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		pipe.Get(context.Background(), key)
	case "DEL":
		if len(cmd.Args) < 1 {
			return fmt.Errorf("DEL command requires at least 1 argument")
		}
		keys := make([]string, len(cmd.Args))
		for i, arg := range cmd.Args {
			keys[i] = fmt.Sprintf("%v", arg)
		}
		pipe.Del(context.Background(), keys...)
	case "HSET":
		if len(cmd.Args) < 3 {
			return fmt.Errorf("HSET command requires at least 3 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		field := fmt.Sprintf("%v", cmd.Args[1])
		value := fmt.Sprintf("%v", cmd.Args[2])
		pipe.HSet(context.Background(), key, field, value)
	case "HGET":
		if len(cmd.Args) < 2 {
			return fmt.Errorf("HGET command requires at least 2 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		field := fmt.Sprintf("%v", cmd.Args[1])
		pipe.HGet(context.Background(), key, field)
	case "LPUSH":
		if len(cmd.Args) < 2 {
			return fmt.Errorf("LPUSH command requires at least 2 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		values := make([]interface{}, len(cmd.Args)-1)
		for i := 1; i < len(cmd.Args); i++ {
			values[i-1] = cmd.Args[i]
		}
		pipe.LPush(context.Background(), key, values...)
	case "RPUSH":
		if len(cmd.Args) < 2 {
			return fmt.Errorf("RPUSH command requires at least 2 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		values := make([]interface{}, len(cmd.Args)-1)
		for i := 1; i < len(cmd.Args); i++ {
			values[i-1] = cmd.Args[i]
		}
		pipe.RPush(context.Background(), key, values...)
	case "SADD":
		if len(cmd.Args) < 2 {
			return fmt.Errorf("SADD command requires at least 2 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		values := make([]interface{}, len(cmd.Args)-1)
		for i := 1; i < len(cmd.Args); i++ {
			values[i-1] = cmd.Args[i]
		}
		pipe.SAdd(context.Background(), key, values...)
	case "ZADD":
		if len(cmd.Args) < 3 {
			return fmt.Errorf("ZADD command requires at least 3 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		score, err := strconv.ParseFloat(fmt.Sprintf("%v", cmd.Args[1]), 64)
		if err != nil {
			return fmt.Errorf("invalid score for ZADD: %v", cmd.Args[1])
		}
		member := fmt.Sprintf("%v", cmd.Args[2])
		pipe.ZAdd(context.Background(), key, &redis.Z{Score: score, Member: member})
	case "EXPIRE":
		if len(cmd.Args) < 2 {
			return fmt.Errorf("EXPIRE command requires at least 2 arguments")
		}
		key := fmt.Sprintf("%v", cmd.Args[0])
		seconds, err := strconv.ParseInt(fmt.Sprintf("%v", cmd.Args[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid expiration time for EXPIRE: %v", cmd.Args[1])
		}
		pipe.Expire(context.Background(), key, time.Duration(seconds)*time.Second)
	default:
		// For unsupported commands, use Do method
		args := make([]interface{}, len(cmd.Args)+1)
		args[0] = cmd.Name
		copy(args[1:], cmd.Args)
		pipe.Do(context.Background(), args...)
	}

	return nil
}

// validateCommand validates a Redis command
func (e *RedisMigrationExecutor) validateCommand(cmd RedisCommand) error {
	if cmd.Name == "" {
		return fmt.Errorf("command name is required")
	}

	// Basic validation for common commands
	switch cmd.Name {
	case "SET", "GET", "DEL", "HSET", "HGET", "LPUSH", "RPUSH", "SADD", "ZADD", "EXPIRE":
		// These are safe commands
		return nil
	case "FLUSHDB", "FLUSHALL":
		return fmt.Errorf("dangerous command %s not allowed in migrations", cmd.Name)
	case "EVAL", "EVALSHA":
		// Lua scripts need special validation
		if len(cmd.Args) == 0 {
			return fmt.Errorf("EVAL command requires script argument")
		}
	}

	return nil
}

// isDangerousCommand checks if the command is potentially dangerous
func (e *RedisMigrationExecutor) isDangerousCommand(cmd RedisCommand) bool {
	dangerousCommands := []string{"FLUSHDB", "FLUSHALL", "SHUTDOWN", "DEBUG", "CONFIG"}
	for _, dangerous := range dangerousCommands {
		if cmd.Name == dangerous {
			return true
		}
	}
	return false
}

// parseRedisInfo parses Redis INFO command output into a map
func parseRedisInfo(info string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(info, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	
	return result
}
