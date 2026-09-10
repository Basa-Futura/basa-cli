// Package fail owns the CLI's failure vocabulary: the error type, the exit
// codes, and constructors for the situations an operator actually hits.
//
// It lives in its own package so the command tree and the HTTP client can both
// depend on it without depending on each other.
package fail

import (
	"errors"
	"fmt"
	"time"
)

// Exit codes. Deliberately small: the operators are not engineers, and a code
// only earns its place if it changes what someone does next.
//
//	0  it worked
//	1  you typed something wrong   -> fix the command
//	2  it isn't there              -> check the id
//	3  you are not logged in       -> basa auth login
//	4  you are not allowed         -> ask someone for access
//
// 3 and 4 are kept apart because that is the distinction that matters to a
// non-engineer: "log in again" versus "ask a human for access".
const (
	CodeOK        = 0
	CodeUsage     = 1
	CodeNotFound  = 2
	CodeAuth      = 3
	CodeForbidden = 4
)

// Error carries a human sentence, the hint telling the operator what to do
// next, and the exit code the process should end with.
type Error struct {
	Code  int
	Msg   string
	Hint  string
	cause error
}

func (e *Error) Error() string {
	if e.Hint != "" {
		return e.Msg + " " + e.Hint
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.cause }

// CodeOf returns the exit code for an error. Anything untyped is treated as a
// usage error rather than a crash — nothing reaches the operator as a stack
// trace.
func CodeOf(err error) int {
	if err == nil {
		return CodeOK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeUsage
}

// MessageOf returns just the sentence, without the hint appended.
func MessageOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Msg
	}
	return err.Error()
}

// HintOf returns the actionable half of an error, if it has one.
func HintOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Hint
	}
	return ""
}

// Usage is for "you typed something wrong".
func Usage(msg string) *Error {
	return &Error{Code: CodeUsage, Msg: msg}
}

func Usagef(format string, args ...any) *Error {
	return &Error{Code: CodeUsage, Msg: fmt.Sprintf(format, args...)}
}

func UsageHint(msg, hint string) *Error {
	return &Error{Code: CodeUsage, Msg: msg, Hint: hint}
}

func UsageHintf(hint, format string, args ...any) *Error {
	return &Error{Code: CodeUsage, Msg: fmt.Sprintf(format, args...), Hint: hint}
}

func NotFound(msg string) *Error {
	return &Error{Code: CodeNotFound, Msg: msg}
}

// NotLoggedIn is "there is no usable token on this machine", which is a
// different situation from the server rejecting one.
func NotLoggedIn(env string) *Error {
	return &Error{
		Code: CodeAuth,
		Msg:  fmt.Sprintf("You are not logged in to %q.", env),
		Hint: fmt.Sprintf("Run: basa auth login --env %s", env),
	}
}

// TokenRejected covers expired, revoked, and malformed tokens alike. The
// operator does the same thing in every case, so they get the same message.
func TokenRejected(env string) *Error {
	return &Error{
		Code: CodeAuth,
		Msg:  "Your session has expired or been revoked.",
		Hint: fmt.Sprintf("Run: basa auth login --env %s", env),
	}
}

func Forbidden(msg, hint string) *Error {
	return &Error{Code: CodeForbidden, Msg: msg, Hint: hint}
}

// Unreachable is the most common operator failure: VPN off, wrong URL, server
// not running. It names the environment and URL rather than surfacing a
// transport error.
func Unreachable(env, url string) *Error {
	return &Error{
		Code: CodeUsage,
		Msg:  fmt.Sprintf("Could not reach the %s environment at %s.", env, url),
		Hint: fmt.Sprintf("Check you are on the network and the URL is right: basa auth status --env %s", env),
	}
}

// RateLimited is the server's throttle answering back. retryAfter is what the
// server said to wait, which the caller reads from its Retry-After header; zero
// means it did not say, and the operator gets the vaguer sentence rather than an
// invented number.
func RateLimited(retryAfter time.Duration) *Error {
	hint := "Wait a minute and try again."
	if retryAfter > 0 {
		hint = fmt.Sprintf("Wait %s and try again.", waitPhrase(retryAfter))
	}
	return &Error{Code: CodeUsage, Msg: "Too many requests.", Hint: hint}
}

// waitPhrase renders a duration as the largest unit that keeps the number
// small, because "wait 2 minutes" is a sentence someone can act on and "wait
// 118 seconds" is one they have to think about.
//
// It always rounds up. Waiting slightly too long costs nothing; waiting
// slightly too little earns another 429.
func waitPhrase(d time.Duration) string {
	secs := int64((d + time.Second - 1) / time.Second)

	switch {
	case secs < 60:
		return count(secs, "second")
	case secs < 3600:
		return count((secs+59)/60, "minute")
	default:
		return count((secs+3599)/3600, "hour")
	}
}

func count(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func ServerError(env string, status int) *Error {
	return &Error{
		Code: CodeUsage,
		Msg:  fmt.Sprintf("The %s environment returned a server error (%d).", env, status),
		Hint: "This is not something you can fix — report it with the time it happened.",
	}
}

func Wrap(code int, msg string, cause error) *Error {
	return &Error{Code: code, Msg: msg, cause: cause}
}
