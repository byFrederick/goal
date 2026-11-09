package api

import (
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/byFrederick/goal/internal/api/gen"
)

// StartingRetryAfter is the Retry-After, in seconds, of the 503 goald-starting answer.
const StartingRetryAfter = 5

// Starting serves goald's listeners while goald starts: until Ready, every request answers 503
// goald-starting at once, never cached, with Retry-After (the Starting response of
// contracts/api/openapi.yaml); after it, the handler Ready was given serves them. The listeners
// bind before goald opens its database, so a client tells a starting goald from a broken one.
type Starting struct {
	ready atomic.Pointer[http.Handler]
}

// NewStarting returns a Starting that is not ready.
func NewStarting() *Starting { return &Starting{} }

// Ready hands every request from now on to h.
func (s *Starting) Ready(h http.Handler) { s.ready.Store(&h) }

func (s *Starting) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := s.ready.Load(); h != nil {
		(*h).ServeHTTP(w, r)
		return
	}

	w.Header().Set("Retry-After", strconv.Itoa(StartingRetryAfter))
	w.Header().Set("Cache-Control", "no-store")

	detail := "goald is starting; retry in a few seconds"
	writeProblem(w, gen.Problem{
		Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable,
		Detail: &detail, Code: optional("goald-starting"),
	})
}
