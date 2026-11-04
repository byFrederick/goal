// Package senders is the catalog of how each webhook sender signs its deliveries (ADR 0040
// decision 12), apart from the tool templates of internal/catalog (ADR 0037 decision 15). Each
// entry (internal/catalog/senders/*.yaml) gives its scheme, the header that carries the signature,
// what is signed and how it is encoded, where the delivery id, the timestamp and the event are, and
// its event table. One Check, driven by the entry, serves every sender: adding one is a file and
// its tests, not code.
package senders

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The header every entry has.
const (
	apiVersion = "goal/v1"
	senderKind = "Sender"
)

// The schemes: an HMAC-SHA256 under the secret, or the secret itself as a shared value.
const (
	SchemeHMAC  = "hmac"
	SchemeToken = "token"
)

// What an hmac entry signs: the raw body, or Standard Webhooks' "<id>.<timestamp>.<body>".
const (
	SignedBody            = "body"
	SignedIDTimestampBody = "id.timestamp.body"
)

// The encodings of a signature (hex, base64) and of a secret (text: its bytes as they are; whsec:
// the base64 after whsec_).
const (
	EncodingHex    = "hex"
	EncodingBase64 = "base64"
	SecretText     = "text"
	SecretWhsec    = "whsec"
)

// settingsToken marks the entry whose header and body paths are the webhook's own
// (domain.TokenSettings).
const settingsToken = "token"

// whsecPrefix starts a Standard Webhooks secret.
const whsecPrefix = "whsec_"

//go:embed *.yaml
var files embed.FS

// Entry is one sender's catalog entry.
type Entry struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name  string `yaml:"name"`
		Title string `yaml:"title"`
	} `yaml:"metadata"`
	Spec Spec `yaml:"spec"`
}

// Spec is how the sender signs and names its deliveries.
type Spec struct {
	Scheme    string     `yaml:"scheme"`
	Signature Signature  `yaml:"signature"`
	Signed    string     `yaml:"signed"`
	Secret    string     `yaml:"secret"`
	ID        Place      `yaml:"id"`
	Timestamp *Timestamp `yaml:"timestamp"`
	Event     Place      `yaml:"event"`
	// Settings is "token" when the webhook's TokenSettings give the header and the body paths.
	Settings string `yaml:"settings"`
	// Events is the event table (ADR 0040 decision 14): each event's normalized fields, as dot
	// paths into its payload.
	Events map[string]map[string]string `yaml:"events"`
}

// Signature is the header that carries the signature or the token: its prefix, the separator
// between several values ("" for one), any of which passes, and an hmac signature's encoding.
type Signature struct {
	Header    string `yaml:"header"`
	Prefix    string `yaml:"prefix"`
	Separator string `yaml:"separator"`
	Encoding  string `yaml:"encoding"`
}

// Place is where a value is: a header, or the first of the body's dot paths that holds one.
type Place struct {
	Header string   `yaml:"header"`
	Path   string   `yaml:"path"`
	Paths  []string `yaml:"paths"`
}

// paths are p's body paths in order.
func (p Place) paths() []string {
	if p.Path != "" {
		return append([]string{p.Path}, p.Paths...)
	}

	return p.Paths
}

// Timestamp is the header of a signed Unix timestamp and how far from now it may be.
type Timestamp struct {
	Header    string        `yaml:"header"`
	Tolerance time.Duration `yaml:"tolerance"`
}

// Catalog is every sender's entry.
type Catalog struct {
	entries map[domain.WebhookSender]Entry
}

// Load reads the entries goald ships. An entry that does not parse, is not named after its file,
// breaks a rule of check, or a sender of domain.WebhookSenders without one fails them all.
func Load() (*Catalog, error) {
	names, err := fs.Glob(files, "*.yaml")
	if err != nil {
		return nil, err
	}

	c := &Catalog{entries: make(map[domain.WebhookSender]Entry, len(names))}

	for _, name := range names {
		data, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}

		e, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("sender catalog %s: %w", name, err)
		}

		if want := strings.TrimSuffix(path.Base(name), ".yaml"); e.Metadata.Name != want {
			return nil, fmt.Errorf("sender catalog %s is named %q", name, e.Metadata.Name)
		}

		c.entries[domain.WebhookSender(e.Metadata.Name)] = e
	}

	for _, s := range domain.WebhookSenders {
		if _, ok := c.entries[s]; !ok {
			return nil, fmt.Errorf("sender catalog: no entry for sender %s", s)
		}
	}

	return c, nil
}

// Parse reads one entry. A file with an unknown key, another apiVersion or kind, or an entry check
// refuses is an error wrapping domain.ErrInvalid.
func Parse(data []byte) (Entry, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var e Entry
	if err := dec.Decode(&e); err != nil && !errors.Is(err, io.EOF) {
		return Entry{}, fmt.Errorf("sender: %w: %w", err, domain.ErrInvalid)
	}

	if e.APIVersion != apiVersion || e.Kind != senderKind {
		return Entry{}, fmt.Errorf("sender %s: apiVersion %s and kind %s are required: %w", e.Metadata.Name, apiVersion, senderKind, domain.ErrInvalid)
	}

	if err := e.check(); err != nil {
		return Entry{}, fmt.Errorf("sender %s: %w: %w", e.Metadata.Name, err, domain.ErrInvalid)
	}

	return e, nil
}

// check refuses an entry Check could not follow.
func (e Entry) check() error {
	s := e.Spec

	switch {
	case !domain.WebhookSender(e.Metadata.Name).Valid():
		return fmt.Errorf("%q is not a sender", e.Metadata.Name)
	case s.Signature.Header == "":
		return errors.New("a signature header is required")
	case s.Settings != "" && s.Settings != settingsToken:
		return fmt.Errorf("settings %q must be token", s.Settings)
	case s.Timestamp != nil && (s.Timestamp.Header == "" || s.Timestamp.Tolerance <= 0):
		return errors.New("a timestamp needs a header and a tolerance")
	case s.Signed == SignedIDTimestampBody && (s.ID.Header == "" || s.Timestamp == nil):
		return errors.New("id.timestamp.body needs an id header and a timestamp")
	}

	return s.checkScheme()
}

// checkScheme refuses an unknown scheme, and what is signed and the encodings that do not fit it.
func (s Spec) checkScheme() error {
	switch s.Scheme {
	case SchemeHMAC:
		if s.Signed != SignedBody && s.Signed != SignedIDTimestampBody {
			return fmt.Errorf("signed %q must be body or id.timestamp.body", s.Signed)
		}

		if s.Signature.Encoding != EncodingHex && s.Signature.Encoding != EncodingBase64 {
			return fmt.Errorf("signature encoding %q must be hex or base64", s.Signature.Encoding)
		}

		if s.Secret != SecretText && s.Secret != SecretWhsec {
			return fmt.Errorf("secret %q must be text or whsec", s.Secret)
		}
	case SchemeToken:
		if s.Signed != "" || s.Signature.Encoding != "" || s.Secret != "" {
			return errors.New("a token scheme signs nothing: no signed, encoding or secret")
		}
	default:
		return fmt.Errorf("scheme %q must be hmac or token", s.Scheme)
	}

	return nil
}

// Entry is sender's entry; false when the catalog has none.
func (c *Catalog) Entry(sender domain.WebhookSender) (Entry, bool) {
	e, ok := c.entries[sender]

	return e, ok
}

// Check checks delivery d to a webhook of sender, with the token sender's settings token, against
// key: in constant time, against the raw body, with key's current value and then its previous one
// while PreviousUntil is after d.At, any of the header's values passing. Only once it passes does
// it read the body, for the delivery's id and event. A missing or wrong signature, token or
// timestamp wraps domain.ErrSignature and says which, never a value.
func (c *Catalog) Check(sender domain.WebhookSender, token domain.TokenSettings, d domain.InboundDelivery, key domain.SigningSecret,
) (domain.CheckedDelivery, error) {
	e, ok := c.entries[sender]
	if !ok {
		return domain.CheckedDelivery{}, fmt.Errorf("sender %q has no catalog entry", sender)
	}

	s := e.Spec.withSettings(token)

	if err := s.verify(d, keys(key, d.At)); err != nil {
		return domain.CheckedDelivery{}, err
	}

	var body *payload
	if len(s.ID.paths()) > 0 || len(s.Event.paths()) > 0 {
		body = parse(d.Body)
	}

	out := domain.CheckedDelivery{ID: s.ID.read(d, body), Event: s.Event.read(d, body)}

	switch {
	case out.ID == "":
		out.ID = hash(d.Body)
	case utf8.RuneCountInString(out.ID) > domain.MaxDeliveryID:
		out.ID = hash([]byte(out.ID))
	}

	if utf8.RuneCountInString(out.Event) > domain.MaxDeliveryID {
		out.Event = string([]rune(out.Event)[:domain.MaxDeliveryID])
	}

	return out, nil
}

// withSettings is s with the webhook's token settings, for the entry that takes them: a header of
// their own holds the value with no prefix.
func (s Spec) withSettings(t domain.TokenSettings) Spec {
	if s.Settings != settingsToken {
		return s
	}

	if t.Header != "" && !strings.EqualFold(t.Header, s.Signature.Header) {
		s.Signature = Signature{Header: t.Header}
	}

	s.ID = Place{Path: t.IDPath}
	s.Event = Place{Path: t.EventPath}

	return s
}

// keys are the values a signature may be made with at instant at: the current one, and the
// previous one until it lapses.
func keys(k domain.SigningSecret, at time.Time) []string {
	out := []string{k.Current}
	if k.Previous != "" && k.PreviousUntil != nil && at.Before(*k.PreviousUntil) {
		out = append(out, k.Previous)
	}

	return out
}

// verify passes when one of the signature header's values matches one of keys.
func (s Spec) verify(d domain.InboundDelivery, keys []string) error {
	values := s.values(d)
	if len(values) == 0 {
		return fmt.Errorf("no %s header with a value: %w", s.Signature.Header, domain.ErrSignature)
	}

	signed, err := s.signed(d)
	if err != nil {
		return err
	}

	for _, k := range keys {
		if s.matches(values, signed, k) {
			return nil
		}
	}

	return fmt.Errorf("%s does not match: %w", s.Signature.Header, domain.ErrSignature)
}

// signed is what d's signature is made over, once its timestamp, if the entry has one, is within
// the tolerance: the body, or "<id>.<timestamp>.<body>".
func (s Spec) signed(d domain.InboundDelivery) ([]byte, error) {
	if s.Timestamp == nil {
		return d.Body, nil
	}

	ts := first(d, s.Timestamp.Header)

	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("no %s header with a Unix time: %w", s.Timestamp.Header, domain.ErrSignature)
	}

	if off := d.At.Sub(time.Unix(sec, 0)).Abs(); off > s.Timestamp.Tolerance {
		return nil, fmt.Errorf("%s is more than %s off: %w", s.Timestamp.Header, s.Timestamp.Tolerance, domain.ErrSignature)
	}

	if s.Signed != SignedIDTimestampBody {
		return d.Body, nil
	}

	id := first(d, s.ID.Header)
	if id == "" {
		return nil, fmt.Errorf("no %s header: %w", s.ID.Header, domain.ErrSignature)
	}

	return append([]byte(id+"."+ts+"."), d.Body...), nil
}

// values are the signature header's values with their prefix cut, split at the separator; a value
// without the prefix is dropped.
func (s Spec) values(d domain.InboundDelivery) []string {
	var out []string

	for _, v := range d.Headers[strings.ToLower(s.Signature.Header)] {
		parts := []string{v}
		if s.Signature.Separator != "" {
			parts = strings.Split(v, s.Signature.Separator)
		}

		for _, p := range parts {
			p = strings.TrimSpace(p)
			if len(p) < len(s.Signature.Prefix) || !strings.EqualFold(p[:len(s.Signature.Prefix)], s.Signature.Prefix) {
				continue
			}

			if p = p[len(s.Signature.Prefix):]; p != "" {
				out = append(out, p)
			}
		}
	}

	return out
}

// matches reports whether one of values is key's token, or key's signature of signed; every
// value is compared, in constant time.
func (s Spec) matches(values []string, signed []byte, key string) bool {
	match := 0

	if s.Scheme == SchemeToken {
		for _, v := range values {
			match |= subtle.ConstantTimeCompare([]byte(v), []byte(key))
		}

		return match == 1
	}

	secret, ok := s.secret(key)
	if !ok {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(signed)
	want := mac.Sum(nil)

	for _, v := range values {
		got, err := s.decode(v)
		if err == nil && hmac.Equal(got, want) {
			match = 1
		}
	}

	return match == 1
}

// secret is key as the hmac's key: its bytes, or the base64 after whsec_.
func (s Spec) secret(key string) ([]byte, bool) {
	if s.Secret != SecretWhsec {
		return []byte(key), key != ""
	}

	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(key, whsecPrefix))

	return b, err == nil && len(b) > 0
}

// decode is a signature value's bytes.
func (s Spec) decode(v string) ([]byte, error) {
	if s.Signature.Encoding == EncodingBase64 {
		return base64.StdEncoding.DecodeString(v)
	}

	return hex.DecodeString(v)
}

// first is the first value of header name; "" for none.
func first(d domain.InboundDelivery, name string) string {
	if vs := d.Headers[strings.ToLower(name)]; len(vs) > 0 {
		return strings.TrimSpace(vs[0])
	}

	return ""
}

// payload is a body read as JSON; nil for one that is not.
type payload struct{ v any }

func parse(body []byte) *payload {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}

	return &payload{v: v}
}

// read is p's value in d: its header's, or the first of its body paths holding a string or a
// number; "" for none.
func (p Place) read(d domain.InboundDelivery, body *payload) string {
	if p.Header != "" {
		return first(d, p.Header)
	}

	if body == nil {
		return ""
	}

	for _, dotted := range p.paths() {
		if v := body.at(dotted); v != "" {
			return v
		}
	}

	return ""
}

// at is the string or number at the dot path; "" for none.
func (p *payload) at(dotted string) string {
	v := p.v

	for key := range strings.SplitSeq(dotted, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}

		v = m[key]
	}

	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

// hash is the hex SHA-256 of b: the id of a delivery that names none.
func hash(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}
