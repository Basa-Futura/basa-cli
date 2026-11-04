package commands

import "testing"

// Audience sizes in a table cell. The boundary rows are the point: 999,950
// rounds to a thousand thousands, which has to read as 1M, not "1000K".
func TestCompactCount(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{950, "950"},
		{1_000, "1K"},
		{48_200, "48.2K"},
		{999_949, "999.9K"},
		{999_950, "1M"},
		{1_000_000, "1M"},
		{1_250_000, "1.3M"},
	} {
		if got := compactCount(tc.in); got != tc.want {
			t.Errorf("compactCount(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGroupDigits(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1_000, "1,000"},
		{48_200, "48,200"},
		{1_234_567, "1,234,567"},
	} {
		if got := groupDigits(tc.in); got != tc.want {
			t.Errorf("groupDigits(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
