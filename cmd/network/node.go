// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/spf13/viper"
)

type node struct {
	nodeID ids.NodeID
	ip     ips.IPPort
	weight uint64
}

func (p *node) String() string { return fmt.Sprintf("Node(%s, %s, %d)", p.nodeID, p.ip, p.weight) }

func getNodes(ctx context.Context, v *viper.Viper) ([]node, error) {
	var (
		uri          = v.GetString(UriKey)
		infoClient   = info.NewClient(uri)
		pChainClient = platformvm.NewClient(uri)
	)
	peers, err := infoClient.Peers(ctx)
	if err != nil {
		return nil, err
	}
	subnetID, err := ids.FromString(v.GetString(SubnetIDKey))
	if err != nil {
		return nil, err
	}
	vdrs, err := pChainClient.GetCurrentValidators(ctx, subnetID, nil)
	if err != nil {
		return nil, err
	}

	// If specified, cut peers to only the specified publicIPs
	if v.IsSet(IPPortKey) {
		publicIPs := v.GetStringSlice(IPPortKey)
		publicIPsSet := make(map[string]struct{})
		for _, publicIP := range publicIPs {
			publicIPsSet[publicIP] = struct{}{}
		}

		selectPeers := make([]info.Peer, 0, len(publicIPs))
		for _, peer := range peers {
			if _, ok := publicIPsSet[peer.PublicIP]; ok {
				selectPeers = append(selectPeers, peer)
			}
		}

		peers = selectPeers
	}

	// If a peer limit is specified, cut peers to the peer limit
	if v.IsSet(PeerLimitKey) {
		peerLimit := v.GetInt(PeerLimitKey)
		if peerLimit > 0 && peerLimit <= len(peers) {
			peers = peers[:peerLimit]
		}
	}

	peerIPToVdr := make(map[ids.NodeID]platformvm.ClientPermissionlessValidator, len(vdrs))
	for _, vdr := range vdrs {
		peerIPToVdr[vdr.NodeID] = vdr
	}

	nodes := make([]node, 0, len(peers))
	for _, peer := range peers {
		publicIP, err := ips.ToIPPort(peer.PublicIP)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node{
			nodeID: peer.ID,
			ip:     publicIP,
			weight: peerIPToVdr[peer.ID].Weight,
		})
	}
	return nodes, nil
}
