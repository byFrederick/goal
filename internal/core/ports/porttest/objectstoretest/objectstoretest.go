// Package objectstoretest is the conformance suite for ports.ObjectStore's ranged read, streamed
// write (ADR 0038 decision 3) and presigned read (decision 7). The in-memory fake in porttest and the S3/R2 adapter run
// it, so a test that reads part of a knowledge snapshot's zip, or streams one in, on the fake
// holds for the bucket.
package objectstoretest

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// StreamBytes is the size of the suite's big streamed write: more than one part of S3's smallest
// multipart part (5 MiB), so an adapter that uploads in parts writes at least two.
const StreamBytes = 5<<20 + 4096 + 1

// Run runs the suite. Tests run in parallel and call newStore once each; stores must be isolated
// from each other.
func Run(t *testing.T, newStore func(t *testing.T) ports.ObjectStore) {
	t.Helper()

	tests := []struct {
		name string
		test func(f fixture)
	}{
		{"GetRange_Middle", testRangeMiddle},
		{"GetRange_End", testRangeEnd},
		{"GetRange_PastTheEnd", testRangePastEnd},
		{"GetRange_MissingKeyIsNotFound", testRangeMissing},
		{"GetRange_NegativeOffsetIsInvalid", testRangeNegative},
		{"PutStream_LargerThanAPart", testStreamLarge},
		{"PutStream_SmallAndEmptyReplace", testStreamSmall},
		{"PutStream_ReaderErrorLeavesTheKey", testStreamReaderError},
		{"PresignGet", testPresignGet},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.test(fixture{t: t, s: newStore(t)})
		})
	}
}

// fixture is one test's store.
type fixture struct {
	t *testing.T
	s ports.ObjectStore
}

const (
	digitsKey = "orgs/acme/knowledge/digits.zip"
	digits    = "0123456789"
)

// putDigits stores the ten digits at digitsKey.
func putDigits(t *testing.T, s ports.ObjectStore) {
	t.Helper()

	if _, err := s.Put(t.Context(), digitsKey, []byte(digits)); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

// readRange is GetRange's bytes.
func readRange(t *testing.T, s ports.ObjectStore, key string, offset, length int64) string {
	t.Helper()

	r, err := s.GetRange(t.Context(), key, offset, length)
	if err != nil {
		t.Fatalf("GetRange(%d, %d): %v", offset, length, err)
	}

	defer func() { _ = r.Close() }()

	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("GetRange(%d, %d): read: %v", offset, length, err)
	}

	return string(b)
}

func testRangeMiddle(f fixture) {
	t, s := f.t, f.s

	putDigits(t, s)

	if got := readRange(t, s, digitsKey, 2, 3); got != "234" {
		t.Errorf("GetRange(2, 3) = %q, want 234", got)
	}

	if got := readRange(t, s, digitsKey, 0, 1); got != "0" {
		t.Errorf("GetRange(0, 1) = %q, want 0", got)
	}
}

func testRangeEnd(f fixture) {
	t, s := f.t, f.s

	putDigits(t, s)

	for _, tc := range []struct {
		offset, length int64
		want           string
	}{
		{7, 3, "789"},  // exactly to the end
		{7, 50, "789"}, // a length past the end stops at it
		{7, -1, "789"}, // the rest
		{0, -1, digits},
		{9, 1, "9"},
	} {
		if got := readRange(t, s, digitsKey, tc.offset, tc.length); got != tc.want {
			t.Errorf("GetRange(%d, %d) = %q, want %q", tc.offset, tc.length, got, tc.want)
		}
	}
}

func testRangePastEnd(f fixture) {
	t, s := f.t, f.s

	putDigits(t, s)

	for _, tc := range []struct{ offset, length int64 }{{10, 5}, {11, 1}, {1 << 40, 1}, {10, -1}, {3, 0}} {
		if got := readRange(t, s, digitsKey, tc.offset, tc.length); got != "" {
			t.Errorf("GetRange(%d, %d) = %q, want nothing", tc.offset, tc.length, got)
		}
	}
}

func testRangeMissing(f fixture) {
	t, s := f.t, f.s

	for _, tc := range []struct{ offset, length int64 }{{0, 10}, {5, -1}, {0, 0}} {
		r, err := s.GetRange(t.Context(), "orgs/acme/knowledge/missing.zip", tc.offset, tc.length)
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("GetRange(%d, %d) of a missing key = %v, want ErrNotFound", tc.offset, tc.length, err)
		}

		if r != nil {
			_ = r.Close()
		}
	}
}

func testRangeNegative(f fixture) {
	t, s := f.t, f.s

	putDigits(t, s)

	r, err := s.GetRange(t.Context(), digitsKey, -1, 2)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("GetRange(-1, 2) = %v, want ErrInvalid", err)
	}

	if r != nil {
		_ = r.Close()
	}
}

// chunks yields its bytes in small reads and hides bytes.Reader's WriterTo, as a file or a pipe
// would, so an adapter cannot take the whole body in one call.
type chunks struct {
	r *bytes.Reader
	// fail, when set, is returned once fail bytes have been read.
	fail  error
	after int
	read  int
}

func (c *chunks) Read(p []byte) (int, error) {
	if c.fail != nil && c.read >= c.after {
		return 0, c.fail
	}

	n, err := c.r.Read(p[:min(len(p), 64<<10)])
	c.read += n

	return n, err
}

func testStreamLarge(f fixture) {
	t, s := f.t, f.s

	body := make([]byte, StreamBytes)
	_, _ = rand.Read(body)

	const key = "orgs/acme/knowledge/source/big.zip"

	info, err := s.PutStream(t.Context(), key, &chunks{r: bytes.NewReader(body)})
	if err != nil {
		t.Fatalf("PutStream: %v", err)
	}

	if info.Key != key || info.Size != StreamBytes || info.ETag == "" {
		t.Errorf("PutStream = %+v, want %s of %d bytes with an ETag", info, key, StreamBytes)
	}

	got, err := s.Get(t.Context(), key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !bytes.Equal(got.Body, body) {
		t.Fatalf("Get after PutStream: %d bytes differ from the %d written", len(got.Body), len(body))
	}

	// A range across the parts' seam reads the same bytes.
	const seam = 5 << 20

	if r := readRange(t, s, key, seam-10, 20); r != string(body[seam-10:seam+10]) {
		t.Errorf("GetRange across the seam differs")
	}

	list, err := s.List(t.Context(), "orgs/acme/knowledge/source/")
	if err != nil || len(list) != 1 || list[0].Size != StreamBytes {
		t.Errorf("List = %+v, %v; want the one object of %d bytes", list, err, StreamBytes)
	}
}

func testStreamSmall(f fixture) {
	t, s := f.t, f.s

	putDigits(t, s)

	info, err := s.PutStream(t.Context(), digitsKey, &chunks{r: bytes.NewReader([]byte("abc"))})
	if err != nil || info.Size != 3 {
		t.Fatalf("PutStream = %+v, %v; want 3 bytes", info, err)
	}

	if got := readRange(t, s, digitsKey, 0, -1); got != "abc" {
		t.Errorf("after PutStream the object is %q, want abc", got)
	}

	const empty = "orgs/acme/knowledge/empty.zip"

	if info, err := s.PutStream(t.Context(), empty, bytes.NewReader(nil)); err != nil || info.Size != 0 {
		t.Fatalf("PutStream of nothing = %+v, %v; want an empty object", info, err)
	}

	got, err := s.Get(t.Context(), empty)
	if err != nil || len(got.Body) != 0 {
		t.Errorf("Get of the empty object = %d bytes, %v", len(got.Body), err)
	}
}

func testStreamReaderError(f fixture) {
	t, s := f.t, f.s

	boom := errors.New("disk gone")
	body := make([]byte, StreamBytes)

	const fresh = "orgs/acme/knowledge/source/broken.zip"

	// Past the first part, so an adapter uploading in parts has one to abort.
	_, err := s.PutStream(t.Context(), fresh, &chunks{r: bytes.NewReader(body), fail: boom, after: 5<<20 + 1})
	if !errors.Is(err, boom) {
		t.Errorf("PutStream with a failing reader = %v, want its error", err)
	}

	if _, err := s.Get(t.Context(), fresh); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Get after a failed PutStream = %v, want ErrNotFound", err)
	}

	putDigits(t, s)

	_, err = s.PutStream(t.Context(), digitsKey, &chunks{r: bytes.NewReader([]byte("xyz")), fail: boom, after: 2})
	if !errors.Is(err, boom) {
		t.Errorf("PutStream over an object with a failing reader = %v, want its error", err)
	}

	if got := readRange(t, s, digitsKey, 0, -1); got != digits {
		t.Errorf("after a failed PutStream the object is %q, want it as it was", got)
	}
}

func testPresignGet(f fixture) {
	t, s := f.t, f.s

	putDigits(t, s)

	u, err := s.PresignGet(t.Context(), digitsKey, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}

	if !strings.Contains(u, digitsKey) {
		t.Errorf("PresignGet = a URL that does not name %s", digitsKey)
	}

	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := s.PresignGet(t.Context(), digitsKey, ttl); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("PresignGet with ttl %s = %v, want ErrInvalid", ttl, err)
		}
	}
}
