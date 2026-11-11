package cli_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Basa-Futura/basa-cli/internal/config"
)

const contactID = "9f3c2a4e-0d51-4b7e-9a6b-2f1c8e7d5a10"

// Two deals with the same contact on two projects: what `--contact` returns.
const contactDealsBody = `{"data":[
  {"id":"K3mQz","stage":{"slug":"contracting","label":"Contracting"},
   "status":{"slug":"awaiting_signature","label":"Awaiting signature"},
   "project":{"id":"9f2c1111-2222-3333-4444-555566667777","name":"Spring Campaign"},
   "brand":{"name":"Acme"},"role":{"name":"Lead Creator"},
   "counterparty":{"name":"Ana Souza","contact_id":"` + contactID + `"},"assigned_to":"Dana Reed",
   "sent_at":"2026-08-01T12:00:00+00:00","updated_at":"2026-08-18T09:30:00+00:00"},
  {"id":"P9wXa","stage":{"slug":"outreach","label":"Outreach"},
   "status":{"slug":"outreach_sent","label":"Outreach Sent"},
   "project":{"id":"9f2c1111-2222-3333-4444-555566668888","name":"Autumn Launch"},
   "brand":{"name":"Northwind"},"role":{"name":"Creator"},
   "counterparty":{"name":"Ana Souza","contact_id":"` + contactID + `"},"assigned_to":null,
   "sent_at":null,"updated_at":"2026-08-10T09:30:00+00:00"}
],"meta":{"current_page":1,"last_page":1,"per_page":25,"total":2}}`

// dealsQueries routes /me (one team, id 7) and the deals endpoints, answering
// deals with status and body and recording every deals query in order.
func dealsQueries(status int, body string, queries *[]url.Values) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/v1/me"):
			_, _ = w.Write([]byte(meWith("Acme Agency")))
		case strings.Contains(r.URL.Path, "/deals"):
			*queries = append(*queries, r.URL.Query())
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no route"}`))
		}
	}
}

func contactDealsHarness(t *testing.T, status int, body string) (*harness, *[]url.Values) {
	t.Helper()
	queries := &[]url.Values{}
	h := newHarness(t, dealsQueries(status, body, queries))
	t.Setenv(config.EnvVarToken, "42|token")
	return h, queries
}

func TestDealsListForwardsTheContactFilter(t *testing.T) {
	h, queries := contactDealsHarness(t, http.StatusOK, contactDealsBody)

	_, stderr, code := h.run("deals", "list", "--env", "local", "--contact", contactID)

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if len(*queries) != 1 || (*queries)[0].Get("contact") != contactID {
		t.Errorf("queries = %v, want one carrying contact=%s", *queries, contactID)
	}
}

// --all walks the pages with the same filters, so every page stays narrowed
// to the contact.
func TestDealsListAllKeepsTheContactFilterOnEveryPage(t *testing.T) {
	body := strings.Replace(contactDealsBody, `"last_page":1`, `"last_page":2`, 1)
	h, queries := contactDealsHarness(t, http.StatusOK, body)

	_, stderr, code := h.run("deals", "list", "--env", "local", "--contact", contactID, "--all")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if len(*queries) != 2 {
		t.Fatalf("made %d deals requests, want 2 pages", len(*queries))
	}
	for i, q := range *queries {
		if q.Get("contact") != contactID {
			t.Errorf("page %d query = %v, want contact=%s", i+1, q, contactID)
		}
	}
}

// Under --contact every row is the same person, so COUNTERPARTY would repeat
// on every line; the name moves to the heading, as a uniform project does.
func TestDealsListWithAContactNamesThemInTheHeading(t *testing.T) {
	h, _ := contactDealsHarness(t, http.StatusOK, contactDealsBody)

	stdout, stderr, code := h.run("deals", "list", "--env", "local", "--contact", contactID)

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	wantOut := "" +
		"ID     STATUS              STAGE        PROJECT          BRAND      ROLE          ASSIGNED   UPDATED\n" +
		"K3mQz  Awaiting signature  Contracting  Spring Campaign  Acme       Lead Creator  Dana Reed  2026-08-18\n" +
		"P9wXa  Outreach Sent       Outreach     Autumn Launch    Northwind  Creator       —          2026-08-10\n"
	if stdout != wantOut {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantOut)
	}
	if !strings.HasPrefix(stderr, "local · Acme Agency · with Ana Souza\n") {
		t.Errorf("stderr:\n%s\nwant the heading to name the contact", stderr)
	}
}

// A team can rename the counterparty on one deal. Then the rows disagree, the
// column stays, and the heading names the filter by its id rather than
// picking one of the names.
func TestDealsListWithAContactKeepsTheColumnWhenNamesDiffer(t *testing.T) {
	body := strings.Replace(contactDealsBody, `"counterparty":{"name":"Ana Souza"`, `"counterparty":{"name":"Ana S. (Spring)"`, 1)
	h, _ := contactDealsHarness(t, http.StatusOK, body)

	stdout, stderr, code := h.run("deals", "list", "--env", "local", "--contact", contactID)

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "COUNTERPARTY") || !strings.Contains(stdout, "Ana S. (Spring)") || !strings.Contains(stdout, "Ana Souza") {
		t.Errorf("stdout:\n%s\nwant the COUNTERPARTY column with both names", stdout)
	}
	if !strings.HasPrefix(stderr, "local · Acme Agency · contact "+contactID+"\n") {
		t.Errorf("stderr:\n%s\nwant the heading to name the contact id", stderr)
	}
}

// Without --contact nothing changes: a uniform counterparty is not collapsed,
// so existing output is exactly what it was.
func TestDealsListWithoutAContactKeepsTheCounterpartyColumn(t *testing.T) {
	h, _ := contactDealsHarness(t, http.StatusOK, contactDealsBody)

	stdout, stderr, code := h.run("deals", "list", "--env", "local")

	if code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "COUNTERPARTY") {
		t.Errorf("stdout:\n%s\nwant the COUNTERPARTY column without --contact", stdout)
	}
	if strings.Contains(stderr, "with Ana Souza") {
		t.Errorf("stderr:\n%s\nwant no contact in the heading without --contact", stderr)
	}
}

// The server validates the id: one that is not this team's contact is a 422
// with its own message.
func TestDealsListRelaysAnInvalidContact(t *testing.T) {
	h, _ := contactDealsHarness(t, http.StatusUnprocessableEntity,
		`{"message":"The selected contact is invalid.","errors":{"contact":["The selected contact is invalid."]}}`)

	stdout, stderr, code := h.run("deals", "list", "--env", "local", "--contact", "not-ours")

	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if stderr != "The selected contact is invalid.\n" {
		t.Errorf("stderr = %q, want the server's message", stderr)
	}
}

// Filtering by contact needs access to the team's contacts; without it the
// server refuses, and that is the "ask someone for access" exit code.
func TestDealsListContactFilterWithoutContactsAccess(t *testing.T) {
	h, _ := contactDealsHarness(t, http.StatusForbidden,
		`{"message":"You do not have permission to view contacts on this team.","hint":"Ask a team admin."}`)

	_, stderr, code := h.run("deals", "list", "--env", "local", "--contact", contactID)

	if code != 4 {
		t.Errorf("exit %d, want 4", code)
	}
	if want := "You do not have permission to view contacts on this team.\nAsk a team admin.\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// `deals show` names the contact, so a deal leads to `contacts show` and back
// to `--contact`.
func TestDealsShowNamesTheContact(t *testing.T) {
	for _, tc := range []struct {
		name         string
		counterparty string
		want         string
	}{
		{"a contact of this team", `{"name":"Ana Souza","contact_id":"` + contactID + `"}`, contactID},
		// Not one of the team's contacts, or no access to contacts: the
		// server sends null and does not say which, so neither does the CLI.
		{"not available", `{"name":"Ana Souza","contact_id":null}`, "not available"},
		{"no counterparty at all", `null`, "—"},
	} {
		body := `{"data":{"id":"K3mQz","stage":{"slug":"contracting","label":"Contracting"},
		  "status":null,"project":null,"brand":null,"role":null,
		  "counterparty":` + tc.counterparty + `,"assigned_to":null,"sent_at":null,"updated_at":null}}`
		h, _ := contactDealsHarness(t, http.StatusOK, body)

		stdout, stderr, code := h.run("deals", "show", "K3mQz", "--env", "local")

		if code != 0 {
			t.Fatalf("%s: exit %d, stderr:\n%s", tc.name, code, stderr)
		}
		if want := "Contact       " + tc.want + "\n"; !strings.Contains(stdout, want) {
			t.Errorf("%s: stdout:\n%s\nwant it to contain %q", tc.name, stdout, want)
		}
	}
}
