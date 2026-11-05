package domain

import (
	"bytes"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
)

// The normalized fields filters read (TriggerFilter.Needs) and that a sender's event table maps
// to dot paths into its payload (ADR 0040 decision 14). An event table may give any other field,
// such as check.name, for placeholders and only.dedupe.
const (
	FieldBranch     = "branch"
	FieldRepo       = "repo"
	FieldLabel      = "label"
	FieldAction     = "action"
	FieldConclusion = "conclusion"
	// FieldAuthor is the sender's login, IntakeEvent.Author; FieldAuthorType its account type,
	// "Bot" for a bot.
	FieldAuthor     = "author"
	FieldAuthorType = "author.type"
	// FieldPaths are the files the event changed, IntakeEvent.Paths: every value of every path.
	FieldPaths = "paths"
)

// EventField is where one normalized field of an event is in its payload: the first of Paths
// that holds a value, with TrimPrefix cut from its start (refs/heads/ from a branch). A path
// through a list reads each of its elements.
type EventField struct {
	Paths      []string
	TrimPrefix string
}

// EventTable is one event's normalized fields, by name, as a sender's catalog entry gives them.
type EventTable map[string]EventField

// Supplies reports whether the event gives field.
func (tb EventTable) Supplies(field string) bool {
	_, ok := tb[field]

	return ok
}

// Unsupplied are the fields of needs the event does not give, in order.
func (tb EventTable) Unsupplied(needs []string) []string {
	var out []string

	for _, n := range needs {
		if !tb.Supplies(n) {
			out = append(out, n)
		}
	}

	return out
}

// ParsePayload decodes body as JSON with numbers kept as text; nil when it is not JSON.
func ParsePayload(body []byte) any {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}

	return v
}

// maxPayloadValues is the most values PayloadValues returns, so a long list costs little.
const maxPayloadValues = 1000

// PayloadValues are the values at the dot path in payload (ParsePayload's), as text: strings,
// numbers and booleans, each element of a list at the end, and across every element of a list on
// the way; nil for none. At most maxPayloadValues.
func PayloadValues(payload any, path string) []string {
	if payload == nil || path == "" {
		return nil
	}

	vs := []any{payload}

	for key := range strings.SplitSeq(path, ".") {
		var next []any

		for _, v := range vs {
			next = append(next, child(v, key)...)
		}

		vs = next
	}

	var out []string

	for _, v := range vs {
		for _, x := range flatten(v) {
			if len(out) == maxPayloadValues {
				return out
			}

			out = append(out, x)
		}
	}

	return out
}

// child is v's key: a map's value, or that of each map in a list.
func child(v any, key string) []any {
	switch x := v.(type) {
	case map[string]any:
		if c, ok := x[key]; ok {
			return []any{c}
		}
	case []any:
		var out []any
		for _, e := range x {
			out = append(out, child(e, key)...)
		}

		return out
	}

	return nil
}

// flatten is v as text: a scalar's, or each scalar of a list.
func flatten(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case json.Number:
		return []string{x.String()}
	case bool:
		if x {
			return []string{"true"}
		}

		return []string{"false"}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, flatten(e)...)
		}

		return out
	}

	return nil
}

// Normalized are an event's normalized fields read from its payload by its table: Fields by name
// (FieldPaths aside), the changed Paths, and the Author with whether it is a Bot (its type is Bot,
// or its login ends in [bot]).
type Normalized struct {
	Fields map[string]string
	Paths  []string
	Author string
	Bot    bool
}

// Normalize reads tb's fields from payload.
func Normalize(tb EventTable, payload any) Normalized {
	out := Normalized{Fields: map[string]string{}}

	for name, f := range tb {
		if name == FieldPaths {
			for _, p := range f.Paths {
				for _, v := range PayloadValues(payload, p) {
					if !slices.Contains(out.Paths, v) {
						out.Paths = append(out.Paths, v)
					}
				}
			}

			continue
		}

		for _, p := range f.Paths {
			if vs := PayloadValues(payload, p); len(vs) > 0 && vs[0] != "" {
				out.Fields[name] = strings.TrimPrefix(vs[0], f.TrimPrefix)

				break
			}
		}
	}

	out.Author = out.Fields[FieldAuthor]
	out.Bot = strings.EqualFold(out.Fields[FieldAuthorType], "bot") || strings.HasSuffix(out.Author, "[bot]")

	return out
}

// NewDeliveryEvent is the event a delivery of event, with the sender's id deliveryID and body, to
// webhook w makes for factory: its key <sender>.<event>, its normalized fields by tb, its payload
// and an idempotency key of the webhook and the delivery id, so a trigger fires once per delivery.
func NewDeliveryEvent(factory FactoryID, w WebhookEndpoint, deliveryID, event string, body []byte, tb EventTable) IntakeEvent {
	payload := ParsePayload(body)
	n := Normalize(tb, payload)

	source := TriggerWebhook
	if w.Sender == SenderGitHub {
		source = TriggerGitHub
	}

	return IntakeEvent{
		FactoryID: factory, Source: source, Key: string(w.Sender) + "." + event, Webhook: w.Name, Sender: w.Sender, Event: event,
		Fields: n.Fields, Payload: payload, Paths: n.Paths, Author: n.Author, Bot: n.Bot,
		IdempotencyKey: string(w.ID) + "/" + deliveryID,
	}
}

// Admits reports whether a delivery from repository repo (owner/name, "" for none) reaches the
// opening's factory: every one does when it lists no repositories, otherwise only one it lists,
// in any case.
func (o WebhookOpening) Admits(repo string) bool {
	return len(o.Repositories) == 0 ||
		repo != "" && slices.ContainsFunc(o.Repositories, func(r string) bool { return strings.EqualFold(r, repo) })
}

// RepositoryName is the owner/name of l's runtime repository, from its URL's path without .git;
// "" when it has none.
func (l Line) RepositoryName() string {
	u, err := url.Parse(l.Runtime.Repository.URL)
	if err != nil || u.Host == "" {
		return ""
	}

	return strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
}

// HasRepository reports whether a delivery from repo belongs to l (ADR 0041 decision 13): repo is
// "" (the event names none), l has no repository, or it is l's, in any case.
func (l Line) HasRepository(repo string) bool {
	own := l.RepositoryName()

	return repo == "" || own == "" || strings.EqualFold(repo, own)
}
