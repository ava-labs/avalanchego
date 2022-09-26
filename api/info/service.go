// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/benchlist"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"

	xchainconfig "github.com/ava-labs/avalanchego/vms/avm/config"
	pchainconfig "github.com/ava-labs/avalanchego/vms/platformvm/config"
)

var (
	errNoChainProvided = errors.New("argument 'chain' not given")
	errNotValidator    = errors.New("this is not a validator node")
)

// Info is the API service for unprivileged info on a node
type Info struct {
	Parameters
	log          logging.Logger
	myIP         ips.DynamicIPPort
	networking   network.Network
	chainManager chains.Manager
	vmManager    vms.Manager
	validators   validators.Set
	benchlist    benchlist.Manager
}

type Parameters struct {
	PChainTxFees pchainconfig.TxFeeUpgrades
	XChainTxFees xchainconfig.TxFees
	Version      *version.Application
	NodeID       ids.NodeID
	NodePOP      *signer.ProofOfPossession
	NetworkID    uint32
	VMManager    vms.Manager
}

// NewService returns a new admin API service
func NewService(
	parameters Parameters,
	log logging.Logger,
	chainManager chains.Manager,
	vmManager vms.Manager,
	myIP ips.DynamicIPPort,
	network network.Network,
	validators validators.Set,
	benchlist benchlist.Manager,
) (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(&Info{
		Parameters:   parameters,
		log:          log,
		chainManager: chainManager,
		vmManager:    vmManager,
		myIP:         myIP,
		networking:   network,
		validators:   validators,
		benchlist:    benchlist,
	}, "info"); err != nil {
		return nil, err
	}
	return &common.HTTPHandler{Handler: newServer}, nil
}

// GetNodeVersionReply are the results from calling GetNodeVersion
type GetNodeVersionReply struct {
	Version         string            `json:"version"`
	DatabaseVersion string            `json:"databaseVersion"`
	GitCommit       string            `json:"gitCommit"`
	VMVersions      map[string]string `json:"vmVersions"`
}

// GetNodeVersion returns the version this node is running
func (service *Info) GetNodeVersion(_ *http.Request, _ *struct{}, reply *GetNodeVersionReply) error {
	service.log.Debug("Info: GetNodeVersion called")

	vmVersions, err := service.vmManager.Versions()
	if err != nil {
		return err
	}

	reply.Version = service.Version.String()
	reply.DatabaseVersion = version.CurrentDatabase.String()
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
func (service *Info) GetNodeID(_ *http.Request, _ *struct{}, reply *GetNodeIDReply) error {
	service.log.Debug("Info: GetNodeID called")

	reply.NodeID = service.NodeID
	reply.NodePOP = service.NodePOP
	return nil
}

// GetNetworkIDReply are the results from calling GetNetworkID
type GetNetworkIDReply struct {
	NetworkID json.Uint32 `json:"networkID"`
}

// GetNodeIPReply are the results from calling GetNodeIP
type GetNodeIPReply struct {
	IP string `json:"ip"`
}

// GetNodeIP returns the IP of this node
func (service *Info) GetNodeIP(_ *http.Request, _ *struct{}, reply *GetNodeIPReply) error {
	service.log.Debug("Info: GetNodeIP called")

	reply.IP = service.myIP.IPPort().String()
	return nil
}

// GetNetworkID returns the network ID this node is running on
func (service *Info) GetNetworkID(_ *http.Request, _ *struct{}, reply *GetNetworkIDReply) error {
	service.log.Debug("Info: GetNetworkID called")

	reply.NetworkID = json.Uint32(service.NetworkID)
	return nil
}

// GetNetworkNameReply is the result from calling GetNetworkName
type GetNetworkNameReply struct {
	NetworkName string `json:"networkName"`
}

// GetNetworkName returns the network name this node is running on
func (service *Info) GetNetworkName(_ *http.Request, _ *struct{}, reply *GetNetworkNameReply) error {
	service.log.Debug("Info: GetNetworkName called")

	reply.NetworkName = constants.NetworkName(service.NetworkID)
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
func (service *Info) GetBlockchainID(_ *http.Request, args *GetBlockchainIDArgs, reply *GetBlockchainIDReply) error {
	service.log.Debug("Info: GetBlockchainID called")

	bID, err := service.chainManager.Lookup(args.Alias)
	reply.BlockchainID = bID
	return err
}

// PeersArgs are the arguments for calling Peers
type PeersArgs struct {
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

type Peer struct {
	peer.Info

	Benched []ids.ID `json:"benched"`
}

// PeersReply are the results from calling Peers
type PeersReply struct {
	// Number of elements in [Peers]
	NumPeers json.Uint64 `json:"numPeers"`
	// Each element is a peer
	Peers []Peer `json:"peers"`
}

// Peers returns the list of current validators
func (service *Info) Peers(_ *http.Request, args *PeersArgs, reply *PeersReply) error {
	service.log.Debug("Info: Peers called")

	peers := service.networking.PeerInfo(args.NodeIDs)
	peerInfo := make([]Peer, len(peers))
	for i, peer := range peers {
		peerInfo[i] = Peer{
			Info:    peer,
			Benched: service.benchlist.GetBenched(peer.ID),
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
func (service *Info) IsBootstrapped(_ *http.Request, args *IsBootstrappedArgs, reply *IsBootstrappedResponse) error {
	service.log.Debug("Info: IsBootstrapped called",
		logging.UserString("chain", args.Chain),
	)

	if args.Chain == "" {
		return errNoChainProvided
	}
	chainID, err := service.chainManager.Lookup(args.Chain)
	if err != nil {
		return fmt.Errorf("there is no chain with alias/ID '%s'", args.Chain)
	}
	reply.IsBootstrapped = service.chainManager.IsBootstrapped(chainID)
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

func (service *Info) Uptime(_ *http.Request, _ *struct{}, reply *UptimeResponse) error {
	service.log.Debug("Info: Uptime called")
	result, isValidator := service.networking.NodeUptime()
	if !isValidator {
		return errNotValidator
	}
	reply.WeightedAveragePercentage = json.Float64(result.WeightedAveragePercentage)
	reply.RewardingStakePercentage = json.Float64(result.RewardingStakePercentage)
	return nil
}

// TODO: Remove all deprecated fields
type GetTxFeeResponse struct {
	// Deprecated: Use the chain specific fields
	TxFee json.Uint64 `json:"txFee"`
	// Deprecated: Use the chain specific fields
	CreationTxFee json.Uint64 `json:"creationTxFee"`
	// Deprecated: Use the chain specific fields
	CreateAssetTxFee json.Uint64 `json:"createAssetTxFee"`
	// Deprecated: Use the chain specific fields
	CreateSubnetTxFee json.Uint64 `json:"createSubnetTxFee"`
	// Deprecated: Use the chain specific fields
	TransformSubnetTxFee json.Uint64 `json:"transformSubnetTxFee"`
	// Deprecated: Use the chain specific fields
	CreateBlockchainTxFee json.Uint64 `json:"createBlockchainTxFee"`
	// Deprecated: Use the chain specific fields
	AddPrimaryNetworkValidatorFee json.Uint64 `json:"addPrimaryNetworkValidatorFee"`
	// Deprecated: Use the chain specific fields
	AddPrimaryNetworkDelegatorFee json.Uint64 `json:"addPrimaryNetworkDelegatorFee"`
	// Deprecated: Use the chain specific fields
	AddSubnetValidatorFee json.Uint64 `json:"addSubnetValidatorFee"`
	// Deprecated: Use the chain specific fields
	AddSubnetDelegatorFee json.Uint64 `json:"addSubnetDelegatorFee"`

	PChainAddPrimaryNetworkValidator json.Uint64 `json:"pChainAddPrimaryNetworkValidator"`
	PChainAddPrimaryNetworkDelegator json.Uint64 `json:"pChainAddPrimaryNetworkDelegator"`
	PChainAddPOASubnetValidator      json.Uint64 `json:"pChainAddPOASubnetValidator"`
	PChainAddPOSSubnetValidator      json.Uint64 `json:"pChainAddPOSSubnetValidator"`
	PChainAddPOSSubnetDelegator      json.Uint64 `json:"pChainAddPOSSubnetDelegator"`
	PChainRemovePOASubnetValidator   json.Uint64 `json:"pChainRemovePOASubnetValidator"`
	PChainCreateSubnet               json.Uint64 `json:"pChainCreateSubnet"`
	PChainCreateChain                json.Uint64 `json:"pChainCreateChain"`
	PChainTransformSubnet            json.Uint64 `json:"pChainTransformSubnet"`
	PChainImport                     json.Uint64 `json:"pChainImport"`
	PChainExport                     json.Uint64 `json:"pChainExport"`

	XChainBase        json.Uint64 `json:"xChainBase"`
	XChainCreateAsset json.Uint64 `json:"xChainCreateAsset"`
	XChainOperation   json.Uint64 `json:"xChainOperation"`
	XChainImport      json.Uint64 `json:"xChainImport"`
	XChainExport      json.Uint64 `json:"xChainExport"`
}

// GetTxFee returns the transaction fee in nAVAX.
func (service *Info) GetTxFee(_ *http.Request, args *struct{}, reply *GetTxFeeResponse) error {
	// Deprecated fields:
	reply.TxFee = json.Uint64(service.XChainTxFees.Base)
	reply.CreationTxFee = json.Uint64(service.XChainTxFees.CreateAsset)
	reply.CreateAssetTxFee = json.Uint64(service.XChainTxFees.CreateAsset)
	reply.CreateSubnetTxFee = json.Uint64(service.PChainTxFees.BlueberryFees.CreateSubnet)
	reply.TransformSubnetTxFee = json.Uint64(service.PChainTxFees.BlueberryFees.TransformSubnet)
	reply.CreateBlockchainTxFee = json.Uint64(service.PChainTxFees.BlueberryFees.CreateChain)
	reply.AddPrimaryNetworkValidatorFee = json.Uint64(service.PChainTxFees.BlueberryFees.AddPrimaryNetworkValidator)
	reply.AddPrimaryNetworkDelegatorFee = json.Uint64(service.PChainTxFees.BlueberryFees.AddPrimaryNetworkDelegator)
	reply.AddSubnetValidatorFee = json.Uint64(service.PChainTxFees.BlueberryFees.AddPOSSubnetValidator)
	reply.AddSubnetDelegatorFee = json.Uint64(service.PChainTxFees.BlueberryFees.AddPOSSubnetDelegator)

	// P-chain fees
	reply.PChainAddPrimaryNetworkValidator = json.Uint64(service.PChainTxFees.BlueberryFees.AddPrimaryNetworkValidator)
	reply.PChainAddPrimaryNetworkDelegator = json.Uint64(service.PChainTxFees.BlueberryFees.AddPrimaryNetworkDelegator)
	reply.PChainAddPOASubnetValidator = json.Uint64(service.PChainTxFees.BlueberryFees.AddPOASubnetValidator)
	reply.PChainAddPOSSubnetValidator = json.Uint64(service.PChainTxFees.BlueberryFees.AddPOSSubnetValidator)
	reply.PChainAddPOSSubnetDelegator = json.Uint64(service.PChainTxFees.BlueberryFees.AddPOSSubnetDelegator)
	reply.PChainRemovePOASubnetValidator = json.Uint64(service.PChainTxFees.BlueberryFees.RemovePOASubnetValidator)
	reply.PChainCreateSubnet = json.Uint64(service.PChainTxFees.BlueberryFees.CreateSubnet)
	reply.PChainCreateChain = json.Uint64(service.PChainTxFees.BlueberryFees.CreateChain)
	reply.PChainTransformSubnet = json.Uint64(service.PChainTxFees.BlueberryFees.TransformSubnet)
	reply.PChainImport = json.Uint64(service.PChainTxFees.BlueberryFees.Import)
	reply.PChainExport = json.Uint64(service.PChainTxFees.BlueberryFees.Export)

	// X-chain fees
	reply.XChainBase = json.Uint64(service.XChainTxFees.Base)
	reply.XChainCreateAsset = json.Uint64(service.XChainTxFees.CreateAsset)
	reply.XChainOperation = json.Uint64(service.XChainTxFees.Operation)
	reply.XChainImport = json.Uint64(service.XChainTxFees.Import)
	reply.XChainExport = json.Uint64(service.XChainTxFees.Export)

	return nil
}

// GetVMsReply contains the response metadata for GetVMs
type GetVMsReply struct {
	VMs map[ids.ID][]string `json:"vms"`
}

// GetVMs lists the virtual machines installed on the node
func (service *Info) GetVMs(_ *http.Request, _ *struct{}, reply *GetVMsReply) error {
	service.log.Debug("Info: GetVMs called")

	// Fetch the VMs registered on this node.
	vmIDs, err := service.VMManager.ListFactories()
	if err != nil {
		return err
	}

	reply.VMs, err = ids.GetRelevantAliases(service.VMManager, vmIDs)
	return err
}
