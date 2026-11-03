package knowledge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// checkTimeout bounds one Check's listing.
const checkTimeout = 30 * time.Second

// Check lists src, a source whose settings are not saved yet, as its sync would: with its kind's
// reader and its secret opened in goald, refused when the secret is open to some factories only
// (secretReader). It stores nothing and counts the items listed, at most domain.MaxSourceFiles+1.
// A source it cannot read answers with the reason, never holding the secret's value; the error is
// for ctx ending only. An upload source not saved yet lists nothing.
func (in *Ingestor) Check(ctx context.Context, src domain.KnowledgeSource) (domain.SourceCheck, error) {
	if src.Kind == domain.SourceUpload && src.ID == "" {
		return domain.SourceCheck{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	spec, reader, err := in.reader(ctx, src)
	if err != nil {
		return in.checkFailed(ctx, spec, err)
	}

	var out domain.SourceCheck

	for _, err := range reader.List(ctx, spec) {
		if err != nil {
			return in.checkFailed(ctx, spec, err)
		}

		if out.Files++; out.Files > domain.MaxSourceFiles {
			break
		}
	}

	return out, nil
}

// checkFailed is a Check that could not list its source: err's message, with the opened secret's
// value cut out, or ctx's own error once it ended for another reason than the check's bound.
func (in *Ingestor) checkFailed(ctx context.Context, spec domain.SyncSpec, err error) (domain.SourceCheck, error) {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.DeadlineExceeded) {
		return domain.SourceCheck{}, cause
	}

	msg := err.Error()
	if spec.Secret != nil && spec.Secret.Value != "" {
		msg = strings.ReplaceAll(msg, spec.Secret.Value, "[secret]")
	}

	in.cfg.Log.InfoContext(ctx, "knowledge source checked", "source", spec.Source.Name, "ok", false)

	return domain.SourceCheck{Error: msg}, nil
}
