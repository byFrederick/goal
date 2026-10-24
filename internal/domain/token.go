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

// ErrBadTaskToken means a task token is malformed, forged or expired.
var ErrBadTaskToken = errors.New("bad task token")

// TaskClaims scope one agent task's access to the MCP gateway: which organization, factory,
// definition version, item, station, agent and run it acts for, which connections it may use
// and until when.
type TaskClaims struct {
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

// SignTaskToken issues a per-task gateway token. Any goald instance holding key can verify it,
// so no token state is stored.
func SignTaskToken(key []byte, c TaskClaims) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("%w: empty signing key", ErrBadTaskToken)
	}

	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode task claims: %w", err)
	}

	enc := base64.RawURLEncoding

	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(key, payload)), nil
}

// VerifyTaskToken checks a token's signature and expiry and returns its claims.
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
