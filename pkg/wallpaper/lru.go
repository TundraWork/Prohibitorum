package wallpaper

import (
	"container/list"
	"sync"
)

// lruCache holds picture bytes under a total-byte ceiling, dropping the
// least recently used picture to make room. Get moves a picture to the front;
// Put inserts or refreshes one and evicts from the back until the new total
// fits.
type lruCache struct {
	mu      sync.Mutex
	max     int64
	total   int64
	order   *list.List               // front = most recently used; values are keys
	items   map[string]*list.Element // key → element
	entries map[string]Image         // key → value
	sizes   map[string]int           // key → byte size (len(Data) + overhead is negligible)
}

func newLRUCache(max int64) *lruCache {
	return &lruCache{
		max:     max,
		order:   list.New(),
		items:   map[string]*list.Element{},
		entries: map[string]Image{},
		sizes:   map[string]int{},
	}
}

func (c *lruCache) get(key string) (Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return Image{}, false
	}
	c.order.MoveToFront(el)
	return c.entries[key], true
}

func (c *lruCache) put(key string, img Image) {
	size := len(img.Data)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.total += int64(size - c.sizes[key])
		c.entries[key] = img
		c.sizes[key] = size
		c.order.MoveToFront(el)
	} else {
		c.items[key] = c.order.PushFront(key)
		c.entries[key] = img
		c.sizes[key] = size
		c.total += int64(size)
	}
	for c.total > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		key := oldest.Value.(string)
		c.total -= int64(c.sizes[key])
		c.order.Remove(oldest)
		delete(c.items, key)
		delete(c.entries, key)
		delete(c.sizes, key)
	}
}
