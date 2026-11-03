package porttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.ObjectStore = (*ObjectStore)(nil)

// ObjectStore is a bucket in memory without versioning, as R2: a Put replaces the object and a
// Delete removes it for good. An object's ETag is derived from its content, as S3's is for a
// single-part upload.
type ObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	writes  map[string]int
}

// NewObjectStore returns an empty bucket.
func NewObjectStore() *ObjectStore {
	return &ObjectStore{objects: map[string][]byte{}, writes: map[string]int{}}
}

// Get implements ports.ObjectStore.
func (s *ObjectStore) Get(_ context.Context, key string) (ports.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	body, ok := s.objects[key]
	if !ok {
		return ports.Object{}, fmt.Errorf("get %s: %w", key, ports.ErrNotFound)
	}

	return ports.Object{ObjectInfo: objectInfo(key, body), Body: slices.Clone(body)}, nil
}

// GetRange implements ports.ObjectStore.
func (s *ObjectStore) GetRange(_ context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 {
		return nil, fmt.Errorf("get %s from %d: a negative offset: %w", key, offset, domain.ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	body, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("get %s: %w", key, ports.ErrNotFound)
	}

	size := int64(len(body))
	from := min(offset, size)

	to := size
	if length >= 0 {
		to = min(from+length, size)
	}

	return io.NopCloser(bytes.NewReader(slices.Clone(body[from:to]))), nil
}

// PutStream implements ports.ObjectStore. The fake reads the whole stream before it writes, so
// a reader that fails leaves the object as it was.
func (s *ObjectStore) PutStream(_ context.Context, key string, r io.Reader) (ports.ObjectInfo, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return ports.ObjectInfo{}, fmt.Errorf("put %s: read: %w", key, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.write(key, body), nil
}

// Put implements ports.ObjectStore.
func (s *ObjectStore) Put(_ context.Context, key string, body []byte) (ports.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.write(key, body), nil
}

// PutIfAbsent implements ports.ObjectStore.
func (s *ObjectStore) PutIfAbsent(_ context.Context, key string, body []byte) (ports.ObjectInfo, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if there, ok := s.objects[key]; ok {
		return objectInfo(key, there), false, nil
	}

	return s.write(key, body), true, nil
}

// List implements ports.ObjectStore.
func (s *ObjectStore) List(_ context.Context, prefix string) ([]ports.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []ports.ObjectInfo

	for key, body := range s.objects {
		if strings.HasPrefix(key, prefix) {
			out = append(out, objectInfo(key, body))
		}
	}

	slices.SortFunc(out, func(a, b ports.ObjectInfo) int { return strings.Compare(a.Key, b.Key) })

	return out, nil
}

// Delete implements ports.ObjectStore.
func (s *ObjectStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.objects, key)

	return nil
}

// DeletePrefix implements ports.ObjectStore.
func (s *ObjectStore) DeletePrefix(_ context.Context, prefix string) (int, error) {
	if prefix == "" {
		return 0, errors.New("delete prefix: an empty prefix names the whole bucket")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for key := range s.objects {
		if strings.HasPrefix(key, prefix) {
			n++

			delete(s.objects, key)
		}
	}

	return n, nil
}

// Writes returns how many times key was written, so a test can tell a write-once object was
// never rewritten.
func (s *ObjectStore) Writes(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writes[key]
}

func (s *ObjectStore) write(key string, body []byte) ports.ObjectInfo {
	s.objects[key] = slices.Clone(body)
	s.writes[key]++

	return objectInfo(key, body)
}

func objectInfo(key string, body []byte) ports.ObjectInfo {
	sum := sha256.Sum256(body)

	return ports.ObjectInfo{Key: key, ETag: `"` + hex.EncodeToString(sum[:16]) + `"`, Size: int64(len(body))}
}
