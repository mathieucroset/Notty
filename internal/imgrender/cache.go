package imgrender

import (
	"container/list"
	"sync"
)

// DefaultCacheSize is the cache capacity used when NewCache gets max <= 0.
const DefaultCacheSize = 64

// CacheKey identifies one rendering of one image file (spec §6.4).
type CacheKey struct {
	Path    string
	ModTime int64 // file modification time, e.g. UnixNano
	Cols    int
	Rows    int
	Proto   Protocol
}

// Rendered is a cached rendering: the rows to place in the frame
// (half-blocks or Kitty placeholders) and, for Kitty, the image id and the
// transmit sequence that must reach the terminal before the rows show
// anything.
type Rendered struct {
	Rows     []string
	KittyID  uint32
	Transmit string
}

type cacheEntry struct {
	key CacheKey
	val Rendered
}

// Cache is a size-bounded LRU of renderings. It is safe for concurrent use.
// Entries it drops are returned to the caller, who deletes their Kitty
// images from the terminal (KittyDelete).
type Cache struct {
	mu    sync.Mutex
	max   int
	ll    *list.List // front = most recently used
	items map[CacheKey]*list.Element
}

// NewCache returns an empty cache holding at most max entries
// (DefaultCacheSize when max <= 0).
func NewCache(max int) *Cache {
	if max <= 0 {
		max = DefaultCacheSize
	}
	return &Cache{max: max, ll: list.New(), items: map[CacheKey]*list.Element{}}
}

// Get returns the rendering for k and marks it most recently used.
func (c *Cache) Get(k CacheKey) (Rendered, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return Rendered{}, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*cacheEntry).val, true
}

// Put stores r under k as the most recently used entry. It returns the
// renderings that left the cache: least recently used entries beyond the
// capacity, and the previous value of k when its Kitty id differs from r's.
func (c *Cache) Put(k CacheKey, r Rendered) (evicted []Rendered) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		e := el.Value.(*cacheEntry)
		if e.val.KittyID != r.KittyID {
			evicted = append(evicted, e.val)
		}
		e.val = r
		c.ll.MoveToFront(el)
		return evicted
	}
	c.items[k] = c.ll.PushFront(&cacheEntry{key: k, val: r})
	for c.ll.Len() > c.max {
		el := c.ll.Back()
		e := el.Value.(*cacheEntry)
		c.ll.Remove(el)
		delete(c.items, e.key)
		evicted = append(evicted, e.val)
	}
	return evicted
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Clear empties the cache and returns every entry it held, for example to
// delete all Kitty images on quit or after the terminal lost them.
func (c *Cache) Clear() []Rendered {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Rendered, 0, c.ll.Len())
	for el := c.ll.Front(); el != nil; el = el.Next() {
		out = append(out, el.Value.(*cacheEntry).val)
	}
	c.ll.Init()
	c.items = map[CacheKey]*list.Element{}
	return out
}
