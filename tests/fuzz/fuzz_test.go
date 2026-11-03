// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/eventlog"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/set"
)

// TestFuzz starts a network in this process for each rapid check, runs
// FUZZ_OPS random ops against it (1000 if unset), and checks invariants before
// the first op and after each one. Runs are not deterministic, so shrinking
// and fail files do not help. rapid stops at the first failing check, and a
// long check reaches state that several short ones never do, so run one check
// per seed:
//
//	go test -run '^TestFuzz$' ./tests/fuzz -rapid.checks=1 \
//		-rapid.shrinktime=0 -rapid.nofailfile -rapid.seed=1
//
// It runs in a synctest bubble, where time skips ahead whenever every
// goroutine waits.
//
// The mutation score is gremlins, run from the module root. It takes no test
// flags, so the test is picked through GOFLAGS and rapid reads its flags from
// the environment. Its integration mode tests ./..., which rebuilds every test
// package that imports the mutant, so the go in gremlins/ narrows that to this
// package. Each gremlins worker builds in its own copy of the module, which
// misses the build cache unless -trimpath keeps source paths out of the cache
// keys:
//
//	PATH=$PWD/tests/fuzz/gremlins:$PATH \
//	GOFLAGS='-run=^TestFuzz$ -trimpath' RAPID_SEED=2 RAPID_CHECKS=1 \
//		RAPID_NOFAILFILE=true RAPID_SHRINKTIME=0 \
//		FUZZ_KILLS=/abs/kills.tsv \
//		gremlins unleash --config tests/fuzz/.gremlins.yaml
//
// gremlins records only that a mutant's test failed. With FUZZ_KILLS set,
// each failed check also appends why to that file (recordFailure). The path
// must be absolute, since each worker runs in its own copy of the module.
//
// rapid shows a check's own log only with -rapid.v or when it reruns a failing
// check, so each check's node data directory and op outcomes are logged
// through t. The outcomes are logged even after a failure: an op that never
// commits explores nothing, and a check that never decides is never tested.
// Setup failures and the cleanups' logs go to t as they happen, so a node that
// hangs in shutdown does not hide them.
func TestFuzz(t *testing.T) {
	inBubble(t, fuzz)
}

func fuzz(t *testing.T) {
	const actors = 3

	// rapid's -rapid.steps only sets the mean of a random op count, so one
	// check can run 20 times as many ops as the next. A fixed count gives each
	// mutant the same work.
	ops := 1000
	if v := os.Getenv("FUZZ_OPS"); v != "" {
		var err error
		ops, err = strconv.Atoi(v)
		require.NoError(t, err, "FUZZ_OPS must be an integer")
	}

	// noDeadline hides t's Deadline from rapid. Converting t to rapid.TB is not
	// enough: rapid type-asserts *testing.T to read its deadline, and Deadline
	// panics in a synctest bubble.
	type noDeadline struct{ rapid.TB }

	var campaign commitTally
	rapid.Check(noDeadline{t}, func(rt *rapid.T) {
		var r *runner
		defer func() {
			if rt.Failed() {
				recordFailure(t, r)
			}
		}()

		root := keptTempDir(rt)
		t.Logf("node data in %s", root)

		r = startRunner(reportingT{testingT: rt, outer: t}, root, actors)
		r.campaign = &campaign
		start := time.Now()
		defer func() {
			t.Logf("op outcomes: %v; reasons: %v", r.stats, r.reasons)
			t.Logf("excused: %s", r.faults.excusedShares(start, time.Now()))
			t.Logf("fair attempts: %v; commits: %v", r.commits.fair, r.commits.commits)
		}()
		r.check(rt)
		for range ops {
			r.op(rt)
			r.check(rt)
		}
		r.finish(rt)
	})
}

// recordFailure appends why a check failed to the file that FUZZ_KILLS names,
// if it is set. A line holds, tab-separated, the mutant that gremlins/go
// names in FUZZ_MUTANT, the stage, and what failed:
//
//   - setup, and the function that failed, if r is nil: the network never
//     started, so no oracle judged it;
//   - oracle, and the check, if a check failed (fail);
//   - harness, and the function that failed, otherwise, such as an API call
//     that returned an error, or "deadline" for a stage that ran out of time,
//     the one failure that does not stop the check.
//
// Call it from a deferred function, while the panic of FailNow still holds
// the stack of the function that failed.
func recordFailure(t *testing.T, r *runner) {
	path := os.Getenv("FUZZ_KILLS")
	if path == "" {
		return
	}

	stage, what := "harness", failedFunc()
	switch {
	case r == nil:
		stage = "setup"
	case r.failed != "":
		stage, what = "oracle", r.failed
	case what == "":
		what = "deadline"
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perms.ReadWrite)
	if err != nil {
		t.Logf("cannot record the failure: %v", err)
		return
	}
	defer f.Close()

	// One write, so lines from parallel runs do not interleave.
	line := fmt.Sprintf("%s\t%s\t%s\n", os.Getenv("FUZZ_MUTANT"), stage, what)
	if _, err := f.WriteString(line); err != nil {
		t.Logf("cannot record the failure: %v", err)
	}
}

// failedFunc names the function of this package that failed a test through
// FailNow, from the stack of the panic that FailNow raised, or "" if no panic
// is running.
func failedFunc() string {
	const pkg = "github.com/ava-labs/avalanchego/tests/fuzz."

	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(1, pcs)])
	panicking := false
	for {
		f, more := frames.Next()
		switch {
		case f.Function == "runtime.gopanic":
			panicking = true
		case panicking && f.File != "<autogenerated>" && strings.HasPrefix(f.Function, pkg):
			return strings.TrimPrefix(f.Function, pkg)
		}
		if !more {
			return ""
		}
	}
}

// startRunner starts a network in this process with its data in root, with a
// Byzantine peer connected to each node, and returns a runner for it. The
// genesis has one Byzantine validator, the runner's proposer, with an eighth
// of the genesis stake (setTestDefaults). The runner writes its
// events to events.log in root, beside the nodes' dirs.
func startRunner(t testingT, root string, actors int) *runner {
	t.Helper()

	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, byzVdrs, key := startNodes(t, root, 1, p)

	gossipers := make([]*byzantinePeer, len(nodes))
	for i, n := range nodes {
		b, err := connectByzantine(t.Context(), &p.net, n.StakingAddress())
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, b.close(context.WithoutCancel(t.Context())))
		})
		gossipers[i] = b
	}

	events, err := os.OpenFile(filepath.Join(root, "events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, perms.ReadWrite)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, events.Close())
	})

	r := newRunner(t, nodes, gossipers, key, actors)
	r.network = &p.net
	r.acps = make(map[ids.NodeID]set.Set[uint32], len(nodes))
	for _, n := range nodes {
		r.stakingAddrs = append(r.stakingAddrs, n.StakingAddress())
		r.nodeEvents = append(r.nodeEvents, n.flushEvents)
		r.acps[n.ID] = n.Config.NetworkConfig.SupportedACPs
		r.chains = append(r.chains, func() []ids.ID { return n.Accepted(constants.PlatformChainID) })
	}
	prop, err := connectProposer(t.Context(), &p.net, r.stakingAddrs, byzVdrs[0])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, prop.close(context.WithoutCancel(t.Context())))
	})
	r.proposer = prop
	r.events = eventlog.NewLogger(events).With("node", "fuzzer")
	r.injector = &injector{
		proxy:    p,
		addrs:    r.stakingAddrs,
		nodeIDs:  r.nodeIDs,
		accepted: func(i int) int { return nodes[i].acceptedLen(constants.PlatformChainID) },
		faults:   &r.faults,
	}
	go r.injector.run(t.Context())
	return r
}

// reportingT is a check's T for setup and cleanup that sends each failure and
// log line to the outer test, as it happens. rapid reports a check's failure
// only after the check's cleanups run, and shows the check's log only with
// -rapid.v, so a cleanup that hangs would hide both.
//
// It does not also send them to the check's T: rapid would print each one a
// second time, and adds nothing, since a failing check's rerun is another run
// (TestFuzz). FailNow still goes to the check's T, so rapid stops the check
// and counts it failed.
type reportingT struct {
	testingT
	outer interface {
		Errorf(format string, args ...any)
		Logf(format string, args ...any)
	}
}

func (r reportingT) Errorf(format string, args ...any) {
	r.outer.Errorf(format, args...)
}

func (r reportingT) Logf(format string, args ...any) {
	r.outer.Logf(format, args...)
}
