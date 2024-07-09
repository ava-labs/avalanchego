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

// TestInitializerSuccess tests the successful initialization flow.
func TestInitializerSuccess(t *testing.T) {
	initializer := newInitializer(vmLocation)
	const vmAddr = "vmAddr"

	// Check for no errors during initialization with the expected version.
	require.NoError(t, initializer.Initialize(context.Background(), version.RPCChainVMProtocol, vmAddr))

	// Verify that vmAddr is set correctly.
	require.Equal(t, vmAddr, initializer.vmAddr)

	// Ensure the channel is closed now.
	_, ok := <-initializer.initialized
	require.False(t, ok, "initialized channel should not be closed yet")
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

// TestInitializerPersistance tests that initialization errors are persisted.
func TestInitializerPersistance(t *testing.T) {
	// Positive test: Successful re-initialization after error.
	initializer := newInitializer(vmLocation)

	require.NoError(t, initializer.Initialize(context.Background(), version.RPCChainVMProtocol, "vmAddr"))
	require.NoError(t, initializer.Initialize(context.Background(), uint(0), "vmAddr")) // This should succeed because the error was already persisted.

	// Negative test: Failing re-initialization after error with specific error type.
	initializer = newInitializer(vmLocation)
	err := initializer.Initialize(context.Background(), uint(0), "vmAddr")
	require.ErrorIs(t, err, runtime.ErrProtocolVersionMismatch)

	// Verify the specific error type and its details.
	expectedError := &errProtocolVersionMismatchDetails{
		current:                         version.Current,
		rpcChainVMProtocolVer:           version.RPCChainVMProtocol,
		vmLocation:                      vmLocation,
		vmLocationRPCChainVMProtocolVer: 0,
	}
	require.Equal(t, expectedError, err)
	require.Equal(t, expectedError.Error(), err.Error())

	// Subsequent initialization with the expected version should also fail due to persisted error.
	err = initializer.Initialize(context.Background(), version.RPCChainVMProtocol, "vmAddr")
	require.ErrorIs(t, err, runtime.ErrProtocolVersionMismatch)
	require.Equal(t, expectedError, err)
}

// TestInitializerError tests that the expected error type and details are returned on initialization failure.
func TestInitializerError(t *testing.T) {
	initializer := newInitializer(vmLocation)
	err := initializer.Initialize(context.Background(), uint(0), "vmAddr")
	require.ErrorIs(t, err, runtime.ErrProtocolVersionMismatch)

	// Verify the specific error type and its details.
	expectedError := &errProtocolVersionMismatchDetails{
		current:                         version.Current,
		rpcChainVMProtocolVer:           version.RPCChainVMProtocol,
		vmLocation:                      vmLocation,
		vmLocationRPCChainVMProtocolVer: 0,
	}
	require.Equal(t, expectedError, err)
	require.Equal(t, expectedError.Error(), err.Error())

	// Verify the underlying wrapped error.
	require.ErrorIs(t, err, runtime.ErrProtocolVersionMismatch)
}
