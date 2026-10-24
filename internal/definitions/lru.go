package definitions

import (
	"container/list"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
)

// lru keeps the versions used most recently whose files fit in max bytes; the newest is kept
// even when it alone is larger. It is safe for concurrent use.
type lru struct {
	mu    sync.Mutex
	max   int64
	size  int64
	order *list.List // of *version, most recent first
	byID  map[domain.DefinitionVersionID]*list.Element
}

func newLRU(maxBytes int64) *lru {
	return &lru{max: maxBytes, order: list.New(), byID: map[domain.DefinitionVersionID]*list.Element{}}
}

// get returns a kept version and marks it used.
func (l *lru) get(id domain.DefinitionVersionID) (*version, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.byID[id]
	if !ok {
		return nil, false
	}

	l.order.MoveToFront(e)

	v, _ := e.Value.(*version)

	return v, true
}

// add keeps v as the most recent version and drops the least recent ones beyond max.
func (l *lru) add(v *version) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if e, ok := l.byID[v.id]; ok {
		l.order.MoveToFront(e)
		return
	}

	l.byID[v.id] = l.order.PushFront(v)
	l.size += v.size

	for l.size > l.max && l.order.Len() > 1 {
		oldest := l.order.Back()
		old, _ := l.order.Remove(oldest).(*version)
		delete(l.byID, old.id)
		l.size -= old.size
	}
}
