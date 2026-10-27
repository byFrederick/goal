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

// TaskKindEval is one try of an eval case (ADR 0025 decision 4). Its token names no connection,
// so the gateway serves it only the knowledge tools: an eval never pushes, comments or calls an
// outside API. The model connection reaches the agent's box apart from the token.
const TaskKindEval TaskKind = "eval"

// TaskClaims scope one agent task's access to the MCP gateway: which organization, factory,
// definition version, item, station, agent and run it acts for, which connections it may use,
// what the organization policy decided at run start (the model and the denied commands) and
// until when.
type TaskClaims struct {
	// Audience is always TaskAudience: SignTaskToken sets it and VerifyTaskToken requires it.
	Audience string `json:"aud"`
	// Kind is empty for a production task and TaskKindEval for an eval try.
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
	ExpiresAt      int64    `json:"e"` // unix seconds
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

// checkKind refuses an eval task's claims that name a connection.
func (c TaskClaims) checkKind() error {
	if c.Kind == TaskKindEval && len(c.Connections) > 0 {
		return fmt.Errorf("%w: an eval task names connections", ErrBadTaskToken)
	}

	return nil
}

func mac(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(payload)

	return h.Sum(nil)
}
