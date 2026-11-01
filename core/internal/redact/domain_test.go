package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// testDomains are fictional fixtures only. They stand in for hosts a user
// might set locally; they are not keepquiet's default.
var testDomains = []DomainReplacement{
	{From: "northwind.cn", To: "harbor.cn"},
	{From: "contoso.com", To: "lumen.com"},
	{From: "fabrikam.net", To: "cedar.net"},
	{From: "adventure-works.io", To: "ridge-works.io"},
}

func TestDomainReplacements(t *testing.T) {
	cases := []struct {
		in, want string
		n        int
	}{
		{"northwind.cn", "harbor.cn", 1},
		{"contoso.com", "lumen.com", 1},
		{"fabrikam.net", "cedar.net", 1},
		{"adventure-works.io", "ridge-works.io", 1},
		{"CONTOSO.COM", "lumen.com", 1},
		{"api.contoso.com", "api.lumen.com", 1},
		{"https://contoso.com/login?next=https://fabrikam.net/x", "https://lumen.com/login?next=https://cedar.net/x", 2},
		{"alice@contoso.com", "alice@lumen.com", 1},
		{"contoso.com.", "lumen.com.", 1},
		// whole labels only: a longer domain or a word that has one in it
		// stays
		{"contoso.com.cn", "contoso.com.cn", 0},
		{"mycontoso.com", "mycontoso.com", 0},
		{"api.fabrikam.net.example", "api.fabrikam.net.example", 0},
		{"notadventure-works.iox", "notadventure-works.iox", 0},
	}
	for _, c := range cases {
		out, n := Mask(c.in, Options{DomainReplacements: testDomains})
		if out != c.want || n != c.n {
			t.Errorf("Mask(%q) = %q, %d; want %q, %d", c.in, out, n, c.want, c.n)
		}
	}
}

func TestDomainReplacementKeepsEmailLocalPart(t *testing.T) {
	in := "alice@contoso.com and bob@gmail.com"
	out, n := Mask(in, Options{Personal: true, DomainReplacements: testDomains})
	if n != 2 || !strings.Contains(out, "alice@lumen.com") {
		t.Fatalf("Mask(%q) = %q, %d; want alice@lumen.com and one masked address", in, out, n)
	}
	if strings.Contains(out, "alice@{{") || strings.Contains(out, "alice@contoso.com") {
		t.Fatalf("email local-part was masked: %q", out)
	}
	if strings.Contains(out, "bob@gmail.com") || !strings.Contains(out, "{{EMAIL_") {
		t.Fatalf("ordinary email was not masked: %q", out)
	}
	if back := RestoreDomains(out, false, testDomains); back != in {
		t.Fatalf("RestoreDomains(%q) = %q, want %q", out, back, in)
	}
	if out, n := Mask("ALICE@CONTOSO.COM", Options{Personal: true, DomainReplacements: testDomains}); out != "ALICE@lumen.com" || n != 1 {
		t.Fatalf("uppercase address = %q, %d; want ALICE@lumen.com", out, n)
	}

	// The existing false-positive protections stay: a git user, a file's
	// name and a reserved example domain are no email to mask.
	const clean = "git@github.com icon@2x.png user@example.com"
	if out, n := Mask(clean, Options{Personal: true, DomainReplacements: testDomains}); out != clean || n != 0 {
		t.Fatalf("Mask(%q) = %q, %d; want it unchanged", clean, out, n)
	}
}

func TestDomainReplacementWinsKnownEmail(t *testing.T) {
	const addr = "alice@contoso.com"
	// Make the whole address known the way a vault from before this
	// replacement would have it.
	if out, _ := Mask(addr, Options{Personal: true, Secrets: true}); !strings.Contains(out, "{{EMAIL_") {
		t.Fatalf("fixture was not a known email: %q", out)
	}
	out, n := Mask(addr, Options{Personal: true, DomainReplacements: testDomains})
	if out != "alice@lumen.com" || n != 1 {
		t.Fatalf("known email = %q, %d; want alice@lumen.com", out, n)
	}
}

func TestReverseDomains(t *testing.T) {
	const in = "alice@lumen.com api.lumen.com lumen.com.cn mylumen.com"
	const want = "alice@contoso.com api.contoso.com lumen.com.cn mylumen.com"
	if got := ReverseDomains(in, testDomains); got != want {
		t.Fatalf("ReverseDomains(%q) = %q, want %q", in, got, want)
	}
}

func TestMaskJSONDomainReplacementAndSigned(t *testing.T) {
	body := `{"model":"fake","messages":[{"role":"user","content":[{"type":"text","text":"write alice@contoso.com\nvisit https://api.contoso.com/x and fabrikam.net"}]}],` +
		`"thinking":{"type":"thinking","thinking":"contoso.com","signature":"SIG-contoso.com"},` +
		`"opaque":{"signature":"SIG-contoso.com"},"url":"https://northwind.cn/a","meta":"adventure-works.io"}`
	out, n := MaskJSON([]byte(body), Options{Personal: true, DomainReplacements: testDomains})
	if !json.Valid(out) {
		t.Fatalf("not JSON: %s", out)
	}
	s := string(out)
	for _, want := range []string{"alice@lumen.com", `\nvisit`, "https://api.lumen.com/x", "cedar.net", "https://harbor.cn/a", "ridge-works.io"} {
		if !strings.Contains(s, want) {
			t.Errorf("%q not in %s", want, s)
		}
	}
	for _, gone := range []string{"alice@contoso.com", "api.contoso.com", "https://northwind.cn/a", `"meta":"adventure-works.io"`} {
		if strings.Contains(s, gone) {
			t.Errorf("%q still in %s", gone, s)
		}
	}
	// A sealed block is checked against its signature and goes byte for
	// byte, replacement host or not.
	for _, signed := range []string{
		`"thinking":{"type":"thinking","thinking":"contoso.com","signature":"SIG-contoso.com"}`,
		`"opaque":{"signature":"SIG-contoso.com"}`,
	} {
		if !strings.Contains(s, signed) || n == 0 {
			t.Fatalf("signed block changed or nothing masked (%d): %s", n, s)
		}
	}
	if back := RestoreJSONDomains(out, testDomains); string(back) != body {
		t.Fatalf("restored:\n%s\nwant:\n%s", back, body)
	}
}
