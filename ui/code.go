package ui

import (
	"regexp"
	"strings"

	"github.com/davitostes/maily/gmail"
)

// Words that mark a message as carrying a login code. Without one of these
// nearby, any number is just a number — an order id, a price, a date.
var codeContextRe = regexp.MustCompile(`(?i)(c[oó]digo|\bcode\b|\botp\b|\b2fa\b|\bmfa\b|one[- ]time|verifica|verify|confirma|autentica|authenticat|\bsenha\b|\btoken\b|\bpin\b|security|seguran[çc]a)`)

// A code is 4-8 digits, or 6-8 characters of uppercase letters and digits.
// The word boundaries matter: they keep a long run (an 18-digit confirmation
// number) from yielding an 8-digit slice of itself.
var codeCandidateRe = regexp.MustCompile(`\b(?:\d{4,8}|[A-Z0-9]{6,8})\b`)

var (
	yearRe  = regexp.MustCompile(`^(?:19|20)\d{2}$`)
	digitRe = regexp.MustCompile(`\d`)
)

// extractCode pulls a verification code out of a message's subject and
// snippet. Empty string means "no code here" — false positives are worse than
// misses, since this silently overwrites the clipboard.
//
// ponytail: subject + snippet only, no extra API call. The snippet is the
// first ~200 chars of the body, which is where these codes live. If a sender
// ever buries one deeper, fetch the full message here.
func extractCode(subject, snippet string) string {
	text := subject + "\n" + snippet
	kw := codeContextRe.FindStringIndex(text)
	if kw == nil {
		return ""
	}

	var before string
	for _, loc := range codeCandidateRe.FindAllStringIndex(text, -1) {
		cand := text[loc[0]:loc[1]]
		if yearRe.MatchString(cand) || !digitRe.MatchString(cand) {
			continue // a year, or a shouty all-letters word
		}
		if loc[0] >= kw[1] {
			return cand // first candidate after the keyword — the usual shape
		}
		if before == "" {
			before = cand // "483920 is your verification code"
		}
	}
	return before
}

// findNewCode returns the first code among freshly arrived messages, newest
// first, along with the sender it came from.
func findNewCode(msgs []gmail.MessageSummary) (code, from string) {
	for _, m := range msgs {
		if c := extractCode(m.Subject, m.Snippet); c != "" {
			return c, strings.TrimSpace(displayFrom(m.From))
		}
	}
	return "", ""
}
