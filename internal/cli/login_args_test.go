package cli_test

import (
	"strings"
	"testing"
)

// The help text promises the token is never taken from the command line. Cobra
// accepted a positional argument and ignored it, which kept the letter of that
// promise while a token sat in shell history with nothing said — and
// cobra.NoArgs would have refused by printing the value back. The refusal has to
// name the problem without repeating the value.
func TestLoginRefusesAPositionalArgumentWithoutEchoingIt(t *testing.T) {
	h := newHarness(t, okHandler)
	withStdin(t, "")
	const leaked = "14|thisIsATokenThatMustNotBeEchoed0123456789"

	_, stderr, code := h.run("auth", "login", "--env", "local", leaked)

	if code == 0 {
		t.Fatal("a positional argument to login must not exit 0")
	}
	if strings.Contains(stderr, "thisIsAToken") {
		t.Errorf("the refusal echoed the argument:\n%s", stderr)
	}
	if !strings.Contains(stderr, "shell history") {
		t.Errorf("should tell the operator the value is now in their shell history, got:\n%s", stderr)
	}
}
