package domain

import "testing"

// A definition without runtime.buildCache keeps the cache on, so versions published before it
// existed keep restoring and saving it.
func TestRuntime_BuildCacheEnabled(t *testing.T) {
	t.Parallel()

	on, off := true, false
	tests := []struct {
		name string
		rt   Runtime
		want bool
	}{
		{name: "omitted", rt: Runtime{}, want: true},
		{name: "true", rt: Runtime{BuildCache: &on}, want: true},
		{name: "false", rt: Runtime{BuildCache: &off}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.rt.BuildCacheEnabled(); got != tc.want {
				t.Errorf("BuildCacheEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
