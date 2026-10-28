package engine

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The agent image's sandbox contract (apps/worker/README.md): the commands goald runs in a sandbox,
// the variables it gives them and where the repository is cloned.
const (
	envGatewayURL   = "GOAL_GATEWAY_URL"
	envGatewayToken = "GOAL_GATEWAY_TOKEN"
	// envFeedback is what goald adds to the brief for one attempt: a part's assembly, people's
	// notes, why the previous attempt was rejected, what the workspace holds.
	envFeedback  = "GOAL_FEEDBACK"
	workspaceDir = "/workspace"
)

var (
	argvPrepare = []string{"goal-agent", "prepare"}
	argvAgent   = []string{"goal-agent", "run"}
)

// box is a claim's sandbox: created by the claim's first run and prepared once; a retry of a
// rejected output runs in it too. A lost box (ports.ErrSandboxLost) is deleted and the next run
// creates a new one. The claim's work deletes it when it ends.
type box struct {
	sandbox ports.Sandbox
	// existed reports whether the item's branch was on the remote when prepare checked it out.
	existed bool
}

// prepared is goal-agent prepare's one stdout line.
type prepared struct {
	Type          string  `json:"type"`
	Seconds       float64 `json:"seconds"`
	Tools         int     `json:"tools"`
	BranchExisted bool    `json:"branch_existed"`
}

// open creates the run's sandbox when it has none (a claim's first run) and prepares it with env
// (the run's task token and what clones the repository, if any); a box already open is left as it
// is. scrub holds the run's secrets.
func (e *Engine) open(ctx context.Context, t runTask, runID domain.RunID, env map[string]string, scrub scrubber) error {
	b := t.box
	if b.sandbox != nil {
		return nil
	}

	started := e.clock.Now()
	size := cmp.Or(t.Size, domain.SandboxMedium)

	sb, err := e.sandboxes.Create(ctx, ports.SandboxSpec{
		OrgID: t.OrgID, FactoryID: t.FactoryID, ItemID: t.Item, RunID: runID, Station: t.Station, Agent: t.Agent,
		Image: e.cfg.Image, Size: t.Size, Env: map[string]string{envGatewayURL: e.cfg.GatewayURL},
		Deadline: started.Add(boxLife(t.RunRequest, e.cfg)),
	})
	if err != nil {
		return fmt.Errorf("create sandbox: %w", scrub.err(err))
	}

	b.sandbox = sb

	e.record(ctx, t.item, runID, domain.EventSandboxStarted, map[string]any{"size": string(size), "seconds": e.since(started)})

	var line *prepared

	started = e.clock.Now()

	st, err := execLines(ctx, sb, ports.Exec{Argv: argvPrepare, Env: env}, func(l []byte) {
		var p prepared
		if json.Unmarshal(l, &p) == nil && p.Type == "prepared" {
			line = &p
		}
	})

	switch {
	case err != nil:
		return fmt.Errorf("prepare the sandbox: %w", scrub.err(err))
	case st.Code != 0 || line == nil:
		return fmt.Errorf("prepare the sandbox: goal-agent prepare exited %d: %s", st.Code, scrub.string(strings.TrimSpace(st.StderrTail)))
	}

	data := map[string]any{"seconds": line.Seconds, "tools": line.Tools}
	if t.Repository.URL != "" {
		b.existed, data["branch_existed"] = line.BranchExisted, line.BranchExisted
	}

	e.record(ctx, t.item, runID, domain.EventPrepared, data)
	e.log.InfoContext(ctx, "sandbox exec", slog.String("run", string(runID)), slog.String("step", "prepare"),
		slog.Int("exit", st.Code), slog.Float64("seconds", e.since(started)))

	return nil
}

// boxLife is how long a run's sandbox may live: as long as both attempts of a claim may, since a
// retry runs in the same sandbox, and never longer than the organization policy's box time limit.
func boxLife(req domain.RunRequest, cfg Config) time.Duration {
	life := maxAttempts*cfg.TaskTimeout + cfg.LeaseTTL
	if req.BoxLimit > 0 {
		life = min(life, req.BoxLimit)
	}

	return life
}

// deleteBox deletes the claim's sandbox, if it has one, even as ctx is cancelled. A sandbox that
// cannot be deleted now is left to ReapOrphans.
func (e *Engine) deleteBox(ctx context.Context, log *slog.Logger, b *box) {
	if b.sandbox == nil {
		return
	}

	ctx, cancel := cleanup(ctx)
	defer cancel()

	if err := b.sandbox.Delete(ctx); err != nil {
		log.WarnContext(ctx, "delete the run's sandbox; the reaper deletes it once the run is over", slog.Any("error", err))
	}
}

// record appends an engine event of the run on its item, logging a failure. A run without an
// item (a Jonah run) records none, as item events need an item.
func (e *Engine) record(ctx context.Context, it domain.Item, runID domain.RunID, kind domain.EventKind, data map[string]any) {
	if it.ID == "" {
		return
	}

	ev := event(it, kind, e.clock.Now(), domain.ActorEngine, data)
	ev.RunID = runID

	if err := e.store.AppendEvents(ctx, ev); err != nil && ctx.Err() == nil {
		e.log.WarnContext(ctx, "record run event", slog.String("item", string(it.ID)), slog.String("kind", string(kind)), slog.Any("error", err))
	}
}

// since is the seconds from start to now, to the millisecond.
func (e *Engine) since(start time.Time) float64 {
	return float64(e.clock.Now().Sub(start).Milliseconds()) / 1000
}

// execLines runs x in sb, calls line with each line of its stdout as it comes and returns how it
// ended once its output has ended.
func execLines(ctx context.Context, sb ports.Sandbox, x ports.Exec, line func([]byte)) (ports.ExitStatus, error) {
	p, err := sb.Exec(ctx, x)
	if err != nil {
		return ports.ExitStatus{}, err
	}

	readErr := eachLine(p.Stdout(), maxLine, line)

	st, err := p.Wait()
	if err != nil {
		return st, err
	}

	if readErr != nil && !errors.Is(readErr, io.ErrClosedPipe) {
		return st, fmt.Errorf("read the command's output: %w", readErr)
	}

	return st, nil
}

// maxLine bounds one stdout line; the worker's finished line carries the whole output.
const maxLine = 16 << 20

// eachLine calls fn with every non-empty line of r, trimmed. Lines longer than limit are
// dropped, but r is always read to its end so the writer never blocks. fn must not keep the
// slice.
func eachLine(r io.Reader, limit int, fn func([]byte)) error {
	br := bufio.NewReaderSize(r, 64<<10)

	var (
		line    []byte
		tooLong bool
	)

	for {
		chunk, err := br.ReadSlice('\n')

		switch {
		case tooLong:
		case len(line)+len(chunk) > limit:
			tooLong, line = true, line[:0]
		default:
			line = append(line, chunk...)
		}

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil && !errors.Is(err, io.EOF):
			return err
		}

		if t := bytes.TrimSpace(line); !tooLong && len(t) > 0 {
			fn(t)
		}

		line, tooLong = line[:0], false

		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

// scrubber replaces secret values with a marker wherever they appear in what a sandbox reports:
// events, errors, outputs and logs (exact strings, best effort: an encoded value gets through).
type scrubber struct {
	r *strings.Replacer
}

// minSecretLen keeps short settings values (true, 1) from being treated as secrets.
const minSecretLen = 8

const redacted = "[redacted]"

func newScrubber(values ...string) scrubber {
	var pairs []string

	for _, v := range values {
		if len(v) >= minSecretLen {
			pairs = append(pairs, v, redacted)
		}
	}

	return scrubber{r: strings.NewReplacer(pairs...)}
}

func (s scrubber) string(v string) string { return s.r.Replace(v) }

// err scrubs err's message. Of what err wraps it keeps only ports.ErrSandboxLost, which the
// engine decides on and whose message holds no secret.
func (s scrubber) err(err error) error {
	scrubbed := &scrubbedError{msg: s.string(err.Error())}
	if errors.Is(err, ports.ErrSandboxLost) {
		scrubbed.kind = ports.ErrSandboxLost
	}

	return scrubbed
}

// scrubbedError is an error scrubbed of the run's secrets, with the sentinel it wrapped, if any.
type scrubbedError struct {
	msg  string
	kind error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.kind }

// value scrubs every string inside a decoded JSON value, in place, and returns it.
func (s scrubber) value(v any) any {
	switch x := v.(type) {
	case string:
		return s.string(x)
	case map[string]any:
		for k, e := range x {
			x[k] = s.value(e)
		}
	case []any:
		for i, e := range x {
			x[i] = s.value(e)
		}
	}

	return v
}

// json scrubs every string inside a JSON document. A document that does not parse, which the
// output check then refuses, is scrubbed as text.
func (s scrubber) json(doc json.RawMessage) json.RawMessage {
	if len(doc) == 0 {
		return doc
	}

	d := json.NewDecoder(bytes.NewReader(doc))
	d.UseNumber()

	var v any
	if err := d.Decode(&v); err != nil || d.More() {
		return json.RawMessage(s.string(string(doc)))
	}

	var out bytes.Buffer

	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(s.value(v)); err != nil {
		return json.RawMessage(s.string(string(doc)))
	}

	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}

// tail keeps the last max bytes of the lines added to it.
type tail struct {
	buf []byte
	max int
}

func (t *tail) add(line []byte) {
	t.buf = append(append(t.buf, line...), '\n')
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
}

func (t *tail) String() string { return strings.ToValidUTF8(string(t.buf), "") }
