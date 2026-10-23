package notify

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	ghp := "ghp_" + strings.Repeat("a", 36)
	gho := "gho_" + strings.Repeat("a", 36)
	ghu := "ghu_" + strings.Repeat("a", 36)
	ghs := "ghs_" + strings.Repeat("a", 36)
	ghr := "ghr_" + strings.Repeat("a", 36)
	pat := "github_pat_" + strings.Repeat("a", 22)
	openai := "sk-" + strings.Repeat("a", 20)
	anthropic := "sk-ant-" + strings.Repeat("a", 20)
	xai := "xai-" + strings.Repeat("a", 20)
	slack := "xoxb-" + strings.Repeat("1", 20)
	aws := "AKIAIOSFODNN7EXAMPLE"
	awsTemp := "ASIAIOSFODNN7EXAMPLE"
	google := "AIza" + strings.Repeat("A", 35)
	stripeSecret := "sk_live_" + strings.Repeat("a", 16)
	stripeRestricted := "rk_live_" + strings.Repeat("a", 16)
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	bearer := "Bearer " + strings.Repeat("a", 16)
	entropy := "Zx9Qm4Lp2Nv8Kb6Hj1Gf3Ds7Aa0Ww5Ee"
	sha40 := "0123456789abcdef0123456789abcdef01234567"
	sha64 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	shaMixed := "0123456789AbCdEf0123456789AbCdEf01234567"
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	uuidUpper := "550E8400-E29B-41D4-A716-446655440000"

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"github personal access token", "use " + ghp + " now", "use [redacted] now"},
		{"github oauth token", "use " + gho + " now", "use [redacted] now"},
		{"github user token", "use " + ghu + " now", "use [redacted] now"},
		{"github server token", "use " + ghs + " now", "use [redacted] now"},
		{"github refresh token", "use " + ghr + " now", "use [redacted] now"},
		{"github fine-grained pat", "use " + pat + " now", "use [redacted] now"},
		{"openai key", "use " + openai + " now", "use [redacted] now"},
		{"anthropic key", "use " + anthropic + " now", "use [redacted] now"},
		{"xai key", "use " + xai + " now", "use [redacted] now"},
		{"slack bot token", "use " + slack + " now", "use [redacted] now"},
		{"slack app token", "use xoxa-" + strings.Repeat("1", 20) + " now", "use [redacted] now"},
		{"slack user token", "use xoxp-" + strings.Repeat("1", 20) + " now", "use [redacted] now"},
		{"slack refresh token", "use xoxr-" + strings.Repeat("1", 20) + " now", "use [redacted] now"},
		{"slack session token", "use xoxs-" + strings.Repeat("1", 20) + " now", "use [redacted] now"},
		{"aws access key", "use " + aws + " now", "use [redacted] now"},
		{"aws temporary key", "use " + awsTemp + " now", "use [redacted] now"},
		{"google api key", "use " + google + " now", "use [redacted] now"},
		{"stripe secret key", "use " + stripeSecret + " now", "use [redacted] now"},
		{"stripe restricted key", "use " + stripeRestricted + " now", "use [redacted] now"},
		{"jwt", "token " + jwt + " end", "token [redacted] end"},
		{"pem private key", "-----BEGIN PRIVATE KEY-----", "[redacted]"},
		{"pem rsa private key", "-----BEGIN RSA PRIVATE KEY-----", "[redacted]"},
		{"pem ec private key", "-----BEGIN EC PRIVATE KEY-----", "[redacted]"},
		{"pem openssh private key", "-----BEGIN OPENSSH PRIVATE KEY-----", "[redacted]"},
		{"pem encrypted private key", "-----BEGIN ENCRYPTED PRIVATE KEY-----", "[redacted]"},
		{"two assignments", "password=hunter2 token=sekrit", "password=[redacted] token=[redacted]"},
		{"password assignment", "password=hunter2", "password=[redacted]"},
		{"passwd assignment", "passwd: s3cret", "passwd: [redacted]"},
		{"pwd assignment", "pwd=x", "pwd=[redacted]"},
		{"secret assignment", "secret=top", "secret=[redacted]"},
		{"token assignment keeps the key", "TOKEN=abc", "TOKEN=[redacted]"},
		{"my_token assignment", "my_token=abc", "my_token=[redacted]"},
		{"api_key assignment", "api_key=abc", "api_key=[redacted]"},
		{"api-key assignment", "api-key=abc", "api-key=[redacted]"},
		{"apikey assignment", "apikey=abc", "apikey=[redacted]"},
		{"auth assignment", "auth=letmein", "auth=[redacted]"},
		{"export github token", "export GITHUB_TOKEN=" + ghp, "export GITHUB_TOKEN=[redacted]"},
		{"token inside a url", "see https://x.test/p?token=abc", "see https://x.test/p?token=[redacted]"},
		{"credentialed url", "clone https://alice:s3cret@example.com/a/b", "clone https://alice:[redacted]@example.com/a/b"},
		{"credentialed url keeps a query token", "https://alice:s3cret@example.com/p?token=abc", "https://alice:[redacted]@example.com/p?token=[redacted]"},
		{"port is not a password", "https://example.com:8443/status", "https://example.com:8443/status"},
		{"quoted password", `password="my secret pass"`, "password=[redacted]"},
		{"quoted token", "token: 'a b c'", "token: [redacted]"},
		{"export quoted api key", `export API_KEY="my secret pass"`, "export API_KEY=[redacted]"},
		{"quoted plain assignment", `author="Jane Doe"`, `author="Jane Doe"`},
		{"assignment after a plain one", "author=Jane token=abc", "author=Jane token=[redacted]"},
		{"bearer token", "Authorization: " + bearer, "Authorization: [redacted]"},
		{"bearer any case", "authorization: bearer " + strings.Repeat("b", 16), "authorization: [redacted]"},
		{"high entropy run", "secret " + entropy + " end", "secret [redacted] end"},
		{"three classes at 32", strings.Repeat("g", 30) + "A+", "[redacted]"},

		{"git sha", "commit " + sha40 + " landed", "commit " + sha40 + " landed"},
		{"git sha256", "commit " + sha64 + " landed", "commit " + sha64 + " landed"},
		{"mixed-case git sha", "commit " + shaMixed + " landed", "commit " + shaMixed + " landed"},
		{"uuid", "id " + uuid + " ok", "id " + uuid + " ok"},
		{"uppercase uuid", "id " + uuidUpper + " ok", "id " + uuidUpper + " ok"},
		{"unix path", "edit /usr/local/go/bin/gofmt please", "edit /usr/local/go/bin/gofmt please"},
		{"repo path", "see internal/notify/redact.go", "see internal/notify/redact.go"},
		{"windows path", `edit C:\Users\me\src\api please`, `edit C:\Users\me\src\api please`},
		{"entropy inside a path", "/tmp/" + entropy + "/file", "/tmp/" + entropy + "/file"},
		{"entropy inside a windows path", `C:\temp\` + entropy + `\file`, `C:\temp\` + entropy + `\file`},
		{"prose", "Do you want to create probe.txt?", "Do you want to create probe.txt?"},
		{"pem public key", "-----BEGIN PUBLIC KEY-----", "-----BEGIN PUBLIC KEY-----"},
		{"password without a value", "password reset flow", "password reset flow"},
		{"short github token", "ghp_abc", "ghp_abc"},
		{"author assignment", "author=Jane", "author=Jane"},
		{"authentication assignment", "authentication=yes", "authentication=yes"},
		{"tokenizer assignment", "tokenizer=fast", "tokenizer=fast"},
		{"bearer of bad news", "Bearer of bad news", "Bearer of bad news"},
		{"one class run", strings.Repeat("x", 64), strings.Repeat("x", 64)},
		{"two class run", strings.Repeat("g", 31) + "1", strings.Repeat("g", 31) + "1"},
		{"sk inside a word", "ask-" + strings.Repeat("a", 26), "ask-" + strings.Repeat("a", 26)},
		{"aws key with an extra letter", aws + "X", aws + "X"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redactSecrets(test.in); got != test.want {
				t.Fatalf("redactSecrets(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
