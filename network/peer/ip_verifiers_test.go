// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestIPVerifiers(t *testing.T) {
	errFoobar := errors.New("foobar")

	type args struct {
		ipBytes []byte
		sig     Signature
	}

	tests := []struct {
		name           string
		args           args
		requiredResult error
		optionalResult error
		err            error
	}{
		{
			name: "fail - all fail",
			args: args{
				ipBytes: nil,
				sig:     Signature{},
			},
			requiredResult: errFoobar,
			optionalResult: errFoobar,
			err:            errFoobar,
		},
		{
			name: "fail - required fails",
			args: args{
				ipBytes: nil,
				sig:     Signature{},
			},
			requiredResult: errFoobar,
			optionalResult: nil,
			err:            errFoobar,
		},
		{
			name: "fail - optional fails",
			args: args{
				ipBytes: nil,
				sig:     Signature{},
			},
			requiredResult: nil,
			optionalResult: errFoobar,
			err:            errFoobar,
		},
		{
			name: "fail - missing required",
			args: args{
				ipBytes: nil,
				sig:     Signature{},
			},
			requiredResult: errMissingSignature,
			optionalResult: nil,
			err:            errMissingSignature,
		},
		{
			name: "success - missing optional",
			args: args{
				ipBytes: nil,
				sig:     Signature{},
			},
			requiredResult: nil,
			optionalResult: errMissingSignature,
			err:            nil,
		},
		{
			name: "success - all pass",
			args: args{
				ipBytes: nil,
				sig:     Signature{},
			},
			requiredResult: nil,
			optionalResult: nil,
			err:            nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			ctrl := gomock.NewController(t)

			required := NewMockIPVerifier(ctrl)
			required.EXPECT().Verify(gomock.Any(), gomock.Any()).
				Return(test.requiredResult).AnyTimes()
			optional := NewMockIPVerifier(ctrl)
			optional.EXPECT().Verify(gomock.Any(), gomock.Any()).
				Return(test.optionalResult).AnyTimes()

			verifier := NewIPVerifiers(map[IPVerifier]bool{
				required: true,
				optional: false,
			})

			r.Equal(test.err, verifier.Verify(test.args.ipBytes, test.args.sig))
		})
	}
}
