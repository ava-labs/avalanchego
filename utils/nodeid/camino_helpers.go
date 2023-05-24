// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package nodeid

import (
	"crypto/rsa"
	"strconv"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

var localNodesCount = 5

func LoadLocalCaminoNodeKeysAndIDs(localStakingPath string) ([]*secp256k1.PrivateKey, []ids.NodeID) {
	nodeKeys := make([]*secp256k1.PrivateKey, localNodesCount)
	nodeIDs := make([]ids.NodeID, localNodesCount)

	for index := 0; index < localNodesCount; index++ {
		secp256Factory := secp256k1.Factory{}
		var nodePrivateKey *secp256k1.PrivateKey

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
		secpPrivateKey := secp256k1.RsaPrivateKeyToSecp256PrivateKey(rsaKey)
		nodePrivateKey, err = secp256Factory.ToPrivateKey(secpPrivateKey.Serialize())
		if err != nil {
			panic(err)
		}
		nodeID := nodePrivateKey.Address()
		nodeKeys[index] = nodePrivateKey
		nodeIDs[index] = ids.NodeID(nodeID)
	}
	return nodeKeys, nodeIDs
}

func GenerateCaminoNodeKeyAndID() (*secp256k1.PrivateKey, ids.NodeID) {
	secp256Factory := secp256k1.Factory{}
	nodePrivateKey, err := secp256Factory.NewPrivateKey()
	if err != nil {
		panic("Couldn't generate private key")
	}
	return nodePrivateKey, ids.NodeID(nodePrivateKey.Address())
}
