package api

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The definitions routes' response shapes, from authoring's results.

func actorOut(a domain.Actor) gen.Actor {
	return gen.Actor{Kind: gen.ActorKind(a.Kind), Id: a.ID, Name: a.Name}
}

// publishedRef names a published version (a factory's current one); nil for none.
func publishedRef(r *ports.VersionRef) *gen.VersionRef {
	if r == nil {
		return nil
	}

	return &gen.VersionRef{Id: string(r.ID), Number: r.Number, PublishedAt: r.At, PublishedBy: actorOut(r.By)}
}

// draftRef names an open draft; nil for none.
func draftRef(r *ports.VersionRef) *gen.DraftRef {
	if r == nil {
		return nil
	}

	return &gen.DraftRef{Id: string(r.ID), Revision: r.Revision, UpdatedAt: r.At, UpdatedBy: actorOut(r.By)}
}

// versionRef names a published version; nil for none.
func versionRef(v *domain.DefinitionVersion) *gen.VersionRef {
	if v == nil {
		return nil
	}

	return &gen.VersionRef{Id: string(v.ID), Number: v.Number, PublishedAt: v.PublishedAt, PublishedBy: actorOut(v.PublishedBy)}
}

func sourceOut(s domain.VersionSource) gen.VersionSource {
	return gen.VersionSource{
		Kind: gen.VersionSourceKind(s.Kind), Template: optional(s.Template), Factory: optional(string(s.Factory)),
		Version: optional(string(s.Version)),
	}
}

func draftOut(v authoring.DraftView) gen.Draft {
	d := v.Draft

	return gen.Draft{
		Id: string(d.ID), Revision: d.Revision, NextNumber: v.NextNumber,
		CreatedAt: d.CreatedAt, CreatedBy: actorOut(d.CreatedBy), UpdatedAt: d.UpdatedAt, UpdatedBy: actorOut(d.UpdatedBy),
		Base: versionRef(v.Base), Current: publishedRef(v.Current), Source: sourceOut(d.Source),
		ChangeCount: v.ChangeCount, ProblemCount: v.ProblemCount, Sections: sectionsOut(v.Sections),
	}
}

func sectionsOut(sections []authoring.SectionSummary) []gen.SectionSummary {
	out := make([]gen.SectionSummary, 0, len(sections))
	for _, s := range sections {
		out = append(out, gen.SectionSummary{
			Section: gen.Section(s.Section), Changes: s.Changes, Problems: s.Problems, Lines: lineRange(s.Lines),
		})
	}

	return out
}

func lineRange(r [2]int) gen.LineRange { return gen.LineRange{r[0], r[1]} }

func saveResultOut(r authoring.SaveResult) gen.SaveResult {
	return gen.SaveResult{
		Revision: r.Revision, Etag: optional(r.ETag), Sha256: optional(string(r.SHA256)), UpdatedAt: r.UpdatedAt,
		ChangeCount: r.ChangeCount, ProblemCount: r.ProblemCount, Sections: sectionsOut(r.Sections),
	}
}

func modelOut(m authoring.Model) (gen.FactoryModel, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode factory model: %w", err)
	}

	return b, nil
}

func rangesOut(r authoring.Ranges) gen.Ranges {
	out := make(gen.Ranges, len(r))
	for p, lines := range r {
		out[p] = lineRange(lines)
	}

	return out
}

func draftModelOut(m authoring.DraftModel) (gen.DraftModel, error) {
	model, err := modelOut(m.Model)
	if err != nil {
		return gen.DraftModel{}, err
	}

	return gen.DraftModel{
		Revision: m.Revision, Model: model, Edited: m.Edited, Ranges: rangesOut(m.Ranges),
		Etags: gen.SectionETags{
			Basics: m.ETags[authoring.Basics], Line: m.ETags[authoring.Line], Agents: m.ETags[authoring.Agents],
			Connections: m.ETags[authoring.Connections], Review: m.ETags[authoring.Review],
		},
	}, nil
}

func countsOut(c authoring.Counts) gen.DefinitionCounts {
	return gen.DefinitionCounts{
		Stations: c.Stations, Agents: c.Agents, Skills: c.Skills, Connections: c.Connections, Contracts: c.Contracts,
		Changes: c.Changes,
	}
}

func validationOut(v authoring.Validation) gen.Validation {
	out := gen.Validation{
		Ok: v.OK, Revision: v.Revision, Counts: countsOut(v.Counts), Checks: make([]gen.CheckResult, 0, len(v.Checks)),
		Problems: problemsOut(v.Problems),
	}

	for _, c := range v.Checks {
		out.Checks = append(out.Checks, gen.CheckResult{
			Id: c.ID, Section: c.Section, Status: gen.CheckResultStatus(c.Status), Summary: c.Summary,
		})
	}

	return out
}

func problemsOut(problems []domain.DefinitionProblem) []gen.DefinitionProblem {
	out := make([]gen.DefinitionProblem, 0, len(problems))

	for _, p := range problems {
		fixes := make([]gen.ProblemFix, 0, len(p.Fixes))
		for _, f := range p.Fixes {
			fixes = append(fixes, gen.ProblemFix{Label: f.Label, Section: f.Section, Patch: patchOut(f.Patch)})
		}

		out = append(out, gen.DefinitionProblem{
			Code: p.Code, Severity: gen.DefinitionProblemSeverity(p.Severity), Check: p.Check, Section: p.Section,
			Pointer: optional(p.Pointer), File: optional(p.File), Line: p.Line, Column: p.Column, EndLine: p.EndLine,
			EndColumn: p.EndColumn, Message: p.Message, Fixes: fixes,
		})
	}

	return out
}

func patchOut(ops []domain.PatchOp) []gen.PatchOp {
	out := make([]gen.PatchOp, 0, len(ops))

	for _, op := range ops {
		o := gen.PatchOp{Op: gen.PatchOpOp(op.Op), Path: op.Path}
		if len(op.Value) > 0 {
			v := slices.Clone(op.Value)
			o.Value = &v
		}

		out = append(out, o)
	}

	return out
}

func itemsAtOut(items []ports.ItemAt) []gen.ItemAt {
	out := make([]gen.ItemAt, 0, len(items))
	for _, it := range items {
		out = append(out, gen.ItemAt{Id: string(it.ID), Number: it.Number, Station: it.Station, State: gen.ItemState(it.State)})
	}

	return out
}

func filesOut(files []authoring.FileInfo) []gen.FileInfo {
	out := make([]gen.FileInfo, 0, len(files))
	for _, f := range files {
		out = append(out, gen.FileInfo{Path: f.Path, Sha256: string(f.SHA256), Size: f.Size})
	}

	return out
}

func versionOut(v authoring.VersionInfo) gen.Version {
	out := gen.Version{
		Id: string(v.ID), Revision: v.Revision, State: gen.VersionState(v.State), Message: v.Message,
		Source: sourceOut(v.Source), Current: v.Current, Counts: countsOut(v.Counts),
	}

	if v.State == domain.VersionPublished {
		number, at, by := v.Number, v.PublishedAt, actorOut(v.PublishedBy)
		out.Number, out.PublishedAt, out.PublishedBy = &number, &at, &by
	}

	return out
}

func versionDetailOut(v authoring.VersionDetail) gen.VersionDetail {
	o := versionOut(v.VersionInfo)

	return gen.VersionDetail{
		Id: o.Id, Number: o.Number, Revision: o.Revision, State: o.State, Message: o.Message, Source: o.Source,
		PublishedAt: o.PublishedAt, PublishedBy: o.PublishedBy, Current: o.Current, Counts: o.Counts, Files: filesOut(v.Files),
	}
}

// compareSide is one side of a comparison: the selector asked for, and the version it named
// (nil for a starting tree no version holds).
func compareSide(selector string, v *domain.DefinitionVersion) gen.CompareSide {
	out := gen.CompareSide{Selector: selector}
	if v == nil {
		return out
	}

	id, state := string(v.ID), gen.VersionState(v.State)
	out.Id, out.State = &id, &state

	if v.State == domain.VersionPublished {
		n := v.Number
		out.Number = &n
	}

	return out
}

func comparisonOut(c authoring.CompareResult, from, to string) gen.Comparison {
	out := gen.Comparison{
		From: compareSide(from, c.From), To: compareSide(to, c.To),
		Files: make([]gen.FileChange, 0, len(c.Files)), Changes: make([]gen.Change, 0, len(c.Changes)),
	}
	out.Counts.Changes, out.Counts.Sections = c.Counts.Changes, c.Counts.Sections

	for _, f := range c.Files {
		out.Files = append(out.Files, gen.FileChange{
			Path: f.Path, Op: gen.FileChangeOp(f.Op), FromSha256: optional(string(f.From)), ToSha256: optional(string(f.To)),
		})
	}

	for _, ch := range c.Changes {
		o := gen.Change{
			Section: ch.Section, Op: gen.ChangeOp(ch.Op), Field: optional(ch.Field), From: ch.From, To: ch.To,
			Summary: ch.Summary, FromLines: lineRange(ch.FromLines), ToLines: lineRange(ch.ToLines),
		}
		o.Target.Kind, o.Target.Id = gen.ChangeTargetKind(ch.Target.Kind), ch.Target.ID

		out.Changes = append(out.Changes, o)
	}

	return out
}

func templateSummaryOut(t authoring.TemplateSummary) gen.TemplateSummary {
	connectors := t.Connectors
	if connectors == nil {
		connectors = []string{}
	}

	return gen.TemplateSummary{
		Name: t.Name, Version: t.Version, Title: t.Title, Summary: t.Summary, Stations: t.Stations, Agents: t.Agents,
		Connectors: connectors, Changed: t.Changed,
	}
}

func templateOut(t authoring.Template) (gen.Template, error) {
	model, err := modelOut(t.Model)
	if err != nil {
		return gen.Template{}, err
	}

	s := templateSummaryOut(t.TemplateSummary)

	return gen.Template{
		Name: s.Name, Version: s.Version, Title: s.Title, Summary: s.Summary, Stations: s.Stations, Agents: s.Agents,
		Connectors: s.Connectors, Changed: s.Changed, Description: t.Description, Model: model,
	}, nil
}
