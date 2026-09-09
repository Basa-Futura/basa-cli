package commands

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Basa-Futura/basa-cli/internal/client"
	"github.com/Basa-Futura/basa-cli/internal/config"
	"github.com/Basa-Futura/basa-cli/internal/fail"
	"github.com/Basa-Futura/basa-cli/internal/output"
)

// Deps is what every command needs: resolved config, a renderer, and the
// environment the operator asked for.
type Deps struct {
	Config   *config.Config
	Out      *output.Writer
	EnvFlag  string
	TeamFlag string
}

// NewMeCmd builds `basa me`.
func NewMeCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "Show your account, teams, and current token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMe(cmd.Context(), deps)
		},
	}
}

// runMe backs both `basa me` and `basa auth status` — the same call answers
// "who am I" and "does my token still work".
func runMe(ctx context.Context, deps *Deps) error {
	c, env, err := clientFor(deps)
	if err != nil {
		return err
	}

	if deps.Out.JSON {
		raw, err := c.MeRaw(ctx)
		if err != nil {
			return err
		}
		return deps.Out.Data(raw, nil)
	}

	me, err := c.Me(ctx)
	if err != nil {
		return err
	}

	return deps.Out.Data(nil, func(w io.Writer) error {
		// When the server reports no explicit expiry, the token is governed by
		// the server's own session limit — and the client does not know what
		// that is. Naming a duration here would duplicate a server setting and
		// then quietly lie the moment it changed, which is worse than saying
		// nothing: an operator would trust a number the client invented.
		expires := "when the server's session limit is reached"
		if me.Token.ExpiresAt != nil && *me.Token.ExpiresAt != "" {
			expires = *me.Token.ExpiresAt
		}

		teams := make([]string, 0, len(me.Teams))
		for _, t := range me.Teams {
			teams = append(teams, fmt.Sprintf("%s (%d)", t.Name, t.ID))
		}
		if len(teams) == 0 {
			teams = append(teams, "none")
		}

		return deps.Out.Record(output.Record{Fields: []output.Field{
			{Key: "Environment", Value: env},
			{Key: "Name", Value: me.Name},
			{Key: "Email", Value: me.Email},
			{Key: "User ID", Value: strconv.FormatInt(me.ID, 10)},
			{Key: "Teams", Value: strings.Join(teams, ", ")},
			{Key: "Token", Value: me.Token.Name},
			{Key: "Can", Value: strings.Join(me.Token.Abilities, ", ")},
			{Key: "Expires", Value: expires},
		}})
	})
}

// clientFor resolves the environment and its stored token into a client.
func clientFor(deps *Deps) (*client.Client, string, error) {
	env, environment, err := deps.Config.Resolve(deps.EnvFlag)
	if err != nil {
		return nil, "", fail.Usage(capitalize(err.Error()) + ".")
	}

	token, err := deps.Config.Token(env)
	if err != nil {
		return nil, "", fail.NotLoggedIn(env)
	}

	return client.New(env, environment.URL, token), env, nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
