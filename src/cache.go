package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheEntry represents a single cache entry with its data and timestamp.
type CacheEntry struct {
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

// MemoryCache is a in-memory cache.
type MemoryCache struct {
	cache   map[string]CacheEntry
	mu      sync.RWMutex
	timeout time.Duration
}

// NewMemoryCache creates a new instance of MemoryCache.
func NewMemoryCache(timeout time.Duration) *MemoryCache {
	return &MemoryCache{
		cache:   make(map[string]CacheEntry),
		timeout: timeout,
	}
}

// Get retrieves a cache entry by key. Returns the data and a boolean indicating if it was found.
func (mc *MemoryCache) Get(key string) ([]byte, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	entry, found := mc.cache[key]
	if !found {
		return nil, false
	}

	if time.Since(entry.Timestamp) > mc.timeout {
		return nil, false
	}

	return entry.Data, true
}

// Set adds or updates a cache entry with the given key and data.
func (mc *MemoryCache) Set(key string, data []byte) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.cache[key] = CacheEntry{
		Data:      data,
		Timestamp: time.Now(),
	}
}

// Age returns the age of a cache entry in seconds. If the entry does not exist, it returns -1.
func (mc *MemoryCache) Age(key string) float64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	entry, found := mc.cache[key]
	if !found {
		return -1
	}

	return time.Since(entry.Timestamp).Seconds()
}

// Clear removes all entries from the cache. Primarily useful for tests.
func (mc *MemoryCache) Clear() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.cache = make(map[string]CacheEntry)
}

// FileCache is a file-based cache.
type FileCache struct {
	path string
}

func NewFileCache(path string) *FileCache {
	return &FileCache{
		path: path,
	}
}

// Get retrieves a cache entry from file, if it exists.
func (c *FileCache) Get() ([]byte, error) {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cache CacheEntry
	if err := json.Unmarshal(raw, &cache); err != nil {
		return nil, err
	}

	return cache.Data, nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".cache-*.tmp")
	if err != nil {
		return err
	}

	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, path)
}

// Set writes a cache entry to file.
func (c *FileCache) Set(data []byte) error {
	cache := CacheEntry{
		Data:      data,
		Timestamp: time.Now(),
	}

	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return atomicWrite(c.path, raw)
}

// Age returns the age of a cache entry in seconds.
func (c *FileCache) Age() (float64, error) {
	stat, err := os.Stat(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return -1, nil
		}
		return -1, err
	}

	return time.Since(stat.ModTime()).Seconds(), nil
}
