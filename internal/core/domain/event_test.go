package domain

import "testing"

// An organization event's name goes on the stream's `event:` line as is, so only lowercase
// words joined by single dashes are names: nothing that could break the line or the framing.
func TestIsEventName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "factory", want: true},
		{name: "credential-expired", want: true},
		{name: "kb2", want: true},
		{name: "", want: false},
		{name: "Factory", want: false},
		{name: "2fa", want: false},
		{name: "a--b", want: false},
		{name: "trailing-", want: false},
		{name: "two words", want: false},
		{name: "line\nbreak", want: false},
		{name: "item\r\ndata: x", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsEventName(tt.name); got != tt.want {
				t.Errorf("IsEventName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
