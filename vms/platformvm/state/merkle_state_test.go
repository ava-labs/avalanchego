// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

func TestSuppliesKeyTest(t *testing.T) {
	require := require.New(t)
	subnetID := ids.GenerateTestID()

	key := merkleSuppliesKey(subnetID)
	prefix, retrievedSubnetID := splitMerkleSuppliesKey(key)

	require.Equal(merkleSuppliesPrefix, prefix)
	require.Equal(subnetID, retrievedSubnetID)
}

func TestPermissionedSubnetKey(t *testing.T) {
	require := require.New(t)
	subnetID := ids.GenerateTestID()
	prefix := permissionedSubnetSectionPrefix

	key := merklePermissionedSubnetKey(subnetID)

	require.Len(key, len(prefix)+len(subnetID[:]))
	require.Equal(prefix, key[0:len(prefix)])
	require.Equal(subnetID[:], key[len(prefix):])
}

func TestElasticSubnetKey(t *testing.T) {
	require := require.New(t)
	subnetID := ids.GenerateTestID()
	prefix := elasticSubnetSectionPrefix

	key := merkleElasticSubnetKey(subnetID)

	require.Len(key, len(prefix)+len(subnetID[:]))
	require.Equal(prefix, key[0:len(prefix)])
	require.Equal(subnetID[:], key[len(prefix):])
}

func TestChainKey(t *testing.T) {
	require := require.New(t)
	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	prefix := chainsSectionPrefix

	keyPrefix := merkleChainPrefix(subnetID)
	key := merkleChainKey(subnetID, chainID)

	require.Len(keyPrefix, len(prefix)+len(subnetID[:]))
	require.Equal(prefix, key[0:len(prefix)])
	require.Equal(subnetID[:], keyPrefix[len(prefix):])

	require.Len(key, len(keyPrefix)+len(chainID[:]))
	require.Equal(chainID[:], key[len(keyPrefix):])
}

func TestUtxoIDKey(t *testing.T) {
	require := require.New(t)
	utxoID := ids.GenerateTestID()
	prefix := utxosSectionPrefix

	key := merkleUtxoIDKey(utxoID)

	require.Len(key, len(prefix)+len(utxoID[:]))
	require.Equal(prefix, key[0:len(prefix)])
	require.Equal(utxoID[:], key[len(prefix):])
}

func TestUtxosIndexKey(t *testing.T) {
	require := require.New(t)
	utxoID := ids.GenerateTestID()

	keys := secp256k1.TestKeys()
	address := keys[1].PublicKey().Address().Bytes()

	key := merkleUtxoIndexKey(address, utxoID)
	rAddress, rUtxoID := splitUtxoIndexKey(key)

	require.Len(key, len(address)+len(utxoID[:]))
	require.Equal(address, rAddress)
	require.Equal(utxoID, rUtxoID)
}

func TestLocalUptimesKey(t *testing.T) {
	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()

	key := merkleLocalUptimesKey(nodeID, subnetID)

	require.Len(key, len(nodeID[:])+len(subnetID[:]))
	require.Equal(nodeID[:], key[0:len(nodeID[:])])
	require.Equal(subnetID[:], key[len(nodeID[:]):])
}

func TestCurrentStakersKey(t *testing.T) {
	require := require.New(t)
	stakerID := ids.GenerateTestID()
	prefix := currentStakersSectionPrefix

	key := merkleCurrentStakersKey(stakerID)

	require.Len(key, len(prefix)+len(stakerID[:]))
	require.Equal(prefix, key[0:len(prefix)])
	require.Equal(stakerID[:], key[len(prefix):])
}

func TestPendingStakersKey(t *testing.T) {
	require := require.New(t)
	stakerID := ids.GenerateTestID()
	prefix := pendingStakersSectionPrefix

	key := merklePendingStakersKey(stakerID)

	require.Len(key, len(prefix)+len(stakerID[:]))
	require.Equal(prefix, key[0:len(prefix)])
	require.Equal(stakerID[:], key[len(prefix):])
}

func TestDelegateeRewardsKey(t *testing.T) {
	require := require.New(t)
	prefix := delegateeRewardsPrefix
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()

	key := merkleDelegateeRewardsKey(nodeID, subnetID)

	require.Len(key, len(prefix)+len(nodeID[:])+len(subnetID[:]))
	require.Equal(prefix, key[0:len(prefix)])
	require.Equal(nodeID[:], key[len(prefix):len(prefix)+len(nodeID[:])])
	require.Equal(subnetID[:], key[len(prefix)+len(nodeID[:]):])
}
