// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rapidtest

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
)

// failRecorder records failures instead of propagating them to the outer test.
type failRecorder struct {
	testing.TB
	failed bool
	errs   []string
	logs   []string
}

func (r *failRecorder) Error(...any) { r.failed = true }
func (r *failRecorder) Errorf(f string, a ...any) {
	r.failed = true
	r.errs = append(r.errs, fmt.Sprintf(f, a...))
}
func (r *failRecorder) Log(a ...any)            { r.logs = append(r.logs, fmt.Sprint(a...)) }
func (r *failRecorder) Logf(f string, a ...any) { r.logs = append(r.logs, fmt.Sprintf(f, a...)) }
func (r *failRecorder) Fatal(...any)            { r.failed = true }
func (r *failRecorder) Fatalf(string, ...any)   { r.failed = true }
func (r *failRecorder) Fail()                   { r.failed = true }
func (r *failRecorder) FailNow()                { r.failed = true }
func (r *failRecorder) Failed() bool            { return r.failed }

// noFailFile stops rapid writing fail files for deliberate failures.
func noFailFile(t *testing.T) {
	t.Helper()
	prev := flag.Lookup("rapid.nofailfile").Value.String()
	require.NoError(t, flag.Set("rapid.nofailfile", "true"), "flag.Set()")
	t.Cleanup(func() { assert.NoError(t, flag.Set("rapid.nofailfile", prev), "flag.Set()") })
}

func TestNewRoutesFailures(t *testing.T) {
	t.Run("cleanup_runs_per_check", func(t *testing.T) {
		var checks, cleaned int
		rapid.Check(t, func(rt *rapid.T) {
			checks++
			New(t, rt).Cleanup(func() { cleaned++ })
		})
		require.Equal(t, checks, cleaned, "New().Cleanup calls")
	})
	t.Run("skip_skips_check_only", func(t *testing.T) {
		var ran int
		rapid.Check(t, func(rt *rapid.T) {
			if rapid.Bool().Draw(rt, "skip") {
				New(t, rt).SkipNow()
			}
			ran++
		})
		require.Positive(t, ran, "unskipped checks")
		require.False(t, t.Skipped(), "outer t skipped")
	})
	t.Run("tempdir_removed_per_check", func(t *testing.T) {
		var dirs []string
		rapid.Check(t, func(rt *rapid.T) {
			dirs = append(dirs, New(t, rt).TempDir())
		})
		require.NotEmpty(t, dirs, "TempDir calls")
		for _, d := range dirs {
			require.NoDirExists(t, d, "New().TempDir() after check")
		}
	})
	t.Run("cleanup_error_fails_check", func(t *testing.T) {
		noFailFile(t)
		rec := &failRecorder{TB: t}
		rapid.Check(rec, func(rt *rapid.T) {
			tb := New(t, rt)
			tb.Cleanup(func() { tb.Errorf("boom") })
		})
		require.True(t, rec.failed, "rapid.Check reported the cleanup error")
		out := strings.Join(rec.errs, "\n")
		require.Contains(t, out, "[rapid] failed after", "failure marker")
		require.Contains(t, strings.Join(rec.logs, "\n"), "boom", "cleanup error logged")
		require.NotContains(t, out, "flaky", "cleanup error must fail the check deterministically")
		require.False(t, t.Failed(), "outer t untouched")
	})
	t.Run("property_failure_survives_cleanups", func(t *testing.T) {
		noFailFile(t)
		rec := &failRecorder{TB: t}
		rapid.Check(rec, func(rt *rapid.T) {
			tb := New(t, rt)
			for range 6 {
				tb.Cleanup(func() {})
			}
			rt.Fatalf("property bug")
		})
		require.True(t, rec.failed, "rapid.Check reported the property failure")
		require.Contains(t, strings.Join(rec.errs, "\n"), "property bug", "failure output")
		require.False(t, t.Failed(), "outer t untouched")
	})
	t.Run("log_error_fails_check", func(t *testing.T) {
		noFailFile(t)
		rec := &failRecorder{TB: t}
		rapid.Check(rec, func(rt *rapid.T) {
			loggingtest.New(New(t, rt), logging.Debug).Error("boom")
		})
		require.True(t, rec.failed, "rapid.Check reported the logged error")
		require.False(t, t.Failed(), "outer t untouched")
	})
}
