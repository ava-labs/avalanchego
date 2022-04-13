// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/utils/sampler"
)

type node struct {
	ip     string
	nodeId string
}

// getIPs returns the beacon IPs for each network
func getNodes(networkID uint32) []node {
	switch networkID {
	case constants.ColumbusID:
		return []node{{
			ip:     "104.154.245.81:9651",
			nodeId: "NodeID-PGHYeLVkU6ZVQEu8CuRBk6pQ2NJNAuzZ4",
		}}
	default:
		return nil
	}
}

// SampleBeacons returns the some beacons this node should connect to
func SampleBeacons(networkID uint32, count int) ([]string, []string) {
	beacons := getNodes(networkID)

	if numBeacons := len(beacons); numBeacons < count {
		count = numBeacons
	}

	sampledIPs := make([]string, 0, count)
	sampledIDs := make([]string, 0, count)

	s := sampler.NewUniform()
	_ = s.Initialize(uint64(len(beacons)))
	indices, _ := s.Sample(count)
	for _, index := range indices {
		sampledIPs = append(sampledIPs, beacons[int(index)].ip)
		sampledIDs = append(sampledIDs, beacons[int(index)].nodeId)
	}

	return sampledIPs, sampledIDs
}
