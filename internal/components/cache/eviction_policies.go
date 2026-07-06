/**
 * @file eviction_policies.go
 * @description Cache eviction policies implementation for memory management
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/eviction_policies.go
 */

package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// LRUEvictionPolicy implements Least Recently Used eviction policy
type LRUEvictionPolicy struct {
	maxSize    int64
	items      map[string]*list.Element
	accessList *list.List
	mutex      sync.RWMutex
}

// LRUItem represents an item in LRU cache
type LRUItem struct {
	Key        string
	Value      interface{}
	AccessTime time.Time
	Size       int64
}

// NewLRUEvictionPolicy creates a new LRU eviction policy
func NewLRUEvictionPolicy(maxSize int64) *LRUEvictionPolicy {
	return &LRUEvictionPolicy{
		maxSize:    maxSize,
		items:      make(map[string]*list.Element),
		accessList: list.New(),
	}
}

// ShouldEvict determines if an item should be evicted (implements CacheEvictionPolicy)
func (p *LRUEvictionPolicy) ShouldEvict(item *CacheItem, stats *CacheStats) bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	// Check if we're at capacity and this item is least frequently used
	if stats.Size >= p.maxSize {
		// Find the least recently used item
		if p.accessList.Len() > 0 {
			back := p.accessList.Back()
			if back != nil {
				lruItem := back.Value.(*LRUItem)
				return lruItem.Key == item.Key
			}
		}
	}
	return false
}

// GetEvictionCandidates returns items that should be evicted (implements CacheEvictionPolicy)
func (p *LRUEvictionPolicy) GetEvictionCandidates(items []*CacheItem, stats *CacheStats) []*CacheItem {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	candidates := make([]*CacheItem, 0)
	
	// If we're over capacity, return LRU items
	if stats.Size > p.maxSize && p.accessList.Len() > 0 {
		// Start from the back (least recently used)
		for element := p.accessList.Back(); element != nil && len(candidates) < len(items)/2; element = element.Prev() {
			lruItem := element.Value.(*LRUItem)
			// Find matching cache item
			for _, item := range items {
				if item.Key == lruItem.Key {
					candidates = append(candidates, item)
					break
				}
			}
		}
	}
	
	return candidates
}

// GetPolicyName returns the policy name (implements CacheEvictionPolicy)
func (p *LRUEvictionPolicy) GetPolicyName() string {
	return "LRU"
}

// Legacy method for backward compatibility
func (p *LRUEvictionPolicy) ShouldEvictLegacy(ctx context.Context, currentSize, itemSize int64) bool {
	return currentSize+itemSize > p.maxSize
}

// SelectVictims selects items to evict
func (p *LRUEvictionPolicy) SelectVictims(ctx context.Context, cache Cache, requiredSpace int64) ([]string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var victims []string
	freedSpace := int64(0)

	// Remove least recently used items until we have enough space
	for freedSpace < requiredSpace && p.accessList.Len() > 0 {
		// Get least recently used item (back of list)
		element := p.accessList.Back()
		if element == nil {
			break
		}

		item := element.Value.(*LRUItem)
		victims = append(victims, item.Key)
		freedSpace += item.Size

		// Remove from tracking
		p.accessList.Remove(element)
		delete(p.items, item.Key)
	}

	return victims, nil
}

// OnAccess updates access tracking
func (p *LRUEvictionPolicy) OnAccess(ctx context.Context, key string, value interface{}, size int64) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()

	if element, exists := p.items[key]; exists {
		// Update existing item
		item := element.Value.(*LRUItem)
		item.AccessTime = now
		item.Value = value
		item.Size = size

		// Move to front (most recently used)
		p.accessList.MoveToFront(element)
	} else {
		// Add new item
		item := &LRUItem{
			Key:        key,
			Value:      value,
			AccessTime: now,
			Size:       size,
		}

		element := p.accessList.PushFront(item)
		p.items[key] = element
	}

	return nil
}

// OnRemove updates tracking when item is removed
func (p *LRUEvictionPolicy) OnRemove(ctx context.Context, key string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if element, exists := p.items[key]; exists {
		p.accessList.Remove(element)
		delete(p.items, key)
	}

	return nil
}

// GetPolicyType returns the policy type
func (p *LRUEvictionPolicy) GetPolicyType() EvictionPolicyType {
	return EvictionPolicyTypeLRU
}

// GetStatistics returns policy statistics
func (p *LRUEvictionPolicy) GetStatistics() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return map[string]interface{}{
		"policy_type":   "LRU",
		"max_size":      p.maxSize,
		"tracked_items": len(p.items),
		"list_length":   p.accessList.Len(),
	}
}

// LFUEvictionPolicy implements Least Frequently Used eviction policy
type LFUEvictionPolicy struct {
	maxSize      int64
	items        map[string]*LFUItem
	frequencies  map[int64]*list.List
	minFrequency int64
	mutex        sync.RWMutex
}

// LFUItem represents an item in LFU cache
type LFUItem struct {
	Key       string
	Value     interface{}
	Frequency int64
	Size      int64
	Element   *list.Element
}

// NewLFUEvictionPolicy creates a new LFU eviction policy
func NewLFUEvictionPolicy(maxSize int64) *LFUEvictionPolicy {
	return &LFUEvictionPolicy{
		maxSize:      maxSize,
		items:        make(map[string]*LFUItem),
		frequencies:  make(map[int64]*list.List),
		minFrequency: 1,
	}
}

// ShouldEvict determines if an item should be evicted (implements CacheEvictionPolicy)
func (p *LFUEvictionPolicy) ShouldEvict(item *CacheItem, stats *CacheStats) bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	// Check if we're at capacity and this item has low frequency
	if stats.Size >= p.maxSize {
		if lfuItem, exists := p.items[item.Key]; exists {
			// Evict if frequency is at minimum
			return lfuItem.Frequency <= p.minFrequency
		}
	}
	return false
}

// GetEvictionCandidates returns items that should be evicted (implements CacheEvictionPolicy)
func (p *LFUEvictionPolicy) GetEvictionCandidates(items []*CacheItem, stats *CacheStats) []*CacheItem {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	candidates := make([]*CacheItem, 0)
	
	// If we're over capacity, return LFU items (least frequently used first)
	if stats.Size > p.maxSize {
		// Get items with minimum frequency
		if minFreqList, exists := p.frequencies[p.minFrequency]; exists {
			for element := minFreqList.Front(); element != nil && len(candidates) < len(items)/2; element = element.Next() {
				lfuItem := element.Value.(*LFUItem)
				// Find matching cache item
				for _, item := range items {
					if item.Key == lfuItem.Key {
						candidates = append(candidates, item)
						break
					}
				}
			}
		}
	}
	
	return candidates
}

// GetPolicyName returns the policy name (implements CacheEvictionPolicy)
func (p *LFUEvictionPolicy) GetPolicyName() string {
	return "LFU"
}

// Legacy method for backward compatibility
func (p *LFUEvictionPolicy) ShouldEvictLegacy(ctx context.Context, currentSize, itemSize int64) bool {
	return currentSize+itemSize > p.maxSize
}

// SelectVictims selects items to evict
func (p *LFUEvictionPolicy) SelectVictims(ctx context.Context, cache Cache, requiredSpace int64) ([]string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var victims []string
	freedSpace := int64(0)

	// Remove least frequently used items until we have enough space
	for freedSpace < requiredSpace {
		// Find minimum frequency list that has items
		var minFreqList *list.List
		for freq := p.minFrequency; freq <= p.minFrequency+100; freq++ {
			if freqList, exists := p.frequencies[freq]; exists && freqList.Len() > 0 {
				minFreqList = freqList
				p.minFrequency = freq
				break
			}
		}

		if minFreqList == nil || minFreqList.Len() == 0 {
			break
		}

		// Remove least recently used item from minimum frequency list
		element := minFreqList.Back()
		if element == nil {
			break
		}

		item := element.Value.(*LFUItem)
		victims = append(victims, item.Key)
		freedSpace += item.Size

		// Remove from tracking
		minFreqList.Remove(element)
		delete(p.items, item.Key)

		// Update minimum frequency if this list is now empty
		if minFreqList.Len() == 0 {
			p.updateMinFrequency()
		}
	}

	return victims, nil
}

// OnAccess updates access tracking
func (p *LFUEvictionPolicy) OnAccess(ctx context.Context, key string, value interface{}, size int64) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if item, exists := p.items[key]; exists {
		// Update existing item
		oldFreq := item.Frequency
		newFreq := oldFreq + 1

		// Remove from old frequency list
		if freqList, exists := p.frequencies[oldFreq]; exists {
			freqList.Remove(item.Element)
			if freqList.Len() == 0 && oldFreq == p.minFrequency {
				p.minFrequency = newFreq
			}
		}

		// Update item
		item.Value = value
		item.Size = size
		item.Frequency = newFreq

		// Add to new frequency list
		if _, exists := p.frequencies[newFreq]; !exists {
			p.frequencies[newFreq] = list.New()
		}
		item.Element = p.frequencies[newFreq].PushFront(item)
	} else {
		// Add new item with frequency 1
		item := &LFUItem{
			Key:       key,
			Value:     value,
			Frequency: 1,
			Size:      size,
		}

		if _, exists := p.frequencies[1]; !exists {
			p.frequencies[1] = list.New()
		}
		item.Element = p.frequencies[1].PushFront(item)
		p.items[key] = item

		// Update minimum frequency
		p.minFrequency = 1
	}

	return nil
}

// OnRemove updates tracking when item is removed
func (p *LFUEvictionPolicy) OnRemove(ctx context.Context, key string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if item, exists := p.items[key]; exists {
		// Remove from frequency list
		if freqList, exists := p.frequencies[item.Frequency]; exists {
			freqList.Remove(item.Element)
			if freqList.Len() == 0 && item.Frequency == p.minFrequency {
				p.updateMinFrequency()
			}
		}

		delete(p.items, key)
	}

	return nil
}

// GetPolicyType returns the policy type
func (p *LFUEvictionPolicy) GetPolicyType() EvictionPolicyType {
	return EvictionPolicyTypeLFU
}

// GetStatistics returns policy statistics
func (p *LFUEvictionPolicy) GetStatistics() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	freqCounts := make(map[int64]int)
	for freq, freqList := range p.frequencies {
		freqCounts[freq] = freqList.Len()
	}

	return map[string]interface{}{
		"policy_type":      "LFU",
		"max_size":         p.maxSize,
		"tracked_items":    len(p.items),
		"min_frequency":    p.minFrequency,
		"frequency_counts": freqCounts,
	}
}

func (p *LFUEvictionPolicy) updateMinFrequency() {
	p.minFrequency = int64(^uint64(0) >> 1) // Max int64
	for freq, freqList := range p.frequencies {
		if freqList.Len() > 0 && freq < p.minFrequency {
			p.minFrequency = freq
		}
	}
}

// FIFOEvictionPolicy implements First In First Out eviction policy
type FIFOEvictionPolicy struct {
	maxSize    int64
	items      map[string]*FIFOItem
	insertList *list.List
	mutex      sync.RWMutex
}

// FIFOItem represents an item in FIFO cache
type FIFOItem struct {
	Key        string
	Value      interface{}
	InsertTime time.Time
	Size       int64
	Element    *list.Element
}

// NewFIFOEvictionPolicy creates a new FIFO eviction policy
func NewFIFOEvictionPolicy(maxSize int64) *FIFOEvictionPolicy {
	return &FIFOEvictionPolicy{
		maxSize:    maxSize,
		items:      make(map[string]*FIFOItem),
		insertList: list.New(),
	}
}

// ShouldEvict determines if an item should be evicted (implements CacheEvictionPolicy)
func (p *FIFOEvictionPolicy) ShouldEvict(item *CacheItem, stats *CacheStats) bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	// Check if we're at capacity and this item is first in queue
	if stats.Size >= p.maxSize {
		if p.insertList.Len() > 0 {
			back := p.insertList.Back()
			if back != nil {
				fifoItem := back.Value.(*FIFOItem)
				return fifoItem.Key == item.Key
			}
		}
	}
	return false
}

// GetEvictionCandidates returns items that should be evicted (implements CacheEvictionPolicy)
func (p *FIFOEvictionPolicy) GetEvictionCandidates(items []*CacheItem, stats *CacheStats) []*CacheItem {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	candidates := make([]*CacheItem, 0)
	
	// If we're over capacity, return FIFO items (oldest first)
	if stats.Size > p.maxSize && p.insertList.Len() > 0 {
		// Start from the back (first inserted)
		for element := p.insertList.Back(); element != nil && len(candidates) < len(items)/2; element = element.Prev() {
			fifoItem := element.Value.(*FIFOItem)
			// Find matching cache item
			for _, item := range items {
				if item.Key == fifoItem.Key {
					candidates = append(candidates, item)
					break
				}
			}
		}
	}
	
	return candidates
}

// GetPolicyName returns the policy name (implements CacheEvictionPolicy)
func (p *FIFOEvictionPolicy) GetPolicyName() string {
	return "FIFO"
}

// Legacy method for backward compatibility
func (p *FIFOEvictionPolicy) ShouldEvictLegacy(ctx context.Context, currentSize, itemSize int64) bool {
	return currentSize+itemSize > p.maxSize
}

// SelectVictims selects items to evict
func (p *FIFOEvictionPolicy) SelectVictims(ctx context.Context, cache Cache, requiredSpace int64) ([]string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var victims []string
	freedSpace := int64(0)

	// Remove first inserted items until we have enough space
	for freedSpace < requiredSpace && p.insertList.Len() > 0 {
		// Get first inserted item (back of list)
		element := p.insertList.Back()
		if element == nil {
			break
		}

		item := element.Value.(*FIFOItem)
		victims = append(victims, item.Key)
		freedSpace += item.Size

		// Remove from tracking
		p.insertList.Remove(element)
		delete(p.items, item.Key)
	}

	return victims, nil
}

// OnAccess updates access tracking
func (p *FIFOEvictionPolicy) OnAccess(ctx context.Context, key string, value interface{}, size int64) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if item, exists := p.items[key]; exists {
		// Update existing item (but don't change position in FIFO)
		item.Value = value
		item.Size = size
	} else {
		// Add new item
		now := time.Now()
		item := &FIFOItem{
			Key:        key,
			Value:      value,
			InsertTime: now,
			Size:       size,
		}

		// Add to front of list (most recently inserted)
		item.Element = p.insertList.PushFront(item)
		p.items[key] = item
	}

	return nil
}

// OnRemove updates tracking when item is removed
func (p *FIFOEvictionPolicy) OnRemove(ctx context.Context, key string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if item, exists := p.items[key]; exists {
		p.insertList.Remove(item.Element)
		delete(p.items, key)
	}

	return nil
}

// GetPolicyType returns the policy type
func (p *FIFOEvictionPolicy) GetPolicyType() EvictionPolicyType {
	return EvictionPolicyTypeFIFO
}

// GetStatistics returns policy statistics
func (p *FIFOEvictionPolicy) GetStatistics() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	var oldestInsertTime, newestInsertTime time.Time
	if p.insertList.Len() > 0 {
		if back := p.insertList.Back(); back != nil {
			oldestInsertTime = back.Value.(*FIFOItem).InsertTime
		}
		if front := p.insertList.Front(); front != nil {
			newestInsertTime = front.Value.(*FIFOItem).InsertTime
		}
	}

	return map[string]interface{}{
		"policy_type":        "FIFO",
		"max_size":           p.maxSize,
		"tracked_items":      len(p.items),
		"queue_length":       p.insertList.Len(),
		"oldest_insert_time": oldestInsertTime,
		"newest_insert_time": newestInsertTime,
	}
}

// TTLEvictionPolicy implements Time To Live eviction policy
type TTLEvictionPolicy struct {
	maxSize       int64
	items         map[string]*TTLItem
	expirationMap map[time.Time][]*TTLItem
	mutex         sync.RWMutex
	cleanupTicker *time.Ticker
	stopChan      chan struct{}
}

// TTLItem represents an item in TTL cache
type TTLItem struct {
	Key       string
	Value     interface{}
	CreatedAt time.Time
	ExpiresAt time.Time
	Size      int64
}

// NewTTLEvictionPolicy creates a new TTL eviction policy
func NewTTLEvictionPolicy(maxSize int64) *TTLEvictionPolicy {
	policy := &TTLEvictionPolicy{
		maxSize:       maxSize,
		items:         make(map[string]*TTLItem),
		expirationMap: make(map[time.Time][]*TTLItem),
		cleanupTicker: time.NewTicker(1 * time.Minute),
		stopChan:      make(chan struct{}),
	}

	// Start cleanup routine
	go policy.cleanupRoutine()

	return policy
}

// ShouldEvict determines if an item should be evicted (implements CacheEvictionPolicy)
func (p *TTLEvictionPolicy) ShouldEvict(item *CacheItem, stats *CacheStats) bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	// Check if this specific item has expired
	if ttlItem, exists := p.items[item.Key]; exists {
		if time.Now().After(ttlItem.ExpiresAt) {
			return true
		}
	}
	
	// If not expired, check if we're at capacity
	return stats.Size >= p.maxSize
}

// GetEvictionCandidates returns items that should be evicted (implements CacheEvictionPolicy)
func (p *TTLEvictionPolicy) GetEvictionCandidates(items []*CacheItem, stats *CacheStats) []*CacheItem {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	candidates := make([]*CacheItem, 0)
	now := time.Now()
	
	// First, collect expired items
	for _, item := range items {
		if ttlItem, exists := p.items[item.Key]; exists {
			if now.After(ttlItem.ExpiresAt) {
				candidates = append(candidates, item)
			}
		}
	}
	
	// If we still need more space and we're over capacity, add oldest items
	if len(candidates) == 0 && stats.Size > p.maxSize {
		// Sort by creation time and take oldest
		sortedItems := make([]*CacheItem, len(items))
		copy(sortedItems, items)
		
		// Simple sort by last accessed time (oldest first)
		for i := 0; i < len(sortedItems)-1; i++ {
			for j := i + 1; j < len(sortedItems); j++ {
				if sortedItems[i].LastAccessed.After(sortedItems[j].LastAccessed) {
					sortedItems[i], sortedItems[j] = sortedItems[j], sortedItems[i]
				}
			}
		}
		
		// Take up to half the items as candidates
		maxCandidates := len(items) / 2
		if maxCandidates == 0 {
			maxCandidates = 1
		}
		
		for i := 0; i < maxCandidates && i < len(sortedItems); i++ {
			candidates = append(candidates, sortedItems[i])
		}
	}
	
	return candidates
}

// GetPolicyName returns the policy name (implements CacheEvictionPolicy)
func (p *TTLEvictionPolicy) GetPolicyName() string {
	return "TTL"
}

// Legacy method for backward compatibility
func (p *TTLEvictionPolicy) ShouldEvictLegacy(ctx context.Context, currentSize, itemSize int64) bool {
	// First try to clean up expired items
	p.cleanupExpiredItems()

	// Check if we still need to evict
	return currentSize+itemSize > p.maxSize
}

// SelectVictims selects items to evict
func (p *TTLEvictionPolicy) SelectVictims(ctx context.Context, cache Cache, requiredSpace int64) ([]string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var victims []string
	freedSpace := int64(0)

	now := time.Now()

	// First, collect expired items
	for key, item := range p.items {
		if now.After(item.ExpiresAt) {
			victims = append(victims, key)
			freedSpace += item.Size
			delete(p.items, key)
		}
	}

	// If we still need more space, evict items closest to expiration
	if freedSpace < requiredSpace {
		type itemWithExpiration struct {
			key       string
			item      *TTLItem
			timeToExp time.Duration
		}

		var sortedItems []itemWithExpiration
		for key, item := range p.items {
			if !now.After(item.ExpiresAt) {
				sortedItems = append(sortedItems, itemWithExpiration{
					key:       key,
					item:      item,
					timeToExp: item.ExpiresAt.Sub(now),
				})
			}
		}

		// Sort by time to expiration (ascending)
		for i := 0; i < len(sortedItems)-1; i++ {
			for j := i + 1; j < len(sortedItems); j++ {
				if sortedItems[i].timeToExp > sortedItems[j].timeToExp {
					sortedItems[i], sortedItems[j] = sortedItems[j], sortedItems[i]
				}
			}
		}

		// Evict items with shortest time to expiration
		for _, itemExp := range sortedItems {
			if freedSpace >= requiredSpace {
				break
			}
			victims = append(victims, itemExp.key)
			freedSpace += itemExp.item.Size
			delete(p.items, itemExp.key)
		}
	}

	return victims, nil
}

// OnAccess updates access tracking
func (p *TTLEvictionPolicy) OnAccess(ctx context.Context, key string, value interface{}, size int64) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()

	if item, exists := p.items[key]; exists {
		// Update existing item
		item.Value = value
		item.Size = size
		// Keep original expiration time
	} else {
		// Add new item with default TTL of 1 hour
		expiresAt := now.Add(1 * time.Hour)
		item := &TTLItem{
			Key:       key,
			Value:     value,
			CreatedAt: now,
			ExpiresAt: expiresAt,
			Size:      size,
		}

		p.items[key] = item

		// Add to expiration map
		if _, exists := p.expirationMap[expiresAt]; !exists {
			p.expirationMap[expiresAt] = make([]*TTLItem, 0)
		}
		p.expirationMap[expiresAt] = append(p.expirationMap[expiresAt], item)
	}

	return nil
}

// OnRemove updates tracking when item is removed
func (p *TTLEvictionPolicy) OnRemove(ctx context.Context, key string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if item, exists := p.items[key]; exists {
		delete(p.items, key)

		// Remove from expiration map
		if items, exists := p.expirationMap[item.ExpiresAt]; exists {
			for i, expItem := range items {
				if expItem.Key == key {
					p.expirationMap[item.ExpiresAt] = append(items[:i], items[i+1:]...)
					break
				}
			}
			if len(p.expirationMap[item.ExpiresAt]) == 0 {
				delete(p.expirationMap, item.ExpiresAt)
			}
		}
	}

	return nil
}

// GetPolicyType returns the policy type
func (p *TTLEvictionPolicy) GetPolicyType() EvictionPolicyType {
	return EvictionPolicyTypeTTL
}

// GetStatistics returns policy statistics
func (p *TTLEvictionPolicy) GetStatistics() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	now := time.Now()
	expiredCount := 0
	for _, item := range p.items {
		if now.After(item.ExpiresAt) {
			expiredCount++
		}
	}

	return map[string]interface{}{
		"policy_type":        "TTL",
		"max_size":           p.maxSize,
		"tracked_items":      len(p.items),
		"expired_items":      expiredCount,
		"expiration_buckets": len(p.expirationMap),
	}
}

func (p *TTLEvictionPolicy) cleanupRoutine() {
	for {
		select {
		case <-p.cleanupTicker.C:
			p.cleanupExpiredItems()
		case <-p.stopChan:
			return
		}
	}
}

func (p *TTLEvictionPolicy) cleanupExpiredItems() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()
	var expiredKeys []string

	for key, item := range p.items {
		if now.After(item.ExpiresAt) {
			expiredKeys = append(expiredKeys, key)
		}
	}

	for _, key := range expiredKeys {
		if item, exists := p.items[key]; exists {
			delete(p.items, key)

			// Remove from expiration map
			if items, exists := p.expirationMap[item.ExpiresAt]; exists {
				for i, expItem := range items {
					if expItem.Key == key {
						p.expirationMap[item.ExpiresAt] = append(items[:i], items[i+1:]...)
						break
					}
				}
				if len(p.expirationMap[item.ExpiresAt]) == 0 {
					delete(p.expirationMap, item.ExpiresAt)
				}
			}
		}
	}
}

// Stop stops the TTL cleanup routine
func (p *TTLEvictionPolicy) Stop() {
	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
	}
	close(p.stopChan)
}

// Factory function to create eviction policies
func NewEvictionPolicy(policyType EvictionPolicyType, maxSize int64) CacheEvictionPolicy {
	switch policyType {
	case EvictionPolicyTypeLRU:
		return NewLRUEvictionPolicy(maxSize)
	case EvictionPolicyTypeLFU:
		return NewLFUEvictionPolicy(maxSize)
	case EvictionPolicyTypeFIFO:
		return NewFIFOEvictionPolicy(maxSize)
	case EvictionPolicyTypeTTL:
		return NewTTLEvictionPolicy(maxSize)
	default:
		return NewLRUEvictionPolicy(maxSize) // Default to LRU
	}
}
