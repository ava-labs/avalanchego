// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for interacting with the P Chain endpoint
type Client interface {
	// GetHeight returns the current block height of the P Chain
	GetHeight() (uint64, error)
	// ExportKey returns the private key corresponding to [address] from [user]'s account
	ExportKey(user api.UserPass, address string) (string, error)
	// ImportKey imports the specified [privateKey] to [user]'s keystore
	ImportKey(user api.UserPass, address string) (string, error)
	// GetBalance returns the balance of [address] on the P Chain
	GetBalance(addr string) (*GetBalanceResponse, error)
	// CreateAddress creates a new address for [user]
	CreateAddress(user api.UserPass) (string, error)
	// ListAddresses returns an array of platform addresses controlled by [user]
	ListAddresses(user api.UserPass) ([]string, error)
	// GetUTXOs returns the byte representation of the UTXOs controlled by [addrs]
	GetUTXOs(
		addrs []string,
		limit uint32,
		startAddress,
		startUTXOID string,
	) ([][]byte, api.Index, error)
	// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addresses]
	// from [sourceChain]
	GetAtomicUTXOs(
		addrs []string,
		sourceChain string,
		limit uint32,
		startAddress,
		startUTXOID string,
	) ([][]byte, api.Index, error)
	// GetSubnets returns information about the specified subnets
	GetSubnets([]ids.ID) ([]APISubnet, error)
	// GetStakingAssetID returns the assetID of the asset used for staking on
	// subnet corresponding to [subnetID]
	GetStakingAssetID(ids.ID) (ids.ID, error)
	// GetCurrentValidators returns the list of current validators for subnet with ID [subnetID]
	GetCurrentValidators(subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, error)
	// GetPendingValidators returns the list of pending validators for subnet with ID [subnetID]
	GetPendingValidators(subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, []interface{}, error)
	// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
	GetCurrentSupply() (uint64, error)
	// SampleValidators returns the nodeIDs of a sample of [sampleSize] validators from the current validator set for subnet with ID [subnetID]
	SampleValidators(subnetID ids.ID, sampleSize uint16) ([]string, error)
	// AddValidator issues a transaction to add a validator to the primary network
	// and returns the txID
	AddValidator(
		user api.UserPass,
		from []string,
		changeAddr string,
		rewardAddress,
		nodeID string,
		stakeAmount,
		startTime,
		endTime uint64,
		delegationFeeRate float32,
	) (ids.ID, error)
	// AddDelegator issues a transaction to add a delegator to the primary network
	// and returns the txID
	AddDelegator(
		user api.UserPass,
		from []string,
		changeAddr string,
		rewardAddress,
		nodeID string,
		stakeAmount,
		startTime,
		endTime uint64,
	) (ids.ID, error)
	// AddSubnetValidator issues a transaction to add validator [nodeID] to subnet
	// with ID [subnetID] and returns the txID
	AddSubnetValidator(
		user api.UserPass,
		from []string,
		changeAddr string,
		subnetID,
		nodeID string,
		stakeAmount,
		startTime,
		endTime uint64,
	) (ids.ID, error)
	// CreateSubnet issues a transaction to create [subnet] and returns the txID
	CreateSubnet(
		user api.UserPass,
		from []string,
		changeAddr string,
		controlKeys []string,
		threshold uint32,
	) (ids.ID, error)
	// ExportAVAX issues an ExportTx transaction and returns the txID
	ExportAVAX(
		user api.UserPass,
		from []string,
		changeAddr string,
		to string,
		amount uint64,
	) (ids.ID, error)
	// ImportAVAX issues an ImportTx transaction and returns the txID
	ImportAVAX(
		user api.UserPass,
		from []string,
		changeAddr,
		to,
		sourceChain string,
	) (ids.ID, error)
	// CreateBlockchain issues a CreateBlockchain transaction and returns the txID
	CreateBlockchain(
		user api.UserPass,
		from []string,
		changeAddr string,
		subnetID ids.ID,
		vmID string,
		fxIDs []string,
		name string,
		genesisData []byte,
	) (ids.ID, error)
	// GetBlockchainStatus returns the current status of blockchain with ID: [blockchainID]
	GetBlockchainStatus(blockchainID string) (BlockchainStatus, error)
	// ValidatedBy returns the ID of the Subnet that validates [blockchainID]
	ValidatedBy(blockchainID ids.ID) (ids.ID, error)
	// Validates returns the list of blockchains that are validated by the subnet with ID [subnetID]
	Validates(subnetID ids.ID) ([]ids.ID, error)
	// GetBlockchains returns the list of blockchains on the platform
	GetBlockchains() ([]APIBlockchain, error)
	// IssueTx issues the transaction and returns its txID
	IssueTx(tx []byte) (ids.ID, error)
	// GetTx returns the byte representation of the transaction corresponding to [txID]
	GetTx(txID ids.ID) ([]byte, error)
	// GetTxStatus returns the status of the transaction corresponding to [txID]
	GetTxStatus(txID ids.ID, includeReason bool) (*GetTxStatusResponse, error)
	// GetStake returns the amount of nAVAX that [addresses] have cumulatively
	// staked on the Primary Network.
	GetStake(addrs []string) (*GetStakeReply, error)
	// GetMinStake returns the minimum staking amount in nAVAX for validators
	// and delegators respectively
	GetMinStake() (uint64, uint64, error)
	// GetTotalStake returns the total amount (in nAVAX) staked on the network
	GetTotalStake() (uint64, error)
	// GetMaxStakeAmount returns the maximum amount of nAVAX staking to the named
	// node during the time period.
	GetMaxStakeAmount(subnetID ids.ID, nodeID string, startTime uint64, endTime uint64) (uint64, error)
	// GetRewardUTXOs returns the reward UTXOs for a transaction
	GetRewardUTXOs(*api.GetTxArgs) ([][]byte, error)
	// GetTimestamp returns the current chain timestamp
	GetTimestamp() (time.Time, error)
	// GetValidatorsAt returns the weights of the validator set of a provided subnet
	// at the specified height.
	GetValidatorsAt(subnetID ids.ID, height uint64) (map[string]uint64, error)
}

// Client implementation for interacting with the P Chain endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with the P Chain endpoint
func NewClient(uri string, requestTimeout time.Duration) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/P", "platform", requestTimeout),
	}
}

func (c *client) GetHeight() (uint64, error) {
	res := &GetHeightResponse{}
	err := c.requester.SendRequest("getHeight", struct{}{}, res)
	return uint64(res.Height), err
}

func (c *client) ExportKey(user api.UserPass, address string) (string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest("exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  address,
	}, res)
	return res.PrivateKey, err
}

func (c *client) ImportKey(user api.UserPass, privateKey string) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res)
	return res.Address, err
}

func (c *client) GetBalance(address string) (*GetBalanceResponse, error) {
	res := &GetBalanceResponse{}
	err := c.requester.SendRequest("getBalance", &api.JSONAddress{
		Address: address,
	}, res)
	return res, err
}

func (c *client) CreateAddress(user api.UserPass) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("createAddress", &user, res)
	return res.Address, err
}

func (c *client) ListAddresses(user api.UserPass) ([]string, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest("listAddresses", &user, res)
	return res.Addresses, err
}

func (c *client) GetUTXOs(addrs []string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	return c.GetAtomicUTXOs(addrs, "", limit, startAddress, startUTXOID)
}

func (c *client) GetAtomicUTXOs(addrs []string, sourceChain string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	res := &api.GetUTXOsReply{}
	err := c.requester.SendRequest("getUTXOs", &api.GetUTXOsArgs{
		Addresses:   addrs,
		SourceChain: sourceChain,
		Limit:       cjson.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddress,
			UTXO:    startUTXOID,
		},
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, api.Index{}, err
	}

	utxos := make([][]byte, len(res.UTXOs))
	for i, utxo := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, utxo)
		if err != nil {
			return nil, api.Index{}, err
		}
		utxos[i] = utxoBytes
	}
	return utxos, res.EndIndex, nil
}

func (c *client) GetSubnets(ids []ids.ID) ([]APISubnet, error) {
	res := &GetSubnetsResponse{}
	err := c.requester.SendRequest("getSubnets", &GetSubnetsArgs{
		IDs: ids,
	}, res)
	return res.Subnets, err
}

func (c *client) GetStakingAssetID(subnetID ids.ID) (ids.ID, error) {
	res := &GetStakingAssetIDResponse{}
	err := c.requester.SendRequest("getStakingAssetID", &GetStakingAssetIDArgs{
		SubnetID: subnetID,
	}, res)
	return res.AssetID, err
}

func (c *client) GetCurrentValidators(subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, error) {
	nodeIDsStr := []string{}
	for _, nodeID := range nodeIDs {
		nodeIDsStr = append(nodeIDsStr, nodeID.PrefixedString(constants.NodeIDPrefix))
	}
	res := &GetCurrentValidatorsReply{}
	err := c.requester.SendRequest("getCurrentValidators", &GetCurrentValidatorsArgs{
		SubnetID: subnetID,
		NodeIDs:  nodeIDsStr,
	}, res)
	return res.Validators, err
}

func (c *client) GetPendingValidators(subnetID ids.ID, nodeIDs []ids.ShortID) ([]interface{}, []interface{}, error) {
	nodeIDsStr := []string{}
	for _, nodeID := range nodeIDs {
		nodeIDsStr = append(nodeIDsStr, nodeID.PrefixedString(constants.NodeIDPrefix))
	}
	res := &GetPendingValidatorsReply{}
	err := c.requester.SendRequest("getPendingValidators", &GetPendingValidatorsArgs{
		SubnetID: subnetID,
		NodeIDs:  nodeIDsStr,
	}, res)
	return res.Validators, res.Delegators, err
}

func (c *client) GetCurrentSupply() (uint64, error) {
	res := &GetCurrentSupplyReply{}
	err := c.requester.SendRequest("getCurrentSupply", struct{}{}, res)
	return uint64(res.Supply), err
}

func (c *client) SampleValidators(subnetID ids.ID, sampleSize uint16) ([]string, error) {
	res := &SampleValidatorsReply{}
	err := c.requester.SendRequest("sampleValidators", &SampleValidatorsArgs{
		SubnetID: subnetID,
		Size:     cjson.Uint16(sampleSize),
	}, res)
	return res.Validators, err
}

func (c *client) AddValidator(
	user api.UserPass,
	from []string,
	changeAddr string,
	rewardAddress,
	nodeID string,
	stakeAmount,
	startTime,
	endTime uint64,
	delegationFeeRate float32,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	jsonStakeAmount := cjson.Uint64(stakeAmount)
	err := c.requester.SendRequest("addValidator", &AddValidatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:      user,
			JSONFromAddrs: api.JSONFromAddrs{From: from},
		},
		APIStaker: APIStaker{
			NodeID:      nodeID,
			StakeAmount: &jsonStakeAmount,
			StartTime:   cjson.Uint64(startTime),
			EndTime:     cjson.Uint64(endTime),
		},
		RewardAddress:     rewardAddress,
		DelegationFeeRate: cjson.Float32(delegationFeeRate),
	}, res)
	return res.TxID, err
}

func (c *client) AddDelegator(
	user api.UserPass,
	from []string,
	changeAddr string,
	rewardAddress,
	nodeID string,
	stakeAmount,
	startTime,
	endTime uint64,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	jsonStakeAmount := cjson.Uint64(stakeAmount)
	err := c.requester.SendRequest("addDelegator", &AddDelegatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		}, APIStaker: APIStaker{
			NodeID:      nodeID,
			StakeAmount: &jsonStakeAmount,
			StartTime:   cjson.Uint64(startTime),
			EndTime:     cjson.Uint64(endTime),
		},
		RewardAddress: rewardAddress,
	}, res)
	return res.TxID, err
}

func (c *client) AddSubnetValidator(
	user api.UserPass,
	from []string,
	changeAddr string,
	subnetID,
	nodeID string,
	stakeAmount,
	startTime,
	endTime uint64,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	jsonStakeAmount := cjson.Uint64(stakeAmount)
	err := c.requester.SendRequest("addSubnetValidator", &AddSubnetValidatorArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		APIStaker: APIStaker{
			NodeID:      nodeID,
			StakeAmount: &jsonStakeAmount,
			StartTime:   cjson.Uint64(startTime),
			EndTime:     cjson.Uint64(endTime),
		},
		SubnetID: subnetID,
	}, res)
	return res.TxID, err
}

func (c *client) CreateSubnet(
	user api.UserPass,
	from []string,
	changeAddr string,
	controlKeys []string,
	threshold uint32,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("createSubnet", &CreateSubnetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		APISubnet: APISubnet{
			ControlKeys: controlKeys,
			Threshold:   cjson.Uint32(threshold),
		},
	}, res)
	return res.TxID, err
}

func (c *client) ExportAVAX(
	user api.UserPass,
	from []string,
	changeAddr string,
	to string,
	amount uint64,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("exportAVAX", &ExportAVAXArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		To:     to,
		Amount: cjson.Uint64(amount),
	}, res)
	return res.TxID, err
}

func (c *client) ImportAVAX(
	user api.UserPass,
	from []string,
	changeAddr,
	to,
	sourceChain string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest("importAVAX", &ImportAVAXArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		To:          to,
		SourceChain: sourceChain,
	}, res)
	return res.TxID, err
}

func (c *client) CreateBlockchain(
	user api.UserPass,
	from []string,
	changeAddr string,
	subnetID ids.ID,
	vmID string,
	fxIDs []string,
	name string,
	genesisData []byte,
) (ids.ID, error) {
	genesisDataStr, err := formatting.EncodeWithChecksum(formatting.Hex, genesisData)
	if err != nil {
		return ids.ID{}, err
	}

	res := &api.JSONTxID{}
	err = c.requester.SendRequest("createBlockchain", &CreateBlockchainArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			UserPass:       user,
			JSONFromAddrs:  api.JSONFromAddrs{From: from},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: changeAddr},
		},
		SubnetID:    subnetID,
		VMID:        vmID,
		FxIDs:       fxIDs,
		Name:        name,
		GenesisData: genesisDataStr,
		Encoding:    formatting.Hex,
	}, res)
	return res.TxID, err
}

func (c *client) GetBlockchainStatus(blockchainID string) (BlockchainStatus, error) {
	res := &GetBlockchainStatusReply{}
	err := c.requester.SendRequest("getBlockchainStatus", &GetBlockchainStatusArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.Status, err
}

func (c *client) ValidatedBy(blockchainID ids.ID) (ids.ID, error) {
	res := &ValidatedByResponse{}
	err := c.requester.SendRequest("validatedBy", &ValidatedByArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.SubnetID, err
}

func (c *client) Validates(subnetID ids.ID) ([]ids.ID, error) {
	res := &ValidatesResponse{}
	err := c.requester.SendRequest("validates", &ValidatesArgs{
		SubnetID: subnetID,
	}, res)
	return res.BlockchainIDs, err
}

func (c *client) GetBlockchains() ([]APIBlockchain, error) {
	res := &GetBlockchainsResponse{}
	err := c.requester.SendRequest("getBlockchains", struct{}{}, res)
	return res.Blockchains, err
}

func (c *client) IssueTx(txBytes []byte) (ids.ID, error) {
	txStr, err := formatting.EncodeWithChecksum(formatting.Hex, txBytes)
	if err != nil {
		return ids.ID{}, err
	}

	res := &api.JSONTxID{}
	err = c.requester.SendRequest("issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res)
	return res.TxID, err
}

func (c *client) GetTx(txID ids.ID) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest("getTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, err
	}
	return formatting.Decode(res.Encoding, res.Tx)
}

func (c *client) GetTxStatus(txID ids.ID, includeReason bool) (*GetTxStatusResponse, error) {
	res := new(GetTxStatusResponse)
	err := c.requester.SendRequest("getTxStatus", &GetTxStatusArgs{
		TxID:          txID,
		IncludeReason: includeReason,
	}, res)
	return res, err
}

func (c *client) GetStake(addrs []string) (*GetStakeReply, error) {
	res := new(GetStakeReply)
	err := c.requester.SendRequest("getStake", &api.JSONAddresses{
		Addresses: addrs,
	}, res)
	return res, err
}

func (c *client) GetMinStake() (uint64, uint64, error) {
	res := new(GetMinStakeReply)
	err := c.requester.SendRequest("getMinStake", struct{}{}, res)
	return uint64(res.MinValidatorStake), uint64(res.MinDelegatorStake), err
}

func (c *client) GetTotalStake() (uint64, error) {
	res := new(GetTotalStakeReply)
	err := c.requester.SendRequest("getTotalStake", struct{}{}, res)
	return uint64(res.Stake), err
}

func (c *client) GetMaxStakeAmount(subnetID ids.ID, nodeID string, startTime, endTime uint64) (uint64, error) {
	res := new(GetMaxStakeAmountReply)
	err := c.requester.SendRequest("getMaxStakeAmount", &GetMaxStakeAmountArgs{
		SubnetID:  subnetID,
		NodeID:    nodeID,
		StartTime: cjson.Uint64(startTime),
		EndTime:   cjson.Uint64(endTime),
	}, res)
	return uint64(res.Amount), err
}

func (c *client) GetRewardUTXOs(args *api.GetTxArgs) ([][]byte, error) {
	res := &GetRewardUTXOsReply{}
	err := c.requester.SendRequest("getRewardUTXOs", args, res)
	if err != nil {
		return nil, err
	}
	utxos := make([][]byte, len(res.UTXOs))
	for i, utxoStr := range res.UTXOs {
		utxoBytes, err := formatting.Decode(res.Encoding, utxoStr)
		if err != nil {
			return nil, err
		}
		utxos[i] = utxoBytes
	}
	return utxos, err
}

func (c *client) GetTimestamp() (time.Time, error) {
	res := &GetTimestampReply{}
	err := c.requester.SendRequest("getTimestamp", struct{}{}, res)
	return res.Timestamp, err
}

func (c *client) GetValidatorsAt(subnetID ids.ID, height uint64) (map[string]uint64, error) {
	res := &GetValidatorsAtReply{}
	err := c.requester.SendRequest("getValidatorsAt", &GetValidatorsAtArgs{
		SubnetID: subnetID,
		Height:   cjson.Uint64(height),
	}, res)
	return res.Validators, err
}
