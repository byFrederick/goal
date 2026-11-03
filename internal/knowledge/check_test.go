package knowledge_test

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/knowledge"
)

// The secret the Check tests read with, and its value, which no answer may hold.
const (
	checkToken = "docs-token"
	checkValue = "test-token"
)

// leakyReader fails every listing with an error that quotes the secret it was given, as a code
// host's answer might.
type leakyReader struct{}

func (leakyReader) List(_ context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	return func(yield func(domain.SourceItem, error) bool) {
		yield(domain.SourceItem{}, errors.New("401: bad credentials "+s.Secret.Value))
	}
}

func (leakyReader) Fetch(context.Context, domain.SyncSpec, string) (domain.SourceDoc, error) {
	return domain.SourceDoc{}, errors.New("not listed")
}

// A Check lists a source that is not saved with its settings, opening its secret as a sync would,
// counts what it lists and stores nothing; an error says why, without the secret's value.
func TestCheck_ListsUnsavedSettings(t *testing.T) {
	t.Parallel()

	w := repoWorld(t)
	ctx := w.ctx()

	for _, s := range []domain.NewSecret{
		{Name: checkToken, Value: checkValue, Scope: domain.SecretScope{All: true}},
		{Name: "narrow-token", Value: "test-narrow", Scope: domain.SecretScope{Factories: []domain.FactoryID{"f1"}}},
	} {
		if _, err := w.secrets.Create(ctx, s, admin); err != nil {
			t.Fatal(err)
		}
	}

	w.docs.set(map[string]string{deployMD: shipOnTuesdays, "b.md": "# B"})

	repo := domain.KnowledgeSource{Name: "guides", Kind: domain.SourceRepository, Secret: checkToken, Config: map[string]string{"repository": "https://git.example.com/acme/guides"}}

	got, err := w.ingestor(nil, nil).Check(ctx, repo)
	w.want("the check", got, err == nil, got.Error == "", got.Files == 2, w.docs.secret == checkValue)
	w.want("the sources after", w.snapshots(repoSource), len(w.snapshots(repoSource)) == 0)

	if _, err := w.kb.Source(ctx, "guides"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("the checked source = %v, want none stored", err)
	}

	narrow := repo
	narrow.Secret = "narrow-token"

	got, err = w.ingestor(nil, nil).Check(ctx, narrow)
	w.want("a secret open to some factories only", got, err == nil, strings.Contains(got.Error, "open to some factories only"), got.Files == 0)

	leaky := knowledge.NewIngestor(knowledge.IngestorConfig{
		Knowledge: w.kb, Secrets: w.secrets, Uploads: w.uploads, Readers: map[string]ports.SourceReader{gitdocsSync: leakyReader{}}, Clock: w.clock,
	})

	got, err = leaky.Check(ctx, repo)
	w.want("a listing that fails", got, err == nil, strings.Contains(got.Error, "bad credentials"), !strings.Contains(got.Error, checkValue))

	got, err = w.ingestor(nil, nil).Check(ctx, domain.KnowledgeSource{Name: "drops", Kind: domain.SourceUpload})
	w.want("an upload source not saved", got, err == nil, got.Error == "", got.Files == 0)
}
