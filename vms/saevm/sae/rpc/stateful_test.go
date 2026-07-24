// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLibevmTracersDoNotApplyBeaconRoot fails once libevm's tracer APIs apply
// the EIP-4788 beacon root themselves (as upstream geth already does), at
// which point [tracerBackend.applyChildBeforeBlock] applies it twice.
//
// TODO(JonathanOppenheimer): when this fails, drop the double application,
// and delete the test.
func TestLibevmTracersDoNotApplyBeaconRoot(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/ava-labs/libevm").Output()
	if err != nil {
		t.Skipf("locating libevm module source: %v", err) // e.g. no go tool under Bazel
	}

	srcs, err := filepath.Glob(filepath.Join(strings.TrimSpace(string(out)), "eth", "tracers", "*.go"))
	require.NoError(t, err, "filepath.Glob()")
	require.NotEmpty(t, srcs, "libevm eth/tracers sources")

	for _, f := range srcs {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err, "os.ReadFile(%q)", f)
		require.NotContains(t, string(src), "BeaconRoot",
			"%s: libevm's tracer APIs now handle the EIP-4788 beacon root, so applyChildBeforeBlock applies it twice; drop it there and delete this test",
			filepath.Base(f),
		)
	}
}
