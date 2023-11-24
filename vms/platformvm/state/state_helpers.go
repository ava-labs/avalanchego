// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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

func merkleSuppliesKey(subnetID ids.ID) []byte {
	key := make([]byte, len(merkleSuppliesPrefix)+ids.IDLen)
	copy(key, merkleSuppliesPrefix)
	copy(key[len(merkleSuppliesPrefix):], subnetID[:])
	return key
}

func splitMerkleSuppliesKey(b []byte) ([]byte, ids.ID) {
	prefix := make([]byte, len(merkleSuppliesPrefix))
	copy(prefix, b)
	subnetID := ids.Empty
	copy(subnetID[:], b[len(merkleSuppliesPrefix):])
	return prefix, subnetID
}

func writeMetadata(chainTime time.Time, lastAcceptedBlkID ids.ID, modifiedSupplies map[ids.ID]uint64, batchOps *[]database.BatchOp) error {
	encodedChainTime, err := chainTime.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to encoding chainTime: %w", err)
	}
	*batchOps = append(*batchOps, database.BatchOp{
		Key:   merkleChainTimeKey,
		Value: encodedChainTime,
	})

	*batchOps = append(*batchOps, database.BatchOp{
		Key:   merkleLastAcceptedBlkIDKey,
		Value: lastAcceptedBlkID[:],
	})

	// lastAcceptedBlockHeight not persisted yet in merkleDB state.
	// TODO: Consider if it should be

	for subnetID, supply := range modifiedSupplies {
		supply := supply
		key := merkleSuppliesKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: database.PackUInt64(supply),
		})
	}
	return nil
}

func merklePermissionedSubnetKey(subnetID ids.ID) []byte {
	key := make([]byte, len(permissionedSubnetSectionPrefix), len(permissionedSubnetSectionPrefix)+len(subnetID[:]))
	copy(key, permissionedSubnetSectionPrefix)
	key = append(key, subnetID[:]...)
	return key
}

func writePermissionedSubnets(permissionedSubnetsToAdd []*txs.Tx, batchOps *[]database.BatchOp) {
	for _, subnetTx := range permissionedSubnetsToAdd {
		key := merklePermissionedSubnetKey(subnetTx.ID())
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: subnetTx.Bytes(),
		})
	}
}

func merkleElasticSubnetKey(subnetID ids.ID) []byte {
	key := make([]byte, len(elasticSubnetSectionPrefix), len(elasticSubnetSectionPrefix)+len(subnetID[:]))
	copy(key, elasticSubnetSectionPrefix)
	key = append(key, subnetID[:]...)
	return key
}

func writeElasticSubnets(elasticSubnetsToAdd map[ids.ID]*txs.Tx, batchOps *[]database.BatchOp) {
	for subnetID, transforkSubnetTx := range elasticSubnetsToAdd {
		key := merkleElasticSubnetKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: transforkSubnetTx.Bytes(),
		})
	}
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

func writeChains(chainsToAdd map[ids.ID][]*txs.Tx, batchOps *[]database.BatchOp) {
	for subnetID, chains := range chainsToAdd {
		for _, chainTx := range chains {
			key := merkleChainKey(subnetID, chainTx.ID())
			*batchOps = append(*batchOps, database.BatchOp{
				Key:   key,
				Value: chainTx.Bytes(),
			})
		}
	}
}

func merkleUtxoIDKey(utxoID ids.ID) []byte {
	key := make([]byte, len(utxosSectionPrefix), len(utxosSectionPrefix)+len(utxoID))
	copy(key, utxosSectionPrefix)
	key = append(key, utxoID[:]...)
	return key
}

func writeUTXOs(modifiedUTXOs map[ids.ID]*avax.UTXO, batchOps *[]database.BatchOp) error {
	for utxoID, utxo := range modifiedUTXOs {
		key := merkleUtxoIDKey(utxoID)
		if utxo == nil { // delete the UTXO
			*batchOps = append(*batchOps, database.BatchOp{
				Key:    key,
				Delete: true,
			})
			continue
		}

		// insert the UTXO
		utxoBytes, err := txs.GenesisCodec.Marshal(txs.Version, utxo)
		if err != nil {
			return fmt.Errorf("failed marshalling utxo %v bytes: %w", utxoID, err)
		}
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: utxoBytes,
		})
	}
	return nil
}

func merkleUtxoIndexKey(address []byte, utxoID ids.ID) []byte {
	key := make([]byte, len(address)+ids.IDLen)
	copy(key, address)
	copy(key[len(address):], utxoID[:])
	return key
}

func splitUtxoIndexKey(b []byte) ([]byte, ids.ID) {
	address := make([]byte, len(b)-ids.IDLen)
	copy(address, b[:len(address)])

	utxoID := ids.Empty
	copy(utxoID[:], b[len(address):])
	return address, utxoID
}

func merkleLocalUptimesKey(nodeID ids.NodeID, subnetID ids.ID) []byte {
	key := make([]byte, len(nodeID)+len(subnetID))
	copy(key, nodeID[:])
	copy(key[ids.NodeIDLen:], subnetID[:])
	return key
}

func merkleCurrentStakersKey(txID ids.ID) []byte {
	key := make([]byte, len(currentStakersSectionPrefix)+len(txID))
	copy(key, currentStakersSectionPrefix)
	copy(key[len(currentStakersSectionPrefix):], txID[:])
	return key
}

func writeCurrentStakers(currentData map[ids.ID]*stakersData, batchOps *[]database.BatchOp) error {
	for stakerTxID, data := range currentData {
		key := merkleCurrentStakersKey(stakerTxID)

		if data.TxBytes == nil {
			*batchOps = append(*batchOps, database.BatchOp{
				Key:    key,
				Delete: true,
			})
			continue
		}

		dataBytes, err := txs.GenesisCodec.Marshal(txs.Version, data)
		if err != nil {
			return fmt.Errorf("failed to serialize current stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: dataBytes,
		})
	}
	return nil
}

func merklePendingStakersKey(txID ids.ID) []byte {
	key := make([]byte, len(pendingStakersSectionPrefix)+len(txID))
	copy(key, pendingStakersSectionPrefix)
	copy(key[len(pendingStakersSectionPrefix):], txID[:])
	return key
}

func writePendingStakers(pendingData map[ids.ID]*stakersData, batchOps *[]database.BatchOp) error {
	for stakerTxID, data := range pendingData {
		key := merklePendingStakersKey(stakerTxID)

		if data.TxBytes == nil {
			*batchOps = append(*batchOps, database.BatchOp{
				Key:    key,
				Delete: true,
			})
			continue
		}

		dataBytes, err := txs.GenesisCodec.Marshal(txs.Version, data)
		if err != nil {
			return fmt.Errorf("failed to serialize pending stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: dataBytes,
		})
	}
	return nil
}

func merkleDelegateeRewardsKey(nodeID ids.NodeID, subnetID ids.ID) []byte {
	key := make([]byte, len(delegateeRewardsPrefix)+len(nodeID)+len(subnetID))
	copy(key, delegateeRewardsPrefix)
	copy(key[len(delegateeRewardsPrefix):], nodeID[:])
	copy(key[len(delegateeRewardsPrefix)+ids.NodeIDLen:], subnetID[:])
	return key
}

func writeDelegateeRewards(modifiedDelegateeRewards map[ids.ID]map[ids.NodeID]uint64, batchOps *[]database.BatchOp) { //nolint:golint,unparam
	for subnetID, nodeDelegateeRewards := range modifiedDelegateeRewards {
		for nodeID, delegateeReward := range nodeDelegateeRewards {
			key := merkleDelegateeRewardsKey(nodeID, subnetID)
			*batchOps = append(*batchOps, database.BatchOp{
				Key:   key,
				Value: database.PackUInt64(delegateeReward),
			})
		}
	}
}

func merkleSubnetOwnersKey(subnetID ids.ID) []byte {
	key := make([]byte, len(subnetOwnersPrefix)+len(subnetID))
	copy(key, delegateeRewardsPrefix)
	copy(key[len(delegateeRewardsPrefix):], subnetID[:])
	return key
}

func writeSubnetOwners(subnetID ids.ID, owner fx.Owner, batchOps *[]database.BatchOp) ([]byte, error) {
	ownerBytes, err := block.GenesisCodec.Marshal(block.Version, &owner)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal subnet owner: %w", err)
	}
	key := merkleSubnetOwnersKey(subnetID)
	*batchOps = append(*batchOps, database.BatchOp{
		Key:   key,
		Value: ownerBytes,
	})
	return ownerBytes, nil
}
