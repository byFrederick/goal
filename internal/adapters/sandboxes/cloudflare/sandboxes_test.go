package cloudflare

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

// firstLine is the output the stand-in commands write first.
const firstLine = "first\n"

// An exec's answer becomes stdout and an exit status, and an answer that does not say how the
// command ended is an error, never a status the runtime did not give (spec 4 §11.2 row 6).
func TestProcess_Decode(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", ports.MaxStderrTail+10)

	tests := []struct {
		name    string
		answer  string
		stdout  string
		status  ports.ExitStatus
		wantErr string
	}{
		{
			name:   "output then the exit",
			answer: `{"o":"fir"}` + "\n" + `{"o":"st\n"}` + "\n" + `{"exit":3,"timed_out":false,"stderr":"oops\n"}` + "\n",
			stdout: firstLine, status: ports.ExitStatus{Code: 3, StderrTail: "oops\n"},
		},
		{
			name:   "timed out, its stderr cut to the tail",
			answer: `{"exit":124,"timed_out":true,"stderr":"` + long + `"}`,
			status: ports.ExitStatus{Code: 124, TimedOut: true, StderrTail: long[10:]},
		},
		{name: "the outcome unknown", answer: `{"o":"first\n"}` + "\n" + `{"error":"the sandbox was deleted"}`, stdout: firstLine, wantErr: "deleted"},
		{name: "cut before the outcome", answer: `{"o":"first\n"}` + "\n", stdout: firstLine, wantErr: "without the command's outcome"},
		{name: "not the protocol", answer: "<html>", wantErr: "invalid character"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				p      process
				stdout strings.Builder
			)

			err := p.decode(strings.NewReader(tt.answer), &stdout)

			if stdout.String() != tt.stdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.stdout)
			}

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want one mentioning %q", err, tt.wantErr)
				}

				return
			}

			if err != nil || p.status != tt.status {
				t.Errorf("status = %+v, %v; want %+v", p.status, err, tt.status)
			}
		})
	}
}

// A stream cut before the command's outcome is attached again from the output already read, so a
// long command survives a dropped connection on the way (spec 4 §5.12 check 4). One that cannot be
// attached again is an error, and the command is killed: the Worker never learns its reader left.
func TestExec_AttachesAgainAfterACut(t *testing.T) {
	t.Parallel()

	tests := []attachCase{
		{
			name: "the rest after the cut",
			attach: func(w http.ResponseWriter) {
				_, _ = io.WriteString(w, `{"o":"second\n"}`+"\n"+`{"exit":0,"timed_out":false,"stderr":""}`+"\n")
			},
			stdout: firstLine + "second\n",
		},
		{
			name:    "an exec the Worker forgot",
			attach:  func(w http.ResponseWriter) { http.Error(w, `{"error":"no such exec"}`, http.StatusNotFound) },
			stdout:  firstLine,
			wantErr: "no such exec",
			kills:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}

// attachCase is a Worker that cuts an exec's stream after its first line, then answers the
// attach with attach.
type attachCase struct {
	name    string
	attach  func(w http.ResponseWriter)
	stdout  string
	wantErr string
	kills   int
}

func (tt attachCase) run(t *testing.T) {
	t.Helper()

	s, offsets, kills := tt.worker(t)

	p, err := (&box{s: s, run: "run-1"}).Exec(t.Context(), ports.Exec{Argv: []string{"long"}})
	if err != nil {
		t.Fatal(err)
	}

	out, _ := io.ReadAll(p.Stdout())
	status, err := p.Wait()

	if string(out) != tt.stdout || *kills != tt.kills || len(*offsets) == 0 || (*offsets)[0] != "6" {
		t.Errorf("stdout %q, %d kills, attached at offsets %q; want %q, %d kills, the first attach at 6 (the bytes read)",
			out, *kills, *offsets, tt.stdout, tt.kills)
	}

	switch {
	case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
		t.Errorf("Wait = %+v, %v; want an error mentioning %q", status, err, tt.wantErr)
	case tt.wantErr == "" && (err != nil || status.Code != 0):
		t.Errorf("Wait = %+v, %v; want exit 0", status, err)
	}
}

// worker is sandboxes on a stand-in Worker, with the offsets it was asked to attach at and the
// kills it got.
func (tt attachCase) worker(t *testing.T) (*Sandboxes, *[]string, *int) {
	t.Helper()

	var (
		offsets []string
		kills   int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exec := strings.HasPrefix(r.URL.Path, "/v1/sandboxes/run-1/execs/")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes/run-1/execs":
			_, _ = io.WriteString(w, `{"o":"first\n"}`+"\n") // and the connection ends
		case r.Method == http.MethodGet && exec:
			offsets = append(offsets, r.URL.Query().Get("offset"))

			tt.attach(w)
		case r.Method == http.MethodDelete && exec:
			kills++
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)

	s, err := New(Config{URL: srv.URL, Key: "key"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	s.attachWait = time.Millisecond

	return s, &offsets, &kills
}

// A failure of the box or of the way to it wraps ports.ErrSandboxLost, so the engine runs the
// item again in a new box; a command's own outcome, a refusal and a broken protocol do not.
func TestSandboxes_TransportErrorsAreSandboxLost(t *testing.T) {
	t.Parallel()

	cut := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"o":"first\n"}`+"\n") }
	answer := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { http.Error(w, body, status) }
	}

	tests := []struct {
		name string
		// create answers the box's PUT, exec its POST and attach the GET of a cut stream; nil is
		// a success that says nothing more.
		create, exec, attach http.HandlerFunc
		down                 bool // the Worker does not answer at all
		lost                 bool
	}{
		{name: "the Worker unreachable", down: true, lost: true},
		{name: "a 5xx on create", create: answer(http.StatusBadGateway, `{"error":"the container did not start"}`), lost: true},
		{name: "a 5xx on exec", exec: answer(http.StatusServiceUnavailable, `{"error":"try later"}`), lost: true},
		{name: "a 404 on exec: the box is gone", exec: answer(http.StatusNotFound, `{"error":"no sandbox for this run"}`), lost: true},
		{name: "a 404 on attach: the exec is gone", exec: cut, attach: answer(http.StatusNotFound, `{"error":"no such exec"}`), lost: true},
		{name: "cut after the last attach", exec: cut, attach: cut, lost: true},
		{
			name: "the outcome unknown",
			exec: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"error":"the command's outcome is unknown: Network connection lost."}`+"\n")
			},
			lost: true,
		},
		{
			name: "the command's own exit",
			exec: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"exit":3,"timed_out":false,"stderr":"oops"}`+"\n")
			},
		},
		{name: "a refusal", exec: answer(http.StatusBadRequest, `{"error":"argv is required"}`)},
		{name: "a 404 on create: no such path", create: answer(http.StatusNotFound, `{"error":"no such path"}`)},
		{name: "not the protocol", exec: func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h := map[string]http.HandlerFunc{http.MethodPut: tt.create, http.MethodPost: tt.exec, http.MethodGet: tt.attach}[r.Method]
				if h != nil {
					h(w, r)
				}
			}))
			t.Cleanup(srv.Close)

			if tt.down {
				srv.Close()
			}

			s, err := New(Config{URL: srv.URL, Key: "key"}, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}

			s.attachWait = time.Millisecond

			err = createAndRun(t, s)
			if err == nil && tt.lost {
				t.Fatal("no error, want one that the box was lost")
			}

			if got := errors.Is(err, ports.ErrSandboxLost); got != tt.lost {
				t.Errorf("error %v: errors.Is(ErrSandboxLost) = %v, want %v", err, got, tt.lost)
			}
		})
	}
}

// createAndRun creates a box and runs one command in it to its end.
func createAndRun(t *testing.T, s *Sandboxes) error {
	t.Helper()

	sb, err := s.Create(t.Context(), ports.SandboxSpec{RunID: "run-1", Image: "goal-agent", Deadline: time.Now().Add(time.Hour)})
	if err != nil {
		return err
	}

	p, err := sb.Exec(t.Context(), ports.Exec{Argv: []string{"long"}})
	if err != nil {
		return err
	}

	_, _ = io.Copy(io.Discard, p.Stdout())
	_, err = p.Wait()

	return err
}
