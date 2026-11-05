package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBadTaskToken means a task token is malformed, forged, expired or not for the gateway.
var ErrBadTaskToken = errors.New("bad task token")

// TaskAudience is the one audience of a task token: the agents' MCP gateway. goald's API never
// accepts a task token, and nothing else verifies one.
const TaskAudience = "goal-gateway"

// TaskKind is what kind of task a token is for: empty for a production run of a line's item.
type TaskKind string

// TaskKindDraftRequest is a draft request's box (ADR 0021 decision 2): it names the request and
// the draft revision whose files it starts from, and no item, station or connection. The gateway
// serves it only its bundle, with the draft's files and no repository.
const TaskKindDraftRequest TaskKind = "draft_request"

// TaskKindJonah is a Jonah review's run (ADR 0034 decision 1). Its token names no
// connection, and it carries the review's window: the gateway serves it the factory's
// definition files, its sent-back events in that window and its recently rejected proposals,
// and no MCP tool.
const TaskKindJonah TaskKind = "jonah"

// TaskKindTry is a try of a past item (ADR 0039 decision 9). Its token names the try (TryOf),
// whose original run's brief, outputs and commit the gateway serves, no item and no connection;
// its environment's env is for setup alone, and the agent gets no files, MCP servers or tools.
const TaskKindTry TaskKind = "try"

// TaskKindGrade is a grade of a try or of its item's original or final result (ADR 0039 decision
// 10). Its token names the try (TryOf) and which result it grades (GradeOf), no item and no
// connection: the gateway serves it spec.grade, the reason the item went back and that result,
// and no repository, definition, file or MCP tool.
const TaskKindGrade TaskKind = "grade"

// TaskKindCheck is a trigger's check (ADR 0040 decision 19). Its token names the trigger
// (Trigger) and no item or station: the gateway serves the trigger's brief and input, its
// agent's environment with ask tools off, no knowledge and no tool of its own.
const TaskKindCheck TaskKind = "check"

// TaskClaims scope one agent task's access to the MCP gateway: which organization, factory,
// definition version, item, station, agent and run it acts for, which connections it may use,
// what the organization policy decided at run start (the model, the denied commands and whether
// the agent gets no environment) and
// until when.
type TaskClaims struct {
	// Audience is always TaskAudience: SignTaskToken sets it and VerifyTaskToken requires it.
	Audience string `json:"aud"`
	// Kind is empty for a production task, TaskKindJonah for a Jonah review,
	// TaskKindDraftRequest for a draft request's box, TaskKindTry for a try of a past item and
	// TaskKindGrade for a grade.
	Kind      TaskKind  `json:"k,omitempty"`
	OrgID     OrgID     `json:"o"`
	FactoryID FactoryID `json:"f"`
	// VersionID is the definition version the run started on: the gateway serves that
	// version's agents and connections.
	VersionID   DefinitionVersionID `json:"v"`
	ItemID      ItemID              `json:"i"`
	Station     string              `json:"s"`
	Agent       string              `json:"a"`
	RunID       RunID               `json:"r"`
	Connections []string            `json:"c,omitempty"`
	// Model is the run's model: its agent's, its factory's or the organization policy's default
	// (ADR 0024). Tokens issued before the policy name none; the gateway then takes the version's.
	Model string `json:"m,omitempty"`
	// DeniedCommands are the organization policy's denied commands at run start, and the web
	// tools (WebFetch, WebSearch) when its Read the web and docs default is off or ask, which the
	// gateway adds to the agent's deny rules whatever the version says.
	DeniedCommands []string `json:"d,omitempty"`
	// Knowledge are the knowledge snapshots the engine pinned at the claim's start (ADR 0038
	// decision 7): the gateway serves the run these snapshots' zips and its prompt's Knowledge
	// section, and no other snapshot.
	Knowledge []KnowledgePin `json:"kn,omitempty"`
	// NoEnvironment says the organization policy's Use secrets was off at run start (ADR 0037
	// decision 12): the gateway's bundle then gives the agent's environment no variables, files
	// or MCP servers. Tokens issued before it leave it false.
	NoEnvironment bool `json:"ne,omitempty"`
	// Branch is the run's branch and Base the branch it is cut from and its pull request goes
	// into, as the engine decided them at run start (RunBase). An empty Base is the default
	// branch; tokens issued before them name neither, and the gateway then takes the item's
	// branch and the default branch.
	Branch string `json:"b,omitempty"`
	Base   string `json:"bb,omitempty"`
	// DraftRequest is the draft request a TaskKindDraftRequest task runs, and DraftRevision the
	// revision of the factory's draft it read; a draft that moved past it is not served.
	DraftRequest  DraftRequestID `json:"dr,omitempty"`
	DraftRevision int64          `json:"dv,omitempty"`
	// WindowStart and WindowEnd (unix seconds) are a Jonah review's window, whose sent-back
	// events the gateway serves it, and Manual says a person asked for the review. Only a
	// TaskKindJonah token carries them, and it always carries a window.
	WindowStart int64 `json:"ws,omitempty"`
	WindowEnd   int64 `json:"we,omitempty"`
	Manual      bool  `json:"jm,omitempty"`
	// TryOf is the past-item try a TaskKindTry task carries out, or a TaskKindGrade task grades
	// a result for; only such tokens name one.
	TryOf PastTryID `json:"tr,omitempty"`
	// GradeOf is which result a TaskKindGrade task grades; only such a token names one, and it
	// always does.
	GradeOf GradeOf `json:"g,omitempty"`
	// Trigger is the trigger a TaskKindCheck task checks for; only such a token names one, and it
	// always does.
	Trigger   string `json:"tg,omitempty"`
	ExpiresAt int64  `json:"e"` // unix seconds
}

// SignTaskToken issues a per-task gateway token for TaskAudience, whatever c.Audience says. Any
// goald instance holding key can verify it, so no token state is stored.
func SignTaskToken(key []byte, c TaskClaims) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("%w: empty signing key", ErrBadTaskToken)
	}

	c.Audience = TaskAudience

	if err := c.checkKind(); err != nil {
		return "", err
	}

	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode task claims: %w", err)
	}

	enc := base64.RawURLEncoding

	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(key, payload)), nil
}

// VerifyTaskToken checks a token's signature, expiry and audience and returns its claims.
func VerifyTaskToken(key []byte, token string, now time.Time) (TaskClaims, error) {
	enc := base64.RawURLEncoding

	body, sig, ok := strings.Cut(token, ".")
	if !ok || len(key) == 0 {
		return TaskClaims{}, ErrBadTaskToken
	}

	payload, err := enc.DecodeString(body)
	if err != nil {
		return TaskClaims{}, fmt.Errorf("%w: %w", ErrBadTaskToken, err)
	}

	got, err := enc.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac(key, payload)) {
		return TaskClaims{}, ErrBadTaskToken
	}

	var c TaskClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return TaskClaims{}, fmt.Errorf("%w: %w", ErrBadTaskToken, err)
	}

	if now.Unix() >= c.ExpiresAt {
		return TaskClaims{}, fmt.Errorf("%w: expired", ErrBadTaskToken)
	}

	// Tokens issued before audiences name none; their runs were released when goald restarted.
	if c.Audience != TaskAudience {
		return TaskClaims{}, fmt.Errorf("%w: not for the gateway", ErrBadTaskToken)
	}

	// Tokens issued before organizations name none; their runs ended when goald was upgraded.
	if c.OrgID == "" {
		return TaskClaims{}, fmt.Errorf("%w: no organization", ErrBadTaskToken)
	}

	// Nor do tokens issued before definition versions name a version.
	if c.VersionID == "" {
		return TaskClaims{}, fmt.Errorf("%w: no definition version", ErrBadTaskToken)
	}

	if err := c.checkKind(); err != nil {
		return TaskClaims{}, err
	}

	return c, nil
}

// checkKind refuses a Jonah or draft request task's claims that name what its kind does not
// have, and any other task's that name a Jonah window or a draft request.
func (c TaskClaims) checkKind() error {
	if err := c.checkJonah(); err != nil {
		return err
	}

	if err := c.checkTry(); err != nil {
		return err
	}

	if err := c.checkGrade(); err != nil {
		return err
	}

	if err := c.checkCheck(); err != nil {
		return err
	}

	return c.checkDraftRequest()
}

// checkJonah refuses a Jonah task's claims that name a connection or have no window, and a window
// or Manual on any other kind of task.
func (c TaskClaims) checkJonah() error {
	switch {
	case c.Kind == TaskKindJonah && len(c.Connections) > 0:
		return fmt.Errorf("%w: a jonah task names connections", ErrBadTaskToken)
	case c.Kind == TaskKindJonah && (c.WindowStart <= 0 || c.WindowEnd <= c.WindowStart):
		return fmt.Errorf("%w: a jonah task needs a window that ends after it starts", ErrBadTaskToken)
	case c.Kind != TaskKindJonah && (c.WindowStart != 0 || c.WindowEnd != 0 || c.Manual):
		return fmt.Errorf("%w: only a jonah task has a window", ErrBadTaskToken)
	}

	return nil
}

// checkDraftRequest refuses a draft request's claims that name an item, a station, a connection or
// no request, and any other task's that name a draft request.
func (c TaskClaims) checkDraftRequest() error {
	switch {
	case c.Kind != TaskKindDraftRequest && (c.DraftRequest != "" || c.DraftRevision != 0):
		return fmt.Errorf("%w: only a draft request's task names a draft request", ErrBadTaskToken)
	case c.Kind == TaskKindDraftRequest && (c.DraftRequest == "" || c.DraftRevision < 0):
		return fmt.Errorf("%w: a draft request's task names no request", ErrBadTaskToken)
	case c.Kind == TaskKindDraftRequest && (c.ItemID != "" || c.Station != "" || len(c.Connections) > 0):
		return fmt.Errorf("%w: a draft request's task names an item, a station or connections", ErrBadTaskToken)
	}

	return nil
}

// checkTry refuses a try's claims that name an item or a connection, or no try or station, and
// any other task's but a grade's that name a try.
func (c TaskClaims) checkTry() error {
	switch {
	case c.Kind != TaskKindTry && c.Kind != TaskKindGrade && c.TryOf != "":
		return fmt.Errorf("%w: only a try's or a grade's task names a try", ErrBadTaskToken)
	case c.Kind == TaskKindTry && (c.TryOf == "" || c.Station == ""):
		return fmt.Errorf("%w: a try's task names no try or station", ErrBadTaskToken)
	case c.Kind == TaskKindTry && (c.ItemID != "" || len(c.Connections) > 0):
		return fmt.Errorf("%w: a try's task names an item or connections", ErrBadTaskToken)
	}

	return nil
}

// checkGrade refuses a grade's claims that name an item or a connection, or no try or result to
// grade, and any other task's that name a result to grade.
func (c TaskClaims) checkGrade() error {
	switch {
	case c.Kind != TaskKindGrade && c.GradeOf != "":
		return fmt.Errorf("%w: only a grade's task names a result to grade", ErrBadTaskToken)
	case c.Kind == TaskKindGrade && (c.TryOf == "" || !c.GradeOf.Valid()):
		return fmt.Errorf("%w: a grade's task names no try or result to grade", ErrBadTaskToken)
	case c.Kind == TaskKindGrade && (c.ItemID != "" || len(c.Connections) > 0):
		return fmt.Errorf("%w: a grade's task names an item or connections", ErrBadTaskToken)
	}

	return nil
}

// checkCheck refuses a check's claims that name an item or a station, or no trigger, and any
// other task's that name a trigger.
func (c TaskClaims) checkCheck() error {
	switch {
	case c.Kind != TaskKindCheck && c.Trigger != "":
		return fmt.Errorf("%w: only a check's task names a trigger", ErrBadTaskToken)
	case c.Kind == TaskKindCheck && (c.Trigger == "" || c.ItemID != "" || c.Station != ""):
		return fmt.Errorf("%w: a check's task names its trigger and no item or station", ErrBadTaskToken)
	}

	return nil
}

func mac(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(payload)

	return h.Sum(nil)
}
