// Package piistub stands in for magpie's internal/pii (the optional local
// PII model) so the vendored redact tests compile without it. agent-redact
// does not ship the model: Installed is always false.
package piistub

import "github.com/bl0omz/agent-redact/internal/redact"

func Installed() bool { return false }
func Stop()           {}
func Find(texts []string) ([][]redact.Found, error) {
	return make([][]redact.Found, len(texts)), nil
}
