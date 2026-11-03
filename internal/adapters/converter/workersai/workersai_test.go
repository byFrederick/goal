package workersai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/adapters/converter/workersai"
	"github.com/byFrederick/goal/internal/core/domain"
)

func TestConvert_SendsTheFileAndReturnsItsMarkdown(t *testing.T) {
	t.Parallel()

	var path, auth, part, filename string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")

		mr, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}

		p, err := mr.NextPart()
		if err != nil {
			t.Error(err)
			return
		}

		b, _ := io.ReadAll(p)
		part, filename = string(b), p.FileName()

		_, _ = io.WriteString(w, `{"success":true,"result":[{"name":"leave.pdf","format":"markdown","data":"# Leave\n\n25 days"}]}`)
	}))
	defer srv.Close()

	c, err := workersai.New(workersai.Config{URL: srv.URL, Key: "test-key"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.Convert(context.Background(), "leave.pdf", "application/pdf", []byte("%PDF-1.7 test"))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if string(got) != "# Leave\n\n25 days" {
		t.Errorf("Markdown = %q", got)
	}

	if path != "/tomarkdown" || auth != "Bearer test-key" || part != "%PDF-1.7 test" || filename != "leave.pdf" {
		t.Errorf("request: path %q, auth %q, part %q, file %q", path, auth, part, filename)
	}
}

func TestConvert_RefusesATypeItDoesNotConvertWithoutAskingTheService(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("the service was called") }))
	defer srv.Close()

	c, _ := workersai.New(workersai.Config{URL: srv.URL})

	if _, err := c.Convert(context.Background(), "a.png", "image/png", []byte("x")); !errors.Is(err, domain.ErrUnsupportedDocument) {
		t.Errorf("Convert of a PNG: err = %v, want ErrUnsupportedDocument", err)
	}
}

func TestConvert_ReportsTheServiceErrorWithoutTheKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"success":false,"errors":[{"message":"token is not allowed"}]}`)
	}))
	defer srv.Close()

	c, _ := workersai.New(workersai.Config{URL: srv.URL, Key: "test-secret-key"})

	_, err := c.Convert(context.Background(), "a.pdf", "application/pdf", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "token is not allowed") || strings.Contains(err.Error(), "test-secret-key") {
		t.Errorf("Convert: err = %v, want the service's reason and no key", err)
	}
}

func TestNew_RefusesAKeyInTheClear(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"http://example.com/ai", "ftp://example.com", "https://user:pw@example.com", ""} {
		if _, err := workersai.New(workersai.Config{URL: url}); err == nil {
			t.Errorf("New(%q) = nil error", url)
		}
	}
}
