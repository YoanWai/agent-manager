package notify

import (
	"regexp"
	"strings"
)

// redactSecrets strips likely credentials from text the banner and the
// notify command quote. The session row is built on another path and
// keeps the original line.
func redactSecrets(s string) string {
	s = redactAssignments(s)
	for _, pattern := range credentialPatterns {
		s = pattern.ReplaceAllString(s, "[redacted]")
	}
	return redactHighEntropy(s)
}

// credentialPatterns are the token shapes a quote can carry verbatim.
// The Anthropic prefix is listed before the OpenAI one, which would
// otherwise swallow it, and a Bearer phrase before the JWT it may wrap.
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}`),
	regexp.MustCompile(`\bgh(?:p|o|u|s|r)_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bxai-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{20,}`),
	regexp.MustCompile(`\bsk_live_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`\brk_live_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35,}`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}={0,2}\.[A-Za-z0-9_-]{10,}={0,2}\.[A-Za-z0-9_-]{10,}={0,2}`),
}

// An assignment redacts its value only. The key stays, so the banner
// still says which setting was quoted.
var assignmentPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([A-Za-z0-9]+(?:[_-][A-Za-z0-9]+)*)(\s*[=:]\s*)(\S+)`)

func redactAssignments(s string) string {
	matches := assignmentPattern.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for _, match := range matches {
		if !keyIsSecret(s[match[2]:match[3]]) {
			continue
		}
		b.WriteString(s[last:match[6]])
		b.WriteString("[redacted]")
		last = match[7]
	}
	b.WriteString(s[last:])
	return b.String()
}

func keyIsSecret(key string) bool {
	parts := strings.FieldsFunc(strings.ToLower(key), func(r rune) bool {
		return r == '_' || r == '-'
	})
	for i, part := range parts {
		switch part {
		case "password", "passwd", "pwd", "secret", "token", "auth", "apikey":
			return true
		}
		if part == "api" && i+1 < len(parts) && parts[i+1] == "key" {
			return true
		}
	}
	return false
}

// A long mixed run is treated as a secret. Hex (git SHAs, including
// mixed case), UUIDs, and path segments are the long runs a quote
// normally carries, so they stay. Two classes are not enough: a SHA is
// already letters and digits.
var entropyRun = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)
var uuidRun = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func redactHighEntropy(s string) string {
	spans := entropyRun.FindAllStringIndex(s, -1)
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for _, span := range spans {
		if !secretRun(s, span[0], span[1]) {
			continue
		}
		b.WriteString(s[last:span[0]])
		b.WriteString("[redacted]")
		last = span[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func secretRun(s string, start, end int) bool {
	// A backslash is not part of the run, so a Windows path segment
	// shows up as a neighbor. A slash is part of the run (base64 uses
	// it), so a Unix path is a run that contains one.
	if start > 0 && s[start-1] == '\\' {
		return false
	}
	if end < len(s) && s[end] == '\\' {
		return false
	}
	run := s[start:end]
	if strings.Contains(run, "/") || hexRun(run) || uuidRun.MatchString(run) {
		return false
	}
	return classCount(run) >= 3
}

func hexRun(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func classCount(s string) int {
	var lower, upper, digit, symbol bool
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= '0' && c <= '9':
			digit = true
		default:
			symbol = true
		}
	}
	n := 0
	if lower {
		n++
	}
	if upper {
		n++
	}
	if digit {
		n++
	}
	if symbol {
		n++
	}
	return n
}
