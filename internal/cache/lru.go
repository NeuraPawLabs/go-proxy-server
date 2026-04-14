package cache

import (
	"container/list"
	"sync"
	"time"
)

// Entry represents a cache entry with expiration time
type Entry struct {
	Value     interface{}
	ExpiresAt time.Time
}

// ShardedLRU implements a sharded LRU cache for better concurrency
// Each shard has its own lock, reducing lock contention
type ShardedLRU struct {
	shards    []*lruShard
	numShards int
}

type lruShard struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element
	lruList  *list.List
}

type lruEntry struct {
	key   string
	value Entry
}

// NewShardedLRU creates a new sharded LRU cache with the specified capacity
func NewShardedLRU(totalCapacity int, numShards int) *ShardedLRU {
	if numShards <= 0 {
		numShards = 16 // default to 16 shards
	}
	shardCapacity := totalCapacity / numShards
	if shardCapacity < 10 {
		shardCapacity = 10 // minimum capacity per shard
	}

	shards := make([]*lruShard, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = &lruShard{
			capacity: shardCapacity,
			cache:    make(map[string]*list.Element),
			lruList:  list.New(),
		}
	}

	return &ShardedLRU{
		shards:    shards,
		numShards: numShards,
	}
}

// getShard returns the shard for a given key using hash-based distribution
func (c *ShardedLRU) getShard(key string) *lruShard {
	// Use FNV-1a hash for fast, well-distributed hashing
	hash := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return c.shards[hash%uint32(c.numShards)]
}

// Get retrieves a value from the sharded LRU cache
func (c *ShardedLRU) Get(key string) (Entry, bool) {
	shard := c.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, ok := shard.cache[key]; ok {
		entry := elem.Value.(*lruEntry)
		// Check if expired
		if time.Now().After(entry.value.ExpiresAt) {
			// Remove expired entry
			shard.lruList.Remove(elem)
			delete(shard.cache, key)
			return Entry{}, false
		}
		// Move to front (most recently used)
		shard.lruList.MoveToFront(elem)
		return entry.value, true
	}
	return Entry{}, false
}

// Put adds or updates a value in the sharded LRU cache
func (c *ShardedLRU) Put(key string, value Entry) {
	shard := c.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Update existing entry
	if elem, ok := shard.cache[key]; ok {
		shard.lruList.MoveToFront(elem)
		elem.Value.(*lruEntry).value = value
		return
	}

	// Add new entry
	entry := &lruEntry{key: key, value: value}
	elem := shard.lruList.PushFront(entry)
	shard.cache[key] = elem

	// Evict least recently used if over capacity
	if shard.lruList.Len() > shard.capacity {
		oldest := shard.lruList.Back()
		if oldest != nil {
			shard.lruList.Remove(oldest)
			delete(shard.cache, oldest.Value.(*lruEntry).key)
		}
	}
}

// CleanExpired removes all expired entries from all shards
func (c *ShardedLRU) CleanExpired() int {
	total := 0
	for _, shard := range c.shards {
		shard.mu.Lock()
		now := time.Now()
		removed := 0

		// Iterate through all entries and remove expired ones
		for elem := shard.lruList.Back(); elem != nil; {
			entry := elem.Value.(*lruEntry)
			prev := elem.Prev()

			if now.After(entry.value.ExpiresAt) {
				shard.lruList.Remove(elem)
				delete(shard.cache, entry.key)
				removed++
			}

			elem = prev
		}
		total += removed
		shard.mu.Unlock()
	}
	return total
}
