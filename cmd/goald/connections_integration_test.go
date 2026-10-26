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

// Spec 4 §11.2 row 12: goald refuses to start while a connection is sealed under a master key id
// it does not hold, missing key included; it starts with that key among others, and without any
// key while nothing is sealed.
func TestGoald_RefusesUnknownMasterKeyID(t *testing.T) {
	t.Parallel()

	db := newDatabase(t)
	store := postgres.New(db.pool)
	log := slog.New(slog.DiscardHandler)
	env := func(keys string) func(string) string {
		return func(name string) string {
			if name == masterkey.KeysEnv {
				return keys
			}

			return ""
		}
	}

	if _, err := openConnections(t.Context(), store, env(""), log); err != nil {
		t.Fatalf("without a key and nothing sealed: %v", err)
	}

	conns, err := openConnections(t.Context(), store, env(masterKeyEntry("k1", 1)), log)
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
		_, err := openConnections(t.Context(), store, env(keys), log)
		if err == nil || !strings.Contains(err.Error(), "master key k1") || strings.Contains(err.Error(), "ghp_x") {
			t.Errorf("with %s: %v; want a refusal naming k1", name, err)
		}
	}

	if _, err := openConnections(t.Context(), store, env(masterKeyEntry("k2", 2)+","+masterKeyEntry("k1", 1)), log); err != nil {
		t.Errorf("with the key among others: %v", err)
	}
}
