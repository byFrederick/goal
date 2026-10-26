package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// adminGoald serves a stub install's platform admins and security log, and records what it was
// sent.
type adminGoald struct {
	srv *httptest.Server

	mu       sync.Mutex
	admins   []gen.PlatformAdmin
	requests []string
}

const (
	listAdmins = "GET /v1/platform/admins"

	adaID = "019a5000-0000-7000-8000-000000000001"
	bobID = "019a5000-0000-7000-8000-000000000002"
)

func newAdminGoald(t *testing.T) *adminGoald {
	t.Helper()

	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	g := &adminGoald{admins: []gen.PlatformAdmin{
		{User: gen.User{Id: adaID, Name: "Ada", Email: "ada@example.com"}, AddedBy: gen.Actor{Kind: gen.ActorKindSystem, Id: "bootstrap", Name: "bootstrap"}, AddedAt: at},
		{User: gen.User{Id: bobID, Name: "Bob", Email: "bob@example.com"}, AddedBy: gen.Actor{Kind: gen.ActorKindUser, Id: adaID, Name: "Ada"}, AddedAt: at},
	}}

	reply := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")

		if status >= http.StatusBadRequest {
			w.Header().Set("Content-Type", "application/problem+json")
		}

		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(listAdmins, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, gen.PlatformAdminList{Admins: g.admins})
	})
	mux.HandleFunc("POST /v1/platform/admins", func(w http.ResponseWriter, r *http.Request) {
		var b gen.NewPlatformAdmin

		_ = json.NewDecoder(r.Body).Decode(&b)

		if b.Email != "cy@example.com" {
			code := "not-found"
			reply(w, http.StatusNotFound, gen.Problem{Title: http.StatusText(http.StatusNotFound), Status: http.StatusNotFound, Code: &code})

			return
		}

		reply(w, http.StatusCreated, gen.PlatformAdmin{User: gen.User{Id: "c", Name: "Cy", Email: b.Email}})
	})
	mux.HandleFunc("DELETE /v1/platform/admins/{user}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("user") == adaID {
			code, detail := "last-platform-admin", "the install keeps at least one platform admin"
			reply(w, http.StatusConflict, gen.Problem{Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict, Code: &code, Detail: &detail})

			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/platform/auth-events", func(w http.ResponseWriter, _ *http.Request) {
		user, ip := adaID, "203.0.113.7"
		next := int64(41)
		reply(w, http.StatusOK, gen.AuthEventPage{Events: []gen.AuthEvent{
			{Id: 42, At: at, Kind: "sign_in.succeeded", UserId: &user, Ip: &ip, Data: map[string]any{"method": "email_code"}},
		}, NextBefore: &next})
	})

	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.requests = append(g.requests, r.Method+" "+r.URL.RequestURI())
		g.mu.Unlock()

		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)

	return g
}

func (g *adminGoald) sent() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]string(nil), g.requests...)
}

func TestAdmin_Commands(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		args    []string
		want    string // in the output
		wantErr string
		sent    []string
	}{
		{
			name: "list", args: []string{subList}, want: "Bob                      bob@example.com                  added by Ada, ",
			sent: []string{listAdmins},
		},
		{
			name: "add", args: []string{subAdd, "cy@example.com"}, want: "✓ Cy (cy@example.com) is a platform admin",
			sent: []string{"POST /v1/platform/admins"},
		},
		{name: "add someone unknown", args: []string{subAdd, "nobody@example.com"}, wantErr: "Not Found", sent: []string{"POST /v1/platform/admins"}},
		{
			name: "remove by email, any case", args: []string{subRemove, "BOB@example.com"}, want: "✓ Bob (bob@example.com) is no longer a platform admin",
			sent: []string{listAdmins, "DELETE /v1/platform/admins/" + bobID},
		},
		{
			name: "remove the last", args: []string{subRemove, "ada@example.com"}, wantErr: "keeps at least one platform admin",
			sent: []string{listAdmins, "DELETE /v1/platform/admins/" + adaID},
		},
		{name: "remove a non-admin", args: []string{subRemove, "cy@example.com"}, wantErr: "is not a platform admin", sent: []string{listAdmins}},
		{
			name: "events", args: []string{"events", "--kind", "sign_in.succeeded"},
			want: `42  ` + time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).Local().Format(time.DateTime) + `  sign_in.succeeded        user=` + adaID +
				` ip=203.0.113.7 {"method":"email_code"}` + "\nolder events: goal admin events --before 41 --kind sign_in.succeeded",
			sent: []string{"GET /v1/platform/auth-events?kind=sign_in.succeeded"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newAdminGoald(t)

			var buf bytes.Buffer

			err := run(t.Context(), append([]string{"admin"}, tt.args...), &printer{w: &buf}, testEnv(t, g.srv.URL, nil), nil)

			switch {
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("goal admin %v = %v; want an error saying %q", tt.args, err, tt.wantErr)
			case tt.wantErr == "" && err != nil:
				t.Fatalf("goal admin %v = %v", tt.args, err)
			case !strings.Contains(buf.String(), tt.want):
				t.Errorf("goal admin %v printed:\n%s\nwant it to contain:\n%s", tt.args, buf.String(), tt.want)
			}

			if got := g.sent(); strings.Join(got, "\n") != strings.Join(tt.sent, "\n") {
				t.Errorf("goal admin %v sent %v; want %v", tt.args, got, tt.sent)
			}
		})
	}
}

func TestAdmin_Usage(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{}, {subList, "x"}, {subAdd}, {subRemove, "a", "b"}, {"events", "x"}, {"promote", "x"}} {
		err := run(t.Context(), append([]string{"admin"}, args...), &printer{w: &bytes.Buffer{}}, testEnv(t, "http://127.0.0.1:1", nil), nil)
		if !errors.Is(err, errUsage) {
			t.Errorf("goal admin %v = %v; want a usage error", args, err)
		}
	}
}
