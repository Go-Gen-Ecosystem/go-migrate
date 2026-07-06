/**
 * @file metrics.go
 * @description Cache metrics collection and monitoring implementation
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/metrics.go
 */

package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultCacheMetrics implements CacheMetrics interface
type DefaultCacheMetrics struct {
	// Atomic counters for thread-safe operations
	hitCount          int64
	missCount         int64
	setCount          int64
	deleteCount       int64
	evictionCount     int64
	errorCount        int64
	
	// Memory and size metrics
	memoryUsage       int64
	itemCount         int64
	totalSize         int64
	
	// Timing metrics
	avgGetTime        int64 // nanoseconds
	avgSetTime        int64 // nanoseconds
	avgDeleteTime     int64 // nanoseconds
	
	// Configuration
	config            *MetricsConfig
	
	// Detailed timing tracking
	getTimings        []time.Duration
	setTimings        []time.Duration
	deleteTimings     []time.Duration
	timingsMutex      sync.RWMutex
	
	// Event tracking
	events            []MetricEvent
	eventsMutex       sync.RWMutex
	
	// Lifecycle
	startTime         time.Time
	lastResetTime     time.Time
	
	// Percentile tracking
	percentileTracker *PercentileTracker
}

// Interface methods for CacheMetrics compatibility

// RecordHit records a cache hit for the interface
func (m *DefaultCacheMetrics) RecordHit(cacheName string) {
	m.recordHitInternal(context.Background(), cacheName, 0)
}

// RecordMiss records a cache miss for the interface
func (m *DefaultCacheMetrics) RecordMiss(cacheName string) {
	m.recordMissInternal(context.Background(), cacheName, 0)
}

// RecordEviction records a cache eviction for the interface
func (m *DefaultCacheMetrics) RecordEviction(cacheName string, reason string) {
	m.recordEvictionInternal(context.Background(), cacheName, reason)
}

// RecordOperation records a cache operation for the interface
func (m *DefaultCacheMetrics) RecordOperation(cacheName string, operation string, duration time.Duration) {
	switch operation {
	case "get":
		m.recordHitInternal(context.Background(), cacheName, duration)
	case "set":
		m.RecordSet(context.Background(), cacheName, 0, duration)
	case "delete":
		m.RecordDelete(context.Background(), cacheName, 0, duration)
	}
}

// GetMetrics returns metrics for a specific cache (interface method)
func (m *DefaultCacheMetrics) GetMetrics(cacheName string) *CacheMetrics {
	// Return nil as this is a placeholder implementation
	return nil
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	EnableDetailedTiming bool
	EnableEventTracking  bool
	MaxTimingSamples     int
	MaxEventSamples      int
	PercentileEnabled    bool
	ResetInterval        time.Duration
}

// MetricEvent represents a cache event
type MetricEvent struct {
	Type      string
	Key       string
	Timestamp time.Time
	Duration  time.Duration
	Success   bool
	Error     string
	Metadata  map[string]interface{}
}

// PercentileTracker tracks percentile metrics
type PercentileTracker struct {
	getLatencies    []time.Duration
	setLatencies    []time.Duration
	deleteLatencies []time.Duration
	mutex           sync.RWMutex
	maxSamples      int
}

// NewCacheMetrics creates a new cache metrics instance
func NewCacheMetrics(config *MetricsConfig) *DefaultCacheMetrics {
	if config == nil {
		config = &MetricsConfig{
			EnableDetailedTiming: true,
			EnableEventTracking:  true,
			MaxTimingSamples:     1000,
			MaxEventSamples:      500,
			PercentileEnabled:    true,
			ResetInterval:        24 * time.Hour,
		}
	}

	metrics := &DefaultCacheMetrics{
		config:            config,
		startTime:         time.Now(),
		lastResetTime:     time.Now(),
		getTimings:        make([]time.Duration, 0, config.MaxTimingSamples),
		setTimings:        make([]time.Duration, 0, config.MaxTimingSamples),
		deleteTimings:     make([]time.Duration, 0, config.MaxTimingSamples),
		events:            make([]MetricEvent, 0, config.MaxEventSamples),
	}

	if config.PercentileEnabled {
		metrics.percentileTracker = &PercentileTracker{
			getLatencies:    make([]time.Duration, 0, config.MaxTimingSamples),
			setLatencies:    make([]time.Duration, 0, config.MaxTimingSamples),
			deleteLatencies: make([]time.Duration, 0, config.MaxTimingSamples),
			maxSamples:      config.MaxTimingSamples,
		}
	}

	// Start automatic reset routine if configured
	if config.ResetInterval > 0 {
		go metrics.autoResetRoutine()
	}

	return metrics
}

// RecordHit records a cache hit
// RecordHitWithContext records a cache hit with context
func (m *DefaultCacheMetrics) RecordHitWithContext(ctx context.Context, key string, duration time.Duration) {
	atomic.AddInt64(&m.hitCount, 1)
	m.updateAverageGetTime(duration)
	
	if m.config.EnableDetailedTiming {
		m.addGetTiming(duration)
	}
	
	if m.config.PercentileEnabled {
		m.percentileTracker.addGetLatency(duration)
	}
	
	if m.config.EnableEventTracking {
		m.addEvent(MetricEvent{
			Type:      "hit",
			Key:       key,
			Timestamp: time.Now(),
			Duration:  duration,
			Success:   true,
		})
	}
}

// RecordMiss records a cache miss
// RecordMissWithContext records a cache miss with context
func (m *DefaultCacheMetrics) RecordMissWithContext(ctx context.Context, key string, duration time.Duration) {
	atomic.AddInt64(&m.missCount, 1)
	m.updateAverageGetTime(duration)
	
	if m.config.EnableDetailedTiming {
		m.addGetTiming(duration)
	}
	
	if m.config.PercentileEnabled {
		m.percentileTracker.addGetLatency(duration)
	}
	
	if m.config.EnableEventTracking {
		m.addEvent(MetricEvent{
			Type:      "miss",
			Key:       key,
			Timestamp: time.Now(),
			Duration:  duration,
			Success:   true,
		})
	}
}

// RecordSet records a cache set operation
func (m *DefaultCacheMetrics) RecordSet(ctx context.Context, key string, size int64, duration time.Duration) {
	atomic.AddInt64(&m.setCount, 1)
	atomic.AddInt64(&m.totalSize, size)
	atomic.AddInt64(&m.itemCount, 1)
	m.updateAverageSetTime(duration)
	
	if m.config.EnableDetailedTiming {
		m.addSetTiming(duration)
	}
	
	if m.config.PercentileEnabled {
		m.percentileTracker.addSetLatency(duration)
	}
	
	if m.config.EnableEventTracking {
		m.addEvent(MetricEvent{
			Type:      "set",
			Key:       key,
			Timestamp: time.Now(),
			Duration:  duration,
			Success:   true,
			Metadata: map[string]interface{}{
				"size": size,
			},
		})
	}
}

// RecordDelete records a cache delete operation
func (m *DefaultCacheMetrics) RecordDelete(ctx context.Context, key string, size int64, duration time.Duration) {
	atomic.AddInt64(&m.deleteCount, 1)
	atomic.AddInt64(&m.totalSize, -size)
	atomic.AddInt64(&m.itemCount, -1)
	m.updateAverageDeleteTime(duration)
	
	if m.config.EnableDetailedTiming {
		m.addDeleteTiming(duration)
	}
	
	if m.config.PercentileEnabled {
		m.percentileTracker.addDeleteLatency(duration)
	}
	
	if m.config.EnableEventTracking {
		m.addEvent(MetricEvent{
			Type:      "delete",
			Key:       key,
			Timestamp: time.Now(),
			Duration:  duration,
			Success:   true,
			Metadata: map[string]interface{}{
				"size": size,
			},
		})
	}
}

// RecordEviction records a cache eviction
// RecordEvictionWithContext records a cache eviction with context
func (m *DefaultCacheMetrics) RecordEvictionWithContext(ctx context.Context, key string, reason string) {
	atomic.AddInt64(&m.evictionCount, 1)
	atomic.AddInt64(&m.itemCount, -1)
	
	if m.config.EnableEventTracking {
		m.addEvent(MetricEvent{
			Type:      "eviction",
			Key:       key,
			Timestamp: time.Now(),
			Success:   true,
			Metadata: map[string]interface{}{
				"reason": reason,
			},
		})
	}
}

// RecordError records a cache error
func (m *DefaultCacheMetrics) RecordError(ctx context.Context, operation string, key string, err error) {
	atomic.AddInt64(&m.errorCount, 1)
	
	if m.config.EnableEventTracking {
		m.addEvent(MetricEvent{
			Type:      "error",
			Key:       key,
			Timestamp: time.Now(),
			Success:   false,
			Error:     err.Error(),
			Metadata: map[string]interface{}{
				"operation": operation,
			},
		})
	}
}

// UpdateMemoryUsage updates memory usage metrics
func (m *DefaultCacheMetrics) UpdateMemoryUsage(ctx context.Context, usage int64) {
	atomic.StoreInt64(&m.memoryUsage, usage)
}

// GetHitCount returns the hit count
func (m *DefaultCacheMetrics) GetHitCount() int64 {
	return atomic.LoadInt64(&m.hitCount)
}

// GetMissCount returns the miss count
func (m *DefaultCacheMetrics) GetMissCount() int64 {
	return atomic.LoadInt64(&m.missCount)
}

// GetHitRatio returns the hit ratio
func (m *DefaultCacheMetrics) GetHitRatio() float64 {
	hits := atomic.LoadInt64(&m.hitCount)
	misses := atomic.LoadInt64(&m.missCount)
	total := hits + misses
	
	if total == 0 {
		return 0.0
	}
	
	return float64(hits) / float64(total)
}

// GetTotalOperations returns the total number of operations
func (m *DefaultCacheMetrics) GetTotalOperations() int64 {
	return atomic.LoadInt64(&m.hitCount) + 
		   atomic.LoadInt64(&m.missCount) + 
		   atomic.LoadInt64(&m.setCount) + 
		   atomic.LoadInt64(&m.deleteCount)
}

// GetAverageGetTime returns the average get operation time
func (m *DefaultCacheMetrics) GetAverageGetTime() time.Duration {
	return time.Duration(atomic.LoadInt64(&m.avgGetTime))
}

// GetAverageSetTime returns the average set operation time
func (m *DefaultCacheMetrics) GetAverageSetTime() time.Duration {
	return time.Duration(atomic.LoadInt64(&m.avgSetTime))
}

// GetMemoryUsage returns the current memory usage
func (m *DefaultCacheMetrics) GetMemoryUsage() int64 {
	return atomic.LoadInt64(&m.memoryUsage)
}

// GetStatistics returns comprehensive statistics
func (m *DefaultCacheMetrics) GetStatistics() map[string]interface{} {
	stats := map[string]interface{}{
		"hit_count":           m.GetHitCount(),
		"miss_count":          m.GetMissCount(),
		"set_count":           atomic.LoadInt64(&m.setCount),
		"delete_count":        atomic.LoadInt64(&m.deleteCount),
		"eviction_count":      atomic.LoadInt64(&m.evictionCount),
		"error_count":         atomic.LoadInt64(&m.errorCount),
		"hit_ratio":           m.GetHitRatio(),
		"total_operations":    m.GetTotalOperations(),
		"memory_usage":        m.GetMemoryUsage(),
		"item_count":          atomic.LoadInt64(&m.itemCount),
		"total_size":          atomic.LoadInt64(&m.totalSize),
		"avg_get_time_ns":     atomic.LoadInt64(&m.avgGetTime),
		"avg_set_time_ns":     atomic.LoadInt64(&m.avgSetTime),
		"avg_delete_time_ns":  atomic.LoadInt64(&m.avgDeleteTime),
		"start_time":          m.startTime,
		"last_reset_time":     m.lastResetTime,
		"uptime_seconds":      time.Since(m.startTime).Seconds(),
	}

	// Add percentile statistics if enabled
	if m.config.PercentileEnabled && m.percentileTracker != nil {
		percentiles := m.percentileTracker.getPercentiles()
		stats["percentiles"] = percentiles
	}

	// Add timing distribution if enabled
	if m.config.EnableDetailedTiming {
		m.timingsMutex.RLock()
		stats["get_timing_samples"] = len(m.getTimings)
		stats["set_timing_samples"] = len(m.setTimings)
		stats["delete_timing_samples"] = len(m.deleteTimings)
		m.timingsMutex.RUnlock()
	}

	// Add event count if enabled
	if m.config.EnableEventTracking {
		m.eventsMutex.RLock()
		stats["event_samples"] = len(m.events)
		m.eventsMutex.RUnlock()
	}

	return stats
}

// GetDetailedTimings returns detailed timing information
func (m *DefaultCacheMetrics) GetDetailedTimings() map[string][]time.Duration {
	if !m.config.EnableDetailedTiming {
		return nil
	}

	m.timingsMutex.RLock()
	defer m.timingsMutex.RUnlock()

	return map[string][]time.Duration{
		"get_timings":    append([]time.Duration(nil), m.getTimings...),
		"set_timings":    append([]time.Duration(nil), m.setTimings...),
		"delete_timings": append([]time.Duration(nil), m.deleteTimings...),
	}
}

// GetEvents returns recent events
func (m *DefaultCacheMetrics) GetEvents() []MetricEvent {
	if !m.config.EnableEventTracking {
		return nil
	}

	m.eventsMutex.RLock()
	defer m.eventsMutex.RUnlock()

	return append([]MetricEvent(nil), m.events...)
}

// Reset resets all metrics
func (m *DefaultCacheMetrics) Reset(ctx context.Context) {
	atomic.StoreInt64(&m.hitCount, 0)
	atomic.StoreInt64(&m.missCount, 0)
	atomic.StoreInt64(&m.setCount, 0)
	atomic.StoreInt64(&m.deleteCount, 0)
	atomic.StoreInt64(&m.evictionCount, 0)
	atomic.StoreInt64(&m.errorCount, 0)
	atomic.StoreInt64(&m.memoryUsage, 0)
	atomic.StoreInt64(&m.itemCount, 0)
	atomic.StoreInt64(&m.totalSize, 0)
	atomic.StoreInt64(&m.avgGetTime, 0)
	atomic.StoreInt64(&m.avgSetTime, 0)
	atomic.StoreInt64(&m.avgDeleteTime, 0)

	m.lastResetTime = time.Now()

	if m.config.EnableDetailedTiming {
		m.timingsMutex.Lock()
		m.getTimings = m.getTimings[:0]
		m.setTimings = m.setTimings[:0]
		m.deleteTimings = m.deleteTimings[:0]
		m.timingsMutex.Unlock()
	}

	if m.config.EnableEventTracking {
		m.eventsMutex.Lock()
		m.events = m.events[:0]
		m.eventsMutex.Unlock()
	}

	if m.config.PercentileEnabled && m.percentileTracker != nil {
		m.percentileTracker.reset()
	}
}

// Private methods

func (m *DefaultCacheMetrics) updateAverageGetTime(duration time.Duration) {
	for {
		oldAvg := atomic.LoadInt64(&m.avgGetTime)
		newAvg := (oldAvg + int64(duration)) / 2
		if atomic.CompareAndSwapInt64(&m.avgGetTime, oldAvg, newAvg) {
			break
		}
	}
}

func (m *DefaultCacheMetrics) updateAverageSetTime(duration time.Duration) {
	for {
		oldAvg := atomic.LoadInt64(&m.avgSetTime)
		newAvg := (oldAvg + int64(duration)) / 2
		if atomic.CompareAndSwapInt64(&m.avgSetTime, oldAvg, newAvg) {
			break
		}
	}
}

func (m *DefaultCacheMetrics) updateAverageDeleteTime(duration time.Duration) {
	for {
		oldAvg := atomic.LoadInt64(&m.avgDeleteTime)
		newAvg := (oldAvg + int64(duration)) / 2
		if atomic.CompareAndSwapInt64(&m.avgDeleteTime, oldAvg, newAvg) {
			break
		}
	}
}

func (m *DefaultCacheMetrics) addGetTiming(duration time.Duration) {
	m.timingsMutex.Lock()
	defer m.timingsMutex.Unlock()

	if len(m.getTimings) >= m.config.MaxTimingSamples {
		// Remove oldest sample
		copy(m.getTimings, m.getTimings[1:])
		m.getTimings = m.getTimings[:len(m.getTimings)-1]
	}
	m.getTimings = append(m.getTimings, duration)
}

func (m *DefaultCacheMetrics) addSetTiming(duration time.Duration) {
	m.timingsMutex.Lock()
	defer m.timingsMutex.Unlock()

	if len(m.setTimings) >= m.config.MaxTimingSamples {
		// Remove oldest sample
		copy(m.setTimings, m.setTimings[1:])
		m.setTimings = m.setTimings[:len(m.setTimings)-1]
	}
	m.setTimings = append(m.setTimings, duration)
}

func (m *DefaultCacheMetrics) addDeleteTiming(duration time.Duration) {
	m.timingsMutex.Lock()
	defer m.timingsMutex.Unlock()

	if len(m.deleteTimings) >= m.config.MaxTimingSamples {
		// Remove oldest sample
		copy(m.deleteTimings, m.deleteTimings[1:])
		m.deleteTimings = m.deleteTimings[:len(m.deleteTimings)-1]
	}
	m.deleteTimings = append(m.deleteTimings, duration)
}

func (m *DefaultCacheMetrics) addEvent(event MetricEvent) {
	m.eventsMutex.Lock()
	defer m.eventsMutex.Unlock()

	if len(m.events) >= m.config.MaxEventSamples {
		// Remove oldest event
		copy(m.events, m.events[1:])
		m.events = m.events[:len(m.events)-1]
	}
	m.events = append(m.events, event)
}

func (m *DefaultCacheMetrics) autoResetRoutine() {
	ticker := time.NewTicker(m.config.ResetInterval)
	defer ticker.Stop()

	for range ticker.C {
		m.Reset(context.Background())
	}
}

// PercentileTracker methods

func (p *PercentileTracker) addGetLatency(duration time.Duration) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.getLatencies) >= p.maxSamples {
		// Remove oldest sample
		copy(p.getLatencies, p.getLatencies[1:])
		p.getLatencies = p.getLatencies[:len(p.getLatencies)-1]
	}
	p.getLatencies = append(p.getLatencies, duration)
}

func (p *PercentileTracker) addSetLatency(duration time.Duration) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.setLatencies) >= p.maxSamples {
		// Remove oldest sample
		copy(p.setLatencies, p.setLatencies[1:])
		p.setLatencies = p.setLatencies[:len(p.setLatencies)-1]
	}
	p.setLatencies = append(p.setLatencies, duration)
}

func (p *PercentileTracker) addDeleteLatency(duration time.Duration) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.deleteLatencies) >= p.maxSamples {
		// Remove oldest sample
		copy(p.deleteLatencies, p.deleteLatencies[1:])
		p.deleteLatencies = p.deleteLatencies[:len(p.deleteLatencies)-1]
	}
	p.deleteLatencies = append(p.deleteLatencies, duration)
}

func (p *PercentileTracker) getPercentiles() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return map[string]interface{}{
		"get_percentiles":    p.calculatePercentiles(p.getLatencies),
		"set_percentiles":    p.calculatePercentiles(p.setLatencies),
		"delete_percentiles": p.calculatePercentiles(p.deleteLatencies),
	}
}

func (p *PercentileTracker) calculatePercentiles(latencies []time.Duration) map[string]time.Duration {
	if len(latencies) == 0 {
		return map[string]time.Duration{}
	}

	// Sort latencies
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	
	// Simple bubble sort for small datasets
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	percentiles := map[string]time.Duration{
		"p50":  sorted[len(sorted)*50/100],
		"p90":  sorted[len(sorted)*90/100],
		"p95":  sorted[len(sorted)*95/100],
		"p99":  sorted[len(sorted)*99/100],
		"min":  sorted[0],
		"max":  sorted[len(sorted)-1],
	}

	return percentiles
}

func (p *PercentileTracker) reset() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.getLatencies = p.getLatencies[:0]
	p.setLatencies = p.setLatencies[:0]
	p.deleteLatencies = p.deleteLatencies[:0]
}

// Helper functions

// GetDefaultMetricsConfig returns default metrics configuration
func GetDefaultMetricsConfig() *MetricsConfig {
	return &MetricsConfig{
		EnableDetailedTiming: true,
		EnableEventTracking:  true,
		MaxTimingSamples:     1000,
		MaxEventSamples:      500,
		PercentileEnabled:    true,
		ResetInterval:        24 * time.Hour,
	}
}

// GetMinimalMetricsConfig returns minimal metrics configuration
func GetMinimalMetricsConfig() *MetricsConfig {
	return &MetricsConfig{
		EnableDetailedTiming: false,
		EnableEventTracking:  false,
		MaxTimingSamples:     0,
		MaxEventSamples:      0,
		PercentileEnabled:    false,
		ResetInterval:        0,
	}
}

// GetHighPerformanceMetricsConfig returns configuration optimized for performance
func GetHighPerformanceMetricsConfig() *MetricsConfig {
	return &MetricsConfig{
		EnableDetailedTiming: false,
		EnableEventTracking:  false,
		MaxTimingSamples:     100,
		MaxEventSamples:      50,
		PercentileEnabled:    true,
		ResetInterval:        1 * time.Hour,
	}
}



// Internal methods to avoid recursion
func (m *DefaultCacheMetrics) recordHitInternal(ctx context.Context, key string, duration time.Duration) {
	atomic.AddInt64(&m.hitCount, 1)
	
	if duration > 0 {
		m.updateAverageGetTime(duration)
		
		if m.config.EnableDetailedTiming {
			m.addGetTiming(duration)
		}
		
		if m.config.PercentileEnabled && m.percentileTracker != nil {
			m.percentileTracker.addGetLatency(duration)
		}
	}
	
	if m.config.EnableEventTracking {
		event := MetricEvent{
			Type:      "hit",
			Key:       key,
			Timestamp: time.Now(),
			Duration:  duration,
			Success:   true,
		}
		m.addEvent(event)
	}
}

func (m *DefaultCacheMetrics) recordMissInternal(ctx context.Context, key string, duration time.Duration) {
	atomic.AddInt64(&m.missCount, 1)
	
	if duration > 0 {
		m.updateAverageGetTime(duration)
		
		if m.config.EnableDetailedTiming {
			m.addGetTiming(duration)
		}
		
		if m.config.PercentileEnabled && m.percentileTracker != nil {
			m.percentileTracker.addGetLatency(duration)
		}
	}
	
	if m.config.EnableEventTracking {
		event := MetricEvent{
			Type:      "miss",
			Key:       key,
			Timestamp: time.Now(),
			Duration:  duration,
			Success:   true,
		}
		m.addEvent(event)
	}
}

func (m *DefaultCacheMetrics) recordEvictionInternal(ctx context.Context, key string, reason string) {
	atomic.AddInt64(&m.evictionCount, 1)
	
	if m.config.EnableEventTracking {
		event := MetricEvent{
			Type:      "eviction",
			Key:       key,
			Timestamp: time.Now(),
			Success:   true,
			Metadata: map[string]interface{}{
				"reason": reason,
			},
		}
		m.addEvent(event)
	}
}
