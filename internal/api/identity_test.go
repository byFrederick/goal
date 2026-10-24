package api_test

import (
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// Under GOAL_AUTH=none the UI shows no sign-in page and names the local user: the sign-in mode is
// none, and the caller is a user of mode none with no account (no email, no session).
func TestAPI_SignInIsOffUnderNone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var config gen.AuthConfig
	if code := h.call(t, http.MethodGet, "/v1/auth/config", "", &config); code != http.StatusOK {
		t.Fatalf("auth config = %d", code)
	}

	if config.Mode != gen.None || len(config.Methods) != 0 {
		t.Errorf("auth config = %+v, want mode none and no methods", config)
	}

	var me gen.Me
	if code := h.call(t, http.MethodGet, "/v1/me", "", &me); code != http.StatusOK {
		t.Fatalf("me = %d", code)
	}

	if me.Kind != gen.MeKindUser || me.Mode != gen.None || me.User != nil || me.Session != nil || me.PlatformAdmin {
		t.Errorf("me = %+v, want the local user of mode none", me)
	}
}
