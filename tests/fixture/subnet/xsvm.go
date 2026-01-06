// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnet

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
)

func NewXSVMOrPanic(name string, key *secp256k1.PrivateKey, nodes ...*tmpnet.Node) *tmpnet.Subnet {
	if len(nodes) == 0 {
		panic("a subnet must be validated by at least one node")
	}

	genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, &genesis.Genesis{
		Timestamp: time.Now().Unix(),
		Allocations: []genesis.Allocation{
			{
				Address: key.Address(),
				Balance: math.MaxUint64,
			},
		},
	})
	if err != nil {
		panic(err)
	}

	return &tmpnet.Subnet{
		Name: name,
		Config: tmpnet.ConfigMap{
			// Reducing this from the 1s default speeds up tx acceptance
			"proposerMinBlockDelay": 0,
		},
		Chains: []*tmpnet.Chain{
			{
				VMID:         constants.XSVMID,
				Genesis:      genesisBytes,
				PreFundedKey: key,
				VersionArgs:  []string{"version-json"},
			},
		},
		ValidatorIDs: tmpnet.NodesToIDs(nodes...),
	}
}
