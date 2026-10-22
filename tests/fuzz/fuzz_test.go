// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"encoding/json"
	"flag"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/perms"
)

var (
	seed      uint64
	steps     int
	tracePath string
)

func init() {
	flag.Uint64Var(&seed, "fuzz-seed", 0, "[optional] seed for generated ops. 0 picks a random seed.")
	flag.IntVar(&steps, "fuzz-steps", 50, "[optional] number of ops to generate")
	flag.StringVar(&tracePath, "fuzz-trace", "", "[optional] run the ops in this trace file instead of generating them")
}

// trace is the file that reproduces a run.
type trace struct {
	Seed uint64 `json:"seed"`
	Ops  []Op   `json:"ops"`
}

// loadTrace reads the trace that --fuzz-trace names, or generates one from
// --fuzz-seed and --fuzz-steps.
func loadTrace() (trace, error) {
	if tracePath != "" {
		b, err := os.ReadFile(tracePath)
		if err != nil {
			return trace{}, err
		}

		var tr trace
		return tr, json.Unmarshal(b, &tr)
	}

	s := seed
	if s == 0 {
		s = rand.Uint64()
	}
	return trace{Seed: s, Ops: Generate(s, steps)}, nil
}

// writeTrace writes tr to dir before the run starts, so a run that crashes
// still leaves it. It returns the file's path.
func writeTrace(dir string, tr trace) (string, error) {
	b, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, "fuzz-trace.json")
	return path, perms.WriteFile(path, b, perms.ReadWrite)
}

// TestFuzz runs a program of ops against nodes in this process and checks
// invariants after each op. Flags pick the program:
//
//	go test -run TestFuzz ./tests/fuzz -args --fuzz-seed=2 --fuzz-steps=200
func TestFuzz(t *testing.T) {
	const actors = 3

	tr, err := loadTrace()
	require.NoError(t, err)

	// The trace goes next to the node data, which stays when the test fails.
	root := keptTempDir(t)
	path, err := writeTrace(root, tr)
	require.NoError(t, err)
	t.Logf("seed %d, trace %s", tr.Seed, path)

	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, _, key := startNodes(t, root, 0, p)

	uris := make([]string, len(nodes))
	gossipers := make([]*byzantinePeer, len(nodes))
	for i, n := range nodes {
		uris[i] = n.APIURI()

		b, err := connectByzantine(t.Context(), n.StakingAddress())
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, b.close(context.WithoutCancel(t.Context())))
		})
		gossipers[i] = b
	}

	tc := tests.NewTestContextWithArgs(t.Context(), tests.NewDefaultLogger(""), t.Errorf, func(r any) {
		t.Errorf("unexpected panic: %v", r)
	})
	// The runner fails through tc, which panics. Recover reports the failure
	// through t.Errorf and runs tc's cleanups.
	defer tc.Recover()

	NewRunner(tc, uris, gossipers, key, actors).Run(tr.Ops)
}
