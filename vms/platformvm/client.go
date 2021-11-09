// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	GetHeight() (uint64, error)
	ExportKey(api.UserPass, string) (string, error)
	ImportKey(api.UserPass, string) (string, error)
	GetBalance(string) (*GetBalanceResponse, error)
	CreateAddress(api.UserPass) (string, error)
	ListAddresses(api.UserPass) ([]string, error)
	GetUTXOs([]string, uint32, string, string) ([][]byte, api.Index, error)
	GetAtomicUTXOs([]string, string, uint32, string, string) ([][]byte, api.Index, error)
	GetSubnets([]ids.ID) ([]APISubnet, error)
	GetStakingAssetID(ids.ID) (ids.ID, error)
	GetCurrentValidators(ids.ID, []ids.ShortID) ([]interface{}, error)
	GetPendingValidators(ids.ID, []ids.ShortID) ([]interface{}, []interface{}, error)
	GetCurrentSupply() (uint64, error)
	SampleValidators(ids.ID, uint16) ([]string, error)
	AddValidator(api.UserPass, []string, string, string, string, uint64, uint64, uint64, float32) (ids.ID, error)
	AddDelegator(api.UserPass, []string, string, string, string, uint64, uint64, uint64) (ids.ID, error)
	AddSubnetValidator(api.UserPass, []string, string, string, string, uint64, uint64, uint64) (ids.ID, error)
	CreateSubnet(api.UserPass, []string, string, []string, uint32) (ids.ID, error)
	ExportAVAX(api.UserPass, []string, string, string, uint64) (ids.ID, error)
	ImportAVAX(api.UserPass, []string, string, string, string) (ids.ID, error)
	CreateBlockchain(api.UserPass, []string, string, ids.ID, string, []string, string, []byte) (ids.ID, error)
	GetBlockchainStatus(string) (BlockchainStatus, error)
	ValidatedBy(ids.ID) (ids.ID, error)
	Validates(ids.ID) ([]ids.ID, error)
	GetBlockchains() ([]APIBlockchain, error)
	IssueTx([]byte) (ids.ID, error)
	GetTx(ids.ID) ([]byte, error)
	GetTxStatus(ids.ID, bool) (*GetTxStatusResponse, error)
	GetStake([]string) (*GetStakeReply, error)
	GetMinStake() (uint64, uint64, error)
	GetTotalStake() (uint64, error)
	GetMaxStakeAmount(ids.ID, string, uint64, uint64) (uint64, error)
	GetRewardUTXOs(*api.GetTxArgs) ([][]byte, error)
	GetTimestamp() (time.Time, error)
	GetValidatorsAt(ids.ID, uint64) (map[string]uint64, error)
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

// GetHeight returns the current block height of the P Chain
func (c *client) GetHeight() (uint64, error) {
	res := &GetHeightResponse{}
	err := c.requester.SendRequest("getHeight", struct{}{}, res)
	return uint64(res.Height), err
}

// ExportKey returns the private key corresponding to [address] from [user]'s account
func (c *client) ExportKey(user api.UserPass, address string) (string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest("exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  address,
	}, res)
	return res.PrivateKey, err
}

// ImportKey imports the specified [privateKey] to [user]'s keystore
func (c *client) ImportKey(user api.UserPass, privateKey string) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res)
	return res.Address, err
}

// GetBalance returns the balance of [address] on the P Chain
func (c *client) GetBalance(address string) (*GetBalanceResponse, error) {
	res := &GetBalanceResponse{}
	err := c.requester.SendRequest("getBalance", &api.JSONAddress{
		Address: address,
	}, res)
	return res, err
}

// CreateAddress creates a new address for [user]
func (c *client) CreateAddress(user api.UserPass) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest("createAddress", &user, res)
	return res.Address, err
}

// ListAddresses returns an array of platform addresses controlled by [user]
func (c *client) ListAddresses(user api.UserPass) ([]string, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest("listAddresses", &user, res)
	return res.Addresses, err
}

// GetUTXOs returns the byte representation of the UTXOs controlled by [addrs]
func (c *client) GetUTXOs(addrs []string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	return c.GetAtomicUTXOs(addrs, "", limit, startAddress, startUTXOID)
}

// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addresses]
// from [sourceChain]
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

// GetSubnets returns information about the specified subnets
func (c *client) GetSubnets(ids []ids.ID) ([]APISubnet, error) {
	res := &GetSubnetsResponse{}
	err := c.requester.SendRequest("getSubnets", &GetSubnetsArgs{
		IDs: ids,
	}, res)
	return res.Subnets, err
}

// GetStakingAssetID returns the assetID of the asset used for staking on
// subnet corresponding to [subnetID]
func (c *client) GetStakingAssetID(subnetID ids.ID) (ids.ID, error) {
	res := &GetStakingAssetIDResponse{}
	err := c.requester.SendRequest("getStakingAssetID", &GetStakingAssetIDArgs{
		SubnetID: subnetID,
	}, res)
	return res.AssetID, err
}

// GetCurrentValidators returns the list of current validators for subnet with ID [subnetID]
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

// GetPendingValidators returns the list of pending validators for subnet with ID [subnetID]
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

// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
func (c *client) GetCurrentSupply() (uint64, error) {
	res := &GetCurrentSupplyReply{}
	err := c.requester.SendRequest("getCurrentSupply", struct{}{}, res)
	return uint64(res.Supply), err
}

// SampleValidators returns the nodeIDs of a sample of [sampleSize] validators from the current validator set for subnet with ID [subnetID]
func (c *client) SampleValidators(subnetID ids.ID, sampleSize uint16) ([]string, error) {
	res := &SampleValidatorsReply{}
	err := c.requester.SendRequest("sampleValidators", &SampleValidatorsArgs{
		SubnetID: subnetID,
		Size:     cjson.Uint16(sampleSize),
	}, res)
	return res.Validators, err
}

// AddValidator issues a transaction to add a validator to the primary network
// and returns the txID
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

// AddDelegator issues a transaction to add a delegator to the primary network
// and returns the txID
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

// AddSubnetValidator issues a transaction to add validator [nodeID] to subnet
// with ID [subnetID] and returns the txID
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

// CreateSubnet issues a transaction to create [subnet] and returns the txID
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

// ExportAVAX issues an ExportTx transaction and returns the txID
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

// ImportAVAX issues an ImportTx transaction and returns the txID
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

// CreateBlockchain issues a CreateBlockchain transaction and returns the txID
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

// GetBlockchainStatus returns the current status of blockchain with ID: [blockchainID]
func (c *client) GetBlockchainStatus(blockchainID string) (BlockchainStatus, error) {
	res := &GetBlockchainStatusReply{}
	err := c.requester.SendRequest("getBlockchainStatus", &GetBlockchainStatusArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.Status, err
}

// ValidatedBy returns the ID of the Subnet that validates [blockchainID]
func (c *client) ValidatedBy(blockchainID ids.ID) (ids.ID, error) {
	res := &ValidatedByResponse{}
	err := c.requester.SendRequest("validatedBy", &ValidatedByArgs{
		BlockchainID: blockchainID,
	}, res)
	return res.SubnetID, err
}

// Validates returns the list of blockchains that are validated by the subnet with ID [subnetID]
func (c *client) Validates(subnetID ids.ID) ([]ids.ID, error) {
	res := &ValidatesResponse{}
	err := c.requester.SendRequest("validates", &ValidatesArgs{
		SubnetID: subnetID,
	}, res)
	return res.BlockchainIDs, err
}

// GetBlockchains returns the list of blockchains on the platform
func (c *client) GetBlockchains() ([]APIBlockchain, error) {
	res := &GetBlockchainsResponse{}
	err := c.requester.SendRequest("getBlockchains", struct{}{}, res)
	return res.Blockchains, err
}

// IssueTx issues the transaction and returns its txID
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

// GetTx returns the byte representation of the transaction corresponding to
// [txID]
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

// GetTxStatus returns the status of the transaction corresponding to [txID]
func (c *client) GetTxStatus(txID ids.ID, includeReason bool) (*GetTxStatusResponse, error) {
	res := new(GetTxStatusResponse)
	err := c.requester.SendRequest("getTxStatus", &GetTxStatusArgs{
		TxID:          txID,
		IncludeReason: includeReason,
	}, res)
	return res, err
}

// GetStake returns the amount of nAVAX that [addresses] have cumulatively
// staked on the Primary Network.
func (c *client) GetStake(addrs []string) (*GetStakeReply, error) {
	res := new(GetStakeReply)
	err := c.requester.SendRequest("getStake", &api.JSONAddresses{
		Addresses: addrs,
	}, res)
	return res, err
}

// GetMinStake returns the minimum staking amount in nAVAX for validators
// and delegators respectively
func (c *client) GetMinStake() (uint64, uint64, error) {
	res := new(GetMinStakeReply)
	err := c.requester.SendRequest("getMinStake", struct{}{}, res)
	return uint64(res.MinValidatorStake), uint64(res.MinDelegatorStake), err
}

// GetTotalStake returns the total amount (in nAVAX) staked on the network
func (c *client) GetTotalStake() (uint64, error) {
	res := new(GetTotalStakeReply)
	err := c.requester.SendRequest("getTotalStake", struct{}{}, res)
	return uint64(res.Stake), err
}

// GetMaxStakeAmount returns the maximum amount of nAVAX staking to the named
// node during the time period.
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

// GetRewardUTXOs returns the reward UTXOs for a transaction
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

// GetTimestamp returns the current chain timestamp
func (c *client) GetTimestamp() (time.Time, error) {
	res := &GetTimestampReply{}
	err := c.requester.SendRequest("getTimestamp", struct{}{}, res)
	return res.Timestamp, err
}

// GetValidatorsAt returns the weights of the validator set of a provided subnet
// at the specified height.
func (c *client) GetValidatorsAt(subnetID ids.ID, height uint64) (map[string]uint64, error) {
	res := &GetValidatorsAtReply{}
	err := c.requester.SendRequest("getValidatorsAt", &GetValidatorsAtArgs{
		SubnetID: subnetID,
		Height:   cjson.Uint64(height),
	}, res)
	return res.Validators, err
}
