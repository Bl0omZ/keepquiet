package redact

import "testing"

// keepquiet addition: Export/Import round trip, so a second process
// restores what the first one masked.
func TestVaultRoundTrip(t *testing.T) {
	fake := "sk-proj-" + "FAKEfakeFAKEfake0123456789abcdefABCDEF"
	out, n := Mask("key="+fake, Options{Secrets: true})
	if n == 0 || out == "key="+fake {
		t.Fatalf("not masked: %q", out)
	}
	saved := Export()
	mu.Lock()
	values = map[string]string{}
	reindexKnown()
	mu.Unlock()
	if got := Restore(out, false); got != out {
		t.Fatalf("restored with empty map: %q", got)
	}
	if Import(saved) == 0 {
		t.Fatal("nothing imported")
	}
	if got := Restore(out, false); got != "key="+fake {
		t.Fatalf("restore after import: %q", got)
	}
	if Import(map[string]string{"not a placeholder": "x"}) != 0 {
		t.Fatal("bad placeholder imported")
	}
}
