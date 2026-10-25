package cloudflare

import (
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/ports"
)

// An exec's answer becomes stdout and an exit status, and an answer that does not say how the
// command ended is an error, never a status the runtime did not give (spec 4 §11.2 row 6).
func TestProcess_Decode(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", ports.MaxStderrTail+10)

	tests := []struct {
		name    string
		answer  string
		stdout  string
		status  ports.ExitStatus
		wantErr string
	}{
		{
			name:   "output then the exit",
			answer: `{"o":"fir"}` + "\n" + `{"o":"st\n"}` + "\n" + `{"exit":3,"timed_out":false,"stderr":"oops\n"}` + "\n",
			stdout: "first\n", status: ports.ExitStatus{Code: 3, StderrTail: "oops\n"},
		},
		{
			name:   "timed out, its stderr cut to the tail",
			answer: `{"exit":124,"timed_out":true,"stderr":"` + long + `"}`,
			status: ports.ExitStatus{Code: 124, TimedOut: true, StderrTail: long[10:]},
		},
		{name: "the outcome unknown", answer: `{"o":"first\n"}` + "\n" + `{"error":"the sandbox was deleted"}`, stdout: "first\n", wantErr: "deleted"},
		{name: "cut before the outcome", answer: `{"o":"first\n"}` + "\n", stdout: "first\n", wantErr: "without the command's outcome"},
		{name: "not the protocol", answer: "<html>", wantErr: "invalid character"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				p      process
				stdout strings.Builder
			)

			err := p.decode(strings.NewReader(tt.answer), &stdout)

			if stdout.String() != tt.stdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.stdout)
			}

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want one mentioning %q", err, tt.wantErr)
				}

				return
			}

			if err != nil || p.status != tt.status {
				t.Errorf("status = %+v, %v; want %+v", p.status, err, tt.status)
			}
		})
	}
}
