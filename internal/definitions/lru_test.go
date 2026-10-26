package definitions

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestLRU_DropsTheLeastRecentlyUsedVersionsBeyondItsBytes(t *testing.T) {
	t.Parallel()

	l := newLRU(25)
	add := func(id domain.DefinitionVersionID, size int64) { l.add(&version{id: id, size: size}) }
	kept := func(id domain.DefinitionVersionID) bool { _, ok := l.get(id); return ok }

	add("v1", 10)
	add("v2", 10)

	if !kept("v1") { // v1 is now the most recent
		t.Fatal("v1 dropped while everything fit")
	}

	add("v3", 10) // 30 bytes: v2, the least recently used, goes

	if kept("v2") || !kept("v1") || !kept("v3") {
		t.Errorf("kept v1 %v, v2 %v, v3 %v; want v1 and v3", kept("v1"), kept("v2"), kept("v3"))
	}

	add("big", 100) // larger than the bound: kept alone

	if !kept("big") || kept("v1") || kept("v3") {
		t.Errorf("after a version over the bound: big %v, v1 %v, v3 %v; want only big", kept("big"), kept("v1"), kept("v3"))
	}
}
