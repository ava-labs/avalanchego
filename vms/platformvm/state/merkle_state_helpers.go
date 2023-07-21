// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/ava-labs/avalanchego/ids"

func merkleSuppliesKey(subnetID ids.ID) []byte {
	key := make([]byte, 0, len(merkleSuppliesPrefix)+len(subnetID[:]))
	copy(key, merkleSuppliesPrefix)
	key = append(key, subnetID[:]...)
	return key
}

func merklePermissionedSubnetKey(subnetID ids.ID) []byte {
	key := make([]byte, 0, len(permissionedSubnetSectionPrefix)+len(subnetID[:]))
	copy(key, permissionedSubnetSectionPrefix)
	key = append(key, subnetID[:]...)
	return key
}

func merkleElasticSubnetKey(subnetID ids.ID) []byte {
	key := make([]byte, 0, len(elasticSubnetSectionPrefix)+len(subnetID[:]))
	copy(key, elasticSubnetSectionPrefix)
	key = append(key, subnetID[:]...)
	return key
}

func merkleChainPrefix(subnetID ids.ID) []byte {
	prefix := make([]byte, 0, len(chainsSectionPrefix)+len(subnetID[:]))
	copy(prefix, chainsSectionPrefix)
	prefix = append(prefix, subnetID[:]...)
	return prefix
}

func merkleChainKey(subnetID ids.ID, chainID ids.ID) []byte {
	prefix := merkleChainPrefix(subnetID)

	key := make([]byte, 0, len(prefix)+len(chainID))
	copy(key, prefix)
	key = append(key, chainID[:]...)
	return key
}

func merkleUtxoIDKey(utxoID ids.ID) []byte {
	key := make([]byte, 0, len(utxosSectionPrefix)+len(utxoID))
	copy(key, utxosSectionPrefix)
	key = append(key, utxoID[:]...)
	return key
}

func merkleRewardUtxosIDPrefix(txID ids.ID) []byte {
	prefix := make([]byte, 0, len(rewardUtxosSectionPrefix)+len(txID))
	copy(prefix, rewardUtxosSectionPrefix)
	prefix = append(prefix, txID[:]...)
	return prefix
}

func merkleRewardUtxoIDKey(txID, utxoID ids.ID) []byte {
	prefix := merkleRewardUtxosIDPrefix(txID)
	key := make([]byte, 0, len(prefix)+len(utxoID))
	copy(key, prefix)
	key = append(key, utxoID[:]...)
	return key
}

func merkleUtxoIndexKey(address []byte, utxoID ids.ID) []byte {
	key := make([]byte, 0, len(address)+len(utxoID))
	copy(key, address)
	key = append(key, utxoID[:]...)
	return key
}

func merkleLocalUptimesKey(nodeID ids.NodeID, subnetID ids.ID) []byte {
	key := make([]byte, 0, len(nodeID)+len(subnetID))
	copy(key, nodeID[:])
	key = append(key, subnetID[:]...)
	return key
}

func merkleCurrentStakersKey(txID ids.ID) []byte {
	key := make([]byte, 0, len(currentStakersSectionPrefix)+len(txID))
	copy(key, currentStakersSectionPrefix)
	key = append(key, txID[:]...)
	return key
}

func merklePendingStakersKey(txID ids.ID) []byte {
	key := make([]byte, 0, len(pendingStakersSectionPrefix)+len(txID))
	copy(key, pendingStakersSectionPrefix)
	key = append(key, txID[:]...)
	return key
}

func merkleDelegateeRewardsKey(nodeID ids.NodeID, subnetID ids.ID) []byte {
	key := make([]byte, 0, len(delegateeRewardsPrefix)+len(nodeID)+len(subnetID))
	copy(key, delegateeRewardsPrefix)
	key = append(key, nodeID[:]...)
	key = append(key, subnetID[:]...)
	return key
}
