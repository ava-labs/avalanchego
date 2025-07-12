// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/config/node"
)

func TestDefaultConfigInitializationUsesExistingDefaultKey(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	require := require.New(t)

	config := node.DefaultSignerConfig{
		SignerKeyPath: filepath.Join(tempDir, ".avalanchego/staking/signer.key"),
	}

	signer1, err := newStakingSigner(config)
	require.NoError(err)
	signer2, err := newStakingSigner(config)
	require.NoError(err)

	require.Equal(signer1.PublicKey(), signer2.PublicKey())
}
