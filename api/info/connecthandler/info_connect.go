// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connecthandler

import (
	"context"
	"encoding/json"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/info/v1/infov1connect"
	"github.com/ava-labs/avalanchego/upgrade"

	infov1 "github.com/ava-labs/avalanchego/proto/pb/info/v1"
)

var _ infov1connect.InfoServiceHandler = (*ConnectInfoService)(nil)

type ConnectInfoService struct {
	info *info.Info
}

// NewConnectInfoService returns a pointer to a ConnectRPC handler for the Info API
func NewConnectInfoService(info *info.Info) *ConnectInfoService {
	return &ConnectInfoService{info: info}
}

// NodeVersion returns version this node is running
func (c *ConnectInfoService) NodeVersion(
	_ context.Context,
	_ *connect.Request[infov1.NodeVersionRequest],
) (*connect.Response[infov1.NodeVersionResponse], error) {
	var jsonResponse info.GetNodeVersionReply
	if err := c.info.GetNodeVersion(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	// Convert VM versions map to protobuf format
	vmVersions := make(map[string]string)
	for id, version := range jsonResponse.VMVersions {
		vmVersions[id] = version
	}

	response := &infov1.NodeVersionResponse{
		Version:            jsonResponse.Version,
		DatabaseVersion:    jsonResponse.DatabaseVersion,
		RpcProtocolVersion: uint32(jsonResponse.RPCProtocolVersion),
		GitCommit:          jsonResponse.GitCommit,
		VmVersions:         vmVersions,
	}

	return connect.NewResponse(response), nil
}

// NodeID returns the node ID of this node
func (c *ConnectInfoService) NodeID(
	_ context.Context,
	_ *connect.Request[infov1.NodeIDRequest],
) (*connect.Response[infov1.NodeIDResponse], error) {
	var jsonResponse info.GetNodeIDReply
	if err := c.info.GetNodeID(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	pop := jsonResponse.NodePOP
	if err := pop.Verify(); err != nil {
		return nil, err
	}
	rawPopJSON, err := pop.MarshalJSON()
	if err != nil {
		return nil, err
	}

	type jsonProofOfPossession struct {
		PublicKey         string `json:"publicKey"`
		ProofOfPossession string `json:"proofOfPossession"`
	}

	var popJSON jsonProofOfPossession
	if err := json.Unmarshal(rawPopJSON, &popJSON); err != nil {
		return nil, err
	}

	response := &infov1.NodeIDResponse{
		NodeId: jsonResponse.NodeID.String(),
		NodePop: &infov1.ProofOfPossession{
			PublicKey:         popJSON.PublicKey,
			ProofOfPossession: popJSON.ProofOfPossession,
		},
	}

	return connect.NewResponse(response), nil
}

// NodeIP returns the IP address of this node
func (c *ConnectInfoService) NodeIP(
	_ context.Context,
	_ *connect.Request[infov1.NodeIPRequest],
) (*connect.Response[infov1.NodeIPResponse], error) {
	var jsonResponse info.GetNodeIPReply
	if err := c.info.GetNodeIP(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &infov1.NodeIPResponse{
		Ip: jsonResponse.IP.String(),
	}

	return connect.NewResponse(response), nil
}

// NetworkID returns the ID of the Avalanche network this node is connected to
func (c *ConnectInfoService) NetworkID(
	_ context.Context,
	_ *connect.Request[infov1.NetworkIDRequest],
) (*connect.Response[infov1.NetworkIDResponse], error) {
	var jsonResponse info.GetNetworkIDReply
	if err := c.info.GetNetworkID(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &infov1.NetworkIDResponse{
		NetworkId: uint32(jsonResponse.NetworkID),
	}

	return connect.NewResponse(response), nil
}

// NetworkName returns the name of the network
func (c *ConnectInfoService) NetworkName(
	_ context.Context,
	_ *connect.Request[infov1.NetworkNameRequest],
) (*connect.Response[infov1.NetworkNameResponse], error) {
	var jsonResponse info.GetNetworkNameReply
	if err := c.info.GetNetworkName(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &infov1.NetworkNameResponse{
		NetworkName: jsonResponse.NetworkName,
	}

	return connect.NewResponse(response), nil
}

// BlockchainID maps an ID string to its canonical chain ID
func (c *ConnectInfoService) BlockchainID(
	_ context.Context,
	request *connect.Request[infov1.BlockchainIDRequest],
) (*connect.Response[infov1.BlockchainIDResponse], error) {
	jsonRequest := info.GetBlockchainIDArgs{
		Alias: request.Msg.Alias,
	}

	var jsonResponse info.GetBlockchainIDReply
	if err := c.info.GetBlockchainID(nil, &jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	response := &infov1.BlockchainIDResponse{
		BlockchainId: jsonResponse.BlockchainID.String(),
	}

	return connect.NewResponse(response), nil
}

// Peers returns metadata for the given peer node IDs
func (c *ConnectInfoService) Peers(
	_ context.Context,
	request *connect.Request[infov1.PeersRequest],
) (*connect.Response[infov1.PeersResponse], error) {
	nodeIDs := make([]ids.NodeID, 0, len(request.Msg.NodeIds))
	for _, nodeIDStr := range request.Msg.NodeIds {
		nodeID, err := ids.NodeIDFromString(nodeIDStr)
		if err != nil {
			return nil, err
		}
		nodeIDs = append(nodeIDs, nodeID)
	}

	jsonRequest := info.PeersArgs{
		NodeIDs: nodeIDs,
	}

	var jsonResponse info.PeersReply
	if err := c.info.Peers(nil, &jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	peers := make([]*infov1.PeerInfo, 0, len(jsonResponse.Peers))
	for _, peer := range jsonResponse.Peers {
		trackedSubnetsIDs := peer.TrackedSubnets.List()
		trackedSubnets := make([]string, len(trackedSubnetsIDs))
		for _, subnetID := range peer.TrackedSubnets.List() {
			trackedSubnets = append(trackedSubnets, subnetID.String())
		}
		for i, id := range trackedSubnetsIDs {
			trackedSubnets[i] = id.String()
		}

		benched := make([]string, len(peer.Benched))
		copy(benched, peer.Benched)

		peers = append(peers, &infov1.PeerInfo{
			Ip:             peer.IP.String(),
			PublicIp:       peer.PublicIP.String(),
			NodeId:         peer.ID.String(),
			Version:        peer.Version,
			LastSent:       timestamppb.New(peer.LastSent),
			LastReceived:   timestamppb.New(peer.LastReceived),
			Benched:        benched,
			ObservedUptime: durationpb.New(time.Duration(peer.ObservedUptime) * time.Second),
			TrackedSubnets: trackedSubnets,
		})
	}

	response := &infov1.PeersResponse{
		NumPeers: uint32(jsonResponse.NumPeers),
		Peers:    peers,
	}

	return connect.NewResponse(response), nil
}

// IsBootstrapped returns whether the named chain has finished its bootstrap process on this node
func (c *ConnectInfoService) IsBootstrapped(
	_ context.Context,
	request *connect.Request[infov1.IsBootstrappedRequest],
) (*connect.Response[infov1.IsBootstrappedResponse], error) {
	jsonRequest := info.IsBootstrappedArgs{
		Chain: request.Msg.Chain,
	}

	var jsonResponse info.IsBootstrappedResponse
	if err := c.info.IsBootstrapped(nil, &jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	response := &infov1.IsBootstrappedResponse{
		IsBootstrapped: jsonResponse.IsBootstrapped,
	}

	return connect.NewResponse(response), nil
}

// Upgrades returns info about all scheduled upgrades for this node
func (c *ConnectInfoService) Upgrades(
	_ context.Context,
	_ *connect.Request[infov1.UpgradesRequest],
) (*connect.Response[infov1.UpgradesResponse], error) {
	var config upgrade.Config
	if err := c.info.Upgrades(nil, nil, &config); err != nil {
		return nil, err
	}

	response := &infov1.UpgradesResponse{
		ApricotPhase1Time:            timestamppb.New(config.ApricotPhase1Time),
		ApricotPhase2Time:            timestamppb.New(config.ApricotPhase2Time),
		ApricotPhase3Time:            timestamppb.New(config.ApricotPhase3Time),
		ApricotPhase4Time:            timestamppb.New(config.ApricotPhase4Time),
		ApricotPhase4MinPChainHeight: config.ApricotPhase4MinPChainHeight,
		ApricotPhase5Time:            timestamppb.New(config.ApricotPhase5Time),
		ApricotPhasePre6Time:         timestamppb.New(config.ApricotPhasePre6Time),
		ApricotPhase6Time:            timestamppb.New(config.ApricotPhase6Time),
		ApricotPhasePost6Time:        timestamppb.New(config.ApricotPhasePost6Time),
		BanffTime:                    timestamppb.New(config.BanffTime),
		CortinaTime:                  timestamppb.New(config.CortinaTime),
		CortinaXChainStopVertexId:    config.CortinaXChainStopVertexID.String(),
		DurangoTime:                  timestamppb.New(config.DurangoTime),
		EtnaTime:                     timestamppb.New(config.EtnaTime),
		FortunaTime:                  timestamppb.New(config.FortunaTime),
		GraniteTime:                  timestamppb.New(config.GraniteTime),
	}

	return connect.NewResponse(response), nil
}

// Uptime returns this node's uptime metrics
func (c *ConnectInfoService) Uptime(
	_ context.Context,
	_ *connect.Request[infov1.UptimeRequest],
) (*connect.Response[infov1.UptimeResponse], error) {
	var jsonResponse info.UptimeResponse
	if err := c.info.Uptime(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &infov1.UptimeResponse{
		RewardingStakePercentage:  float64(jsonResponse.RewardingStakePercentage),
		WeightedAveragePercentage: float64(jsonResponse.WeightedAveragePercentage),
	}

	return connect.NewResponse(response), nil
}

// VMs returns a map of VM IDs to their known aliases
func (c *ConnectInfoService) VMs(
	_ context.Context,
	_ *connect.Request[infov1.VMsRequest],
) (*connect.Response[infov1.VMsResponse], error) {
	var jsonResponse info.GetVMsReply
	if err := c.info.GetVMs(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	vms := make(map[string]*infov1.VMAliases)
	for vmID, aliases := range jsonResponse.VMs {
		vms[vmID.String()] = &infov1.VMAliases{
			Aliases: aliases,
		}
	}

	fxs := make(map[string]string)
	for fxID, name := range jsonResponse.Fxs {
		fxs[fxID.String()] = name
	}

	response := &infov1.VMsResponse{
		Vms: vms,
		Fxs: fxs,
	}

	return connect.NewResponse(response), nil
}
