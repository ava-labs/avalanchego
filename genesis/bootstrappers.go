// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/utils/sampler"
)

var bootstrappersPerNetworkID map[uint32][]Bootstrapper

func init() {
	f, err := os.OpenFile("bootstrappers.json", os.O_RDWR, 0600)
	if err != nil {
		panic(fmt.Sprintf("failed to read bootstrappers.json %v", err))
	}
	err = json.NewDecoder(f).Decode(&bootstrappersPerNetworkID)
	if err != nil {
		panic(fmt.Sprintf("failed to decode bootstrappers.json %v", err))
	}
}

// Represents the relationship between the nodeID and the nodeIP.
// Sometimes called "anchor" or "beacon" node.
type Bootstrapper struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}

// SampleBootstrappers returns the some beacons this node should connect to
func SampleBootstrappers(networkID uint32, count int) []Bootstrapper {
	bootstrappers := bootstrappersPerNetworkID[networkID]
	if numIPs := len(bootstrappers); numIPs < count {
		count = numIPs
	}

	s := sampler.NewUniform()
	s.Initialize(uint64(len(bootstrappers)))
	indices, _ := s.Sample(count)

	sampled := make([]Bootstrapper, 0, len(indices))
	for _, index := range indices {
		sampled = append(sampled, bootstrappers[int(index)])
	}

	return sampled
}
