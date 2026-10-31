package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// testDomains are the organisation's hosts, as keepquiet's default config
// has them. All values are fake test fixtures.
var testDomains = []DomainReplacement{
	{From: "antgroup-inc.cn", To: "Xgroup-inc.cn"},
	{From: "alipay.com", To: "Xpay.com"},
	{From: "antfin.com", To: "Xfin.com"},
	{From: "alibaba-inc.com", To: "Xbb-inc.com"},
}

func TestDomainReplacements(t *testing.T) {
	cases := []struct {
		in, want string
		n        int
	}{
		{"antgroup-inc.cn", "Xgroup-inc.cn", 1},
		{"alipay.com", "Xpay.com", 1},
		{"antfin.com", "Xfin.com", 1},
		{"alibaba-inc.com", "Xbb-inc.com", 1},
		{"ALIPAY.COM", "Xpay.com", 1},
		{"api.alipay.com", "api.Xpay.com", 1},
		{"https://alipay.com/login?next=https://antfin.com/x", "https://Xpay.com/login?next=https://Xfin.com/x", 2},
		{"alice@alipay.com", "alice@Xpay.com", 1},
		{"alipay.com.", "Xpay.com.", 1},
		// whole labels only: a longer domain or a word that has one in it
		// stays
		{"alipay.com.cn", "alipay.com.cn", 0},
		{"myalipay.com", "myalipay.com", 0},
		{"api.antfin.com.example", "api.antfin.com.example", 0},
		{"notalibaba-inc.comx", "notalibaba-inc.comx", 0},
	}
	for _, c := range cases {
		out, n := Mask(c.in, Options{DomainReplacements: testDomains})
		if out != c.want || n != c.n {
			t.Errorf("Mask(%q) = %q, %d; want %q, %d", c.in, out, n, c.want, c.n)
		}
	}
}

func TestDomainReplacementKeepsEmailLocalPart(t *testing.T) {
	in := "alice@alipay.com and bob@gmail.com"
	out, n := Mask(in, Options{Personal: true, DomainReplacements: testDomains})
	if n != 2 || !strings.Contains(out, "alice@Xpay.com") {
		t.Fatalf("Mask(%q) = %q, %d; want alice@Xpay.com and one masked address", in, out, n)
	}
	if strings.Contains(out, "alice@{{") || strings.Contains(out, "alice@alipay.com") {
		t.Fatalf("email local-part was masked: %q", out)
	}
	if strings.Contains(out, "bob@gmail.com") || !strings.Contains(out, "{{EMAIL_") {
		t.Fatalf("ordinary email was not masked: %q", out)
	}
	if back := RestoreDomains(out, false, testDomains); back != in {
		t.Fatalf("RestoreDomains(%q) = %q, want %q", out, back, in)
	}
	if out, n := Mask("ALICE@ALIPAY.COM", Options{Personal: true, DomainReplacements: testDomains}); out != "ALICE@Xpay.com" || n != 1 {
		t.Fatalf("uppercase address = %q, %d; want ALICE@Xpay.com", out, n)
	}

	// The existing false-positive protections stay: a git user, a file's
	// name and a reserved example domain are no email to mask.
	const clean = "git@github.com icon@2x.png user@example.com"
	if out, n := Mask(clean, Options{Personal: true, DomainReplacements: testDomains}); out != clean || n != 0 {
		t.Fatalf("Mask(%q) = %q, %d; want it unchanged", clean, out, n)
	}
}

func TestDomainReplacementWinsKnownEmail(t *testing.T) {
	const addr = "alice@alipay.com"
	// Make the whole address known the way a vault from before this
	// replacement would have it.
	if out, _ := Mask(addr, Options{Personal: true, Secrets: true}); !strings.Contains(out, "{{EMAIL_") {
		t.Fatalf("fixture was not a known email: %q", out)
	}
	out, n := Mask(addr, Options{Personal: true, DomainReplacements: testDomains})
	if out != "alice@Xpay.com" || n != 1 {
		t.Fatalf("known email = %q, %d; want alice@Xpay.com", out, n)
	}
}

func TestReverseDomains(t *testing.T) {
	const in = "alice@Xpay.com api.Xpay.com Xpay.com.cn myXpay.com"
	const want = "alice@alipay.com api.alipay.com Xpay.com.cn myXpay.com"
	if got := ReverseDomains(in, testDomains); got != want {
		t.Fatalf("ReverseDomains(%q) = %q, want %q", in, got, want)
	}
}

func TestMaskJSONDomainReplacementAndSigned(t *testing.T) {
	body := `{"model":"fake","messages":[{"role":"user","content":[{"type":"text","text":"write alice@alipay.com\nvisit https://api.alipay.com/x and antfin.com"}]}],` +
		`"thinking":{"type":"thinking","thinking":"alipay.com","signature":"SIG-alipay.com"},` +
		`"opaque":{"signature":"SIG-alipay.com"},"url":"https://antgroup-inc.cn/a","meta":"alibaba-inc.com"}`
	out, n := MaskJSON([]byte(body), Options{Personal: true, DomainReplacements: testDomains})
	if !json.Valid(out) {
		t.Fatalf("not JSON: %s", out)
	}
	s := string(out)
	for _, want := range []string{"alice@Xpay.com", `\nvisit`, "https://api.Xpay.com/x", "Xfin.com", "https://Xgroup-inc.cn/a", "Xbb-inc.com"} {
		if !strings.Contains(s, want) {
			t.Errorf("%q not in %s", want, s)
		}
	}
	for _, gone := range []string{"alice@alipay.com", "api.alipay.com", "https://antgroup-inc.cn/a", `"meta":"alibaba-inc.com"`} {
		if strings.Contains(s, gone) {
			t.Errorf("%q still in %s", gone, s)
		}
	}
	// A sealed block is checked against its signature and goes byte for
	// byte, replacement host or not.
	for _, signed := range []string{
		`"thinking":{"type":"thinking","thinking":"alipay.com","signature":"SIG-alipay.com"}`,
		`"opaque":{"signature":"SIG-alipay.com"}`,
	} {
		if !strings.Contains(s, signed) || n == 0 {
			t.Fatalf("signed block changed or nothing masked (%d): %s", n, s)
		}
	}
	if back := RestoreJSONDomains(out, testDomains); string(back) != body {
		t.Fatalf("restored:\n%s\nwant:\n%s", back, body)
	}
}
