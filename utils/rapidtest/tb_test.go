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

// failRecorder stands in for the outer test; its embedded [testing.TB] is nil.
type failRecorder struct {
	testing.TB
	failed bool
	errs   []string
	logs   []string
}

func (*failRecorder) Helper()      {}
func (*failRecorder) Name() string { return "failRecorder" }

func (r *failRecorder) fail(msg string) {
	r.failed = true
	r.errs = append(r.errs, msg)
}

func (r *failRecorder) log(msg string) { r.logs = append(r.logs, msg) }

func (r *failRecorder) Error(a ...any)            { r.fail(fmt.Sprint(a...)) }
func (r *failRecorder) Errorf(f string, a ...any) { r.fail(fmt.Sprintf(f, a...)) }
func (r *failRecorder) Fatal(a ...any)            { r.fail(fmt.Sprint(a...)) }
func (r *failRecorder) Fatalf(f string, a ...any) { r.fail(fmt.Sprintf(f, a...)) }
func (r *failRecorder) Log(a ...any)              { r.log(fmt.Sprint(a...)) }
func (r *failRecorder) Logf(f string, a ...any)   { r.log(fmt.Sprintf(f, a...)) }
func (r *failRecorder) Fail()                     { r.failed = true }
func (r *failRecorder) FailNow()                  { r.failed = true }
func (r *failRecorder) Failed() bool              { return r.failed }

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
		Check(t, func(t TB) {
			checks++
			curr := checks
			t.Cleanup(func() {
				require.Equal(t, curr, checks, "New().Cleanup() triggered in different check")
				cleaned++
			})
		})
		require.Equal(t, checks, cleaned, "New().Cleanup()s triggered")
	})
	t.Run("skip_skips_check_only", func(t *testing.T) {
		rec := &failRecorder{}
		var entered, ran int
		rapid.Check(rec, func(rt *rapid.T) {
			entered++
			if rapid.Bool().Draw(rt, "skip") {
				New(rec, rt).SkipNow()
			}
			ran++
		})
		assert.Positive(t, ran, "unskipped checks")
		assert.Less(t, ran, entered, "checks past New().SkipNow()")
		assert.Falsef(t, rec.failed, "outer %T.Failed()", rec)
	})
	t.Run("tempdir_removed_per_check", func(t *testing.T) {
		Check(t, func(t TB) {
			var dir string
			t.Cleanup(func() {
				require.NotEmpty(t, dir, "closure over New().TempDir() return value")
				require.NoDirExists(t, dir, "New().TempDir() in Cleanup() registered before")
			})
			dir = t.TempDir()
			require.DirExists(t, dir, "New().TempDir()")
		})
	})
	t.Run("cleanup_error_fails_check", func(t *testing.T) {
		noFailFile(t)
		rec := &failRecorder{}
		Check(rec, func(t TB) {
			t.Cleanup(func() { t.Errorf("boom") })
		})
		assert.True(t, rec.failed, "rapid.Check reported the cleanup error")
		out := strings.Join(rec.errs, "\n")
		assert.Contains(t, out, "[rapid] failed after", "failure marker")
		assert.Contains(t, strings.Join(rec.logs, "\n"), "boom", "cleanup error logged")
		assert.NotContains(t, out, "flaky", "cleanup error must fail the check deterministically")
	})
	t.Run("property_failure_survives_cleanups", func(t *testing.T) {
		noFailFile(t)
		rec := &failRecorder{}
		Check(rec, func(t TB) {
			for range 6 {
				t.Cleanup(func() {})
			}
			t.Fatalf("property bug")
		})
		assert.True(t, rec.failed, "rapid.Check reported the property failure")
		out := strings.Join(rec.errs, "\n")
		assert.Contains(t, out, "[rapid] failed after", "failure marker")
		assert.Contains(t, out, "property bug", "failure output")
	})
	t.Run("log_error_fails_check", func(t *testing.T) {
		noFailFile(t)
		rec := &failRecorder{}
		Check(rec, func(t TB) {
			loggingtest.New(t, logging.Debug).Error("boom")
		})
		assert.True(t, rec.failed, "rapid.Check reported the logged error")
		assert.Contains(t, strings.Join(rec.errs, "\n"), "[rapid] failed after", "failure marker")
	})
}
