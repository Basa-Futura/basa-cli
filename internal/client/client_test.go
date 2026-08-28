package client

import (
	"net/http"
	"testing"
	"time"
)

// The end-to-end behaviour lives in internal/cli, against a fake server. This
// file exists for the one branch that cannot be tested that way without
// sleeping or racing the clock: Retry-After's HTTP-date form, whose result
// depends on the current time. Injecting now makes it deterministic.
func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		// Laravel's throttle sends this form.
		{"seconds", "34", 34 * time.Second},
		{"seconds with surrounding space", "  34  ", 34 * time.Second},
		{"a full minute", "60", time.Minute},

		// Nothing to honour: the caller must not invent a number.
		{"absent", "", 0},
		{"blank", "   ", 0},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"unparseable", "soon", 0},
		{"float", "1.5", 0},

		// RFC 9110 also allows a date, which a proxy may substitute.
		{"future date", "Fri, 28 Aug 2026 12:00:45 GMT", 45 * time.Second},
		{"past date", "Fri, 28 Aug 2026 11:59:00 GMT", 0},
		{"the present", "Fri, 28 Aug 2026 12:00:00 GMT", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			if tc.value != "" {
				header.Set("Retry-After", tc.value)
			}

			if got := retryAfter(header, now); got != tc.want {
				t.Errorf("retryAfter(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
