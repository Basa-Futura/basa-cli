package commands

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Basa-Futura/basa-cli/internal/client"
	"github.com/Basa-Futura/basa-cli/internal/fail"
	"github.com/Basa-Futura/basa-cli/internal/output"
)

// Where the contacts table cuts its free-text columns. The id is a 36-character
// UUID and has to stay whole — it is what `contacts show` takes — so the rest
// share what is left of a terminal. `show` and --json carry every value whole.
const (
	contactNameWidth      = 28
	contactEmailWidth     = 32
	contactPlatformsWidth = 30
	contactTagsWidth      = 30
)

// NewContactsCmd builds the `basa contacts` group.
func NewContactsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "contacts",
		Aliases: []string{"contact"},
		Short:   "List and show the team's contacts",
		Long: `List and show the team's contacts — the web's Talent Network.

Every contact is shown the way your team sees them: if your team renamed
someone or gave them another email, that is what you see, and what --search
matches. Another team's notes, tags or overrides on the same person never
appear.

Phone numbers, rates and deal history are not here on purpose: rates and
history are deal data, and live under basa deals. To find who is on a
campaign, filter by project: basa contacts list --project <project id>.`,
		Example: `  basa contacts list --env staging
  basa contacts list --env staging --search ana
  basa contacts list --env staging --project 9f2c1111-2222-3333-4444-555566667777
  basa contacts list --env staging --sort-by updated_at --sort-order desc
  basa contacts show 9f3c2a4e-0d51-4b7e-9a6b-2f1c8e7d5a10 --env staging`,
	}

	cmd.AddCommand(newContactsListCmd(deps), newContactsShowCmd(deps))

	return cmd
}

func newContactsListCmd(deps *Deps) *cobra.Command {
	var (
		search    string
		project   string
		sortBy    string
		sortOrder string
		limit     int
		page      int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the team's contacts",
		Args:  rejectStrayArgs("contacts list", "contacts show <id>"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			filters := client.ContactFilters{Search: search, Project: project}

			// Whether each flag was typed, not whether it is non-empty, so an
			// empty or out-of-range value reaches the server and its 422 answers
			// it — see newDealsListCmd.
			if cmd.Flags().Changed("sort-by") {
				filters.SortBy = &sortBy
			}
			if cmd.Flags().Changed("sort-order") {
				filters.SortOrder = &sortOrder
			}
			if cmd.Flags().Changed("limit") {
				filters.Limit = &limit
			}
			if cmd.Flags().Changed("page") {
				filters.Page = &page
			}

			return runContactsList(cmd.Context(), deps, filters)
		},
	}

	cmd.Flags().StringVar(&search, "search", "", "Only those whose name or email contains this")
	cmd.Flags().StringVar(&project, "project", "", "Only those with a deal on this project (a project id)")
	cmd.Flags().StringVar(&sortBy, "sort-by", "", `Order by "name" (the default), "email" or "updated_at"`)
	cmd.Flags().StringVar(&sortOrder, "sort-order", "", `"asc" (the default) or "desc"`)
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "How many per page (1-100, default 25)")
	cmd.Flags().IntVar(&page, "page", 0, "Which page (default 1)")

	return cmd
}

func runContactsList(ctx context.Context, deps *Deps, filters client.ContactFilters) error {
	c, env, err := clientFor(deps)
	if err != nil {
		return err
	}

	team, err := resolveTeam(ctx, deps, c)
	if err != nil {
		return err
	}

	if deps.Out.JSON {
		raw, err := c.ContactsRaw(ctx, team.ID, filters)
		if err != nil {
			return contactsListError(err, env)
		}
		return deps.Out.Data(raw, nil)
	}

	page, err := c.Contacts(ctx, team.ID, filters)
	if err != nil {
		return contactsListError(err, env)
	}

	// Which system, which team, and which slice. The project filter is named
	// here because an empty result means something different under it: it
	// matches only through deals you may list, so "nobody" can also mean "no
	// deals you can see".
	heading := env + " · " + team.Name
	if filters.Project != "" {
		heading += " · project " + filters.Project
	}
	deps.Out.Notice("%s", heading)

	if len(page.Contacts) == 0 {
		if page.Meta.Total > 0 {
			deps.Out.Notice("Page %d is past the end: the contacts fill %d %s.", page.Meta.CurrentPage, page.Meta.LastPage, nounCount(page.Meta.LastPage, "page"))
		} else {
			deps.Out.Notice("No contacts matched.")
		}
		return nil
	}

	// A column that would be a dash on every row says nothing, so it goes, the
	// way empty columns collapse elsewhere in this CLI.
	showPlatforms, showTags := false, false
	for _, ct := range page.Contacts {
		showPlatforms = showPlatforms || len(ct.Platforms) > 0
		showTags = showTags || len(ct.Tags) > 0
	}

	headers := []string{"ID", "NAME", "EMAIL"}
	if showPlatforms {
		headers = append(headers, "PLATFORMS")
	}
	if showTags {
		headers = append(headers, "TAGS")
	}
	headers = append(headers, "UPDATED")

	rows := make([][]string, 0, len(page.Contacts))
	for _, ct := range page.Contacts {
		row := []string{
			ct.ID,
			truncate(derefOr(ct.Name, "—"), contactNameWidth),
			truncate(derefOr(ct.Email, "—"), contactEmailWidth),
		}
		if showPlatforms {
			row = append(row, truncate(orDash(platformsSummary(ct.Platforms)), contactPlatformsWidth))
		}
		if showTags {
			row = append(row, truncate(orDash(tagsSummary(ct)), contactTagsWidth))
		}
		rows = append(rows, append(row, shortDate(ct.UpdatedAt)))
	}

	if err := deps.Out.Data(nil, func(io.Writer) error {
		return deps.Out.Table(output.Table{Headers: headers, Rows: rows})
	}); err != nil {
		return err
	}

	if page.Meta.Total > len(page.Contacts) {
		from := (page.Meta.CurrentPage-1)*page.Meta.PerPage + 1
		to := from + len(page.Contacts) - 1
		next := "Narrow it with --search or --project."
		if page.Meta.CurrentPage < page.Meta.LastPage {
			next = fmt.Sprintf("Next: --page %d. Or narrow it with --search or --project.", page.Meta.CurrentPage+1)
		}
		deps.Out.Notice("Showing %d–%d of %d. %s", from, to, page.Meta.Total, next)
	}

	return nil
}

// contactsListError rewords a 404 from the listing. The team in the path came
// from your own /me a moment ago, and the server answers 404 on a team route
// only for a team that is not yours — so a 404 here means the route itself is
// missing: a server from before contacts (production, until it is deployed).
// "That does not exist, or you cannot see it" would send the operator looking
// for a mistake they did not make. A 404 from `show` is left alone; there it
// means what it says.
func contactsListError(err error, env string) error {
	if fail.CodeOf(err) != fail.CodeNotFound {
		return err
	}
	return fail.UsageHint(
		fmt.Sprintf("The %s environment does not offer contacts yet.", env),
		"It needs a newer Basa server; the web's Talent Network has the same list.",
	)
}

func newContactsShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one contact, with your team's notes",
		// At most one, so a bare `contacts show` keeps its own question.
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fail.UsageHint("Which contact?", "Pass its id: basa contacts show <id> --env <name>")
			}

			return runContactsShow(cmd.Context(), deps, args[0])
		},
	}
}

func runContactsShow(ctx context.Context, deps *Deps, id string) error {
	c, env, err := clientFor(deps)
	if err != nil {
		return err
	}

	team, err := resolveTeam(ctx, deps, c)
	if err != nil {
		return err
	}

	if deps.Out.JSON {
		raw, err := c.ContactRaw(ctx, team.ID, id)
		if err != nil {
			return err
		}
		return deps.Out.Data(raw, nil)
	}

	ct, err := c.Contact(ctx, team.ID, id)
	if err != nil {
		return err
	}

	fields := []output.Field{
		{Key: "Environment", Value: env},
		{Key: "Team", Value: team.Name},
		{Key: "Contact", Value: ct.ID},
		{Key: "Name", Value: derefOr(ct.Name, "—")},
		{Key: "Email", Value: derefOr(ct.Email, "—")},
	}

	// One line per platform: a person's socials are the point of a talent
	// network, and a single joined line would be cut long before the third.
	if len(ct.Platforms) == 0 {
		fields = append(fields, output.Field{Key: "Platforms", Value: "none"})
	}
	for i, p := range ct.Platforms {
		key := ""
		if i == 0 {
			key = "Platforms"
		}
		fields = append(fields, output.Field{Key: key, Value: platformLine(p)})
	}

	fields = append(fields,
		output.Field{Key: "Tags", Value: orNone(tagsSummary(*ct))},
		output.Field{Key: "Updated", Value: shortDate(ct.UpdatedAt)},
	)

	// Free text the team typed, so it can run to several lines. Each line gets
	// its own row under the one label, or the second line would start in the
	// label column and read as a field of its own.
	notes := strings.Split(strings.TrimRight(derefOr(ct.Notes, ""), "\n"), "\n")
	if len(notes) == 1 && notes[0] == "" {
		notes = []string{"none"}
	}
	for i, line := range notes {
		key := ""
		if i == 0 {
			key = "Notes"
		}
		fields = append(fields, output.Field{Key: key, Value: line})
	}

	return deps.Out.Data(nil, func(io.Writer) error {
		return deps.Out.Record(output.Record{Fields: fields})
	})
}

// --- formatting ------------------------------------------------------------

// platformsSummary is the table's one-cell view of someone's socials: each
// platform with its audience, the one the team marked preferred first.
func platformsSummary(platforms []client.ContactPlatform) string {
	ordered := make([]client.ContactPlatform, 0, len(platforms))
	for _, p := range platforms {
		if p.Preferred {
			ordered = append(ordered, p)
		}
	}
	for _, p := range platforms {
		if !p.Preferred {
			ordered = append(ordered, p)
		}
	}

	parts := make([]string, 0, len(ordered))
	for _, p := range ordered {
		part := p.Platform
		if p.Followers != nil {
			part += " " + compactCount(*p.Followers)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// platformLine is one platform in full, for `show`.
func platformLine(p client.ContactPlatform) string {
	parts := []string{p.Platform}
	if p.Handle != nil && *p.Handle != "" {
		parts = append(parts, *p.Handle)
	}
	if p.Followers != nil {
		parts = append(parts, groupDigits(*p.Followers)+" "+nounCount(int(*p.Followers), "follower"))
	}
	// Stored as a whole percent ("3.1" is 3.1%), and the web prints it
	// verbatim with a % after it. Doing the same keeps the two in step; a
	// value the team typed with its own % is left alone rather than doubled.
	if p.EngagementRate != nil && *p.EngagementRate != "" {
		rate := *p.EngagementRate
		if !strings.HasSuffix(rate, "%") {
			rate += "%"
		}
		parts = append(parts, rate+" engagement")
	}
	if p.Preferred {
		parts = append(parts, "preferred")
	}
	return strings.Join(parts, " · ")
}

// tagsSummary is "Tier: Priority, Region: LATAM", in the server's order (by
// type, then name). A tag with no type is just its name.
func tagsSummary(ct client.Contact) string {
	parts := make([]string, 0, len(ct.Tags))
	for _, t := range ct.Tags {
		if t.Type != nil && *t.Type != "" {
			parts = append(parts, *t.Type+": "+t.Name)
		} else {
			parts = append(parts, t.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// compactCount is an audience size the way people say it: 950, 48.2K, 1.3M.
// For a table cell only; `show` prints the exact number.
//
// Integer arithmetic, rounding halves up. Float formatting rounds halves to
// even, so 1,250,000 came out as "1.2M", and where 999,950 lands would depend
// on binary representation. Here it is exact: 999,950 rounds to 1,000.0K,
// which is said as 1M.
func compactCount(n int64) string {
	if n < 1_000 {
		return strconv.FormatInt(n, 10)
	}
	if t := (n + 50) / 100; t < 10_000 {
		return tenths(t) + "K"
	}
	return tenths((n+50_000)/100_000) + "M"
}

// tenths renders a count of tenths with one decimal place, dropping a ".0".
func tenths(t int64) string {
	if t%10 == 0 {
		return strconv.FormatInt(t/10, 10)
	}
	return fmt.Sprintf("%d.%d", t/10, t%10)
}

// groupDigits writes 48200 as "48,200".
func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// orNone is for a list the server sent and that happens to be empty, where a
// dash would read as data it failed to send.
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
