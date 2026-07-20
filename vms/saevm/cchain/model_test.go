// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// scopedTB adapts the outer *testing.T for use inside a rapid property check.
// It scopes Cleanup callbacks to a single check (run via close) and routes
// failure methods through the active *rapid.T so rapid can shrink failing
// action sequences. All other testing.TB behaviour (Context, TempDir, Logf,
// Helper, ...) delegates to the embedded outer TB.
type scopedTB struct {
	testing.TB

	mu       sync.Mutex
	rt       *rapid.T
	cleanups []func()
}

func newScopedTB(t *testing.T) *scopedTB { return &scopedTB{TB: t} }

func (s *scopedTB) setRapidT(rt *rapid.T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rt = rt
}

func (s *scopedTB) Cleanup(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanups = append(s.cleanups, f)
}

// close runs and clears the scoped cleanups in reverse registration order,
// mirroring testing.T cleanup semantics for a single rapid check.
func (s *scopedTB) close() {
	s.mu.Lock()
	fns := s.cleanups
	s.cleanups = nil
	s.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

func (s *scopedTB) rapidT() *rapid.T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rt
}

func (s *scopedTB) Error(args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Error(args...)
		return
	}
	s.TB.Error(args...)
}

func (s *scopedTB) Errorf(format string, args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Errorf(format, args...)
		return
	}
	s.TB.Errorf(format, args...)
}

func (s *scopedTB) Fatal(args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Fatal(args...)
		return
	}
	s.TB.Fatal(args...)
}

func (s *scopedTB) Fatalf(format string, args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Fatalf(format, args...)
		return
	}
	s.TB.Fatalf(format, args...)
}

func (s *scopedTB) Fail() {
	if rt := s.rapidT(); rt != nil {
		rt.Fail()
		return
	}
	s.TB.Fail()
}

func (s *scopedTB) FailNow() {
	if rt := s.rapidT(); rt != nil {
		rt.FailNow()
		return
	}
	s.TB.FailNow()
}

func TestScopedTBCleanupOrder(t *testing.T) {
	s := newScopedTB(t)
	var got []int
	s.Cleanup(func() { got = append(got, 1) })
	s.Cleanup(func() { got = append(got, 2) })
	s.close()
	require.Equal(t, []int{2, 1}, got, "scopedTB.close() cleanup order")
	s.close() // idempotent
	require.Equal(t, []int{2, 1}, got, "scopedTB.close() second call is a no-op")
}
