// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewResolver(t *testing.T) {
	type test struct {
		service string
		err     error
	}
	tests := []test{
		{
			service: OpenDNSName,
			err:     nil,
		},
		{
			service: IFConfigName,
			err:     nil,
		},
		{
			service: IFConfigCoName,
			err:     nil,
		},
		{
			service: IFConfigMeName,
			err:     nil,
		},
		{
			service: strings.ToUpper(IFConfigMeName),
			err:     nil,
		},
		{
			service: "not a valid resolution service name",
			err:     errUnknownResolver,
		},
	}
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			require := require.New(t)
			_, err := NewResolver(tt.service)
			require.ErrorIs(err, tt.err)
		})
	}
}
