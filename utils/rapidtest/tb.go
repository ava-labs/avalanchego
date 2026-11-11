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

// TB is a [testing.TB] bound to the current rapid check.
type TB interface {
	testing.TB
	// Repeat is [rapid.T.Repeat].
	Repeat(map[string]func(*rapid.T))
}

// New returns a [TB] that binds logging, failures, skips and cleanups to the
// current rapid check, so helpers taking a [testing.TB] fail (and shrink) the
// check instead of the outer test. Methods rapid doesn't provide, such as Name
// and Setenv, use tb.
func New(tb testing.TB, rt *rapid.T) TB {
	return checkTB{tb, rt}
}

// Check is [rapid.Check] with prop receiving the check as a [TB].
func Check(tb testing.TB, prop func(TB)) {
	tb.Helper()
	rapid.Check(tb, func(rt *rapid.T) {
		prop(New(tb, rt))
	})
}

// MakeFuzz is [rapid.MakeFuzz] with prop receiving the check as a [TB].
func MakeFuzz(prop func(TB)) func(*testing.T, []byte) {
	return func(t *testing.T, b []byte) {
		t.Helper()
		rapid.MakeFuzz(func(rt *rapid.T) {
			prop(New(t, rt))
		})(t, b)
	}
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

func (b checkTB) Repeat(actions map[string]func(*rapid.T)) { b.rt.Repeat(actions) }

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
	dir := b.TB.TempDir()
	b.rt.Cleanup(func() { assert.NoErrorf(b.TB, os.RemoveAll(dir), "os.RemoveAll(%q)", dir) })
	return dir
}
