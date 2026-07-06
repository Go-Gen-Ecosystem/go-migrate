/**
 * @file migration_script_cache.go
 * @description Specialized migration script cache implementation using Flyweight pattern
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/migration_script_cache.go
 */

package cache

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultMigrationScriptCache implements MigrationScriptCache interface
type DefaultMigrationScriptCache struct {
	*DefaultFlyweightCache
	scriptFactory    *MigrationScriptFactory
	scriptStats      *ScriptCacheStats
	scriptMutex      sync.RWMutex
	versionIndex     map[string][]string // version pattern -> script IDs
	typeIndex        map[ScriptType][]string // script type -> script IDs
	tagIndex         map[string][]string // tag -> script IDs
	preloadedScripts map[string]*MigrationScript
}

// NewMigrationScriptCache creates a new migration script cache
func NewMigrationScriptCache(config *CacheConfig) (*DefaultMigrationScriptCache, error) {
	if config == nil {
		config = DefaultMigrationScriptCacheConfig()
	}

	// Create base flyweight cache
	flyweightCache, err := NewFlyweightCache(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create flyweight cache: %w", err)
	}

	cache := &DefaultMigrationScriptCache{
		DefaultFlyweightCache: flyweightCache,
		scriptFactory:         NewMigrationScriptFactory(),
		scriptStats: &ScriptCacheStats{
			CacheStats:       *flyweightCache.GetStatistics(),
			ScriptsByType:    make(map[ScriptType]int64),
		},
		versionIndex:     make(map[string][]string),
		typeIndex:        make(map[ScriptType][]string),
		tagIndex:         make(map[string][]string),
		preloadedScripts: make(map[string]*MigrationScript),
	}

	return cache, nil
}

// GetScript retrieves a migration script
func (c *DefaultMigrationScriptCache) GetScript(ctx context.Context, scriptID string) (*MigrationScript, error) {
	c.scriptMutex.RLock()
	defer c.scriptMutex.RUnlock()

	// Check preloaded scripts first
	if script, exists := c.preloadedScripts[scriptID]; exists {
		script.UpdateLastAccessed()
		c.updateScriptStats(true)
		return script, nil
	}

	// Get from flyweight cache
	flyweight, exists, err := c.DefaultFlyweightCache.Get(ctx, scriptID)
	if err != nil {
		return nil, fmt.Errorf("failed to get script from cache: %w", err)
	}

	if !exists {
		c.updateScriptStats(false)
		return nil, fmt.Errorf("script not found: %s", scriptID)
	}

	script, ok := flyweight.(*MigrationScript)
	if !ok {
		return nil, fmt.Errorf("cached object is not a migration script")
	}

	c.updateScriptStats(true)
	return script, nil
}

// CacheScript caches a migration script
func (c *DefaultMigrationScriptCache) CacheScript(ctx context.Context, script *MigrationScript) error {
	c.scriptMutex.Lock()
	defer c.scriptMutex.Unlock()

	// Set flyweight metadata
	script.flyweightCreatedAt = time.Now()
	script.flyweightLastAccessed = time.Now()

	// Store in flyweight cache
	if err := c.DefaultFlyweightCache.Set(ctx, script.ID, script, c.config.TTL); err != nil {
		return fmt.Errorf("failed to cache script: %w", err)
	}

	// Update indexes
	c.updateIndexes(script)

	// Update script statistics
	c.scriptStats.ScriptCount++
	c.scriptStats.ScriptsByType[script.Type]++
	c.scriptStats.TotalScriptSize += script.Size
	c.scriptStats.AverageScriptSize = c.scriptStats.TotalScriptSize / c.scriptStats.ScriptCount

	return nil
}

// GetScriptsByVersion retrieves scripts by version pattern
func (c *DefaultMigrationScriptCache) GetScriptsByVersion(ctx context.Context, versionPattern string) ([]*MigrationScript, error) {
	c.scriptMutex.RLock()
	defer c.scriptMutex.RUnlock()

	var scripts []*MigrationScript
	var scriptIDs []string

	// Check if exact pattern exists in index
	if ids, exists := c.versionIndex[versionPattern]; exists {
		scriptIDs = ids
	} else {
		// Use regex matching for pattern
		regex, err := regexp.Compile(versionPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid version pattern: %w", err)
		}

		// Search through all cached scripts
		for version, ids := range c.versionIndex {
			if regex.MatchString(version) {
				scriptIDs = append(scriptIDs, ids...)
			}
		}
	}

	// Retrieve scripts
	for _, scriptID := range scriptIDs {
		script, err := c.GetScript(ctx, scriptID)
		if err != nil {
			continue // Skip missing scripts
		}
		scripts = append(scripts, script)
	}

	// Sort by version
	sort.Slice(scripts, func(i, j int) bool {
		return scripts[i].Version < scripts[j].Version
	})

	return scripts, nil
}

// GetScriptsByType retrieves scripts by type
func (c *DefaultMigrationScriptCache) GetScriptsByType(ctx context.Context, scriptType ScriptType) ([]*MigrationScript, error) {
	c.scriptMutex.RLock()
	defer c.scriptMutex.RUnlock()

	scriptIDs, exists := c.typeIndex[scriptType]
	if !exists {
		return []*MigrationScript{}, nil
	}

	var scripts []*MigrationScript
	for _, scriptID := range scriptIDs {
		script, err := c.GetScript(ctx, scriptID)
		if err != nil {
			continue // Skip missing scripts
		}
		scripts = append(scripts, script)
	}

	// Sort by version
	sort.Slice(scripts, func(i, j int) bool {
		return scripts[i].Version < scripts[j].Version
	})

	return scripts, nil
}

// InvalidateScript invalidates a cached script
func (c *DefaultMigrationScriptCache) InvalidateScript(ctx context.Context, scriptID string) error {
	c.scriptMutex.Lock()
	defer c.scriptMutex.Unlock()

	// Get script before deletion for index cleanup
	script, err := c.GetScript(ctx, scriptID)
	if err != nil {
		return nil // Script doesn't exist, nothing to invalidate
	}

	// Remove from flyweight cache
	if err := c.DefaultFlyweightCache.Delete(ctx, scriptID); err != nil {
		return fmt.Errorf("failed to delete script from cache: %w", err)
	}

	// Remove from preloaded scripts
	delete(c.preloadedScripts, scriptID)

	// Update indexes
	c.removeFromIndexes(script)

	// Update script statistics
	c.scriptStats.ScriptCount--
	c.scriptStats.ScriptsByType[script.Type]--
	c.scriptStats.TotalScriptSize -= script.Size
	if c.scriptStats.ScriptCount > 0 {
		c.scriptStats.AverageScriptSize = c.scriptStats.TotalScriptSize / c.scriptStats.ScriptCount
	} else {
		c.scriptStats.AverageScriptSize = 0
	}

	return nil
}

// PreloadScripts preloads scripts based on criteria
func (c *DefaultMigrationScriptCache) PreloadScripts(ctx context.Context, criteria *PreloadCriteria) error {
	c.scriptMutex.Lock()
	defer c.scriptMutex.Unlock()

	var candidateScripts []*MigrationScript

	// Get scripts by type
	if len(criteria.ScriptTypes) > 0 {
		for _, scriptType := range criteria.ScriptTypes {
			scripts, err := c.GetScriptsByType(ctx, scriptType)
			if err != nil {
				continue
			}
			candidateScripts = append(candidateScripts, scripts...)
		}
	}

	// Filter by version pattern
	if criteria.VersionPattern != "" {
		var filteredScripts []*MigrationScript
		regex, err := regexp.Compile(criteria.VersionPattern)
		if err != nil {
			return fmt.Errorf("invalid version pattern: %w", err)
		}

		for _, script := range candidateScripts {
			if regex.MatchString(script.Version) {
				filteredScripts = append(filteredScripts, script)
			}
		}
		candidateScripts = filteredScripts
	}

	// Filter by tags
	if len(criteria.Tags) > 0 {
		var filteredScripts []*MigrationScript
		for _, script := range candidateScripts {
			if c.hasAnyTag(script.Tags, criteria.Tags) {
				filteredScripts = append(filteredScripts, script)
			}
		}
		candidateScripts = filteredScripts
	}

	// Sort scripts
	if criteria.SortBy != "" {
		c.sortScripts(candidateScripts, criteria.SortBy, criteria.SortOrder)
	}

	// Limit count
	if criteria.MaxCount > 0 && len(candidateScripts) > criteria.MaxCount {
		candidateScripts = candidateScripts[:criteria.MaxCount]
	}

	// Preload scripts
	preloadedCount := int64(0)
	for _, script := range candidateScripts {
		c.preloadedScripts[script.ID] = script
		preloadedCount++
	}

	c.scriptStats.PreloadedScripts = preloadedCount
	return nil
}

// GetScriptStatistics returns script cache statistics
func (c *DefaultMigrationScriptCache) GetScriptStatistics() *ScriptCacheStats {
	c.scriptMutex.RLock()
	defer c.scriptMutex.RUnlock()

	// Update base cache stats
	baseStats := c.DefaultFlyweightCache.GetStatistics()
	c.scriptStats.CacheStats = *baseStats

	// Create a copy to avoid race conditions
	statsCopy := *c.scriptStats
	return &statsCopy
}

// Private methods

func (c *DefaultMigrationScriptCache) updateIndexes(script *MigrationScript) {
	// Update version index
	versionKey := script.Version
	c.versionIndex[versionKey] = append(c.versionIndex[versionKey], script.ID)

	// Update type index
	c.typeIndex[script.Type] = append(c.typeIndex[script.Type], script.ID)

	// Update tag index
	for _, tag := range script.Tags {
		c.tagIndex[tag] = append(c.tagIndex[tag], script.ID)
	}
}

func (c *DefaultMigrationScriptCache) removeFromIndexes(script *MigrationScript) {
	// Remove from version index
	versionKey := script.Version
	if ids, exists := c.versionIndex[versionKey]; exists {
		c.versionIndex[versionKey] = c.removeStringFromSlice(ids, script.ID)
		if len(c.versionIndex[versionKey]) == 0 {
			delete(c.versionIndex, versionKey)
		}
	}

	// Remove from type index
	if ids, exists := c.typeIndex[script.Type]; exists {
		c.typeIndex[script.Type] = c.removeStringFromSlice(ids, script.ID)
		if len(c.typeIndex[script.Type]) == 0 {
			delete(c.typeIndex, script.Type)
		}
	}

	// Remove from tag index
	for _, tag := range script.Tags {
		if ids, exists := c.tagIndex[tag]; exists {
			c.tagIndex[tag] = c.removeStringFromSlice(ids, script.ID)
			if len(c.tagIndex[tag]) == 0 {
				delete(c.tagIndex, tag)
			}
		}
	}
}

func (c *DefaultMigrationScriptCache) removeStringFromSlice(slice []string, item string) []string {
	for i, v := range slice {
		if v == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func (c *DefaultMigrationScriptCache) updateScriptStats(hit bool) {
	if hit {
		c.scriptStats.HitCount++
	} else {
		c.scriptStats.MissCount++
	}
	c.scriptStats.CalculateHitRatio()
}

func (c *DefaultMigrationScriptCache) hasAnyTag(scriptTags, filterTags []string) bool {
	for _, filterTag := range filterTags {
		for _, scriptTag := range scriptTags {
			if scriptTag == filterTag {
				return true
			}
		}
	}
	return false
}

func (c *DefaultMigrationScriptCache) sortScripts(scripts []*MigrationScript, sortBy, sortOrder string) {
	ascending := sortOrder != "desc"

	switch sortBy {
	case "version":
		sort.Slice(scripts, func(i, j int) bool {
			if ascending {
				return scripts[i].Version < scripts[j].Version
			}
			return scripts[i].Version > scripts[j].Version
		})
	case "created_at":
		sort.Slice(scripts, func(i, j int) bool {
			if ascending {
				return scripts[i].CreatedAt.Before(scripts[j].CreatedAt)
			}
			return scripts[i].CreatedAt.After(scripts[j].CreatedAt)
		})
	case "size":
		sort.Slice(scripts, func(i, j int) bool {
			if ascending {
				return scripts[i].Size < scripts[j].Size
			}
			return scripts[i].Size > scripts[j].Size
		})
	case "type":
		sort.Slice(scripts, func(i, j int) bool {
			if ascending {
				return scripts[i].Type < scripts[j].Type
			}
			return scripts[i].Type > scripts[j].Type
		})
	}
}

// MigrationScriptFactory creates migration script flyweights
type MigrationScriptFactory struct {
	mutex sync.RWMutex
}

// NewMigrationScriptFactory creates a new migration script factory
func NewMigrationScriptFactory() *MigrationScriptFactory {
	return &MigrationScriptFactory{}
}

// CreateFlyweight creates a new migration script flyweight
func (f *MigrationScriptFactory) CreateFlyweight(intrinsicState interface{}) (Flyweight, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	stateMap, ok := intrinsicState.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid intrinsic state type")
	}

	script := &MigrationScript{
		flyweightCreatedAt:   time.Now(),
		flyweightLastAccessed: time.Now(),
	}

	// Extract intrinsic state
	if id, ok := stateMap["id"].(string); ok {
		script.ID = id
	}
	if version, ok := stateMap["version"].(string); ok {
		script.Version = version
	}
	if scriptType, ok := stateMap["type"].(ScriptType); ok {
		script.Type = scriptType
	}
	if content, ok := stateMap["content"].(string); ok {
		script.Content = content
		script.Size = int64(len(content))
	}
	if checksum, ok := stateMap["checksum"].(string); ok {
		script.Checksum = checksum
	}
	if size, ok := stateMap["size"].(int64); ok {
		script.Size = size
	}

	return script, nil
}

// GetFlyweightType returns the type of flyweight this factory creates
func (f *MigrationScriptFactory) GetFlyweightType() string {
	return "migration_script"
}

// ValidateIntrinsicState validates the intrinsic state
func (f *MigrationScriptFactory) ValidateIntrinsicState(state interface{}) error {
	stateMap, ok := state.(map[string]interface{})
	if !ok {
		return fmt.Errorf("intrinsic state must be a map[string]interface{}")
	}

	// Validate required fields
	requiredFields := []string{"id", "version", "type", "content"}
	for _, field := range requiredFields {
		if _, exists := stateMap[field]; !exists {
			return fmt.Errorf("missing required field: %s", field)
		}
	}

	// Validate script type
	if scriptType, ok := stateMap["type"].(string); ok {
		if !isValidScriptType(scriptType) {
			return fmt.Errorf("invalid script type: %s", scriptType)
		}
	}

	return nil
}

// Helper functions

func isValidScriptType(scriptType string) bool {
	validTypes := []string{
		string(ScriptTypeUp),
		string(ScriptTypeDown),
		string(ScriptTypeSeed),
		string(ScriptTypeRollback),
		string(ScriptTypeSchema),
		string(ScriptTypeData),
		string(ScriptTypeCustom),
	}

	for _, validType := range validTypes {
		if scriptType == validType {
			return true
		}
	}
	return false
}

// DefaultMigrationScriptCacheConfig returns default configuration for migration script cache
func DefaultMigrationScriptCacheConfig() *CacheConfig {
	config := DefaultFlyweightCacheConfig()
	config.Name = "migration_script_cache"
	config.MaxSize = 500 // Reasonable limit for migration scripts
	config.TTL = 1 * time.Hour // Scripts can be cached longer
	config.CleanupInterval = 10 * time.Minute
	config.EvictionPolicy = EvictionPolicyLRU
	config.EvictionBatchSize = 5
	return config
}

// CreateMigrationScript creates a new migration script
func CreateMigrationScript(id, version, content string, scriptType ScriptType, tags []string) *MigrationScript {
	now := time.Now()
	return &MigrationScript{
		ID:        id,
		Version:   version,
		Type:      scriptType,
		Content:   content,
		Size:      int64(len(content)),
		CreatedAt: now,
		UpdatedAt: now,
		Tags:      tags,
		Properties: make(map[string]interface{}),
		flyweightCreatedAt:   now,
		flyweightLastAccessed: now,
	}
}

// GenerateScriptChecksum generates a checksum for a migration script
func GenerateScriptChecksum(script *MigrationScript) string {
	// Simple checksum generation (in real implementation, use proper hashing)
	content := fmt.Sprintf("%s:%s:%s:%s", script.ID, script.Version, script.Type, script.Content)
	return fmt.Sprintf("checksum_%d", len(content)) // Placeholder
}

// ValidateScript validates a migration script
func ValidateScript(script *MigrationScript) error {
	if script.ID == "" {
		return fmt.Errorf("script ID cannot be empty")
	}
	if script.Version == "" {
		return fmt.Errorf("script version cannot be empty")
	}
	if script.Content == "" {
		return fmt.Errorf("script content cannot be empty")
	}
	if script.Type == "" {
		return fmt.Errorf("script type cannot be empty")
	}
	if !isValidScriptType(string(script.Type)) {
		return fmt.Errorf("invalid script type: %s", script.Type)
	}
	return nil
}

// GetScriptKey generates a cache key for a migration script
func GetScriptKey(id, version string) string {
	return fmt.Sprintf("script:%s:%s", id, version)
}

// ParseScriptKey parses a script cache key
func ParseScriptKey(key string) (id, version string, err error) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[0] != "script" {
		return "", "", fmt.Errorf("invalid script key format: %s", key)
	}
	return parts[1], parts[2], nil
}
