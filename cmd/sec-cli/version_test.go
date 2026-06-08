package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVersionInjection builds the binary with a linker-injected version and
// asserts the `version` command prints it. This is the guarantee the release
// pipeline (goreleaser) depends on: `-ldflags -X main.version=<tag>` overrides
// the 0.0.0-dev sentinel. It builds a real binary, so it is skipped under -short.
func TestVersionInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped under -short")
	}

	const want = "v9.9.9-injected"
	bin := filepath.Join(t.TempDir(), "sec-cli")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	build := exec.Command("go", "build", "-ldflags", "-X main.version="+want, "-o", bin, ".")
	out, err := build.CombinedOutput()
	require.NoErrorf(t, err, "go build failed: %s", out)

	got, err := exec.Command(bin, "version").Output()
	require.NoError(t, err)
	require.Equal(t, want, strings.TrimSpace(string(got)))
}
