package api_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// genPkg is the package of the contract's generated types, whose required arrays nullArrays checks.
const genPkg = "github.com/byFrederick/goal/internal/api/gen"

var unmarshaler = reflect.TypeFor[json.Unmarshaler]()

// nullArrays lists the JSON paths in body that are null where T, by its generated gen types, has a
// required, non-nullable array: a non-pointer slice without omitempty. Pointers to slices, omitempty
// fields and types with their own UnmarshalJSON (the unions) may be null and are not walked.
func nullArrays[T any](body []byte) ([]string, error) {
	return nullArraysOf(reflect.TypeFor[T](), body)
}

// nullArraysOf is nullArrays for a body read as t.
func nullArraysOf(t reflect.Type, body []byte) ([]string, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}

	var out []string

	walkNulls(t, v, "$", &out)
	slices.Sort(out)

	return out, nil
}

func walkNulls(t reflect.Type, v any, path string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if v == nil || t.Implements(unmarshaler) || reflect.PointerTo(t).Implements(unmarshaler) {
		return
	}

	switch v := v.(type) {
	case map[string]any:
		walkObject(t, v, path, out)
	case []any:
		if k := t.Kind(); k == reflect.Slice || k == reflect.Array {
			for i, e := range v {
				walkNulls(t.Elem(), e, path+"["+strconv.Itoa(i)+"]", out)
			}
		}
	}
}

// walkObject walks obj as t: a struct by its fields, a map by its values.
func walkObject(t reflect.Type, obj map[string]any, path string, out *[]string) {
	if t.Kind() == reflect.Struct {
		walkStruct(t, obj, path, out)

		return
	}

	if t.Kind() == reflect.Map {
		for k, e := range obj {
			walkNulls(t.Elem(), e, path+"."+k, out)
		}
	}
}

// walkStruct walks obj by the json fields of struct t, noting null at a required array.
func walkStruct(t reflect.Type, obj map[string]any, path string, out *[]string) {
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}

		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}

		if f.Anonymous && name == "" {
			walkNulls(f.Type, obj, path, out)

			continue
		}

		if name == "" {
			name = f.Name
		}

		val, present := obj[name]
		if present && val == nil && required(t, f, opts) {
			*out = append(*out, path+"."+name)

			continue
		}

		walkNulls(f.Type, val, path+"."+name, out)
	}
}

// required reports whether f, of struct t, is an array the contract requires and never makes null.
func required(t reflect.Type, f reflect.StructField, opts string) bool {
	return t.PkgPath() == genPkg && f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() != reflect.Uint8 &&
		!strings.Contains(","+opts+",", ",omitempty,") && !f.Type.Implements(unmarshaler)
}

// TestNoNullArrays_FlagsNilRequiredSlice proves the decode check flags null at a required array,
// nested in a list of structs, and accepts [] and an omitted or null optional list.
func TestNoNullArrays_FlagsNilRequiredSlice(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, body string
		want       []string
	}{
		{"null required", `{"groups":[{"rows":[{"secrets":null,"uses":[]}]}]}`, []string{"$.groups[0].rows[0].secrets"}},
		{"empty required", `{"groups":[{"rows":[{"secrets":[],"uses":[]}]}]}`, nil},
		{"nested null", `{"groups":[{"rows":[{"secrets":[],"uses":[{"agents":null}]}]}]}`, []string{"$.groups[0].rows[0].uses[0].agents"}},
		{"top-level null", `{"groups":null}`, []string{"$.groups"}},
	}

	for _, c := range cases {
		got, err := nullArrays[gen.Access]([]byte(c.body))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}

		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: null arrays %v, want %v", c.name, got, c.want)
		}
	}

	// A nullable list (*[]T) may be null, and an optional one (omitempty) omitted.
	for _, b := range []string{`{"factories":null}`, `{}`} {
		if got, _ := nullArrays[gen.ApiKey]([]byte(b)); len(got) != 0 {
			t.Errorf("%s: null arrays %v, want none", b, got)
		}
	}
}
