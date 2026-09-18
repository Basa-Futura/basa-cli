package cli_test

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Basa-Futura/basa-cli/internal/config"
)

// Notification ids are UUIDs, like a project's, so the fixtures use real-length
// ones — the id in the table is what `notifications show` takes.
const (
	assignedID = "9f2c1111-2222-3333-4444-555566667777"
	signedID   = "9f2c8888-9999-aaaa-bbbb-ccccddddeeee"
)

// Two unread entries: the shape the default filter produces, where a READ
// column would say "no" on every row and so must not appear.
const notificationsBody = `{"data":[
  {"id":"` + assignedID + `","type":"deal_assigned",
   "message":"Dana Reed assigned you to the Autumn Launch deal.",
   "read":false,"read_at":null,"created_at":"2026-09-17T09:30:00+00:00"},
  {"id":"` + signedID + `","type":"contract_signed",
   "message":"Northwind Trading signed the Spring Campaign contract.",
   "read":false,"read_at":null,"created_at":"2026-09-16T12:00:00+00:00"}
],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":2}}`

const emptyNotificationsBody = `{"data":[],
  "meta":{"current_page":1,"last_page":1,"per_page":25,"total":0}}`

// notificationsAPI serves the notifications endpoints and records what it was
// asked for. It records /me hits too, because a notification is not team-scoped
// and nothing here should be resolving a team.
type notificationsCall struct {
	query   string
	path    string
	meHits  int
	feedHit int
}

func notificationsAPI(body string, call *notificationsCall) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v1/me"):
			if call != nil {
				call.meHits++
			}
			_, _ = w.Write([]byte(meWith("Acme Agency")))
		case strings.Contains(r.URL.Path, "/notifications"):
			if call != nil {
				call.feedHit++
				call.query = r.URL.RawQuery
				call.path = r.URL.Path
			}
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no route"}`))
		}
	}
}

func TestNotificationsListRendersATable(t *testing.T) {
	h := newHarness(t, notificationsAPI(notificationsBody, nil))
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, stderr, code := h.run("notifications", "list", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, want 0. stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"ID", "WHEN", "TYPE", "MESSAGE",
		assignedID, "deal_assigned", "2026-09-17",
		"Dana Reed assigned you to the Autumn Launch deal."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table is missing %q\n--- stdout ---\n%s", want, stdout)
		}
	}
	// Every row is unread under the default filter, so the column would repeat
	// itself on every line — the same rule projects applies to ARCHIVED.
	if strings.Contains(stdout, "READ") {
		t.Errorf("READ column should be hidden when the page is uniform\n%s", stdout)
	}
}

// The heading has to name the slice being shown. The default hides read rows,
// so without it an empty or short listing is ambiguous in the one direction
// that sends someone looking for a bug.
func TestNotificationsListHeadingNamesTheFilter(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{}, "unread"},
		{[]string{"--read", "all"}, "all"},
		{[]string{"--read", "true"}, "read"},
	} {
		h := newHarness(t, notificationsAPI(notificationsBody, nil))
		t.Setenv(config.EnvVarToken, "42|token")

		args := append([]string{"notifications", "list", "--env", "local"}, tc.args...)
		_, stderr, code := h.run(args...)

		if code != 0 {
			t.Fatalf("%v: exit %d. stderr:\n%s", tc.args, code, stderr)
		}
		if !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: heading should say %q, got:\n%s", tc.args, tc.want, stderr)
		}
	}
}

func TestNotificationsListForwardsItsFilters(t *testing.T) {
	var call notificationsCall
	h := newHarness(t, notificationsAPI(notificationsBody, &call))
	t.Setenv(config.EnvVarToken, "42|token")

	_, stderr, code := h.run("notifications", "list", "--env", "local",
		"--read", "all", "--sort-order", "asc", "--limit", "50")

	if code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"read=all", "sort_order=asc", "per_page=50"} {
		if !strings.Contains(call.query, want) {
			t.Errorf("query %q is missing %q", call.query, want)
		}
	}
}

// Nothing given means nothing sent: the server owns the default, not this
// client, so an unflagged run must not invent read=false.
func TestNotificationsListSendsNoFiltersByDefault(t *testing.T) {
	var call notificationsCall
	h := newHarness(t, notificationsAPI(notificationsBody, &call))
	t.Setenv(config.EnvVarToken, "42|token")

	if _, stderr, code := h.run("notifications", "list", "--env", "local"); code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	if call.query != "" {
		t.Errorf("should send no query parameters, sent %q", call.query)
	}
}

// `--read=` is a shell interpolating an empty variable. It has to reach the
// server so its 422 answers it, rather than being folded into the default and
// reported as a successful listing. Same defect ProjectFilters.Archived names.
func TestNotificationsListForwardsAnEmptyRead(t *testing.T) {
	var call notificationsCall
	h := newHarness(t, notificationsAPI(notificationsBody, &call))
	t.Setenv(config.EnvVarToken, "42|token")

	if _, stderr, code := h.run("notifications", "list", "--env", "local", "--read="); code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	if !strings.Contains(call.query, "read=") {
		t.Errorf("an empty --read must still be sent, query was %q", call.query)
	}
}

// A notification is addressed to a user and `notifications` has no team column,
// so this must not resolve a team — and must not call /me to do it.
func TestNotificationsListIsNotTeamScoped(t *testing.T) {
	var call notificationsCall
	h := newHarness(t, notificationsAPI(notificationsBody, &call))
	t.Setenv(config.EnvVarToken, "42|token")

	if _, stderr, code := h.run("notifications", "list", "--env", "local"); code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	if call.meHits != 0 {
		t.Errorf("resolved a team it does not need (%d /me calls)", call.meHits)
	}
	if strings.Contains(call.path, "/teams/") {
		t.Errorf("path must carry no team, got %q", call.path)
	}
	if call.path != "/api/v1/notifications" {
		t.Errorf("path = %q, want /api/v1/notifications", call.path)
	}
}

// An empty page must answer the question that was asked. "No notifications"
// under a filter that hides read rows would be a different, wrong claim.
func TestNotificationsListEmptyAnswersTheFilterAsked(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{}, "Nothing unread"},
		{[]string{"--read", "all"}, "No notifications matched."},
	} {
		h := newHarness(t, notificationsAPI(emptyNotificationsBody, nil))
		t.Setenv(config.EnvVarToken, "42|token")

		args := append([]string{"notifications", "list", "--env", "local"}, tc.args...)
		_, stderr, code := h.run(args...)

		if code != 0 {
			t.Fatalf("%v: exit %d. stderr:\n%s", tc.args, code, stderr)
		}
		if !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: want %q, got:\n%s", tc.args, tc.want, stderr)
		}
	}
}

// The READ column earns its place only when the page holds both.
func TestNotificationsListShowsReadColumnWhenMixed(t *testing.T) {
	mixed := `{"data":[
	  {"id":"` + assignedID + `","type":"deal_assigned","message":"Unread one.",
	   "read":false,"read_at":null,"created_at":"2026-09-17T09:30:00+00:00"},
	  {"id":"` + signedID + `","type":"contract_signed","message":"Read one.",
	   "read":true,"read_at":"2026-09-17T10:00:00+00:00","created_at":"2026-09-16T12:00:00+00:00"}
	],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":2}}`

	h := newHarness(t, notificationsAPI(mixed, nil))
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, stderr, code := h.run("notifications", "list", "--env", "local", "--read", "all")

	if code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "READ") {
		t.Errorf("READ column should appear on a mixed page\n%s", stdout)
	}
}

// A long message is cut in the table so it cannot take the alignment of every
// other column with it — and cut on a rune boundary, because these strings are
// server-translated and carry accents in es/pt.
func TestNotificationsListTruncatesLongMessagesSafely(t *testing.T) {
	long := strings.Repeat("ó", 200)
	body := `{"data":[
	  {"id":"` + assignedID + `","type":"deal_assigned","message":"` + long + `",
	   "read":false,"read_at":null,"created_at":"2026-09-17T09:30:00+00:00"}
	],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":1}}`

	h := newHarness(t, notificationsAPI(body, nil))
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, stderr, code := h.run("notifications", "list", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	if !utf8.ValidString(stdout) {
		t.Error("truncation produced invalid UTF-8 — it must cut runes, not bytes")
	}
	if !strings.Contains(stdout, "…") {
		t.Errorf("a cut message should be marked with an ellipsis\n%s", stdout)
	}
	if strings.Contains(stdout, long) {
		t.Error("the full message should not reach the table")
	}
}

func TestNotificationsShowRendersTheWholeMessage(t *testing.T) {
	long := strings.Repeat("a", 200)
	body := `{"data":{"id":"` + assignedID + `","type":"deal_assigned",
	  "message":"` + long + `","read":true,"read_at":"2026-09-17T10:00:00+00:00",
	  "created_at":"2026-09-17T09:30:00+00:00"}}`

	h := newHarness(t, notificationsAPI(body, nil))
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, stderr, code := h.run("notifications", "show", assignedID, "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	// Untruncated here: showing one is the way to read the whole thing.
	if !strings.Contains(stdout, long) {
		t.Errorf("show must not truncate the message\n%s", stdout)
	}
	if !strings.Contains(stdout, "2026-09-17") {
		t.Errorf("should report when it was read\n%s", stdout)
	}
}

// An unread notification has no read_at, and "—" beside "Read  no" would read
// as data the server failed to send rather than as nothing to send.
func TestNotificationsShowSaysWhenItIsUnread(t *testing.T) {
	body := `{"data":{"id":"` + assignedID + `","type":"deal_assigned",
	  "message":"Something happened.","read":false,"read_at":null,
	  "created_at":"2026-09-17T09:30:00+00:00"}}`

	h := newHarness(t, notificationsAPI(body, nil))
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, stderr, code := h.run("notifications", "show", assignedID, "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "not read yet") {
		t.Errorf("should say it is unread, got:\n%s", stdout)
	}
}

func TestNotificationsShowNeedsAnID(t *testing.T) {
	h := newHarness(t, notificationsAPI(notificationsBody, nil))
	t.Setenv(config.EnvVarToken, "42|token")

	_, stderr, code := h.run("notifications", "show", "--env", "local")

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "Which notification?") {
		t.Errorf("should ask which one, got:\n%s", stderr)
	}
}

// JSON mode passes the API's own shape through, so a script sees the server's
// field names and the untruncated message.
func TestNotificationsListJSONIsTheAPIShape(t *testing.T) {
	h := newHarness(t, notificationsAPI(notificationsBody, nil))
	t.Setenv(config.EnvVarToken, "42|token")

	stdout, stderr, code := h.run("notifications", "list", "--env", "local", "--json")

	if code != 0 {
		t.Fatalf("exit %d. stderr:\n%s", code, stderr)
	}
	for _, want := range []string{`"read_at"`, `"created_at"`, `"deal_assigned"`,
		"Dana Reed assigned you to the Autumn Launch deal."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("JSON output is missing %q\n%s", want, stdout)
		}
	}
}
