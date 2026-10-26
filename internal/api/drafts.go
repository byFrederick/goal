package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The draft routes (B §8.2). Every write carries a precondition, so a writer never overwrites
// what another tab or the CLI saved without knowing it: If-Match with the draft's revision
// (discard), the section's etag (a Visual save or revert) or the file's sha256 (a file write or
// delete), or If-None-Match: * to create a file. A write without one is 428; a stale one is 412
// with the current value.

func (s *server) GetDraft(ctx context.Context, req gen.GetDraftRequestObject) (gen.GetDraftResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	v, err := s.defs.Draft(ctx, rec.ID)
	if err != nil {
		return nil, err
	}

	return gen.GetDraft200JSONResponse{
		Body:    s.draftOut(v),
		Headers: gen.GetDraft200ResponseHeaders{ETag: quote(strconv.FormatInt(v.Draft.Revision, 10))},
	}, nil
}

func (s *server) OpenDraft(ctx context.Context, req gen.OpenDraftRequestObject) (gen.OpenDraftResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	var r authoring.OpenDraftRequest

	if b := req.Body; b != nil {
		if r.Base, err = selector(deref(b.Base)); err != nil {
			return nil, err
		}

		r.Replace = deref(b.Replace)
	}

	v, created, err := s.defs.OpenDraft(ctx, rec.ID, r)
	if err != nil {
		return nil, err
	}

	if created {
		return gen.OpenDraft201JSONResponse(s.draftOut(v)), nil
	}

	return gen.OpenDraft200JSONResponse(s.draftOut(v)), nil
}

func (s *server) DiscardDraft(ctx context.Context, req gen.DiscardDraftRequestObject) (gen.DiscardDraftResponseObject, error) {
	tag, err := ifMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}

	revision, err := strconv.ParseInt(tag, 10, 64)
	if err != nil || revision < 1 {
		return nil, fmt.Errorf("%w: If-Match %q is not a draft revision", errInvalid, tag)
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if err := s.defs.DiscardDraft(ctx, rec.ID, revision); err != nil {
		return nil, err
	}

	return gen.DiscardDraft204Response{}, nil
}

func (s *server) GetDraftModel(ctx context.Context, req gen.GetDraftModelRequestObject) (gen.GetDraftModelResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	m, err := s.defs.DraftModel(ctx, rec.ID)
	if err != nil {
		return nil, err
	}

	out, err := draftModelOut(m)
	if err != nil {
		return nil, err
	}

	return gen.GetDraftModel200JSONResponse(out), nil
}

func (s *server) SaveDraftSection(ctx context.Context, req gen.SaveDraftSectionRequestObject) (gen.SaveDraftSectionResponseObject, error) {
	tag, err := ifMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}

	b := req.Body
	if len(bytes.TrimSpace(b.Value)) == 0 {
		return nil, fmt.Errorf("%w: a section save needs the section's value", errInvalid)
	}

	files, err := filesOf(b.Files)
	if err != nil {
		return nil, err
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	res, err := s.defs.SaveSection(ctx, rec.ID, authoring.SectionSave{
		Section: authoring.Section(req.Section), IfMatch: tag, Value: b.Value, Files: files, RemoveFiles: deref(b.RemoveFiles),
	})
	if err != nil {
		return nil, err
	}

	return gen.SaveDraftSection200JSONResponse(saveResultOut(res)), nil
}

func (s *server) RevertDraftSection(ctx context.Context, req gen.RevertDraftSectionRequestObject) (gen.RevertDraftSectionResponseObject, error) {
	tag, err := ifMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	res, err := s.defs.RevertSection(ctx, rec.ID, authoring.Section(req.Section), tag, req.Body.Pointers)
	if err != nil {
		return nil, err
	}

	return gen.RevertDraftSection200JSONResponse(saveResultOut(res)), nil
}

func (s *server) ListDraftFiles(ctx context.Context, req gen.ListDraftFilesRequestObject) (gen.ListDraftFilesResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	files, err := s.defs.DraftFiles(ctx, rec.ID)
	if err != nil {
		return nil, err
	}

	return gen.ListDraftFiles200JSONResponse{Files: filesOut(files)}, nil
}

func (s *server) GetDraftFile(ctx context.Context, req gen.GetDraftFileRequestObject) (gen.GetDraftFileResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	f, err := s.defs.VersionFile(ctx, rec.ID, authoring.VersionSelector{Kind: authoring.SelectDraft}, req.Path)
	if err != nil {
		return nil, err
	}

	return gen.GetDraftFile200ApplicationoctetStreamResponse{
		Body: bytes.NewReader(f.Content), ContentLength: int64(len(f.Content)),
		Headers: gen.GetDraftFile200ResponseHeaders{ETag: quote(string(f.SHA256))},
	}, nil
}

func (s *server) PutDraftFile(ctx context.Context, req gen.PutDraftFileRequestObject) (gen.PutDraftFileResponseObject, error) {
	w, err := fileWrite(req.Params)
	if err != nil {
		return nil, err
	}

	if w.Content, err = readFile(req.Body); err != nil {
		return nil, err
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	w.Path = req.Path

	res, err := s.defs.PutFile(ctx, rec.ID, w)
	if err != nil {
		return nil, err
	}

	return gen.PutDraftFile200JSONResponse(saveResultOut(res)), nil
}

// fileWrite reads a file write's precondition: If-Match with the hash it is based on, or
// If-None-Match: * to create the file. Exactly one of them.
func fileWrite(p gen.PutDraftFileParams) (authoring.FileWrite, error) {
	switch {
	case p.IfMatch != nil && p.IfNoneMatch != nil:
		return authoring.FileWrite{}, fmt.Errorf("%w: send If-Match or If-None-Match, not both", errInvalid)
	case p.IfNoneMatch != nil:
		if strings.TrimSpace(*p.IfNoneMatch) != "*" {
			return authoring.FileWrite{}, fmt.Errorf("%w: If-None-Match must be *", errInvalid)
		}

		return authoring.FileWrite{Create: true}, nil
	default:
		tag, err := ifMatch(p.IfMatch)

		return authoring.FileWrite{IfMatch: domain.BlobHash(tag)}, err
	}
}

func (s *server) DeleteDraftFile(ctx context.Context, req gen.DeleteDraftFileRequestObject) (gen.DeleteDraftFileResponseObject, error) {
	tag, err := ifMatch(req.Params.IfMatch)
	if err != nil {
		return nil, err
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	res, err := s.defs.DeleteFile(ctx, rec.ID, req.Path, domain.BlobHash(tag))
	if err != nil {
		return nil, err
	}

	return gen.DeleteDraftFile200JSONResponse(saveResultOut(res)), nil
}

func (s *server) ReplaceDraftTree(ctx context.Context, req gen.ReplaceDraftTreeRequestObject) (gen.ReplaceDraftTreeResponseObject, error) {
	files, err := filesOf(&req.Body.Files)
	if err != nil {
		return nil, err
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	res, err := s.defs.ReplaceTree(ctx, rec.ID, files, req.Body.ExpectedRevision)
	if err != nil {
		return nil, err
	}

	return gen.ReplaceDraftTree200JSONResponse(saveResultOut(res)), nil
}

func (s *server) ValidateDraft(ctx context.Context, req gen.ValidateDraftRequestObject) (gen.ValidateDraftResponseObject, error) {
	var r authoring.ValidateRequest

	if b := req.Body; b != nil {
		overlay, err := filesOf(b.Overlay)
		if err != nil {
			return nil, err
		}

		r = authoring.ValidateRequest{Overlay: overlay, StationMoves: deref(b.StationMoves)}
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	v, err := s.defs.ValidateDraft(ctx, rec.ID, r)
	if err != nil {
		return nil, err
	}

	return gen.ValidateDraft200JSONResponse(validationOut(v)), nil
}

func (s *server) PublishDraft(ctx context.Context, req gen.PublishDraftRequestObject) (gen.PublishDraftResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	b := req.Body

	v, err := s.defs.Publish(ctx, rec.ID, authoring.PublishRequest{
		Revision: b.Revision, ExpectedCurrent: domain.DefinitionVersionID(deref(b.ExpectedCurrentVersionId)),
		Message: deref(b.Message), StationMoves: deref(b.StationMoves), Start: b.Start,
	})
	if err != nil {
		return nil, err
	}

	// The new version as the history lists it, with its counts.
	published, err := s.defs.Version(ctx, rec.ID, authoring.VersionSelector{Kind: authoring.SelectNumber, Number: v.Number})
	if err != nil {
		return nil, fmt.Errorf("read published version %d: %w", v.Number, err)
	}

	return gen.PublishDraft201JSONResponse(s.versionOut(published.VersionInfo)), nil
}

// ifMatch reads a write's If-Match: one strong entity tag, quoted. Without one the write is
// refused (428): authoring would take it to overwrite whatever is there.
func ifMatch(h *string) (string, error) {
	if h == nil || strings.TrimSpace(*h) == "" {
		return "", errPreconditionRequired
	}

	v := strings.TrimSpace(*h)

	tag, ok := strings.CutPrefix(v, `"`)
	if ok {
		tag, ok = strings.CutSuffix(tag, `"`)
	}

	if !ok || tag == "" || strings.ContainsAny(tag, `",`) {
		return "", fmt.Errorf("%w: If-Match %q is not one quoted entity tag", errInvalid, v)
	}

	return tag, nil
}

// quote makes v an entity tag.
func quote(v string) string { return `"` + v + `"` }

// selector reads a version selector; "" is the zero selector (the default of the call).
func selector(v string) (authoring.VersionSelector, error) {
	if v == "" {
		return authoring.VersionSelector{}, nil
	}

	return authoring.ParseVersion(v)
}

// filesOf reads a file map: each file's content is exactly one of text and base64.
func filesOf(m *gen.FileMap) (map[string][]byte, error) {
	if m == nil {
		return nil, nil
	}

	out := make(map[string][]byte, len(*m))

	for path, c := range *m {
		b, err := fileContent(c)
		if err != nil {
			return nil, fmt.Errorf("%w: file %q: %w", errInvalid, path, err)
		}

		out[path] = b
	}

	return out, nil
}

// fileContent reads one file's content: a TextFile ({text}) or a Base64File ({base64}), nothing
// else beside.
func fileContent(c gen.FileContent) ([]byte, error) {
	raw, err := c.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, errors.New("a file is an object")
	}

	_, isText := members["text"]
	_, isBase64 := members["base64"]

	switch {
	case len(members) == 1 && isText:
		t, err := c.AsTextFile()

		return []byte(t.Text), err
	case len(members) == 1 && isBase64:
		b, err := c.AsBase64File()

		return b.Base64, err
	default:
		return nil, errors.New("a file is exactly one of text and base64")
	}
}
