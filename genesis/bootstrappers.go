// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"bytes"
	"encoding/json"
	"fmt"

	_ "embed"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

var (
	//go:embed bootstrappers.json
	bootstrappersPerNetworkRawJSON []byte
)

var bootstrappersPerNetwork map[uint32][]Bootstrapper

func init() {
	err := json.NewDecoder(bytes.NewReader(bootstrappersPerNetworkRawJSON)).Decode(&bootstrappersPerNetwork)
	if err != nil {
		panic(fmt.Sprintf("failed to decode bootstrappers.json %v", err))
	}
}

// Represents the relationship between the nodeID and the nodeIP.
// Sometimes called "anchor" or "beacon" node.
type Bootstrapper struct {
	ID ids.NodeID `json:"id"`
	IP ips.IPDesc `json:"ip"`
}

// SampleBootstrappers returns the some beacons this node should connect to
func SampleBootstrappers(networkID uint32, count int) []Bootstrapper {
	bootstrappers := bootstrappersPerNetwork[networkID]
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
