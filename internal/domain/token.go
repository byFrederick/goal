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

// TaskClaims scope one agent task's access to the MCP gateway: which organization, factory,
// definition version, item, station, agent and run it acts for, which connections it may use
// and until when.
type TaskClaims struct {
	// Audience is always TaskAudience: SignTaskToken sets it and VerifyTaskToken requires it.
	Audience  string    `json:"aud"`
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
	ExpiresAt   int64               `json:"e"` // unix seconds
}

// SignTaskToken issues a per-task gateway token for TaskAudience, whatever c.Audience says. Any
// goald instance holding key can verify it, so no token state is stored.
func SignTaskToken(key []byte, c TaskClaims) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("%w: empty signing key", ErrBadTaskToken)
	}

	c.Audience = TaskAudience

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

	return c, nil
}

func mac(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(payload)

	return h.Sum(nil)
}
