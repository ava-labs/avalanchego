// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"github.com/ava-labs/avalanchego/tests/fixture/subnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

var (
	SubnetEVMNameA = "subnet-evm-a"
	SubnetEVMNameB = "subnet-evm-b"
)

func SubnetEVMSubnetsOrPanic(preFundedKeys []*secp256k1.PrivateKey, nodes ...*tmpnet.Node) []*tmpnet.Subnet {
	subnetANodes := nodes
	subnetBNodes := nodes
	if len(nodes) > 1 {
		// Validate tmpnet bootstrap of a disjoint validator set
		midpoint := len(nodes) / 2
		subnetANodes = nodes[:midpoint]
		subnetBNodes = nodes[midpoint:]
	}
	return []*tmpnet.Subnet{
		subnet.NewSubnetEVMSubnetOrPanic(SubnetEVMNameA, preFundedKeys, subnetANodes...),
		subnet.NewSubnetEVMSubnetOrPanic(SubnetEVMNameB, preFundedKeys, subnetBNodes...),
	}
}
