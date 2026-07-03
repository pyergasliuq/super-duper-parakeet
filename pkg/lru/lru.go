// Package lru is a fixed-size LRU cache for bytes payloads.
//
// Used to cache recent file-processing results so a user re-uploading the
// same file gets the answer instantly. Keys are SHA-256 of the input file.
package lru

import (
	"container/list"
	"sync"
)

// Cache is a thread-safe LRU cache with a max size in bytes.
type Cache struct {
	maxBytes int
	curBytes int
	items    map[string]*list.Element
	order    *list.List
	mu       sync.Mutex
}

type entry struct {
	key   string
	value []byte
}

// New returns a Cache with the given max size in bytes.
func New(maxBytes int) *Cache {
	return &Cache{
		maxBytes: maxBytes,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get returns the cached bytes for key, or nil if not present.
func (c *Cache) Get(key string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*entry).value
	}
	return nil
}

// Set adds a key/value pair. Evicts oldest entries until curBytes <= maxBytes.
func (c *Cache) Set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// If key exists, update.
	if el, ok := c.items[key]; ok {
		old := el.Value.(*entry)
		c.curBytes -= len(old.value)
		old.value = value
		c.curBytes += len(value)
		c.order.MoveToFront(el)
	} else {
		e := &entry{key: key, value: value}
		el := c.order.PushFront(e)
		c.items[key] = el
		c.curBytes += len(value)
	}
	// Evict.
	for c.curBytes > c.maxBytes && c.order.Len() > 0 {
		back := c.order.Back()
		if back == nil {
			break
		}
		e := back.Value.(*entry)
		c.curBytes -= len(e.value)
		delete(c.items, e.key)
		c.order.Remove(back)
	}
}

// Size returns the current size in bytes.
func (c *Cache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.curBytes
}

// Len returns the number of entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
