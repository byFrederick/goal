package knowledge

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// converter turns a source's items into Markdown files (ADR 0038 decision 2): text, Markdown,
// JSON and YAML as they are and HTML with its headings as Markdown headings, built in through
// Extract; any other type through next, the conversion service, or refused without one.
type converter struct {
	next ports.Converter
}

// Convert implements ports.Converter.
func (c converter) Convert(ctx context.Context, name, mediaType string, body []byte) ([]byte, error) {
	if _, err := typeOf(mediaType); err == nil {
		text, err := Extract(mediaType, body)
		if err != nil {
			return nil, err
		}

		return []byte(text), nil
	}

	if c.next == nil {
		return nil, fmt.Errorf("%q is not text, Markdown, HTML, JSON or YAML, and this goald converts no other type: %w", mediaType, domain.ErrUnsupportedDocument)
	}

	md, err := c.next.Convert(ctx, name, mediaType, body)
	if err != nil {
		return nil, fmt.Errorf("convert %s: %w", name, err)
	}

	return md, nil
}
