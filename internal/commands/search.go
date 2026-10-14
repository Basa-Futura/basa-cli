package commands

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Basa-Futura/basa-cli/internal/client"
	"github.com/Basa-Futura/basa-cli/internal/fail"
	"github.com/Basa-Futura/basa-cli/internal/output"
)

// Where the free-text columns are cut. Names, not sentences, so most never
// reach it; a contract named after its whole template is the usual exception.
// The id column is what makes this tight — a project's is a 36-character
// UUID — and it has to stay, because it is what `show` takes. --json carries
// every value whole.
const (
	searchTitleWidth    = 40
	searchSubtitleWidth = 32
)

// NewSearchCmd builds `basa search`.
func NewSearchCmd(deps *Deps) *cobra.Command {
	var (
		types []string
		limit int
		page  int
	)

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search projects, deals and contracts by name",
		Long: `Search projects, deals and contracts by name, across every team you belong to.

A term matches the names you can see: a project or its brand, a deal's
counterparty, project, brand or role, and a contract or its recipient. It is
a plain, case-insensitive substring — no wildcards, no fuzzy matching — of at
least two characters. A deal found by its brand or role shows a title that
does not contain the term; that is the match, not a mistake.

Unlike the other listings, search does not need --team when you belong to
several: spanning them is the point, and every result names its team. Pass
--team to narrow it to one.

Results come in the server's order, best match first: an exact title, then a
title that starts with the term, then the rest, most recently updated first.`,
		Example: `  basa search acme
  basa search dana reed --type deal
  basa search northwind --team "Acme Agency" --limit 50
  basa search acme --page 2 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fail.UsageHint("What should basa search for?", "Pass a name: basa search <term>")
			}

			// Several words are one term, so a name can be typed unquoted.
			filters := client.SearchFilters{
				Term:  strings.Join(args, " "),
				Types: client.SearchTypes,
			}

			// Whether the flag was typed, not whether it is non-empty: see
			// newDealsListCmd. `--type=` reaches the server, whose 422 answers it.
			if cmd.Flags().Changed("type") {
				filters.Types = append([]string{}, types...)
			}
			if cmd.Flags().Changed("limit") {
				filters.Limit = &limit
			}
			if cmd.Flags().Changed("page") {
				filters.Page = &page
			}

			return runSearch(cmd.Context(), deps, filters)
		},
	}

	cmd.Flags().StringSliceVar(&types, "type", nil, "Only these types: project, deal, contract (comma-separated or repeated)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "How many per page (1-50, default 25)")
	cmd.Flags().IntVar(&page, "page", 0, "Which page of results (default 1)")

	return cmd
}

func runSearch(ctx context.Context, deps *Deps, filters client.SearchFilters) error {
	c, env, err := clientFor(deps)
	if err != nil {
		return err
	}

	// /me is needed for --team, which is matched against your teams' names,
	// and for the table, which names teams where the API gives only ids. A
	// --json search without --team needs neither and makes one request.
	var me *client.Me
	if deps.TeamFlag != "" || !deps.Out.JSON {
		if me, err = c.Me(ctx); err != nil {
			return err
		}
	}

	if deps.TeamFlag != "" {
		team, err := teamFrom(me, deps)
		if err != nil {
			return err
		}
		filters.Team = &team.ID
	}

	if deps.Out.JSON {
		raw, err := c.SearchRaw(ctx, filters)
		if err != nil {
			return searchError(err, env)
		}
		return deps.Out.Data(raw, nil)
	}

	page, err := c.Search(ctx, filters)
	if err != nil {
		return searchError(err, env)
	}

	teamName := teamNamer(me.Teams)

	searched := make([]string, 0, len(page.Meta.Teams))
	for _, id := range page.Meta.Teams {
		searched = append(searched, teamName(id))
	}
	deps.Out.Notice("%s · %q · %s", env, filters.Term, teamsPhrase(searched))

	if len(page.Meta.Teams) == 0 {
		deps.Out.Notice("No team was searched: those teams are disabled, or you cannot list these types in them.")
		return nil
	}

	if page.Meta.Total == 0 {
		deps.Out.Notice("Nothing matched %q.", filters.Term)
		return nil
	}

	oneTeam, uniformTeam := uniformSearchTeam(page.Hits)

	summary := searchTally(page.Meta)
	if uniformTeam && len(page.Meta.Teams) > 1 && len(page.Hits) == page.Meta.Total {
		// The TEAM column is dropped below when every row would repeat it, so
		// say it here instead — but only once all the hits are on screen, or
		// "all in" would be a claim about rows nobody fetched.
		summary += " · all in " + teamName(oneTeam)
	}
	deps.Out.Notice("%s", summary)

	if len(page.Hits) == 0 {
		// A page past the end: the search found things, just not this far.
		deps.Out.Notice("Page %d is past the end: the results fill %d %s.", page.Meta.CurrentPage, page.Meta.LastPage, nounCount(page.Meta.LastPage, "page"))
		return nil
	}

	showType := !uniformSearchType(page.Hits)
	showTeam := !uniformTeam

	var headers []string
	if showType {
		headers = append(headers, "TYPE")
	}
	headers = append(headers, "ID")
	if showTeam {
		headers = append(headers, "TEAM")
	}
	headers = append(headers, "TITLE", "SUBTITLE", "STATUS")

	rows := make([][]string, 0, len(page.Hits))
	for _, hit := range page.Hits {
		var row []string
		if showType {
			row = append(row, hit.Type)
		}
		row = append(row, hit.ID)
		if showTeam {
			row = append(row, teamName(hit.Team))
		}
		rows = append(rows, append(row,
			truncate(hit.Title, searchTitleWidth),
			truncate(derefOr(hit.Subtitle, "—"), searchSubtitleWidth),
			searchStatus(hit),
		))
	}

	if err := deps.Out.Data(nil, func(io.Writer) error {
		return deps.Out.Table(output.Table{Headers: headers, Rows: rows})
	}); err != nil {
		return err
	}

	if page.Meta.Total > len(page.Hits) {
		from := (page.Meta.CurrentPage-1)*page.Meta.PerPage + 1
		to := from + len(page.Hits) - 1
		next := "Narrow it with --type or --team."
		if page.Meta.CurrentPage < page.Meta.LastPage {
			next = fmt.Sprintf("Next: --page %d. Or narrow it with --type or --team.", page.Meta.CurrentPage+1)
		}
		deps.Out.Notice("Showing %d–%d of %d. %s", from, to, page.Meta.Total, next)
	}

	if hint := showHint(page.Hits[0], deps, len(me.Teams) > 1); hint != "" {
		deps.Out.Notice("Show one: %s", hint)
	}

	return nil
}

// searchError rewords the one error search can raise that the rest of the CLI
// would get wrong. The search contract has no 404: an empty search is a 200,
// and a team you cannot read is dropped rather than refused. So a 404 here
// means the route itself is missing — a server from before search — and "that
// does not exist, or you cannot see it" would send the operator hunting for an
// id they never typed.
func searchError(err error, env string) error {
	if fail.CodeOf(err) != fail.CodeNotFound {
		return err
	}
	return fail.UsageHint(
		fmt.Sprintf("The %s environment does not offer search yet.", env),
		"Until it does, list instead: basa projects list, basa deals list, basa contracts list.",
	)
}

// showHint is the command that opens a hit, built from the first one so it
// can be copied as it stands. --env and --team appear only where they would
// be needed: a flag the operator has never had to type is noise in a hint.
// An environment chosen through BASA_ENV still applies to the copied command,
// so only one typed as --env is repeated.
func showHint(hit client.SearchHit, deps *Deps, severalTeams bool) string {
	group := map[string]string{"project": "projects", "deal": "deals", "contract": "contracts"}[hit.Type]
	if group == "" {
		return ""
	}

	hint := fmt.Sprintf("basa %s show %s", group, hit.ID)
	if deps.EnvFlag != "" {
		hint += " --env " + deps.EnvFlag
	}
	if severalTeams {
		hint += fmt.Sprintf(" --team %d", hit.Team)
	}
	return hint
}

// teamNamer names a team id from /me. The API answers in ids; an id /me does
// not list — your membership changed between the two requests — still renders,
// as the bare id rather than as nothing.
func teamNamer(teams []client.Team) func(int64) string {
	names := make(map[int64]string, len(teams))
	for _, t := range teams {
		names[t.ID] = t.Name
	}
	return func(id int64) string {
		if name, ok := names[id]; ok {
			return name
		}
		return fmt.Sprintf("team %d", id)
	}
}

// teamsPhrase names the searched teams in the heading, or counts them once
// listing them would crowd out the line.
func teamsPhrase(names []string) string {
	switch {
	case len(names) == 0:
		return "no teams"
	case len(names) <= 3:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%d teams", len(names))
	}
}

// searchTally is "87 hits · 3 projects · 72 deals · 12 contracts": the whole
// search, not this page. Types in the order the CLI asks for them, then any
// the server counted that this CLI does not know, so none goes unmentioned.
func searchTally(meta client.SearchMeta) string {
	parts := []string{fmt.Sprintf("%d %s", meta.Total, nounCount(meta.Total, "hit"))}

	known := map[string]bool{}
	for _, t := range client.SearchTypes {
		known[t] = true
		if n, ok := meta.Counts[t]; ok {
			parts = append(parts, fmt.Sprintf("%d %s", n, nounCount(n, t)))
		}
	}

	var others []string
	for t := range meta.Counts {
		if !known[t] {
			others = append(others, t)
		}
	}
	sort.Strings(others)
	for _, t := range others {
		parts = append(parts, fmt.Sprintf("%d %s", meta.Counts[t], t))
	}

	return strings.Join(parts, " · ")
}

func uniformSearchType(hits []client.SearchHit) bool {
	for _, h := range hits[1:] {
		if h.Type != hits[0].Type {
			return false
		}
	}
	return true
}

func uniformSearchTeam(hits []client.SearchHit) (int64, bool) {
	if len(hits) == 0 {
		return 0, false
	}
	for _, h := range hits[1:] {
		if h.Team != hits[0].Team {
			return 0, false
		}
	}
	return hits[0].Team, true
}

// searchStatus is the hit's status label. A live project has none, and an
// em dash says so the way every other absent value in this CLI does.
func searchStatus(hit client.SearchHit) string {
	if hit.Status == nil || hit.Status.Label == "" {
		return "—"
	}
	return hit.Status.Label
}
