package domain_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestSavedView_Validate(t *testing.T) {
	t.Parallel()

	keys := func(n int) map[string][]string {
		f := map[string][]string{}
		for i := range n {
			f[fmt.Sprintf("k%d", i)] = []string{"v"}
		}

		return f
	}

	values := func(n int) map[string][]string {
		return map[string][]string{"state": make([]string, n)}
	}

	tests := []struct {
		name     string
		view     domain.SavedView
		wantName string
		wantErr  bool
	}{
		{"name trimmed", domain.SavedView{Name: "  mine \t"}, "mine", false},
		{"name of 60 characters", domain.SavedView{Name: strings.Repeat("é", 60)}, strings.Repeat("é", 60), false},
		{"name of 61 characters", domain.SavedView{Name: strings.Repeat("é", 61)}, "", true},
		{"empty name", domain.SavedView{Name: ""}, "", true},
		{"blank name", domain.SavedView{Name: "   "}, "", true},
		{"no filters", domain.SavedView{Name: "a"}, "a", false},
		{"20 keys", domain.SavedView{Name: "a", Filters: keys(20)}, "a", false},
		{"21 keys", domain.SavedView{Name: "a", Filters: keys(21)}, "", true},
		{"20 values", domain.SavedView{Name: "a", Filters: values(20)}, "a", false},
		{"21 values", domain.SavedView{Name: "a", Filters: values(21)}, "", true},
		{"value of 200 characters", domain.SavedView{Name: "a", Filters: map[string][]string{"q": {strings.Repeat("é", 200)}}}, "a", false},
		{"value of 201 characters", domain.SavedView{Name: "a", Filters: map[string][]string{"q": {strings.Repeat("é", 201)}}}, "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := tc.view
			err := v.Validate()

			if tc.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("Validate() = %v; want domain.ErrInvalid", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Validate() = %v", err)
			}

			if v.Name != tc.wantName {
				t.Errorf("Name = %q; want %q", v.Name, tc.wantName)
			}

			if v.Filters == nil {
				t.Error("Filters = nil; want an empty map")
			}
		})
	}
}

func TestSavedView_ValidateNilValuesBecomeEmpty(t *testing.T) {
	t.Parallel()

	v := domain.SavedView{Name: "a", Filters: map[string][]string{"q": nil}}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}

	if v.Filters["q"] == nil {
		t.Error(`Filters["q"] = nil; want an empty list`)
	}
}

func TestErrSavedViewNameTaken_WrapsErrInvalid(t *testing.T) {
	t.Parallel()

	if !errors.Is(domain.ErrSavedViewNameTaken, domain.ErrInvalid) {
		t.Error("ErrSavedViewNameTaken does not wrap ErrInvalid")
	}
}
