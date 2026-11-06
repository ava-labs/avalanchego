// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rapidtest

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// New returns a [testing.TB] that binds logging, failures, skips and cleanups
// to the current rapid check, so helpers taking a [testing.TB] fail (and
// shrink) the check instead of the outer test. Methods rapid doesn't provide,
// such as Name and Setenv, use tb.
func New(tb testing.TB, rt *rapid.T) testing.TB {
	return checkTB{tb, rt}
}

type checkTB struct {
	testing.TB
	rt *rapid.T
}

func (b checkTB) Helper()                      { b.rt.Helper() }
func (b checkTB) Log(args ...any)              { b.rt.Log(args...) }
func (b checkTB) Logf(f string, args ...any)   { b.rt.Logf(f, args...) }
func (b checkTB) Output() io.Writer            { return b.rt.Output() }
func (b checkTB) Error(args ...any)            { b.rt.Error(args...) }
func (b checkTB) Errorf(f string, args ...any) { b.rt.Errorf(f, args...) }
func (b checkTB) Fatal(args ...any)            { b.rt.Fatal(args...) }
func (b checkTB) Fatalf(f string, args ...any) { b.rt.Fatalf(f, args...) }
func (b checkTB) FailNow()                     { b.rt.FailNow() }
func (b checkTB) Fail()                        { b.rt.Fail() }
func (b checkTB) Failed() bool                 { return b.rt.Failed() }
func (b checkTB) Skip(args ...any)             { b.rt.Skip(args...) }
func (b checkTB) Skipf(f string, args ...any)  { b.rt.Skipf(f, args...) }
func (b checkTB) SkipNow()                     { b.rt.SkipNow() }
func (b checkTB) Context() context.Context     { return b.rt.Context() }

// Cleanup fails the check on errors raised during f, which rapid would
// otherwise only notice after the property returns.
func (b checkTB) Cleanup(f func()) {
	b.rt.Cleanup(func() {
		already := b.rt.Failed()
		f()
		if !already && b.rt.Failed() {
			b.rt.FailNow()
		}
	})
}

// TempDir is removed when the check ends, after cleanups registered later.
func (b checkTB) TempDir() string {
	b.Helper()
	dir, err := os.MkdirTemp(b.TB.TempDir(), "rapid")
	if err != nil {
		b.Fatalf("os.MkdirTemp(): %v", err)
	}
	b.rt.Cleanup(func() { assert.NoError(b.TB, os.RemoveAll(dir), "os.RemoveAll()") })
	return dir
}
