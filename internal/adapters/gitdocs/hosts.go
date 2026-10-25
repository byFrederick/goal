package gitdocs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// github is GitHub's REST API: https://api.github.com for github.com, https://api.<host> for a
// GitHub Enterprise Cloud host (<name>.ghe.com) and https://<host>/api/v3 for a GitHub
// Enterprise Server, as the github code host adapter's. A branch's whole tree is one request.
type github struct{}

const githubDotCom = "github.com"

func (github) name() string { return "GitHub" }

func (github) api(host string) string {
	if host == githubDotCom || strings.HasSuffix(host, ".ghe.com") {
		return "https://api." + host
	}

	return "https://" + host + "/api/v3"
}

func (github) checkRepo(repo string) error {
	if s := strings.Split(repo, "/"); len(s) != 2 || !names(s) {
		return errors.New("a GitHub repository URL is https://<host>/<owner>/<name>")
	}

	return nil
}

func (github) authorize(h http.Header, token string) {
	h.Set("Authorization", "Bearer "+token)
	h.Set("X-GitHub-Api-Version", "2022-11-28")
}

func (github) repoURL(s source) string {
	owner, name, _ := strings.Cut(s.repo, "/")

	return s.api + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

func (g github) defaultBranch(ctx context.Context, r *Reader, s source) (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}

	if _, err := r.getJSON(ctx, s, g.repoURL(s), &repo); err != nil {
		return "", err
	}

	if repo.DefaultBranch == "" {
		return "", errNoDefaultBranch
	}

	return repo.DefaultBranch, nil
}

func (g github) files(ctx context.Context, r *Reader, s source, branch string, yield func(file) bool) error {
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}

	// The branch is one path segment, its slashes escaped.
	if _, err := r.getJSON(ctx, s, g.repoURL(s)+"/git/trees/"+url.PathEscape(branch)+"?recursive=1", &tree); err != nil {
		return err
	}

	// Files left out of a cut tree would have their documents deleted.
	if tree.Truncated {
		return errors.New("GitHub truncated the tree (over 100,000 entries or 7 MB): the repository is too large to list")
	}

	for _, e := range tree.Tree {
		if e.Type == "blob" && e.Mode != modeSymlink && !yield(file{path: e.Path, blob: e.SHA}) {
			return nil
		}
	}

	return nil
}

func (g github) raw(s source, p string) (u, accept string) {
	u = g.repoURL(s) + "/contents/" + segments(p)
	if s.branch != "" {
		u += "?" + url.Values{"ref": {s.branch}}.Encode()
	}

	return u, "application/vnd.github.raw+json"
}

func (github) webURL(s source, branch, p string) string {
	return s.web + "/blob/" + segments(branch) + "/" + segments(p)
}

// gitlab is GitLab's REST API v4 at https://<host>/api/v4 (gitlab.com or a self-managed GitLab),
// as the gitlab code host adapter's. A project is named by its full path, URL-encoded; its tree
// comes in pages of 100.
type gitlab struct{}

// gitlabPage is how many tree entries one request lists, GitLab's most.
const gitlabPage = 100

func (gitlab) name() string { return "GitLab" }

func (gitlab) api(host string) string { return "https://" + host + "/api/v4" }

func (gitlab) checkRepo(repo string) error {
	// GitLab's paths put a - segment between a project and its pages (/-/tree/main).
	if s := strings.Split(repo, "/"); len(s) < 2 || !names(s) || slices.Contains(s, "-") {
		return errors.New("a GitLab project URL is https://<host>/<namespace>/<project>")
	}

	return nil
}

func (gitlab) authorize(h http.Header, token string) { h.Set("PRIVATE-TOKEN", token) }

func (gitlab) projectURL(s source) string { return s.api + "/projects/" + url.PathEscape(s.repo) }

func (g gitlab) defaultBranch(ctx context.Context, r *Reader, s source) (string, error) {
	var project struct {
		DefaultBranch string `json:"default_branch"`
	}

	if _, err := r.getJSON(ctx, s, g.projectURL(s), &project); err != nil {
		return "", err
	}

	if project.DefaultBranch == "" {
		return "", errNoDefaultBranch
	}

	return project.DefaultBranch, nil
}

func (g gitlab) files(ctx context.Context, r *Reader, s source, branch string, yield func(file) bool) error {
	for page := 1; ; page++ {
		q := url.Values{"recursive": {"true"}, "ref": {branch}, "per_page": {strconv.Itoa(gitlabPage)}, "page": {strconv.Itoa(page)}}

		var entries []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
		}

		h, err := r.getJSON(ctx, s, g.projectURL(s)+"/repository/tree?"+q.Encode(), &entries)
		if err != nil {
			return err
		}

		for _, e := range entries {
			if e.Type == "blob" && e.Mode != modeSymlink && !yield(file{path: e.Path, blob: e.ID}) {
				return nil
			}
		}

		switch next := h.Get("X-Next-Page"); next {
		case "":
			return nil
		case strconv.Itoa(page + 1):
		default:
			return fmt.Errorf("GitLab's page after %d is %q", page, next)
		}
	}
}

func (g gitlab) raw(s source, p string) (u, accept string) {
	u = g.projectURL(s) + "/repository/files/" + url.PathEscape(p) + "/raw"
	if s.branch != "" {
		u += "?" + url.Values{"ref": {s.branch}}.Encode()
	}

	return u, ""
}

func (gitlab) webURL(s source, branch, p string) string {
	return s.web + "/-/blob/" + segments(branch) + "/" + segments(p)
}

// names reports whether every one of a repository path's segments is a name: not empty, . or ..
func names(segments []string) bool {
	for _, s := range segments {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}

	return true
}

// segments escapes each of p's segments, keeping its slashes.
func segments(p string) string {
	s := strings.Split(p, "/")
	for i := range s {
		s[i] = url.PathEscape(s[i])
	}

	return strings.Join(s, "/")
}
