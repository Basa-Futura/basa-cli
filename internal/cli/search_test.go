package cli_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Basa-Futura/basa-cli/internal/config"
)

const searchProjectID = "0199c4e2-6f0b-7a51-9d3e-1c2b3a4d5e6f"

// The contract's worked example (docs/server/search-endpoint.md §3 in
// basa-web), spread over the two teams meWith gives ids 7 and 8: a project and
// a deal in Acme Agency, a contract in Northwind. One page of three out of 87.
const searchBody = `{"data":[
  {"type":"project","id":"` + searchProjectID + `","team":7,
   "title":"Acme Spring Launch","subtitle":"Acme Beverages","status":null},
  {"type":"deal","id":"K3mQz","team":7,"title":"Dana Reed","subtitle":"Acme Spring Launch",
   "status":{"slug":"outreach_sent","label":"Outreach Sent"}},
  {"type":"contract","id":"Xq7Lp","team":8,"title":"Acme Partnership Agreement","subtitle":null,
   "status":{"slug":"ready_for_signature","label":"Ready for Signature"}}
],"meta":{"current_page":1,"last_page":29,"per_page":3,"total":87,
  "counts":{"project":3,"deal":72,"contract":12},"teams":[7,8]}}`

type searchCall struct {
	meHits     int
	searchHits int
	path       string
	query      url.Values
}

// searchAPI serves /me with the given teams and answers /search with status
// and body, recording what search was asked.
func searchAPI(teams []string, status int, body string, call *searchCall) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v1/me"):
			call.meHits++
			_, _ = w.Write([]byte(meWith(teams...)))
		case r.URL.Path == "/api/v1/search":
			call.searchHits++
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

func searchHarness(t *testing.T, teams []string, status int, body string) (*harness, *searchCall) {
	t.Helper()
	call := &searchCall{}
	h := newHarness(t, searchAPI(teams, status, body, call))
	t.Setenv(config.EnvVarToken, "42|token")
	return h, call
}

var twoTeams = []string{"Acme Agency", "Northwind"}

func TestSearchRendersHitsInTheServersOrder(t *testing.T) {
	h, _ := searchHarness(t, twoTeams, http.StatusOK, searchBody)

	stdout, stderr, code := h.run("search", "acme", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	wantOut := "" +
		"TYPE      ID                                    TEAM         TITLE                       SUBTITLE            STATUS\n" +
		"project   0199c4e2-6f0b-7a51-9d3e-1c2b3a4d5e6f  Acme Agency  Acme Spring Launch          Acme Beverages      —\n" +
		"deal      K3mQz                                 Acme Agency  Dana Reed                   Acme Spring Launch  Outreach Sent\n" +
		"contract  Xq7Lp                                 Northwind    Acme Partnership Agreement  —                   Ready for Signature\n"
	if stdout != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantOut)
	}
	wantErr := "" +
		"local · \"acme\" · Acme Agency, Northwind\n" +
		"87 hits · 3 projects · 72 deals · 12 contracts\n" +
		"Showing 1–3 of 87. Next: --page 2. Or narrow it with --type or --team.\n" +
		"Show one: basa projects show " + searchProjectID + " --env local --team 7\n"
	if stderr != wantErr {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr, wantErr)
	}
}

// Searching spans teams by design, so a caller with several must not be asked
// to pick one, the way every team-scoped listing asks.
func TestSearchSendsTheTermAndEveryKnownTypeButNoTeam(t *testing.T) {
	h, call := searchHarness(t, twoTeams, http.StatusOK, searchBody)

	_, stderr, code := h.run("search", "dana", "reed", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if call.path != "/api/v1/search" {
		t.Errorf("path = %q, want /api/v1/search", call.path)
	}
	if got := call.query.Get("q"); got != "dana reed" {
		t.Errorf("q = %q, want the words joined, %q", got, "dana reed")
	}
	// The contract asks a client to name the types it renders, so a type the
	// server adds later never reaches a CLI without a command for it.
	if got := call.query.Get("type"); got != "project,deal,contract" {
		t.Errorf("type = %q, want %q", got, "project,deal,contract")
	}
	for _, absent := range []string{"team", "per_page", "page"} {
		if call.query.Has(absent) {
			t.Errorf("%s = %q was sent, want it absent by default", absent, call.query.Get(absent))
		}
	}
}

func TestSearchForwardsItsFlags(t *testing.T) {
	h, call := searchHarness(t, twoTeams, http.StatusOK, searchBody)

	_, stderr, code := h.run("search", "acme", "--env", "local",
		"--team", "northwind", "--type", "deal,contract", "--limit", "50", "--page", "2")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	want := map[string]string{"q": "acme", "team": "8", "type": "deal,contract", "per_page": "50", "page": "2"}
	for k, v := range want {
		if got := call.query.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// Validation is the server's: an empty --type reaches it, and its 422 is what
// the operator reads, verbatim.
func TestSearchForwardsAnEmptyTypeAndRelaysThe422(t *testing.T) {
	h, call := searchHarness(t, twoTeams, http.StatusUnprocessableEntity,
		`{"message":"The type field is required.","errors":{"type":["The type field is required."]}}`)

	stdout, stderr, code := h.run("search", "acme", "--env", "local", "--type=")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !call.query.Has("type") || call.query.Get("type") != "" {
		t.Errorf("type = %q (sent: %v), want an empty type forwarded", call.query.Get("type"), call.query.Has("type"))
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on failure", stdout)
	}
	if stderr != "The type field is required.\n" {
		t.Errorf("stderr = %q, want the server's message", stderr)
	}
}

// When every hit is the same type and in one team, those columns would repeat
// on every row; they collapse into the heading instead, as elsewhere.
func TestSearchCollapsesUniformColumns(t *testing.T) {
	body := `{"data":[
	  {"type":"deal","id":"K3mQz","team":8,"title":"Dana Reed","subtitle":"Acme Spring Launch",
	   "status":{"slug":"outreach_sent","label":"Outreach Sent"}},
	  {"type":"deal","id":"P9wXa","team":8,"title":"Sam Rivera","subtitle":"Acme Spring Launch",
	   "status":{"slug":"signed","label":"Signed"}}
	],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":2,
	  "counts":{"deal":2},"teams":[7,8]}}`
	h, _ := searchHarness(t, twoTeams, http.StatusOK, body)

	stdout, stderr, code := h.run("search", "acme", "--env", "local", "--type", "deal")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	wantOut := "" +
		"ID     TITLE       SUBTITLE            STATUS\n" +
		"K3mQz  Dana Reed   Acme Spring Launch  Outreach Sent\n" +
		"P9wXa  Sam Rivera  Acme Spring Launch  Signed\n"
	if stdout != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantOut)
	}
	wantErr := "" +
		"local · \"acme\" · Acme Agency, Northwind\n" +
		"2 hits · 2 deals · all in Northwind\n" +
		"Show one: basa deals show K3mQz --env local --team 8\n"
	if stderr != wantErr {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr, wantErr)
	}
}

// With one team there is nothing for --team to choose, so the hint leaves it
// out, as resolveTeam does for the other commands.
func TestSearchHintOmitsTeamForASingleTeam(t *testing.T) {
	body := `{"data":[{"type":"contract","id":"Xq7Lp","team":7,"title":"NDA","subtitle":null,"status":null}],
	  "meta":{"current_page":1,"last_page":1,"per_page":25,"total":1,
	  "counts":{"project":0,"deal":0,"contract":1},"teams":[7]}}`
	h, _ := searchHarness(t, []string{"Acme Agency"}, http.StatusOK, body)

	_, stderr, code := h.run("search", "nda", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if want := "Show one: basa contracts show Xq7Lp --env local\n"; !strings.HasSuffix(stderr, want) {
		t.Errorf("stderr:\n%s\nwant it to end with %q", stderr, want)
	}
	if want := "1 hit · 0 projects · 0 deals · 1 contract\n"; !strings.Contains(stderr, want) {
		t.Errorf("stderr:\n%s\nwant the tally %q", stderr, want)
	}
}

func TestSearchWithNoMatches(t *testing.T) {
	body := `{"data":[],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":0,
	  "counts":{"project":0,"deal":0,"contract":0},"teams":[7,8]}}`
	h, _ := searchHarness(t, twoTeams, http.StatusOK, body)

	stdout, stderr, code := h.run("search", "zzz", "--env", "local")

	if code != 0 {
		t.Errorf("exit %d, want 0: an empty search is an answer, not a failure", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if want := "local · \"zzz\" · Acme Agency, Northwind\nNothing matched \"zzz\".\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// A team that cannot list any requested type is dropped server-side, and an
// empty meta.teams means nothing was searched at all. "Nothing matched" would
// claim a search that never ran.
func TestSearchSaysWhenNoTeamWasSearched(t *testing.T) {
	body := `{"data":[],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":0,
	  "counts":{"contract":0},"teams":[]}}`
	h, _ := searchHarness(t, twoTeams, http.StatusOK, body)

	_, stderr, code := h.run("search", "acme", "--env", "local", "--team", "acme", "--type", "contract")

	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	want := "local · \"acme\" · no teams\n" +
		"No team was searched: those teams are disabled, or you cannot list these types in them.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestSearchPastTheLastPage(t *testing.T) {
	body := `{"data":[],"meta":{"current_page":9,"last_page":4,"per_page":25,"total":87,
	  "counts":{"project":3,"deal":72,"contract":12},"teams":[7,8]}}`
	h, _ := searchHarness(t, twoTeams, http.StatusOK, body)

	_, stderr, _ := h.run("search", "acme", "--env", "local", "--page", "9")

	if want := "Page 9 is past the end: the results fill 4 pages.\n"; !strings.HasSuffix(stderr, want) {
		t.Errorf("stderr:\n%s\nwant it to end with %q", stderr, want)
	}
}

// --json is the server's payload untouched, and needs no /me when there is no
// --team to resolve: one request.
func TestSearchJSONIsTheAPIShapeInOneRequest(t *testing.T) {
	h, call := searchHarness(t, twoTeams, http.StatusOK, searchBody)

	stdout, stderr, code := h.run("search", "acme", "--env", "local", "--json")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	var got, want any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	_ = json.Unmarshal([]byte(searchBody), &want)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("stdout = %s\nwant the API's payload = %s", gotJSON, wantJSON)
	}
	if call.meHits != 0 || call.searchHits != 1 {
		t.Errorf("made %d /me and %d /search requests, want 0 and 1", call.meHits, call.searchHits)
	}
}

// The search contract has no 404, so one means the server predates search.
// The generic "does not exist, or you cannot see it" would send the operator
// looking for an id they never typed.
func TestSearchAgainstAServerWithoutSearch(t *testing.T) {
	h, _ := searchHarness(t, twoTeams, http.StatusNotFound, `{"message":"Not Found"}`)

	_, stderr, code := h.run("search", "acme", "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "The local environment does not offer search yet.\n" +
		"Until it does, list instead: basa projects list, basa deals list, basa contracts list.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// Search answers 503 when its query times out, and says to try again. That is
// advice the operator can follow, unlike "report it".
func TestSearchUnavailableRelaysTheServersAdvice(t *testing.T) {
	h, _ := searchHarness(t, twoTeams, http.StatusServiceUnavailable,
		`{"message":"Search is temporarily unavailable. Try again shortly."}`)

	_, stderr, code := h.run("search", "acme", "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "Search is temporarily unavailable. Try again shortly.\nWait a moment and try again.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestSearchNeedsATerm(t *testing.T) {
	h, call := searchHarness(t, twoTeams, http.StatusOK, searchBody)

	_, stderr, code := h.run("search", "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if want := "What should basa search for?\nPass a name: basa search <term>\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if call.searchHits != 0 {
		t.Errorf("searched %d times with no term, want 0", call.searchHits)
	}
}

func TestSearchTeamMustBeOneOfYours(t *testing.T) {
	h, call := searchHarness(t, twoTeams, http.StatusOK, searchBody)

	_, stderr, code := h.run("search", "acme", "--env", "local", "--team", "globex")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if want := "No team of yours matches \"globex\".\nYour teams: Acme Agency (7), Northwind (8)\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if call.searchHits != 0 {
		t.Errorf("searched %d times for an unknown team, want 0", call.searchHits)
	}
}

// Staging's real shape (2026-10-15): contract titles that differ only at the
// end, no recipient so every subtitle is null, and a page of one team out of
// several searched with more beyond it. The dash-only SUBTITLE column goes and
// the title takes its room, and the team is named for this page — "all in"
// would claim rows that were not fetched.
func TestSearchOnAPageWithoutSubtitlesOrAllItsHits(t *testing.T) {
	body := `{"data":[
	  {"type":"contract","id":"ZTRMU","team":8,"title":"BASA Influencer Campaign - Creator Agreement - AMMENDMENT",
	   "subtitle":null,"status":{"slug":"ready_for_signature","label":"Ready for Signature"}},
	  {"type":"contract","id":"eba6l","team":8,"title":"BASA Influencer Campaign - Creator Agreement",
	   "subtitle":null,"status":{"slug":"signed","label":"Signed"}}
	],"meta":{"current_page":1,"last_page":29,"per_page":2,"total":57,
	  "counts":{"contract":57},"teams":[7,8]}}`
	h, _ := searchHarness(t, twoTeams, http.StatusOK, body)

	stdout, stderr, code := h.run("search", "basa", "--env", "local", "--type", "contract")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	wantOut := "" +
		"ID     TITLE                                                      STATUS\n" +
		"ZTRMU  BASA Influencer Campaign - Creator Agreement - AMMENDMENT  Ready for Signature\n" +
		"eba6l  BASA Influencer Campaign - Creator Agreement               Signed\n"
	if stdout != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s\nstderr:\n%s", stdout, wantOut, stderr)
	}
	if want := "57 hits · 57 contracts · this page in Northwind\n"; !strings.Contains(stderr, want) {
		t.Errorf("stderr:\n%s\nwant it to contain %q", stderr, want)
	}
}

// Staging's hosting platform turns the API's slow-search 503 into an HTML 504.
// Narrowing is what fixes it, so search says that, not "report it".
func TestSearchTooSlowSaysToAskForLess(t *testing.T) {
	h, _ := searchHarness(t, twoTeams, http.StatusGatewayTimeout, `<!DOCTYPE html><html><body>Error code: 503</body></html>`)

	stdout, stderr, code := h.run("search", "basa", "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	want := "The local environment took too long to search all of that (504).\n" +
		"Ask for less: narrow it with --team or --type, or use a longer term.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// Every other command gets the general 504: worth another try, not first a
// thing to report.
func TestAGatewayTimeoutElsewhereSaysTryAgain(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/me") {
			_, _ = w.Write([]byte(meWith("Acme Agency")))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte(`<html>Error code: 503</html>`))
	})
	t.Setenv(config.EnvVarToken, "42|token")

	_, stderr, code := h.run("deals", "list", "--env", "local")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "The local environment took too long to answer (504).\n" +
		"Try again in a moment. If it keeps happening, report it with the time it happened.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
