// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

type node struct {
	ip     string
	nodeID string
}

// getIPs returns the beacon IPs for each network
func getNodes(networkID uint32) []node {
	switch networkID {
	case constants.ColumbusID:
		return []node{
			{
				ip:     "34.91.158.85:9651",
				nodeID: "NodeID-6XD16eZ22fadTKq3qsxro9TPFZyxTiFv3",
			},
			{
				ip:     "35.205.189.109:9651",
				nodeID: "NodeID-6rsqgkg4F1i3SBjzj4tS5ucQWH7JMEouj",
			},
		}
	case constants.CaminoID:
		return []node{
			// Camino Foundation
			{
				ip:     "34.79.120.198:9651",
				nodeID: "NodeID-MUTNPmSqwWtchZhVvkWRfF8SUK8FtfnwP",
			},
			// Chain4Travel
			{
				ip:     "34.147.107.229:9651",
				nodeID: "NodeID-KumZcRwRSAE7CUkFE18ZLMnPsCVDpQXz8",
			},
		}
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
		sampledIDs = append(sampledIDs, beacons[int(index)].nodeID)
	}

	return sampledIPs, sampledIDs
}
