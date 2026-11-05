package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ErrCodeHostUnlisted means a repository on a host that is not one of the organization's code
// hosts (ListedCodeHosts): goald sends it no token (ADR 0041, decision 1). It wraps ErrInvalid.
var ErrCodeHostUnlisted = fmt.Errorf("not one of the organization's code hosts: %w", ErrInvalid)

// CodeRepository is one repository on a listed code host, what ports.CodeHost works on (ADR 0041,
// decision 7). Owner is the repository's owner path as its URL spells it: an account or
// organization on GitHub, a group and its subgroups (slash-separated) on GitLab. Build one with
// ParseCodeRepository, which looks its host up in the organization's list.
type CodeRepository struct {
	Host        CodeHost
	Owner, Name string
}

// Key is r's normalized host/owner/name, in lower case.
func (r CodeRepository) Key() string {
	return strings.ToLower(r.Host.Host + "/" + r.Owner + "/" + r.Name)
}

// URL is r's https URL, without .git.
func (r CodeRepository) URL() string { return "https://" + r.Host.Host + "/" + r.Owner + "/" + r.Name }

// String is r's key, for errors and logs.
func (r CodeRepository) String() string { return r.Key() }

// ListedCodeHosts are the organization's code hosts: the built-in ones, then added, the hosts it
// stored (ports.CodeHosts).
func ListedCodeHosts(added []CodeHost) []CodeHost { return append(BuiltInCodeHosts(), added...) }

// pathSegment is one segment of a repository's path on either kind of host.
var pathSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ParseCodeRepository is the repository raw names on one of hosts (ListedCodeHosts): an https URL
// of the repository (https://<host>/<owner>/<name>, .git and a trailing slash optional), or of a
// pull request in it on GitHub (/<owner>/<name>/pull/<n>) or a merge request on GitLab
// (/<group>/.../<name>/-/merge_requests/<n>). The host is looked up with ListedCodeHost, so one
// with a port is never listed; an unlisted host wraps ErrCodeHostUnlisted and names the host, and
// a URL that names no repository wraps ErrInvalid. Errors never show the URL's credentials.
func ParseCodeRepository(hosts []CodeHost, raw string) (CodeRepository, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return CodeRepository{}, fmt.Errorf("%q is not a repository URL: %w", redactedURL(raw), ErrInvalid)
	}

	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return CodeRepository{}, fmt.Errorf("%s is not a repository's https URL without credentials, such as https://github.com/acme/app: %w",
			u.Redacted(), ErrInvalid)
	}

	host, ok := ListedCodeHost(hosts, u.Host)
	if !ok {
		return CodeRepository{}, fmt.Errorf("%s is %w, so goald sends it no token: an owner can add it under Code hosts in the organization's settings",
			strings.ToLower(u.Host), ErrCodeHostUnlisted)
	}

	segs, ok := repositorySegments(host.Kind, strings.Split(strings.Trim(u.Path, "/"), "/"))
	if !ok || slices.ContainsFunc(segs, func(s string) bool { return !pathSegment.MatchString(s) }) {
		return CodeRepository{}, fmt.Errorf("%s names no repository of %s, such as https://%s/acme/app: %w", u.Redacted(), host.Host, host.Host, ErrInvalid)
	}

	return CodeRepository{Host: host, Owner: strings.Join(segs[:len(segs)-1], "/"), Name: segs[len(segs)-1]}, nil
}

// repositorySegments are the owner and name segments of a repository path on a host of kind, or
// of a pull request or merge request path in it, and false for any other path.
func repositorySegments(kind CodeHostKind, segs []string) ([]string, bool) {
	number := func(s string) bool {
		n, err := strconv.Atoi(s)
		return err == nil && n > 0 && s[0] != '0'
	}

	if kind == CodeHostGitLab {
		if i := slices.Index(segs, "-"); i >= 0 {
			if i < 2 || len(segs) != i+3 || segs[i+1] != "merge_requests" || !number(segs[i+2]) {
				return nil, false
			}

			return segs[:i], true
		}

		if len(segs) < 2 {
			return nil, false
		}
	} else {
		switch {
		case len(segs) == 4 && segs[2] == "pull" && number(segs[3]):
			return segs[:2], true
		case len(segs) != 2:
			return nil, false
		}
	}

	segs = slices.Clone(segs)
	segs[len(segs)-1] = strings.TrimSuffix(segs[len(segs)-1], ".git")

	return segs, segs[len(segs)-1] != ""
}

// redactedURL is raw for an error when it does not parse: cut at its last @, which may end
// credentials.
func redactedURL(raw string) string {
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		return "…" + raw[i+1:]
	}

	return raw
}
