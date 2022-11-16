// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodeid

import (
	"crypto/rsa"
	"strconv"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto"
)

var localNodesCount = 5

func LoadLocalCaminoNodeKeysAndIDs(localStakingPath string) ([]*crypto.PrivateKeySECP256K1R, []ids.NodeID) {
	nodeKeys := make([]*crypto.PrivateKeySECP256K1R, localNodesCount)
	nodeIDs := make([]ids.NodeID, localNodesCount)

	for index := 0; index < localNodesCount; index++ {
		secp256Factory := crypto.FactorySECP256K1R{}
		var nodePrivateKey crypto.PrivateKey

		cert, err := staking.LoadTLSCertFromFiles(
			localStakingPath+"staker"+strconv.Itoa(index+1)+".key",
			localStakingPath+"staker"+strconv.Itoa(index+1)+".crt")
		if err != nil {
			panic(err)
		}

		rsaKey, ok := cert.PrivateKey.(*rsa.PrivateKey)
		if !ok {
			panic("Wrong private key type")
		}
		secpPrivateKey := crypto.RsaPrivateKeyToSecp256PrivateKey(rsaKey)
		nodePrivateKey, err = secp256Factory.ToPrivateKey(secpPrivateKey.Serialize())
		if err != nil {
			panic(err)
		}
		nodeSECP256PrivateKey, ok := nodePrivateKey.(*crypto.PrivateKeySECP256K1R)
		if !ok {
			panic("Could not cast node's private key to PrivateKeySECP256K1R")
		}
		nodeID := nodePrivateKey.PublicKey().Address()
		nodeKeys[index] = nodeSECP256PrivateKey
		nodeIDs[index] = ids.NodeID(nodeID)
	}
	return nodeKeys, nodeIDs
}

func GenerateCaminoNodeKeyAndID() (*crypto.PrivateKeySECP256K1R, ids.NodeID) {
	secp256Factory := crypto.FactorySECP256K1R{}
	nodePrivateKey, err := secp256Factory.NewPrivateKey()
	if err != nil {
		panic("Couldn't generate private key")
	}
	nodeSECP256PrivateKey, ok := nodePrivateKey.(*crypto.PrivateKeySECP256K1R)
	if !ok {
		panic("Could not cast node's private key to PrivateKeySECP256K1R")
	}
	nodeID := nodePrivateKey.PublicKey().Address()
	return nodeSECP256PrivateKey, ids.NodeID(nodeID)
}
