package redact

// vault.go is NOT part of magpie: it was added by keepquiet.
//
// magpie keeps the placeholder → value map in the gateway's memory, as one
// long-lived process. keepquiet's hooks are short-lived processes (one
// per hook call), so the map has to be saved between calls and loaded
// again for Restore and for the "known value" matching to work. Export and
// Import are the only way in or out of the unexported map; nothing else in
// the package was changed.

import (
	"regexp"
)

var vaultPlaceholder = regexp.MustCompile(`^\{\{[A-Z][A-Z0-9_]*_[a-z2-7]{8}\}\}$`)

// Export is a copy of every placeholder this process knows, with its value.
// Made-up keys (Options.Shape) are not included: keepquiet turns Shape off.
func Export() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]string, len(values))
	for p, v := range values {
		if _, shaped := shapes[p]; shaped {
			continue
		}
		out[p] = v
	}
	return out
}

// Import adds placeholders saved by Export (by this or another process with
// the same key) and says how many were new. A placeholder not in the form
// Mask writes, or with an empty value, is skipped.
func Import(m map[string]string) int {
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for p, v := range m {
		if v == "" || !vaultPlaceholder.MatchString(p) {
			continue
		}
		if _, ok := values[p]; ok {
			continue
		}
		makeRoom()
		values[p] = v
		kinds[kindOf(p)] = true
		addKnown(p, v)
		n++
	}
	return n
}

// Count is how many placeholders Export would return.
func Count() int {
	mu.RLock()
	defer mu.RUnlock()
	n := 0
	for p := range values {
		if _, shaped := shapes[p]; !shaped {
			n++
		}
	}
	return n
}
