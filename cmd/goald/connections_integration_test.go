//go:build integration

package main

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/adapters/masterkey/masterkey"
	"github.com/byFrederick/goal/internal/adapters/store/postgres"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// masterKeyEntry is GOAL_MASTER_KEYS's entry for id with a secret of 32 bytes b.
func masterKeyEntry(id string, b byte) string {
	return id + ":" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
}

// keysEnv is an environment with GOAL_MASTER_KEYS keys and nothing else.
func keysEnv(keys string) func(string) string {
	return func(name string) string {
		if name == masterkey.KeysEnv {
			return keys
		}

		return ""
	}
}

// Spec 4 §11.2 row 12: goald refuses to start while a connection is sealed under a master key id
// it does not hold, missing key included; it starts with that key among others, and without any
// key while nothing is sealed.
func TestGoald_RefusesUnknownMasterKeyID(t *testing.T) {
	t.Parallel()

	db := newDatabase(t)
	store := postgres.New(db.pool)
	log := slog.New(slog.DiscardHandler)
	env := keysEnv

	if _, _, _, err := openConnections(t.Context(), store, env(""), log); err != nil {
		t.Fatalf("without a key and nothing sealed: %v", err)
	}

	conns, _, _, err := openConnections(t.Context(), store, env(masterKeyEntry("k1", 1)), log)
	if err != nil {
		t.Fatal(err)
	}

	org, err := store.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	// goald's start made the platform kinds the platform's: github is one.
	if _, err := conns.Create(domain.WithOrg(t.Context(), org.ID), domain.NewConnection{
		Name: "github", Kind: "github", Secrets: map[string]string{"token": "ghp_x"},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	for name, keys := range map[string]string{"another key": masterKeyEntry("k2", 2), "no key": ""} {
		_, _, _, err := openConnections(t.Context(), store, env(keys), log)
		if err == nil || !strings.Contains(err.Error(), "master key k1") || strings.Contains(err.Error(), "ghp_x") {
			t.Errorf("with %s: %v; want a refusal naming k1", name, err)
		}
	}

	if _, _, _, err := openConnections(t.Context(), store, env(masterKeyEntry("k2", 2)+","+masterKeyEntry("k1", 1)), log); err != nil {
		t.Errorf("with the key among others: %v", err)
	}
}

// ADR 0017: a credential counts as a connection does. One sealed under k3 alone stops a goald that
// holds k2 and k1, and the refusal names the key and credentials, never the value.
func TestGoald_RefusesUnknownCredentialKeyID(t *testing.T) {
	t.Parallel()

	db := newDatabase(t)
	store := postgres.New(db.pool)
	log := slog.New(slog.DiscardHandler)

	_, creds, _, err := openConnections(t.Context(), store, keysEnv(masterKeyEntry("k3", 3)), log)
	if err != nil {
		t.Fatal(err)
	}

	org, err := store.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := creds.Create(domain.WithOrg(t.Context(), org.ID), domain.NewCredential{
		Name: "sentry-token", Type: domain.CredentialAPIKey, Scope: domain.CredentialScope{All: true}, Value: "sk-only-k3",
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	_, _, _, err = openConnections(t.Context(), store, keysEnv(masterKeyEntry("k2", 2)+","+masterKeyEntry("k1", 1)), log)
	if err == nil || !strings.Contains(err.Error(), "master key k3") || !strings.Contains(err.Error(), "credentials") ||
		strings.Contains(err.Error(), "sk-only-k3") {
		t.Errorf("with a credential sealed under k3: %v; want a refusal naming k3 and credentials", err)
	}
}
