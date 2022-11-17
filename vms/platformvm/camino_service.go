// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errNotCaminoBuilder  = errors.New("tx builder must provide CaminoBuilder interface")
	errSerializeTx       = "couldn't serialize TX: %w"
	errEncodeTx          = "couldn't encode TX as string: %w"
	errInvalidChangeAddr = "couldn't parse changeAddr: %w"
	errCreateTx          = "couldn't create tx: %w"
)

// GetConfigurationReply is the response from calling GetConfiguration.
type GetConfigurationReply struct {
	// The NetworkID
	NetworkID json.Uint32 `json:"networkID"`
	// The fee asset ID
	AssetID ids.ID `json:"assetID"`
	// The symbol of the fee asset ID
	AssetSymbol string `json:"assetSymbol"`
	// beech32HRP use in addresses
	Hrp string `json:"hrp"`
	// Primary network blockchains
	Blockchains []APIBlockchain `json:"blockchains"`
	// The minimum duration a validator has to stake
	MinStakeDuration json.Uint64 `json:"minStakeDuration"`
	// The maximum duration a validator can stake
	MaxStakeDuration json.Uint64 `json:"maxStakeDuration"`
	// The minimum amount of tokens one must bond to be a validator
	MinValidatorStake json.Uint64 `json:"minValidatorStake"`
	// The maximum amount of tokens bondable to a validator
	MaxValidatorStake json.Uint64 `json:"maxValidatorStake"`
	// The minimum delegation fee
	MinDelegationFee json.Uint32 `json:"minDelegationFee"`
	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake json.Uint64 `json:"minDelegatorStake"`
	// The minimum consumption rate
	MinConsumptionRate json.Uint64 `json:"minConsumptionRate"`
	// The maximum consumption rate
	MaxConsumptionRate json.Uint64 `json:"maxConsumptionRate"`
	// The supply cap for the native token (AVAX)
	SupplyCap json.Uint64 `json:"supplyCap"`
	// The codec version used for serializing
	CodecVersion json.Uint16 `json:"codecVersion"`
}

// GetMinStake returns the minimum staking amount in nAVAX.
func (service *Service) GetConfiguration(_ *http.Request, _ *struct{}, reply *GetConfigurationReply) error {
	service.vm.ctx.Log.Debug("Platform: GetConfiguration called")

	// Fee Asset ID, NetworkID and HRP
	reply.NetworkID = json.Uint32(service.vm.ctx.NetworkID)
	reply.AssetID = service.vm.GetFeeAssetID()
	reply.AssetSymbol = constants.TokenSymbol(service.vm.ctx.NetworkID)
	reply.Hrp = constants.GetHRP(service.vm.ctx.NetworkID)

	// Blockchains of the primary network
	blockchains := &GetBlockchainsResponse{}
	if err := service.appendBlockchains(constants.PrimaryNetworkID, blockchains); err != nil {
		return err
	}
	reply.Blockchains = blockchains.Blockchains

	// Staking information
	reply.MinStakeDuration = json.Uint64(service.vm.MinStakeDuration)
	reply.MaxStakeDuration = json.Uint64(service.vm.MaxStakeDuration)

	reply.MaxValidatorStake = json.Uint64(service.vm.MaxValidatorStake)
	reply.MinValidatorStake = json.Uint64(service.vm.MinValidatorStake)

	reply.MinDelegationFee = json.Uint32(service.vm.MinDelegationFee)
	reply.MinDelegatorStake = json.Uint64(service.vm.MinDelegatorStake)

	reply.MinConsumptionRate = json.Uint64(service.vm.RewardConfig.MinConsumptionRate)
	reply.MaxConsumptionRate = json.Uint64(service.vm.RewardConfig.MaxConsumptionRate)

	reply.SupplyCap = json.Uint64(service.vm.RewardConfig.SupplyCap)

	// Codec information
	reply.CodecVersion = json.Uint16(txs.Version)

	return nil
}

type SetAddressStateArgs struct {
	api.JSONSpendHeader

	Address string `json:"address"`
	State   uint8  `json:"state"`
	Remove  bool   `json:"remove"`
}

// AddAdressState issues an AddAdressStateTx
func (service *Service) SetAddressState(_ *http.Request, args *SetAddressStateArgs, response api.JSONTxID) error {
	service.vm.ctx.Log.Debug("Platform: SetAddressState called")

	keys, err := service.getKeystoreKeys(&args.JSONSpendHeader)
	if err != nil {
		return err
	}

	tx, err := service.buildAddressStateTx(args, keys)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()

	if err = service.vm.Builder.AddUnverifiedTx(tx); err != nil {
		return err
	}
	return nil
}

type GetAddressStateTxArgs struct {
	SetAddressStateArgs

	Encoding formatting.Encoding `json:"encoding"`
}

type GetAddressStateTxReply struct {
	Tx string `json:"tx"`
}

// GetAddressStateTx returnes an unsigned AddAddressStateTx
func (service *Service) GetAddressStateTx(_ *http.Request, args *GetAddressStateTxArgs, response *GetAddressStateTxReply) error {
	service.vm.ctx.Log.Debug("Platform: GetAddressStateTx called")

	keys, err := service.getFakeKeys(&args.JSONSpendHeader)
	if err != nil {
		return err
	}

	tx, err := service.buildAddressStateTx(&args.SetAddressStateArgs, keys)
	if err != nil {
		return err
	}

	bytes, err := txs.Codec.Marshal(txs.Version, tx.Unsigned)
	if err != nil {
		return fmt.Errorf(errSerializeTx, err)
	}

	if response.Tx, err = formatting.Encode(args.Encoding, bytes); err != nil {
		return fmt.Errorf(errEncodeTx, err)
	}
	return nil
}

func (service *Service) getKeystoreKeys(args *api.JSONSpendHeader) (*secp256k1fx.Keychain, error) {
	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(service.addrManager, args.From)
	if err != nil {
		return nil, err
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return nil, err
	}
	defer user.Close()

	// Get the user's keys
	privKeys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil {
		return nil, fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Parse the change address.
	if len(privKeys.Keys) == 0 {
		return nil, errNoKeys
	}

	if err = user.Close(); err != nil {
		return nil, err
	}
	return privKeys, nil
}

func (service *Service) getFakeKeys(args *api.JSONSpendHeader) (*secp256k1fx.Keychain, error) {
	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(service.addrManager, args.From)
	if err != nil {
		return nil, err
	}

	privKeys := secp256k1fx.NewKeychain()
	for fromAddr := range fromAddrs {
		privKeys.Add(crypto.FakePrivateKey(fromAddr))
	}
	return privKeys, nil
}

func (service *Service) buildAddressStateTx(args *SetAddressStateArgs, keys *secp256k1fx.Keychain) (*txs.Tx, error) {
	var changeAddr ids.ShortID
	if len(args.ChangeAddr) > 0 {
		var err error
		if changeAddr, err = avax.ParseServiceAddress(service.addrManager, args.ChangeAddr); err != nil {
			return nil, fmt.Errorf(errInvalidChangeAddr, err)
		}
	}

	targetAddr, err := avax.ParseServiceAddress(service.addrManager, args.Address)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse param Address: %w", err)
	}

	builder, ok := service.vm.txBuilder.(builder.CaminoBuilder)
	if !ok {
		return nil, errNotCaminoBuilder
	}

	// Create the transaction
	tx, err := builder.NewAddAddressStateTx(
		targetAddr,  // Address to change state
		args.Remove, // Add or remove State
		args.State,  // The state to change
		keys.Keys,   // Keys providing the staked tokens
		changeAddr,
	)
	if err != nil {
		return nil, fmt.Errorf(errCreateTx, err)
	}
	return tx, nil
}
