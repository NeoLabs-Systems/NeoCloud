package notes

import (
	"container/list"
	"sync"
)

const maxCached = 5000

// The parsed card of each note, in memory only, keyed by node id and etag:
// listing reads (and decrypts) only the notes that changed. Nothing about a
// note's text is written anywhere but its file.
type cardCache struct {
	mu      sync.Mutex
	order   *list.List
	entries map[string]*list.Element
}

type cached struct {
	id, etag string
	userID   int64
	content  content
}

// content is the part of a card that comes from the file's text.
type content struct {
	properties
	snippet   string
	checklist checklist
}

func newCardCache() *cardCache {
	return &cardCache{order: list.New(), entries: map[string]*list.Element{}}
}

func (c *cardCache) get(userID int64, id, etag string) (content, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element := c.entries[id]
	if element == nil {
		return content{}, false
	}
	entry := element.Value.(cached)
	if entry.userID != userID || entry.etag != etag {
		return content{}, false
	}
	c.order.MoveToBack(element)
	return entry.content, true
}

func (c *cardCache) put(userID int64, id, etag string, value content) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element := c.entries[id]; element != nil {
		c.order.Remove(element)
	}
	c.entries[id] = c.order.PushBack(cached{id: id, etag: etag, userID: userID, content: value})
	for c.order.Len() > maxCached {
		oldest := c.order.Remove(c.order.Front()).(cached)
		delete(c.entries, oldest.id)
	}
}
