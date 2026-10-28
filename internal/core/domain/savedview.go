package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrSavedViewNameTaken means another view has the name (case ignored): another personal view of
// the same owner, or another shared view of the same factory. It wraps ErrInvalid.
var ErrSavedViewNameTaken = fmt.Errorf("saved view name taken: %w", ErrInvalid)

// Saved view limits (ADR 0020, decision 1). Lengths count characters, as char_length does.
const (
	MaxSavedViewName        = 60
	MaxSavedViewFilterKeys  = 20
	MaxSavedViewKeyValues   = 20
	MaxSavedViewFilterValue = 200
)

// SavedViewID is a saved view's id (a UUID).
type SavedViewID string

// SavedView is a named set of the Items page's URL search params on a factory. A personal view
// (Shared false) is seen only by its owner; a shared view by every reader of the factory.
type SavedView struct {
	ID      SavedViewID
	Factory FactoryID
	// Owner is the person who made the view.
	Owner  UserID
	Name   string
	Shared bool
	// Filters maps each search param to its values, so the view reopens the URL it was saved from.
	Filters   map[string][]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate trims the view's name and checks it and the filters against the limits, each failure
// wrapping ErrInvalid. Nil filters and nil value lists become empty ones.
func (v *SavedView) Validate() error {
	v.Name = strings.TrimSpace(v.Name)

	if n := utf8.RuneCountInString(v.Name); n < 1 || n > MaxSavedViewName {
		return fmt.Errorf("saved view name must be 1 to %d characters: %w", MaxSavedViewName, ErrInvalid)
	}

	if len(v.Filters) > MaxSavedViewFilterKeys {
		return fmt.Errorf("saved view has %d filters, at most %d: %w", len(v.Filters), MaxSavedViewFilterKeys, ErrInvalid)
	}

	filters := make(map[string][]string, len(v.Filters))

	for key, values := range v.Filters {
		if len(values) > MaxSavedViewKeyValues {
			return fmt.Errorf("saved view filter %q has %d values, at most %d: %w", key, len(values), MaxSavedViewKeyValues, ErrInvalid)
		}

		for _, value := range values {
			if utf8.RuneCountInString(value) > MaxSavedViewFilterValue {
				return fmt.Errorf("saved view filter %q has a value over %d characters: %w", key, MaxSavedViewFilterValue, ErrInvalid)
			}
		}

		if values == nil {
			values = []string{}
		}

		filters[key] = values
	}

	v.Filters = filters

	return nil
}
