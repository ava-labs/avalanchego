// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
)

const (
	vmLocation = "vm-location-path"
)

// TestInitializerVersions tests the initialization flow for different versions as well as validate the most-once logic.
func TestInitializerVersions(t *testing.T) {
	const vmAddr = "vmAddr"
	badVersion := ^uint(0)
	testCases := []struct {
		name      string
		version   uint
		expectErr *errProtocolVersionMismatchDetails
	}{
		{
			name:      "test success",
			version:   version.RPCChainVMProtocol,
			expectErr: nil,
		},
		{
			name:    "bad version",
			version: badVersion,
			expectErr: &errProtocolVersionMismatchDetails{
				current:                         version.Current,
				rpcChainVMProtocolVer:           version.RPCChainVMProtocol,
				vmLocation:                      vmLocation,
				vmLocationRPCChainVMProtocolVer: badVersion,
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			initializer := newInitializer(vmLocation)
			if testCase.expectErr != nil {
				require.Equal(t, initializer.Initialize(context.Background(), testCase.version, vmAddr), testCase.expectErr, "expected error for unexpected version")

				// Call with known-good version to verify at most-once init
				err := initializer.Initialize(context.Background(), version.RPCChainVMProtocol, vmAddr)
				require.Equal(t, err, testCase.expectErr, "expected error for subsequent call")
				require.ErrorIs(t, err, runtime.ErrProtocolVersionMismatch)
			} else {
				// Check for no errors during initialization with the expected version.
				require.NoError(t, initializer.Initialize(context.Background(), testCase.version, vmAddr))

				// Call with known-bad version to verify at most-once init
				require.NoError(t, initializer.Initialize(context.Background(), badVersion, vmAddr))
			}
			// Verify that vmAddr is set correctly.
			require.Equal(t, vmAddr, initializer.vmAddr)

			// Ensure the channel is closed now.
			_, ok := <-initializer.initialized
			require.False(t, ok, "initialized channel should not be closed yet")
		})
	}
}

// TestInitializerFail tests that initialization fails for unexpected versions.
func TestInitializerFail(t *testing.T) {
	for v := uint(0); v < version.RPCChainVMProtocol*10; v++ {
		initializer := newInitializer(vmLocation)
		err := initializer.Initialize(context.Background(), v, "vmAddr")

		// Check for successful initialization only with the expected version.
		if v == version.RPCChainVMProtocol {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, runtime.ErrProtocolVersionMismatch, "expected error for unexpected version")
		}
	}
}
