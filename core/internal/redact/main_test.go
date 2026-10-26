package redact

// agent-redact modification: magpie's TestMain used its internal/testenv
// to run the tests in a temporary home. Here the same isolation is done
// locally (temporary HOME / XDG dirs), since testenv is not vendored.

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "agent-redact-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CACHE_HOME", home+"/.cache")
	os.Setenv("XDG_CONFIG_HOME", home+"/.config")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
