package cli_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Basa-Futura/basa-cli/internal/config"
)

const (
	anaID = "9f3c2a4e-0d51-4b7e-9a6b-2f1c8e7d5a10"
	samID = "9f3c2a4e-0d51-4b7e-9a6b-2f1c8e7d5a11"
)

// The API reference's example (basa-web docs/cli/api.md) plus a contact with
// no socials, no tags and no email, which is what a seller with neither an
// override nor an account email looks like. One page of two out of 30.
const contactsBody = `{"data":[
  {"id":"` + anaID + `","name":"Ana Souza","email":"ana@example.com",
   "platforms":[
     {"platform":"tiktok","handle":"ana.tt","followers":1250000,"engagement_rate":"7","preferred":false},
     {"platform":"instagram","handle":"anasouza","followers":48200,"engagement_rate":"3.1","preferred":true}],
   "tags":[{"type":"Region","name":"LATAM"},{"type":"Tier","name":"Priority"}],
   "updated_at":"2026-10-21T14:03:11+00:00"},
  {"id":"` + samID + `","name":"Sam Rivera","email":null,"platforms":[],"tags":[],
   "updated_at":"2026-10-20T09:00:00+00:00"}
],"meta":{"current_page":1,"last_page":15,"per_page":2,"total":30}}`

const contactBody = `{"data":{"id":"` + anaID + `","name":"Ana Souza","email":"ana@example.com",
  "platforms":[
    {"platform":"instagram","handle":"anasouza","followers":48200,"engagement_rate":"3.1","preferred":true},
    {"platform":"youtube","handle":null,"followers":null,"engagement_rate":null,"preferred":false}],
  "tags":[{"type":null,"name":"Repeat"}],
  "updated_at":"2026-10-21T14:03:11+00:00",
  "notes":"Prefers email.\nAgent: Jordan Lee\n"}}`

type contactsCall struct {
	path  string
	query url.Values
}

// contactsAPI serves /me with one team (Acme Agency, id 7) and the contacts
// endpoints with the given status and body, recording what was asked.
func contactsAPI(status int, body string, call *contactsCall) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v1/me"):
			_, _ = w.Write([]byte(meWith("Acme Agency")))
		case strings.Contains(r.URL.Path, "/contacts"):
			call.path = r.URL.Path
			call.query = r.URL.Query()
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no route"}`))
		}
	}
}

func contactsHarness(t *testing.T, status int, body string) (*harness, *contactsCall) {
	t.Helper()
	call := &contactsCall{}
	h := newHarness(t, contactsAPI(status, body, call))
	t.Setenv(config.EnvVarToken, "42|token")
	return h, call
}

func TestContactsListRendersATable(t *testing.T) {
	h, call := contactsHarness(t, http.StatusOK, contactsBody)

	stdout, stderr, code := h.run("contacts", "list", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if call.path != "/api/v1/teams/7/contacts" {
		t.Errorf("path = %q, want the team's contacts", call.path)
	}
	// The preferred platform leads, whatever order the team stored them in.
	wantOut := "" +
		"ID                                    NAME        EMAIL            PLATFORMS                     TAGS                           UPDATED\n" +
		anaID + "  Ana Souza   ana@example.com  instagram 48.2K, tiktok 1.3M  Region: LATAM, Tier: Priority  2026-10-21\n" +
		samID + "  Sam Rivera  —                —                             —                              2026-10-20\n"
	if stdout != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantOut)
	}
	wantErr := "local · Acme Agency\n" +
		"Showing 1–2 of 30. Next: --page 2. Or narrow it with --search or --project.\n"
	if stderr != wantErr {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr, wantErr)
	}
}

// Columns that would be a dash on every row go, as elsewhere in this CLI.
func TestContactsListDropsEmptyColumns(t *testing.T) {
	body := `{"data":[{"id":"` + samID + `","name":"Sam Rivera","email":"sam@example.com",
	  "platforms":[],"tags":[],"updated_at":"2026-10-20T09:00:00+00:00"}],
	  "meta":{"current_page":1,"last_page":1,"per_page":25,"total":1}}`
	h, _ := contactsHarness(t, http.StatusOK, body)

	stdout, stderr, code := h.run("contacts", "list", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	wantOut := "" +
		"ID                                    NAME        EMAIL            UPDATED\n" +
		samID + "  Sam Rivera  sam@example.com  2026-10-20\n"
	if stdout != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantOut)
	}
}

func TestContactsListForwardsItsFilters(t *testing.T) {
	h, call := contactsHarness(t, http.StatusOK, contactsBody)

	_, stderr, code := h.run("contacts", "list", "--env", "local",
		"--search", "ana", "--project", "9f2c1111-2222-3333-4444-555566667777",
		"--sort-by", "updated_at", "--sort-order", "desc", "--limit", "50", "--page", "3")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	want := map[string]string{
		"search": "ana", "project": "9f2c1111-2222-3333-4444-555566667777",
		"sort_by": "updated_at", "sort_order": "desc", "per_page": "50", "page": "3",
	}
	for k, v := range want {
		if got := call.query.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// The project filter changes what an empty page means, so the heading
	// names it.
	if !strings.HasPrefix(stderr, "local · Acme Agency · project 9f2c1111-2222-3333-4444-555566667777\n") {
		t.Errorf("stderr:\n%s\nwant the heading to name the project", stderr)
	}
}

func TestContactsListSendsNoFiltersByDefault(t *testing.T) {
	h, call := contactsHarness(t, http.StatusOK, contactsBody)

	_, _, code := h.run("contacts", "list", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(call.query) != 0 {
		t.Errorf("query = %v, want none: every default is the server's", call.query)
	}
}

// A typed but empty --sort-by is forwarded, so the server's 422 answers it
// rather than the default quietly applying.
func TestContactsListForwardsAnEmptySortAndRelaysThe422(t *testing.T) {
	h, call := contactsHarness(t, http.StatusUnprocessableEntity,
		`{"message":"The selected sort by is invalid.","errors":{"sort_by":["The selected sort by is invalid."]}}`)

	stdout, stderr, code := h.run("contacts", "list", "--env", "local", "--sort-by=")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !call.query.Has("sort_by") || call.query.Get("sort_by") != "" {
		t.Errorf("sort_by = %q (sent: %v), want an empty value forwarded", call.query.Get("sort_by"), call.query.Has("sort_by"))
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if stderr != "The selected sort by is invalid.\n" {
		t.Errorf("stderr = %q, want the server's message", stderr)
	}
}

func TestContactsListEmpty(t *testing.T) {
	h, _ := contactsHarness(t, http.StatusOK, `{"data":[],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":0}}`)

	stdout, stderr, code := h.run("contacts", "list", "--env", "local", "--search", "zzz")

	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if want := "local · Acme Agency\nNo contacts matched.\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestContactsListPastTheLastPage(t *testing.T) {
	h, _ := contactsHarness(t, http.StatusOK, `{"data":[],"meta":{"current_page":9,"last_page":2,"per_page":25,"total":30}}`)

	_, stderr, _ := h.run("contacts", "list", "--env", "local", "--page", "9")

	if want := "Page 9 is past the end: the contacts fill 2 pages.\n"; !strings.HasSuffix(stderr, want) {
		t.Errorf("stderr:\n%s\nwant it to end with %q", stderr, want)
	}
}

func TestContactsShowRendersEverything(t *testing.T) {
	h, call := contactsHarness(t, http.StatusOK, contactBody)

	stdout, stderr, code := h.run("contacts", "show", anaID, "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if call.path != "/api/v1/teams/7/contacts/"+anaID {
		t.Errorf("path = %q", call.path)
	}
	// One line per platform under one label, and the notes the same way, so
	// a second line never lands in the label column. Engagement is a whole
	// percent, shown with % as the web shows it.
	want := "" +
		"Environment  local\n" +
		"Team         Acme Agency\n" +
		"Contact      " + anaID + "\n" +
		"Name         Ana Souza\n" +
		"Email        ana@example.com\n" +
		"Platforms    instagram · anasouza · 48,200 followers · 3.1% engagement · preferred\n" +
		"             youtube\n" +
		"Tags         Repeat\n" +
		"Updated      2026-10-21\n" +
		"Notes        Prefers email.\n" +
		"             Agent: Jordan Lee\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

// No socials, tags or notes are answers the server gave, so they read "none",
// not a dash.
func TestContactsShowSaysNoneForEmptyLists(t *testing.T) {
	body := `{"data":{"id":"` + samID + `","name":"Sam Rivera","email":null,"platforms":[],"tags":[],
	  "updated_at":null,"notes":null}}`
	h, _ := contactsHarness(t, http.StatusOK, body)

	stdout, stderr, code := h.run("contacts", "show", samID, "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"Email        —\n", "Platforms    none\n", "Tags         none\n", "Updated      —\n", "Notes        none\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout:\n%s\nwant it to contain %q", stdout, want)
		}
	}
}

// Another team's contact, a made-up id and garbage are all the same 404 on
// this endpoint, by design. The CLI says so the usual way and exits 2.
func TestContactsShowNotFound(t *testing.T) {
	h, _ := contactsHarness(t, http.StatusNotFound, `{"message":"Not found."}`)

	_, stderr, code := h.run("contacts", "show", "nope", "--env", "local")

	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if want := "That does not exist, or you cannot see it.\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// The listing's team came from /me, so a 404 can only be a server without the
// route — production, on the day contacts merged (2026-11-04, checked live).
func TestContactsListAgainstAServerWithoutContacts(t *testing.T) {
	h, _ := contactsHarness(t, http.StatusNotFound, `{"message":"The route api/v1/teams/7/contacts could not be found."}`)

	_, stderr, code := h.run("contacts", "list", "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "The local environment does not offer contacts yet.\n" +
		"It needs a newer Basa server; the web's Talent Network has the same list.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// Refused contact.list or contact.view: the server's reason is relayed and the
// exit code is the "ask someone for access" one.
func TestContactsListForbidden(t *testing.T) {
	h, _ := contactsHarness(t, http.StatusForbidden,
		`{"message":"You do not have permission to view contacts on this team.","hint":"Ask a team admin."}`)

	_, stderr, code := h.run("contacts", "list", "--env", "local")

	if code != 4 {
		t.Errorf("exit %d, want 4", code)
	}
	if want := "You do not have permission to view contacts on this team.\nAsk a team admin.\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestContactsShowNeedsAnID(t *testing.T) {
	h, call := contactsHarness(t, http.StatusOK, contactBody)

	_, stderr, code := h.run("contacts", "show", "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if want := "Which contact?\nPass its id: basa contacts show <id> --env <name>\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if call.path != "" {
		t.Errorf("requested %q with no id, want no request", call.path)
	}
}

func TestContactsListRejectsAStrayArgument(t *testing.T) {
	h, _ := contactsHarness(t, http.StatusOK, contactsBody)

	_, stderr, code := h.run("contacts", "list", anaID, "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "basa contacts list takes no arguments, but got \"" + anaID + "\".\nDid you mean: basa contacts show <id>\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestContactsJSONIsTheAPIShape(t *testing.T) {
	h, _ := contactsHarness(t, http.StatusOK, contactBody)

	stdout, stderr, code := h.run("contacts", "show", anaID, "--env", "local", "--json")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got, want any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	_ = json.Unmarshal([]byte(contactBody), &want)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("stdout = %s\nwant the API's payload = %s", gotJSON, wantJSON)
	}
}
