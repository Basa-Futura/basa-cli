package commands

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Basa-Futura/basa-cli/internal/client"
	"github.com/Basa-Futura/basa-cli/internal/fail"
	"github.com/Basa-Futura/basa-cli/internal/output"
)

// messageWidth is where a message is cut in the table.
//
// The message is the only free-text column in this CLI, and tabwriter does not
// wrap — one long sentence would push every other column off the right edge and
// take the alignment of the whole table with it. `show` and `--json` both carry
// the untruncated text, so nothing is lost, only deferred.
//
// The number is arithmetic, not taste. The other columns cost a 36-character
// UUID, a 10-character date, a type of about 16, and two spaces between each:
// 68 before a word of the message is printed. 56 puts the worst line near 124,
// which a 120-column terminal wraps once instead of shredding. The id is what
// makes this tight and it has to stay — it is the argument `show` takes.
const messageWidth = 56

// NewNotificationsCmd builds the `basa notifications` group.
func NewNotificationsCmd(deps *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notifications",
		Aliases: []string{"notification", "notifs"},
		Short:   "List and show your notifications",
		Long: `List and show your notifications.

Unread first and unread only: with no flags this answers "what's new", which is
the tab the web opens on. Pass --read all to see everything, or --read true for
only what you have already seen.

These are YOUR notifications, not a team's. The rows are addressed to you
personally, so unlike every other listing here there is no team to choose and
--team does not apply.

The feed says what happened, not what it happened to: the API publishes the
message text and no deal, contract or project id, so there is nothing to pass
to basa deals show. Read the sentence and search for the deal by name.`,
		Example: `  basa notifications list
  basa notifications list --read all
  basa notifications list --read all --limit 50 --sort-order asc
  basa notifications show 9f2c1111-2222-3333-4444-555566667777`,
	}

	cmd.AddCommand(newNotificationsListCmd(deps), newNotificationsShowCmd(deps))

	return cmd
}

func newNotificationsListCmd(deps *Deps) *cobra.Command {
	var (
		read      string
		sortOrder string
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your notifications, unread only by default",
		Args:  rejectStrayArgs("notifications list", "notifications show <id>"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			var filters client.NotificationFilters

			// Whether the operator typed the flag, not whether what they typed
			// was non-empty — see ProjectFilters.Archived. `--read=` is a shell
			// interpolating an empty variable, which is an invalid request the
			// server should answer, not a silent fall-through to the default.
			if cmd.Flags().Changed("read") {
				filters.Read = &read
			}
			if cmd.Flags().Changed("sort-order") {
				filters.SortOrder = &sortOrder
			}
			if cmd.Flags().Changed("limit") {
				filters.Limit = &limit
			}

			return runNotificationsList(cmd.Context(), deps, filters)
		},
	}

	// A string rather than a bool: the server's parameter has three states.
	cmd.Flags().StringVar(&read, "read", "", `Which to include: "false" for unread (the default), "true" for read only, "all" for both`)
	cmd.Flags().StringVar(&sortOrder, "sort-order", "", `Newest first ("desc", the default) or oldest first ("asc")`)
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "How many to show (1-100, default 25)")

	return cmd
}

func runNotificationsList(ctx context.Context, deps *Deps, filters client.NotificationFilters) error {
	c, env, err := clientFor(deps)
	if err != nil {
		return err
	}

	// No resolveTeam, unlike every sibling listing. The caller is the scope.

	if deps.Out.JSON {
		raw, err := c.NotificationsRaw(ctx, filters)
		if err != nil {
			return err
		}
		return deps.Out.Data(raw, nil)
	}

	page, err := c.Notifications(ctx, filters)
	if err != nil {
		return err
	}

	// Which system, and which slice of the feed. The second half matters more
	// here than anywhere else in this CLI: the default hides read rows, so an
	// empty listing means "nothing unread" and not "nothing ever". Without the
	// heading saying so, silence is ambiguous in the one direction that would
	// send someone looking for a bug.
	deps.Out.Notice("%s · %s", env, readingOf(filters.Read))

	if len(page.Notifications) == 0 {
		deps.Out.Notice("%s", emptyFor(filters.Read))

		return nil
	}

	headers := []string{"ID", "WHEN", "TYPE", "MESSAGE"}
	// Only when the page actually holds both, the way projects treats ARCHIVED.
	// Under the default filter every row is unread, so the column would say the
	// same thing on every line.
	mixedRead := mixedReadState(page.Notifications)
	if mixedRead {
		headers = append(headers, "READ")
	}

	rows := make([][]string, 0, len(page.Notifications))
	for _, n := range page.Notifications {
		row := []string{
			n.ID,
			shortDate(n.CreatedAt),
			derefOr(n.Type, "—"),
			truncate(derefOr(n.Message, "—"), messageWidth),
		}
		if mixedRead {
			row = append(row, yesNo(n.Read))
		}
		rows = append(rows, row)
	}

	if err := deps.Out.Data(nil, func(io.Writer) error {
		return deps.Out.Table(output.Table{Headers: headers, Rows: rows})
	}); err != nil {
		return err
	}

	if page.Meta.Total > len(page.Notifications) {
		deps.Out.Notice("Showing %d of %d. Use --limit to see more.", len(page.Notifications), page.Meta.Total)
	}

	return nil
}

func newNotificationsShowCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one notification, with its full message",
		// At most one, so a bare `notifications show` keeps its own question.
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fail.UsageHint("Which notification?", "Pass its id: basa notifications show <id>")
			}

			return runNotificationsShow(cmd.Context(), deps, args[0])
		},
	}
}

func runNotificationsShow(ctx context.Context, deps *Deps, id string) error {
	c, env, err := clientFor(deps)
	if err != nil {
		return err
	}

	if deps.Out.JSON {
		raw, err := c.NotificationRaw(ctx, id)
		if err != nil {
			return err
		}
		return deps.Out.Data(raw, nil)
	}

	n, err := c.Notification(ctx, id)
	if err != nil {
		return err
	}

	return deps.Out.Data(nil, func(io.Writer) error {
		return deps.Out.Record(output.Record{Fields: []output.Field{
			{Key: "Environment", Value: env},
			{Key: "Notification", Value: n.ID},
			{Key: "Type", Value: derefOr(n.Type, "—")},
			// Untruncated: this is the whole reason to show one.
			{Key: "Message", Value: derefOr(n.Message, "—")},
			{Key: "Read", Value: yesNo(n.Read)},
			// Only meaningful when it is read, and "—" beside Read: no would
			// read as data the server failed to send rather than as nothing to
			// send. Same reasoning as the yesNo comment in projects.go.
			{Key: "Read at", Value: readAtOf(n)},
			{Key: "Received", Value: shortDate(n.CreatedAt)},
		}})
	})
}

// --- formatting ------------------------------------------------------------

// readingOf names the slice of the feed being shown, in the operator's words
// rather than the parameter's. A nil filter is the server's default.
func readingOf(read *string) string {
	if read == nil {
		return "unread"
	}

	switch *read {
	case "true":
		return "read"
	case "all":
		return "all"
	case "false":
		return "unread"
	default:
		// Anything else never reaches here — the server rejects it with a 422
		// before there is a page to head — but a value echoed back beats a
		// heading that quietly claims one of the three known readings.
		return *read
	}
}

// emptyFor answers the empty listing in terms of what was asked for, so "no
// unread notifications" cannot be misread as "no notifications".
func emptyFor(read *string) string {
	if read != nil && (*read == "true" || *read == "all") {
		return "No notifications matched."
	}
	return "Nothing unread. Use --read all to see the rest."
}

// readAtOf renders the read timestamp, or says plainly that there is not one.
func readAtOf(n *client.Notification) string {
	if !n.Read {
		return "not read yet"
	}
	return shortDate(n.ReadAt)
}

// mixedReadState reports whether the page holds both read and unread rows.
func mixedReadState(notifications []client.Notification) bool {
	if len(notifications) == 0 {
		return false
	}
	first := notifications[0].Read
	for _, n := range notifications[1:] {
		if n.Read != first {
			return true
		}
	}
	return false
}

// truncate cuts a string to max characters, marking the cut with an ellipsis.
//
// Counted in RUNES, not bytes. Notification messages are server-rendered and
// already translated — resources/lang carries en, es and pt — so a byte slice
// would land mid-character on any accented word and emit invalid UTF-8 into the
// operator's terminal.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
