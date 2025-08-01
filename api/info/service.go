// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/gorilla/rpc/v2"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errNoChainProvided = errors.New("argument 'chain' not given")

	mainnetGetTxFeeResponse = GetTxFeeResponse{
		CreateSubnetTxFee:             json.Uint64(1 * units.Avax),
		TransformSubnetTxFee:          json.Uint64(10 * units.Avax),
		CreateBlockchainTxFee:         json.Uint64(1 * units.Avax),
		AddPrimaryNetworkValidatorFee: json.Uint64(0),
		AddPrimaryNetworkDelegatorFee: json.Uint64(0),
		AddSubnetValidatorFee:         json.Uint64(units.MilliAvax),
		AddSubnetDelegatorFee:         json.Uint64(units.MilliAvax),
	}
	fujiGetTxFeeResponse = GetTxFeeResponse{
		CreateSubnetTxFee:             json.Uint64(100 * units.MilliAvax),
		TransformSubnetTxFee:          json.Uint64(1 * units.Avax),
		CreateBlockchainTxFee:         json.Uint64(100 * units.MilliAvax),
		AddPrimaryNetworkValidatorFee: json.Uint64(0),
		AddPrimaryNetworkDelegatorFee: json.Uint64(0),
		AddSubnetValidatorFee:         json.Uint64(units.MilliAvax),
		AddSubnetDelegatorFee:         json.Uint64(units.MilliAvax),
	}
	defaultGetTxFeeResponse = GetTxFeeResponse{
		CreateSubnetTxFee:             json.Uint64(100 * units.MilliAvax),
		TransformSubnetTxFee:          json.Uint64(100 * units.MilliAvax),
		CreateBlockchainTxFee:         json.Uint64(100 * units.MilliAvax),
		AddPrimaryNetworkValidatorFee: json.Uint64(0),
		AddPrimaryNetworkDelegatorFee: json.Uint64(0),
		AddSubnetValidatorFee:         json.Uint64(units.MilliAvax),
		AddSubnetDelegatorFee:         json.Uint64(units.MilliAvax),
	}
)

// Info is the API service for unprivileged info on a node
type Info struct {
	Parameters
	Log          logging.Logger
	Validators   validators.Manager
	MyIP         *utils.Atomic[netip.AddrPort]
	Networking   network.Network
	ChainManager chains.Manager
	VMManager    vms.Manager
	Benchlist    benchlist.Manager
}

type Parameters struct {
	Version   *version.Application
	NodeID    ids.NodeID
	NodePOP   *signer.ProofOfPossession
	NetworkID uint32
	VMManager vms.Manager
	Upgrades  upgrade.Config

	TxFee            uint64
	CreateAssetTxFee uint64
}

func NewService(info *Info) (http.Handler, error) {
	server := rpc.NewServer()
	codec := json.NewCodec()
	server.RegisterCodec(codec, "application/json")
	server.RegisterCodec(codec, "application/json;charset=UTF-8")
	return server, server.RegisterService(
		info,
		"info",
	)
}

// GetNodeVersionReply are the results from calling GetNodeVersion
type GetNodeVersionReply struct {
	Version            string            `json:"version"`
	DatabaseVersion    string            `json:"databaseVersion"`
	RPCProtocolVersion json.Uint32       `json:"rpcProtocolVersion"`
	GitCommit          string            `json:"gitCommit"`
	VMVersions         map[string]string `json:"vmVersions"`
}

// GetNodeVersion returns the version this node is running
func (i *Info) GetNodeVersion(_ *http.Request, _ *struct{}, reply *GetNodeVersionReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "getNodeVersion"),
	)

	vmVersions, err := i.VMManager.Versions()
	if err != nil {
		return err
	}

	reply.Version = i.Version.String()
	reply.DatabaseVersion = version.CurrentDatabase.String()
	reply.RPCProtocolVersion = json.Uint32(version.RPCChainVMProtocol)
	reply.GitCommit = version.GitCommit
	reply.VMVersions = vmVersions
	return nil
}

// GetNodeIDReply are the results from calling GetNodeID
type GetNodeIDReply struct {
	NodeID  ids.NodeID                `json:"nodeID"`
	NodePOP *signer.ProofOfPossession `json:"nodePOP"`
}

// GetNodeID returns the node ID of this node
func (i *Info) GetNodeID(_ *http.Request, _ *struct{}, reply *GetNodeIDReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "getNodeID"),
	)

	reply.NodeID = i.NodeID
	reply.NodePOP = i.NodePOP
	return nil
}

// GetNetworkIDReply are the results from calling GetNetworkID
type GetNetworkIDReply struct {
	NetworkID json.Uint32 `json:"networkID"`
}

// GetNodeIPReply are the results from calling GetNodeIP
type GetNodeIPReply struct {
	IP netip.AddrPort `json:"ip"`
}

// GetNodeIP returns the IP of this node
func (i *Info) GetNodeIP(_ *http.Request, _ *struct{}, reply *GetNodeIPReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "getNodeIP"),
	)

	reply.IP = i.MyIP.Get()
	return nil
}

// GetNetworkID returns the network ID this node is running on
func (i *Info) GetNetworkID(_ *http.Request, _ *struct{}, reply *GetNetworkIDReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "getNetworkID"),
	)

	reply.NetworkID = json.Uint32(i.NetworkID)
	return nil
}

// GetNetworkNameReply is the result from calling GetNetworkName
type GetNetworkNameReply struct {
	NetworkName string `json:"networkName"`
}

// GetNetworkName returns the network name this node is running on
func (i *Info) GetNetworkName(_ *http.Request, _ *struct{}, reply *GetNetworkNameReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "getNetworkName"),
	)

	reply.NetworkName = constants.NetworkName(i.NetworkID)
	return nil
}

// GetBlockchainIDArgs are the arguments for calling GetBlockchainID
type GetBlockchainIDArgs struct {
	Alias string `json:"alias"`
}

// GetBlockchainIDReply are the results from calling GetBlockchainID
type GetBlockchainIDReply struct {
	BlockchainID ids.ID `json:"blockchainID"`
}

// GetBlockchainID returns the blockchain ID that resolves the alias that was supplied
func (i *Info) GetBlockchainID(_ *http.Request, args *GetBlockchainIDArgs, reply *GetBlockchainIDReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "getBlockchainID"),
	)

	bID, err := i.ChainManager.Lookup(args.Alias)
	reply.BlockchainID = bID
	return err
}

// PeersArgs are the arguments for calling Peers
type PeersArgs struct {
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

type Peer struct {
	peer.Info

	Benched []string `json:"benched"`
}

// PeersReply are the results from calling Peers
type PeersReply struct {
	// Number of elements in [Peers]
	NumPeers json.Uint64 `json:"numPeers"`
	// Each element is a peer
	Peers []Peer `json:"peers"`
}

// Peers returns the list of current validators
func (i *Info) Peers(_ *http.Request, args *PeersArgs, reply *PeersReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "peers"),
	)

	peers := i.Networking.PeerInfo(args.NodeIDs)
	peerInfo := make([]Peer, len(peers))
	for index, peer := range peers {
		benchedIDs := i.Benchlist.GetBenched(peer.ID)
		benchedAliases := make([]string, len(benchedIDs))
		for idx, id := range benchedIDs {
			alias, err := i.ChainManager.PrimaryAlias(id)
			if err != nil {
				return fmt.Errorf("failed to get primary alias for chain ID %s: %w", id, err)
			}
			benchedAliases[idx] = alias
		}
		peerInfo[index] = Peer{
			Info:    peer,
			Benched: benchedAliases,
		}
	}

	reply.Peers = peerInfo
	reply.NumPeers = json.Uint64(len(reply.Peers))
	return nil
}

// IsBootstrappedArgs are the arguments for calling IsBootstrapped
type IsBootstrappedArgs struct {
	// Alias of the chain
	// Can also be the string representation of the chain's ID
	Chain string `json:"chain"`
}

// IsBootstrappedResponse are the results from calling IsBootstrapped
type IsBootstrappedResponse struct {
	// True iff the chain exists and is done bootstrapping
	IsBootstrapped bool `json:"isBootstrapped"`
}

// IsBootstrapped returns nil and sets [reply.IsBootstrapped] == true iff [args.Chain] exists and is done bootstrapping
// Returns an error if the chain doesn't exist
func (i *Info) IsBootstrapped(_ *http.Request, args *IsBootstrappedArgs, reply *IsBootstrappedResponse) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "isBootstrapped"),
		logging.UserString("chain", args.Chain),
	)

	if args.Chain == "" {
		return errNoChainProvided
	}
	chainID, err := i.ChainManager.Lookup(args.Chain)
	if err != nil {
		return fmt.Errorf("there is no chain with alias/ID '%s'", args.Chain)
	}
	reply.IsBootstrapped = i.ChainManager.IsBootstrapped(chainID)
	return nil
}

// Upgrades returns the upgrade schedule this node is running.
func (i *Info) Upgrades(_ *http.Request, _ *struct{}, reply *upgrade.Config) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "upgrades"),
	)

	*reply = i.Parameters.Upgrades
	return nil
}

// UptimeResponse are the results from calling Uptime
type UptimeResponse struct {
	// RewardingStakePercentage shows what percent of network stake thinks we're
	// above the uptime requirement.
	RewardingStakePercentage json.Float64 `json:"rewardingStakePercentage"`

	// WeightedAveragePercentage is the average perceived uptime of this node,
	// weighted by stake.
	// Note that this is different from RewardingStakePercentage, which shows
	// the percent of the network stake that thinks this node is above the
	// uptime requirement. WeightedAveragePercentage is weighted by uptime.
	// i.e If uptime requirement is 85 and a peer reports 40 percent it will be
	// counted (40*weight) in WeightedAveragePercentage but not in
	// RewardingStakePercentage since 40 < 85
	WeightedAveragePercentage json.Float64 `json:"weightedAveragePercentage"`
}

func (i *Info) Uptime(_ *http.Request, _ *struct{}, reply *UptimeResponse) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "uptime"),
	)

	result, err := i.Networking.NodeUptime()
	if err != nil {
		return fmt.Errorf("couldn't get node uptime: %w", err)
	}
	reply.WeightedAveragePercentage = json.Float64(result.WeightedAveragePercentage)
	reply.RewardingStakePercentage = json.Float64(result.RewardingStakePercentage)
	return nil
}

type ACP struct {
	SupportWeight json.Uint64         `json:"supportWeight"`
	Supporters    set.Set[ids.NodeID] `json:"supporters"`
	ObjectWeight  json.Uint64         `json:"objectWeight"`
	Objectors     set.Set[ids.NodeID] `json:"objectors"`
	AbstainWeight json.Uint64         `json:"abstainWeight"`
}

type ACPsReply struct {
	ACPs map[uint32]*ACP `json:"acps"`
}

func (a *ACPsReply) getACP(acpNum uint32) *ACP {
	acp, ok := a.ACPs[acpNum]
	if !ok {
		acp = &ACP{}
		a.ACPs[acpNum] = acp
	}
	return acp
}

func (i *Info) Acps(_ *http.Request, _ *struct{}, reply *ACPsReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "acps"),
	)

	reply.ACPs = make(map[uint32]*ACP, constants.CurrentACPs.Len())
	peers := i.Networking.PeerInfo(nil)
	for _, peer := range peers {
		weight := json.Uint64(i.Validators.GetWeight(constants.PrimaryNetworkID, peer.ID))
		if weight == 0 {
			continue
		}

		for acpNum := range peer.SupportedACPs {
			acp := reply.getACP(acpNum)
			acp.Supporters.Add(peer.ID)
			acp.SupportWeight += weight
		}
		for acpNum := range peer.ObjectedACPs {
			acp := reply.getACP(acpNum)
			acp.Objectors.Add(peer.ID)
			acp.ObjectWeight += weight
		}
	}

	totalWeight, err := i.Validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}
	for acpNum := range constants.CurrentACPs {
		acp := reply.getACP(acpNum)
		acp.AbstainWeight = json.Uint64(totalWeight) - acp.SupportWeight - acp.ObjectWeight
	}
	return nil
}

type GetTxFeeResponse struct {
	TxFee                         json.Uint64 `json:"txFee"`
	CreateAssetTxFee              json.Uint64 `json:"createAssetTxFee"`
	CreateSubnetTxFee             json.Uint64 `json:"createSubnetTxFee"`
	TransformSubnetTxFee          json.Uint64 `json:"transformSubnetTxFee"`
	CreateBlockchainTxFee         json.Uint64 `json:"createBlockchainTxFee"`
	AddPrimaryNetworkValidatorFee json.Uint64 `json:"addPrimaryNetworkValidatorFee"`
	AddPrimaryNetworkDelegatorFee json.Uint64 `json:"addPrimaryNetworkDelegatorFee"`
	AddSubnetValidatorFee         json.Uint64 `json:"addSubnetValidatorFee"`
	AddSubnetDelegatorFee         json.Uint64 `json:"addSubnetDelegatorFee"`
}

// GetTxFee returns the transaction fee in nAVAX.
func (i *Info) GetTxFee(_ *http.Request, _ *struct{}, reply *GetTxFeeResponse) error {
	i.Log.Warn("deprecated API called",
		zap.String("service", "info"),
		zap.String("method", "getTxFee"),
	)

	switch i.NetworkID {
	case constants.MainnetID:
		*reply = mainnetGetTxFeeResponse
	case constants.FujiID:
		*reply = fujiGetTxFeeResponse
	default:
		*reply = defaultGetTxFeeResponse
	}
	reply.TxFee = json.Uint64(i.TxFee)
	reply.CreateAssetTxFee = json.Uint64(i.CreateAssetTxFee)
	return nil
}

// GetVMsReply contains the response metadata for GetVMs
type GetVMsReply struct {
	VMs map[ids.ID][]string `json:"vms"`
	Fxs map[ids.ID]string   `json:"fxs"`
}

// GetVMs lists the virtual machines installed on the node
func (i *Info) GetVMs(_ *http.Request, _ *struct{}, reply *GetVMsReply) error {
	i.Log.Debug("API called",
		zap.String("service", "info"),
		zap.String("method", "getVMs"),
	)

	// Fetch the VMs registered on this node.
	vmIDs, err := i.VMManager.ListFactories()
	if err != nil {
		return err
	}

	reply.VMs, err = ids.GetRelevantAliases(i.VMManager, vmIDs)
	reply.Fxs = map[ids.ID]string{
		secp256k1fx.ID: secp256k1fx.Name,
		nftfx.ID:       nftfx.Name,
		propertyfx.ID:  propertyfx.Name,
	}
	return err
}
