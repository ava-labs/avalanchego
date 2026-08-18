// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unwind

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errCloser struct{ error }

func (c errCloser) Close() error {
	return c.error
}

func TestCloseIfPointsToNonNil(t *testing.T) {
	errA := errors.New("A")
	errB := errors.New("B")
	errC := errors.New("C")

	tests := []struct {
		name    string
		retErr  error
		closers Closers
		want    []error
	}{
		{
			name:    "return_nil_with_closers",
			retErr:  nil,
			closers: Closers{errCloser{errA}, errCloser{errB}},
			want:    nil,
		},
		{
			name:    "return_err_with_closers",
			retErr:  errA,
			closers: Closers{errCloser{errB}, errCloser{errC}},
			want:    []error{errA, errC, errB}, // Closers are reversed for unwinding
		},
		{
			name:   "return_err_without_closers",
			retErr: errB,
			want:   []error{errB},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("Returning error: %v", tt.retErr)
			t.Logf("Closers pushed in order: %v", tt.closers)

			got := func() (retErr error) {
				// This pattern simulates typical usage, as further demonstrated
				// in the method's example.
				defer tt.closers.CloseIfPointsToNonNil(&retErr)
				return tt.retErr
			}()

			if len(tt.want) == 0 {
				assert.NoError(t, got)
			} else {
				type errs interface{ Unwrap() []error }
				unwrapper, ok := got.(errs)
				require.True(t, ok, "returned error implements `Unwrap() []error`")
				if diff := cmp.Diff(tt.want, unwrapper.Unwrap(), cmpopts.EquateErrors()); diff != "" {
					t.Errorf("(-want +got):\n%s", diff)
				}
			}
		})
	}
}
