package porttest

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.ObjectStore = (*ObjectStore)(nil)

// ObjectStore is a versioned bucket in memory. Like S3 with versioning on, every Put adds a
// version and Delete adds a delete marker, so old versions stay readable.
type ObjectStore struct {
	mu       sync.Mutex
	versions map[string][]objectVersion // key → versions, oldest first
	next     int
}

type objectVersion struct {
	id      string
	body    []byte
	deleted bool
}

// NewObjectStore returns an empty bucket.
func NewObjectStore() *ObjectStore {
	return &ObjectStore{versions: map[string][]objectVersion{}}
}

// Get implements ports.ObjectStore.
func (s *ObjectStore) Get(_ context.Context, key, version string) (ports.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vs := s.versions[key]
	if version == "" {
		if len(vs) == 0 || vs[len(vs)-1].deleted {
			return ports.Object{}, fmt.Errorf("get %s: %w", key, ports.ErrNotFound)
		}

		v := vs[len(vs)-1]

		return object(key, v), nil
	}

	for _, v := range vs {
		if v.id == version && !v.deleted {
			return object(key, v), nil
		}
	}

	return ports.Object{}, fmt.Errorf("get %s version %s: %w", key, version, ports.ErrNotFound)
}

// Put implements ports.ObjectStore.
func (s *ObjectStore) Put(_ context.Context, key string, body []byte) (ports.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := objectVersion{id: s.newVersion(), body: slices.Clone(body)}
	s.versions[key] = append(s.versions[key], v)

	return object(key, v).ObjectInfo, nil
}

// List implements ports.ObjectStore.
func (s *ObjectStore) List(_ context.Context, prefix string) ([]ports.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []ports.ObjectInfo

	for key, vs := range s.versions {
		if v := vs[len(vs)-1]; strings.HasPrefix(key, prefix) && !v.deleted {
			out = append(out, object(key, v).ObjectInfo)
		}
	}

	slices.SortFunc(out, func(a, b ports.ObjectInfo) int { return strings.Compare(a.Key, b.Key) })

	return out, nil
}

// Delete implements ports.ObjectStore.
func (s *ObjectStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if vs := s.versions[key]; len(vs) > 0 && !vs[len(vs)-1].deleted {
		s.versions[key] = append(vs, objectVersion{id: s.newVersion(), deleted: true})
	}

	return nil
}

// Versions returns how many versions, delete markers included, key has had.
func (s *ObjectStore) Versions(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.versions[key])
}

func (s *ObjectStore) newVersion() string {
	s.next++

	return "v" + strconv.Itoa(s.next)
}

func object(key string, v objectVersion) ports.Object {
	return ports.Object{
		ObjectInfo: ports.ObjectInfo{Key: key, Version: v.id, Size: int64(len(v.body))},
		Body:       slices.Clone(v.body),
	}
}
