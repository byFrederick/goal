// Package cloudflare runs sandboxes on Cloudflare Containers (ports.Sandboxes): an HTTP client of
// the control Worker's sandbox API (contracts/api/sandbox.yaml, deploy/cloudflare). The Worker keeps one
// Durable Object per run, which starts the run's container, streams its commands and destroys it
// at its deadline even if no goald is left.
package cloudflare

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Sandboxes = (*Sandboxes)(nil)

// Config is where the sandbox API is and its key.
type Config struct {
	// URL is the Worker's sandbox API (GOAL_SANDBOX_URL): http://sandbox.internal from goald's
	// container.
	URL string
	// Key is the bearer the Worker checks (GOAL_SANDBOX_KEY).
	Key string
	// Client sends the requests; nil is a client of its own. Exec streams last as long as their
	// commands, so it must have no overall timeout.
	Client *http.Client
}

// killLimit bounds the kill a cancelled command's Process sends after its caller left.
const killLimit = 30 * time.Second

// An exec stream cut before the command's outcome is attached again from the output already read,
// at most maxAttaches times, after attachWait (default) doubling each time: a long command must
// survive a connection Cloudflare drops on the way (spec 4 §5.12 check 4).
const (
	maxAttaches       = 5
	defaultAttachWait = time.Second
)

// errCut is a stream that ended before the command's outcome.
var errCut = errors.New("the stream ended without the command's outcome")

// Sandboxes is ports.Sandboxes on the control Worker. It is safe for concurrent use.
type Sandboxes struct {
	base   *url.URL
	key    string
	client *http.Client
	log    *slog.Logger
	// attachWait is the pause before the first attach of a cut exec stream; it doubles each time.
	attachWait time.Duration
}

// New returns sandboxes on the Worker cfg names.
func New(cfg Config, log *slog.Logger) (*Sandboxes, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.User != nil {
		return nil, fmt.Errorf("sandbox API URL %q is not an http(s) origin such as http://sandbox.internal", cfg.URL)
	}

	if cfg.Key == "" {
		return nil, errors.New("the sandbox API needs its key (GOAL_SANDBOX_KEY)")
	}

	u.Path = strings.TrimSuffix(u.Path, "/")

	return &Sandboxes{base: u, key: cfg.Key, client: cmp.Or(cfg.Client, &http.Client{}), log: log, attachWait: defaultAttachWait}, nil
}

// spec is a sandbox as the API takes it.
type spec struct {
	Org      string            `json:"org"`
	Factory  string            `json:"factory"`
	Item     string            `json:"item"`
	Station  string            `json:"station"`
	Agent    string            `json:"agent"`
	Image    string            `json:"image"`
	Size     string            `json:"size"`
	Env      map[string]string `json:"env"`
	Deadline time.Time         `json:"deadline"`
}

// entry is a sandbox as the API lists it.
type entry struct {
	Run string `json:"run"`
	Org string `json:"org"`
}

// Create implements ports.Sandboxes. The Worker answers once the container runs.
func (s *Sandboxes) Create(ctx context.Context, sp ports.SandboxSpec) (ports.Sandbox, error) {
	if sp.RunID == "" || sp.Image == "" || sp.Deadline.IsZero() {
		return nil, errors.New("sandbox spec: run, image and deadline are required")
	}

	start := time.Now()
	size := cmp.Or(sp.Size, domain.SandboxMedium)
	body := spec{
		Org: string(sp.OrgID), Factory: string(sp.FactoryID), Item: string(sp.ItemID), Station: sp.Station, Agent: sp.Agent,
		Image: sp.Image, Size: string(size), Env: sp.Env, Deadline: sp.Deadline.UTC(),
	}

	if body.Env == nil {
		body.Env = map[string]string{}
	}

	status, err := s.send(ctx, http.MethodPut, s.boxPath(sp.RunID), body)
	if err != nil {
		return nil, fmt.Errorf("create sandbox of run %s: %w", sp.RunID, err)
	}

	if status == http.StatusCreated {
		s.log.InfoContext(ctx, "sandbox created", slog.String("run", string(sp.RunID)), slog.String("size", string(size)),
			slog.Float64("seconds", time.Since(start).Seconds()))
	}

	return &box{s: s, run: sp.RunID}, nil
}

// List implements ports.Sandboxes: every box the Worker holds, whichever goald started it.
func (s *Sandboxes) List(ctx context.Context) ([]ports.Leftover, error) {
	body, err := s.call(ctx, http.MethodGet, "/v1/sandboxes", nil)
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}
	defer func() { _ = body.Close() }()

	var list struct {
		Sandboxes []entry `json:"sandboxes"`
	}

	if err := json.NewDecoder(io.LimitReader(body, maxList)).Decode(&list); err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}

	out := make([]ports.Leftover, 0, len(list.Sandboxes))
	for _, e := range list.Sandboxes {
		out = append(out, ports.Leftover{Run: domain.RunID(e.Run), Org: domain.OrgID(e.Org)})
	}

	return out, nil
}

// maxList bounds the list's answer: some 40 bytes a box.
const maxList = 64 << 20

// Delete implements ports.Sandboxes: the Worker destroys the run's container, which ends its
// commands; a run without one is not an error.
func (s *Sandboxes) Delete(ctx context.Context, run domain.RunID) error {
	if _, err := s.send(ctx, http.MethodDelete, s.boxPath(run), nil); err != nil {
		return fmt.Errorf("delete sandbox of run %s: %w", run, err)
	}

	s.log.InfoContext(ctx, "sandbox deleted", slog.String("run", string(run)))

	return nil
}

func (s *Sandboxes) boxPath(run domain.RunID) string {
	return "/v1/sandboxes/" + url.PathEscape(string(run))
}

// send is call for an answer whose body says nothing more: it returns the status.
func (s *Sandboxes) send(ctx context.Context, method, path string, body any) (int, error) {
	res, err := s.do(ctx, method, path, body)
	if err != nil {
		return 0, err
	}

	_ = res.Body.Close()

	return res.StatusCode, nil
}

// call sends one request and returns the body of its answer, which the caller closes.
func (s *Sandboxes) call(ctx context.Context, method, path string, body any) (io.ReadCloser, error) {
	res, err := s.do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}

	return res.Body, nil
}

// do sends one request with the key and returns its answer when it is a success; any other
// answer is an error with the Worker's reason. body, when not nil, goes as JSON.
func (s *Sandboxes) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var payload io.Reader

	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}

		payload = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.base.String()+path, payload)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.key)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}

		return nil, fmt.Errorf("%w: %w", ports.ErrSandboxLost, err)
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		defer func() { _ = res.Body.Close() }()

		var problem struct {
			Error string `json:"error"`
		}

		_ = json.NewDecoder(io.LimitReader(res.Body, 4<<10)).Decode(&problem)

		return nil, &answerError{status: res.StatusCode, reason: cmp.Or(problem.Error, http.StatusText(res.StatusCode))}
	}

	return res, nil
}

// answerError is an answer of the sandbox API other than a success. A 5xx is the Worker or the
// box failing (ports.ErrSandboxLost); anything else is a refusal.
type answerError struct {
	status int
	reason string
}

func (e *answerError) Error() string {
	return fmt.Sprintf("the sandbox API answered %d: %s", e.status, e.reason)
}

func (e *answerError) Is(target error) bool {
	return target == ports.ErrSandboxLost && e.status >= http.StatusInternalServerError
}

// gone marks the 404 an exec or an attach gets when its box or command no longer exists (a
// restart, a redeploy) as ports.ErrSandboxLost; elsewhere a 404 is a path the Worker lacks.
func gone(err error) error {
	var a *answerError
	if errors.As(err, &a) && a.status == http.StatusNotFound {
		return fmt.Errorf("%w: %w", ports.ErrSandboxLost, err)
	}

	return err
}

// box is one run's container.
type box struct {
	s   *Sandboxes
	run domain.RunID
}

// Delete implements ports.Sandbox.
func (b *box) Delete(ctx context.Context) error { return b.s.Delete(ctx, b.run) }

// execRequest is a command as the API takes it.
type execRequest struct {
	ID       string            `json:"id"`
	Argv     []string          `json:"argv"`
	Env      map[string]string `json:"env,omitempty"`
	Dir      string            `json:"dir,omitempty"`
	TimeoutS int64             `json:"timeout_s,omitempty"`
}

// Exec implements ports.Sandbox: the Worker runs the command and streams its output, then how it
// ended; a stream cut on the way is attached again where it stopped. Cancelling ctx ends Stdout at
// once and asks the Worker to kill the command.
func (b *box) Exec(ctx context.Context, e ports.Exec) (ports.Process, error) {
	if len(e.Argv) == 0 {
		return nil, errors.New("exec without a command")
	}

	id := make([]byte, 16)
	_, _ = rand.Read(id)

	x := execRequest{ID: hex.EncodeToString(id), Argv: e.Argv, Env: e.Env, Dir: e.Dir, TimeoutS: seconds(e.Timeout)}

	body, err := b.s.call(ctx, http.MethodPost, b.s.boxPath(b.run)+"/execs", x)
	if err != nil {
		return nil, fmt.Errorf("exec in sandbox of run %s: %w", b.run, gone(err))
	}

	stdoutR, stdoutW := io.Pipe()
	p := &process{b: b, id: x.ID, stdout: stdoutR, done: make(chan struct{})}

	go p.read(ctx, body, stdoutW)

	return p, nil
}

// seconds is a timeout in whole seconds, rounded up: the API counts in seconds.
func seconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}

	return int64(math.Ceil(d.Seconds()))
}

// process is one command running in a box.
type process struct {
	b      *box
	id     string
	stdout *io.PipeReader
	// offset is the bytes of output written to stdout so far: where an attach starts.
	offset int64

	done   chan struct{}
	status ports.ExitStatus
	err    error
}

// Stdout implements ports.Process.
func (p *process) Stdout() io.Reader { return p.stdout }

// Wait implements ports.Process.
func (p *process) Wait() (ports.ExitStatus, error) {
	<-p.done
	return p.status, p.err
}

// line is one line of an exec's answer: a piece of output, how the command ended, or why its
// outcome is unknown.
type line struct {
	Out      *string `json:"o"`
	Exit     *int    `json:"exit"`
	TimedOut bool    `json:"timed_out"`
	Stderr   string  `json:"stderr"`
	Error    *string `json:"error"`
}

// read copies the answer's output to stdout until its last line, then records how the command
// ended. A cut stream is attached again from the output already read. When ctx ends first, stdout
// ends at once and the Worker is asked to kill the command.
func (p *process) read(ctx context.Context, body io.ReadCloser, stdout *io.PipeWriter) {
	defer close(p.done)

	// A write waiting for the caller returns once ctx ends; so does the read of body, whose
	// request has ctx.
	stop := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	defer stop()

	err := p.decode(body, stdout)
	_ = body.Close()

	for attach := 0; errors.Is(err, errCut) && ctx.Err() == nil && attach < maxAttaches; attach++ {
		p.b.s.log.WarnContext(ctx, "sandbox exec stream cut", slog.String("run", string(p.b.run)),
			slog.Int64("offset", p.offset), slog.Int("attach", attach+1))

		select {
		case <-ctx.Done():
		case <-time.After(p.b.s.attachWait << attach):
		}

		if ctx.Err() != nil {
			break
		}

		path := fmt.Sprintf("%s/execs/%s?offset=%d", p.b.s.boxPath(p.b.run), p.id, p.offset)

		if body, err = p.b.s.call(ctx, http.MethodGet, path, nil); err != nil {
			err = gone(err)
			break
		}

		err = p.decode(body, stdout)
		_ = body.Close()
	}

	if ctx.Err() != nil {
		p.err = fmt.Errorf("exec in sandbox of run %s: %w", p.b.run, ctx.Err())
		_ = stdout.Close()

		p.kill(ctx)

		return
	}

	if errors.Is(err, errCut) {
		err = fmt.Errorf("%w: %w", ports.ErrSandboxLost, err)
	}

	if err != nil {
		p.err = fmt.Errorf("exec in sandbox of run %s: %w", p.b.run, err)
		// Its outcome is lost to goald: nobody waits for the command any longer.
		p.kill(ctx)
	}

	_ = stdout.Close()
}

// decode reads the answer's lines into stdout and the status. A stream that ends, or breaks,
// before the outcome is errCut; one that is not the protocol is an error of its own.
func (p *process) decode(body io.Reader, stdout io.Writer) error {
	dec := json.NewDecoder(body)

	for {
		var l line
		if err := dec.Decode(&l); err != nil {
			var (
				syntax *json.SyntaxError
				typ    *json.UnmarshalTypeError
			)

			if errors.As(err, &syntax) || errors.As(err, &typ) {
				return err
			}

			if errors.Is(err, io.EOF) {
				return errCut
			}

			return fmt.Errorf("%w: %w", errCut, err)
		}

		switch {
		case l.Out != nil:
			if _, err := io.WriteString(stdout, *l.Out); err != nil {
				return err
			}

			p.offset += int64(len(*l.Out))
		case l.Exit != nil:
			p.status = ports.ExitStatus{Code: *l.Exit, TimedOut: l.TimedOut, StderrTail: lastBytes(l.Stderr, ports.MaxStderrTail)}
			return nil
		case l.Error != nil: // the Worker lost the command: its box went or the box's connection broke
			return fmt.Errorf("%w: %s", ports.ErrSandboxLost, *l.Error)
		}
	}
}

// kill asks the Worker to end the command, which no one waits for any longer. A failure is
// logged: the box's deadline, or its deletion, ends the command anyway.
func (p *process) kill(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), killLimit)
	defer cancel()

	if _, err := p.b.s.send(ctx, http.MethodDelete, p.b.s.boxPath(p.b.run)+"/execs/"+p.id, nil); err != nil {
		p.b.s.log.WarnContext(ctx, "kill a sandbox command", slog.String("run", string(p.b.run)), slog.Any("error", err))
	}
}

// lastBytes is the end of s, at most n bytes.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[len(s)-n:]
}
