// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package infoconnect

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/connectproto/pb/info/infoconnect"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/formatting"

	infopb "github.com/ava-labs/avalanchego/connectproto/pb/info"
)

var _ infoconnect.InfoServiceHandler = (*Service)(nil)

type Service struct {
	Info *info.Info
}

func (s *Service) GetNodeVersion(
	_ context.Context,
	_ *connect.Request[infopb.GetNodeVersionRequest],
) (*connect.Response[infopb.GetNodeVersionResponse], error) {
	var jsonResponse info.GetNodeVersionReply
	if err := s.Info.GetNodeVersion(nil, nil, &jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to get node version: %w", err)
	}

	vmVersions := make(map[string]string)
	for id, version := range jsonResponse.VMVersions {
		vmVersions[id] = version
	}

	response := &infopb.GetNodeVersionResponse{
		Version:            jsonResponse.Version,
		DatabaseVersion:    jsonResponse.DatabaseVersion,
		RpcProtocolVersion: uint32(jsonResponse.RPCProtocolVersion),
		GitCommit:          jsonResponse.GitCommit,
		VmVersions:         vmVersions,
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetNodeID(
	_ context.Context,
	_ *connect.Request[infopb.GetNodeIDRequest],
) (*connect.Response[infopb.GetNodeIDResponse], error) {
	var jsonResponse info.GetNodeIDReply
	if err := s.Info.GetNodeID(nil, nil, &jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to get node id: %w", err)
	}

	response := &infopb.GetNodeIDResponse{
		NodeId: jsonResponse.NodeID.String(),
	}

	if jsonResponse.NodePOP != nil {
		pkStr, err := formatting.Encode(
			formatting.HexNC,
			jsonResponse.NodePOP.PublicKey[:],
		)
		if err != nil {
			return nil, fmt.Errorf("failed to encode public key: %w", err)
		}

		signatureStr, err := formatting.Encode(
			formatting.HexNC,
			jsonResponse.NodePOP.ProofOfPossession[:],
		)
		if err != nil {
			return nil, fmt.Errorf("failed to encode proof-of-possession: %w", err)
		}

		response.ProofOfPossession = &infopb.ProofOfPossession{
			PublicKey: pkStr,
			Signature: signatureStr,
		}
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetNodeIP(
	_ context.Context,
	_ *connect.Request[infopb.GetNodeIPRequest],
) (*connect.Response[infopb.GetNodeIPResponse], error) {
	var jsonResponse info.GetNodeIPReply
	if err := s.Info.GetNodeIP(nil, nil, &jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to get node ip: %w", err)
	}

	response := &infopb.GetNodeIPResponse{
		Ip: jsonResponse.IP.String(),
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetNetworkID(
	_ context.Context,
	_ *connect.Request[infopb.GetNetworkIDRequest],
) (*connect.Response[infopb.GetNetworkIDResponse], error) {
	var jsonResponse info.GetNetworkIDReply
	if err := s.Info.GetNetworkID(nil, nil, &jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to get network id: %w", err)
	}

	response := &infopb.GetNetworkIDResponse{
		NetworkId: uint32(jsonResponse.NetworkID),
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetNetworkName(
	_ context.Context,
	_ *connect.Request[infopb.GetNetworkNameRequest],
) (*connect.Response[infopb.GetNetworkNameResponse], error) {
	var jsonResponse info.GetNetworkNameReply
	if err := s.Info.GetNetworkName(nil, nil, &jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to get network name: %w", err)
	}

	response := &infopb.GetNetworkNameResponse{
		NetworkName: jsonResponse.NetworkName,
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetChainID(
	_ context.Context,
	request *connect.Request[infopb.GetChainIDRequest],
) (*connect.Response[infopb.GetChainIDResponse], error) {
	jsonRequest := info.GetBlockchainIDArgs{
		Alias: request.Msg.Alias,
	}

	var jsonResponse info.GetBlockchainIDReply
	if err := s.Info.GetBlockchainID(nil,
		&jsonRequest,
		&jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to get blockchain id: %w", err)
	}

	response := &infopb.GetChainIDResponse{
		ChainId: jsonResponse.BlockchainID.String(),
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetPeers(
	_ context.Context,
	request *connect.Request[infopb.GetPeersRequest],
) (*connect.Response[infopb.GetPeersResponse], error) {
	nodeIDs := make([]ids.NodeID, len(request.Msg.NodeIds))
	for _, nodeIDStr := range request.Msg.NodeIds {
		nodeID, err := ids.NodeIDFromString(nodeIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid nodeID %s: %w", nodeIDStr, err)
		}
		nodeIDs = append(nodeIDs, nodeID)
	}

	jsonRequest := info.PeersArgs{
		NodeIDs: nodeIDs,
	}

	var jsonResponse info.PeersReply
	if err := s.Info.Peers(nil, &jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	peers := make([]*infopb.Peer, 0, len(jsonResponse.Peers))
	for _, peer := range jsonResponse.Peers {
		trackedSubnetsIDs := peer.TrackedSubnets.List()
		trackedSubnets := make([]string, len(trackedSubnetsIDs))
		for i, id := range trackedSubnetsIDs {
			trackedSubnets[i] = id.String()
		}

		benched := make([]string, len(peer.Benched))
		copy(benched, peer.Benched)

		peers = append(peers, &infopb.Peer{
			Ip:             peer.IP.String(),
			PublicIp:       peer.PublicIP.String(),
			NodeId:         peer.ID.String(),
			Version:        peer.Version,
			LastSent:       timestamppb.New(peer.LastSent),
			LastReceived:   timestamppb.New(peer.LastReceived),
			ObservedUptime: uint32(peer.ObservedUptime),
			TrackedSubnets: trackedSubnets,
			Benched:        peer.Benched,
		})
	}

	response := &infopb.GetPeersResponse{
		Peers: peers,
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetBootstrapped(
	_ context.Context,
	request *connect.Request[infopb.GetBootstrappedRequest],
) (*connect.Response[infopb.GetBootstrappedResponse], error) {
	jsonRequest := info.IsBootstrappedArgs{
		Chain: request.Msg.Chain,
	}

	var jsonResponse info.IsBootstrappedResponse
	if err := s.Info.IsBootstrapped(nil,
		&jsonRequest,
		&jsonResponse); err != nil {
		return nil, err
	}

	response := &infopb.GetBootstrappedResponse{
		Bootstrapped: jsonResponse.IsBootstrapped,
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetUpgrades(
	_ context.Context,
	_ *connect.Request[infopb.GetUpgradesRequest],
) (*connect.Response[infopb.GetUpgradesResponse], error) {
	var config upgrade.Config
	if err := s.Info.Upgrades(nil, nil, &config); err != nil {
		return nil, err
	}

	response := &infopb.GetUpgradesResponse{
		ApricotPhase_1Time:            timestamppb.New(config.ApricotPhase1Time),
		ApricotPhase_2Time:            timestamppb.New(config.ApricotPhase2Time),
		ApricotPhase_3Time:            timestamppb.New(config.ApricotPhase3Time),
		ApricotPhase_4Time:            timestamppb.New(config.ApricotPhase4Time),
		ApricotPhase_4MinPChainHeight: config.ApricotPhase4MinPChainHeight,
		ApricotPhase_5Time:            timestamppb.New(config.ApricotPhase5Time),
		ApricotPhasePre_6Time:         timestamppb.New(config.ApricotPhasePre6Time),
		ApricotPhase_6Time:            timestamppb.New(config.ApricotPhase6Time),
		ApricotPhasePost_6Time:        timestamppb.New(config.ApricotPhasePost6Time),
		BanffTime:                     timestamppb.New(config.BanffTime),
		CortinaTime:                   timestamppb.New(config.CortinaTime),
		CortinaXChainStopVertexId:     config.CortinaXChainStopVertexID.String(),
		DurangoTime:                   timestamppb.New(config.DurangoTime),
		EtnaTime:                      timestamppb.New(config.EtnaTime),
		FortunaTime:                   timestamppb.New(config.FortunaTime),
		GraniteTime:                   timestamppb.New(config.GraniteTime),
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetUptime(
	_ context.Context,
	_ *connect.Request[infopb.GetUptimeRequest],
) (*connect.Response[infopb.GetUptimeResponse], error) {
	var jsonResponse info.UptimeResponse
	if err := s.Info.Uptime(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	response := &infopb.GetUptimeResponse{
		RewardingStakePercentage:  float64(jsonResponse.RewardingStakePercentage),
		WeightedAveragePercentage: float64(jsonResponse.WeightedAveragePercentage),
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetACPs(
	_ context.Context,
	_ *connect.Request[infopb.GetACPsRequest],
) (*connect.Response[infopb.GetACPsResponse], error) {
	jsonResponse := &info.ACPsReply{}
	if err := s.Info.Acps(nil, nil, jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to get acps: %w", err)
	}

	response := &infopb.GetACPsResponse{
		Acps: make(map[string]*infopb.ACP),
	}

	for acpNumber, acpInfo := range jsonResponse.ACPs {
		supporters := make([]string, len(acpInfo.Supporters))
		for s := range acpInfo.Supporters {
			supporters = append(supporters, s.String())
		}

		objectors := make([]string, len(acpInfo.Objectors))
		for o := range acpInfo.Objectors {
			objectors = append(objectors, o.String())
		}

		acpStr := fmt.Sprintf("ACP-%d", acpNumber)
		response.Acps[acpStr] = &infopb.ACP{
			SupportWeight: uint64(acpInfo.SupportWeight),
			Supporters:    supporters,
			ObjectWeight:  uint64(acpInfo.ObjectWeight),
			Objectors:     objectors,
			AbstainWeight: uint64(acpInfo.AbstainWeight),
		}
	}

	return connect.NewResponse(response), nil
}

func (s *Service) GetVMs(
	_ context.Context,
	_ *connect.Request[infopb.GetVMsRequest],
) (*connect.Response[infopb.GetVMsResponse], error) {
	var jsonResponse info.GetVMsReply
	if err := s.Info.GetVMs(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	vms := make(map[string]*infopb.VMAliases)
	for vmID, aliases := range jsonResponse.VMs {
		vms[vmID.String()] = &infopb.VMAliases{
			Aliases: aliases,
		}
	}

	fxs := make(map[string]string)
	for fxID, name := range jsonResponse.Fxs {
		fxs[fxID.String()] = name
	}

	response := &infopb.GetVMsResponse{
		Vms: vms,
		Fxs: fxs,
	}

	return connect.NewResponse(response), nil
}
