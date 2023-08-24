// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// helpers types to store data on merkleDB
type uptimes struct {
	Duration    time.Duration `serialize:"true"`
	LastUpdated uint64        `serialize:"true"` // Unix time in seconds

	// txID        ids.ID // TODO ABENEGIA: is it needed by delegators and not validators?
	lastUpdated time.Time
}

type stakersData struct {
	TxBytes         []byte `serialize:"true"` // nit signals remove
	PotentialReward uint64 `serialize:"true"`
}

type weightDiffKey struct {
	subnetID ids.ID
	nodeID   ids.NodeID
}

func merkleSuppliesKeyPrefix() []byte {
	prefix := make([]byte, len(merkleSuppliesPrefix))
	copy(prefix, merkleSuppliesPrefix)
	return prefix
}

func merkleSuppliesKey(subnetID ids.ID) []byte {
	key := merkleSuppliesKeyPrefix()
	key = append(key, subnetID[:]...)
	return key
}

func splitMerkleSuppliesKey(b []byte) ([]byte, ids.ID) {
	prefix := make([]byte, len(merkleSuppliesPrefix))
	copy(prefix, b)
	subnetID := ids.Empty
	copy(subnetID[:], b[len(merkleSuppliesPrefix):])
	return prefix, subnetID
}

func merklePermissionedSubnetKey(subnetID ids.ID) []byte {
	key := make([]byte, len(permissionedSubnetSectionPrefix), len(permissionedSubnetSectionPrefix)+len(subnetID[:]))
	copy(key, permissionedSubnetSectionPrefix)
	key = append(key, subnetID[:]...)
	return key
}

func merkleElasticSubnetKey(subnetID ids.ID) []byte {
	key := make([]byte, len(elasticSubnetSectionPrefix), len(elasticSubnetSectionPrefix)+len(subnetID[:]))
	copy(key, elasticSubnetSectionPrefix)
	key = append(key, subnetID[:]...)
	return key
}

func merkleChainPrefix(subnetID ids.ID) []byte {
	prefix := make([]byte, len(chainsSectionPrefix), len(chainsSectionPrefix)+len(subnetID[:]))
	copy(prefix, chainsSectionPrefix)
	prefix = append(prefix, subnetID[:]...)
	return prefix
}

func merkleChainKey(subnetID ids.ID, chainID ids.ID) []byte {
	key := merkleChainPrefix(subnetID)
	key = append(key, chainID[:]...)
	return key
}

func merkleUtxoIDKey(utxoID ids.ID) []byte {
	key := make([]byte, len(utxosSectionPrefix), len(utxosSectionPrefix)+len(utxoID))
	copy(key, utxosSectionPrefix)
	key = append(key, utxoID[:]...)
	return key
}

func merkleRewardUtxosIDPrefix(txID ids.ID) []byte {
	prefix := make([]byte, len(rewardUtxosSectionPrefix), len(rewardUtxosSectionPrefix)+len(txID))
	copy(prefix, rewardUtxosSectionPrefix)
	prefix = append(prefix, txID[:]...)
	return prefix
}

func merkleRewardUtxoIDKey(txID, utxoID ids.ID) []byte {
	key := merkleRewardUtxosIDPrefix(txID)
	key = append(key, utxoID[:]...)
	return key
}

func merkleUtxoIndexPrefix(address []byte) []byte {
	prefix := make([]byte, len(address))
	copy(prefix, address)
	return prefix
}

func merkleUtxoIndexKey(address []byte, utxoID ids.ID) []byte {
	key := merkleUtxoIndexPrefix(address)
	key = append(key, utxoID[:]...)
	return key
}

func splitUtxoIndexKey(b []byte) ([]byte, ids.ID) {
	utxoID := ids.Empty
	address := make([]byte, len(b)-len(utxoID))
	copy(address, b[:len(address)])
	copy(utxoID[:], b[len(address):])
	return address, utxoID
}

func merkleLocalUptimesKey(nodeID ids.NodeID, subnetID ids.ID) []byte {
	key := make([]byte, len(nodeID), len(nodeID)+len(subnetID))
	copy(key, nodeID[:])
	key = append(key, subnetID[:]...)
	return key
}

func merkleCurrentStakersKey(txID ids.ID) []byte {
	key := make([]byte, len(currentStakersSectionPrefix), len(currentStakersSectionPrefix)+len(txID))
	copy(key, currentStakersSectionPrefix)
	key = append(key, txID[:]...)
	return key
}

func merklePendingStakersKey(txID ids.ID) []byte {
	key := make([]byte, len(pendingStakersSectionPrefix), len(pendingStakersSectionPrefix)+len(txID))
	copy(key, pendingStakersSectionPrefix)
	key = append(key, txID[:]...)
	return key
}

func merkleDelegateeRewardsKey(nodeID ids.NodeID, subnetID ids.ID) []byte {
	key := make([]byte, len(delegateeRewardsPrefix), len(delegateeRewardsPrefix)+len(nodeID)+len(subnetID))
	copy(key, delegateeRewardsPrefix)
	key = append(key, nodeID[:]...)
	key = append(key, subnetID[:]...)
	return key
}
