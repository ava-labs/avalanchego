// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"go.uber.org/zap"

	utilsjson "github.com/ava-labs/avalanchego/utils/json"
)

var (
	errSerializeTx            = "couldn't serialize TX: %w"
	errEncodeTx               = "couldn't encode TX as string: %w"
	errInvalidChangeAddr      = "couldn't parse changeAddr: %w"
	errCreateTx               = "couldn't create tx: %w"
	errCreateTransferables    = errors.New("couldn't create transferables")
	errSerializeTransferables = errors.New("couldn't serialize transferables")
	errEncodeTransferables    = errors.New("couldn't encode transferables as string")
)

// CaminoService defines the API calls that can be made to the platform chain
type CaminoService struct {
	Service
}

type GetBalanceResponseV2 struct {
	Balances               map[ids.ID]utilsjson.Uint64 `json:"balances"`
	UnlockedOutputs        map[ids.ID]utilsjson.Uint64 `json:"unlockedOutputs"`
	BondedOutputs          map[ids.ID]utilsjson.Uint64 `json:"bondedOutputs"`
	DepositedOutputs       map[ids.ID]utilsjson.Uint64 `json:"depositedOutputs"`
	DepositedBondedOutputs map[ids.ID]utilsjson.Uint64 `json:"bondedDepositedOutputs"`
	UTXOIDs                []*avax.UTXOID              `json:"utxoIDs"`
}
type GetBalanceResponseWrapper struct {
	LockModeBondDeposit bool
	avax                GetBalanceResponse
	camino              GetBalanceResponseV2
}

func (response GetBalanceResponseWrapper) MarshalJSON() ([]byte, error) {
	if !response.LockModeBondDeposit {
		return json.Marshal(response.avax)
	}
	return json.Marshal(response.camino)
}

// GetBalance gets the balance of an address
func (s *CaminoService) GetBalance(_ *http.Request, args *GetBalanceRequest, response *GetBalanceResponseWrapper) error {
	caminoConfig, err := s.vm.state.CaminoConfig()
	if err != nil {
		return err
	}
	response.LockModeBondDeposit = caminoConfig.LockModeBondDeposit
	if !caminoConfig.LockModeBondDeposit {
		return s.Service.GetBalance(nil, args, &response.avax)
	}

	if args.Address != nil {
		args.Addresses = append(args.Addresses, *args.Address)
	}

	s.vm.ctx.Log.Debug("Platform: GetBalance called",
		logging.UserStrings("addresses", args.Addresses),
	)

	// Parse to address
	addrs, err := avax.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return err
	}

	utxos, err := avax.GetAllUTXOs(s.vm.state, addrs)
	if err != nil {
		return fmt.Errorf("couldn't get UTXO set of %v: %w", args.Addresses, err)
	}

	unlockedOutputs := map[ids.ID]utilsjson.Uint64{}
	bondedOutputs := map[ids.ID]utilsjson.Uint64{}
	depositedOutputs := map[ids.ID]utilsjson.Uint64{}
	depositedBondedOutputs := map[ids.ID]utilsjson.Uint64{}
	balances := map[ids.ID]utilsjson.Uint64{}
	var utxoIDs []*avax.UTXOID

utxoFor:
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		switch out := utxo.Out.(type) {
		case *secp256k1fx.TransferOutput:
			unlockedOutputs[assetID] = utilsjson.SafeAdd(unlockedOutputs[assetID], utilsjson.Uint64(out.Amount()))
			balances[assetID] = utilsjson.SafeAdd(balances[assetID], utilsjson.Uint64(out.Amount()))
		case *locked.Out:
			switch out.LockState() {
			case locked.StateBonded:
				bondedOutputs[assetID] = utilsjson.SafeAdd(bondedOutputs[assetID], utilsjson.Uint64(out.Amount()))
				balances[assetID] = utilsjson.SafeAdd(balances[assetID], utilsjson.Uint64(out.Amount()))
			case locked.StateDeposited:
				depositedOutputs[assetID] = utilsjson.SafeAdd(depositedOutputs[assetID], utilsjson.Uint64(out.Amount()))
				balances[assetID] = utilsjson.SafeAdd(balances[assetID], utilsjson.Uint64(out.Amount()))
			case locked.StateDepositedBonded:
				depositedBondedOutputs[assetID] = utilsjson.SafeAdd(depositedBondedOutputs[assetID], utilsjson.Uint64(out.Amount()))
				balances[assetID] = utilsjson.SafeAdd(balances[assetID], utilsjson.Uint64(out.Amount()))
			default:
				s.vm.ctx.Log.Warn("Unexpected utxo lock state")
				continue utxoFor
			}
		default:
			s.vm.ctx.Log.Warn("unexpected output type in UTXO",
				zap.String("type", fmt.Sprintf("%T", out)),
			)
			continue utxoFor
		}

		utxoIDs = append(utxoIDs, &utxo.UTXOID)
	}

	response.camino = GetBalanceResponseV2{balances, unlockedOutputs, bondedOutputs, depositedOutputs, depositedBondedOutputs, utxoIDs}
	return nil
}

// GetConfigurationReply is the response from calling GetConfiguration.
type GetConfigurationReply struct {
	// The NetworkID
	NetworkID utilsjson.Uint32 `json:"networkID"`
	// The fee asset ID
	AssetID ids.ID `json:"assetID"`
	// The symbol of the fee asset ID
	AssetSymbol string `json:"assetSymbol"`
	// beech32HRP use in addresses
	Hrp string `json:"hrp"`
	// Primary network blockchains
	Blockchains []APIBlockchain `json:"blockchains"`
	// The minimum duration a validator has to stake
	MinStakeDuration utilsjson.Uint64 `json:"minStakeDuration"`
	// The maximum duration a validator can stake
	MaxStakeDuration utilsjson.Uint64 `json:"maxStakeDuration"`
	// The minimum amount of tokens one must bond to be a validator
	MinValidatorStake utilsjson.Uint64 `json:"minValidatorStake"`
	// The maximum amount of tokens bondable to a validator
	MaxValidatorStake utilsjson.Uint64 `json:"maxValidatorStake"`
	// The minimum delegation fee
	MinDelegationFee utilsjson.Uint32 `json:"minDelegationFee"`
	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake utilsjson.Uint64 `json:"minDelegatorStake"`
	// The minimum consumption rate
	MinConsumptionRate utilsjson.Uint64 `json:"minConsumptionRate"`
	// The maximum consumption rate
	MaxConsumptionRate utilsjson.Uint64 `json:"maxConsumptionRate"`
	// The supply cap for the native token (AVAX)
	SupplyCap utilsjson.Uint64 `json:"supplyCap"`
	// The codec version used for serializing
	CodecVersion utilsjson.Uint16 `json:"codecVersion"`
	// Camino VerifyNodeSignature
	VerifyNodeSignature bool `json:"verifyNodeSignature"`
	// Camino LockModeBondDeposit
	LockModeBondDeposit bool `json:"lockModeBondDeposit"`
}

// GetConfiguration returns platformVM configuration
func (s *CaminoService) GetConfiguration(_ *http.Request, _ *struct{}, reply *GetConfigurationReply) error {
	s.vm.ctx.Log.Debug("Platform: GetConfiguration called")

	// Fee Asset ID, NetworkID and HRP
	reply.NetworkID = utilsjson.Uint32(s.vm.ctx.NetworkID)
	reply.AssetID = s.vm.GetFeeAssetID()
	reply.AssetSymbol = constants.TokenSymbol(s.vm.ctx.NetworkID)
	reply.Hrp = constants.GetHRP(s.vm.ctx.NetworkID)

	// Blockchains of the primary network
	blockchains := &GetBlockchainsResponse{}
	if err := s.appendBlockchains(constants.PrimaryNetworkID, blockchains); err != nil {
		return err
	}
	reply.Blockchains = blockchains.Blockchains

	// Staking information
	reply.MinStakeDuration = utilsjson.Uint64(s.vm.MinStakeDuration)
	reply.MaxStakeDuration = utilsjson.Uint64(s.vm.MaxStakeDuration)

	reply.MaxValidatorStake = utilsjson.Uint64(s.vm.MaxValidatorStake)
	reply.MinValidatorStake = utilsjson.Uint64(s.vm.MinValidatorStake)

	reply.MinDelegationFee = utilsjson.Uint32(s.vm.MinDelegationFee)
	reply.MinDelegatorStake = utilsjson.Uint64(s.vm.MinDelegatorStake)

	reply.MinConsumptionRate = utilsjson.Uint64(s.vm.RewardConfig.MinConsumptionRate)
	reply.MaxConsumptionRate = utilsjson.Uint64(s.vm.RewardConfig.MaxConsumptionRate)

	reply.SupplyCap = utilsjson.Uint64(s.vm.RewardConfig.SupplyCap)

	// Codec information
	reply.CodecVersion = utilsjson.Uint16(txs.Version)

	caminoConfig, err := s.vm.state.CaminoConfig()
	if err != nil {
		return err
	}
	reply.VerifyNodeSignature = caminoConfig.VerifyNodeSignature
	reply.LockModeBondDeposit = caminoConfig.LockModeBondDeposit

	return nil
}

type SetAddressStateArgs struct {
	api.JSONSpendHeader

	Address string `json:"address"`
	State   uint8  `json:"state"`
	Remove  bool   `json:"remove"`
}

// AddAdressState issues an AddAdressStateTx
func (s *CaminoService) SetAddressState(_ *http.Request, args *SetAddressStateArgs, response *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("Platform: SetAddressState called")

	keys, err := s.getKeystoreKeys(&args.JSONSpendHeader)
	if err != nil {
		return err
	}

	tx, err := s.buildAddressStateTx(args, keys)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()

	if err = s.vm.Builder.AddUnverifiedTx(tx); err != nil {
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
func (s *CaminoService) GetAddressStateTx(_ *http.Request, args *GetAddressStateTxArgs, response *GetAddressStateTxReply) error {
	s.vm.ctx.Log.Debug("Platform: GetAddressStateTx called")

	keys, err := s.getFakeKeys(&args.JSONFromAddrs)
	if err != nil {
		return err
	}

	tx, err := s.buildAddressStateTx(&args.SetAddressStateArgs, keys)
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

type SpendArgs struct {
	api.JSONFromAddrs
	api.JSONChangeAddr

	LockMode     byte                `json:"lockMode"`
	AmountToLock uint64              `json:"amountToLock"`
	AmountToBurn uint64              `json:"amountToBurn"`
	Encoding     formatting.Encoding `json:"encoding"`
}

type SpendReply struct {
	Ins  string `json:"ins"`
	Outs string `json:"outs"`
}

func (s *CaminoService) Spend(_ *http.Request, args *SpendArgs, response *SpendReply) error {
	s.vm.ctx.Log.Debug("Platform: Spend called")

	keys, err := s.getFakeKeys(&args.JSONFromAddrs)
	if err != nil {
		return err
	}
	if len(keys.Keys) == 0 {
		return errNoKeys
	}

	changeAddr := ids.ShortEmpty
	if len(args.ChangeAddr) > 0 {
		var err error
		if changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr); err != nil {
			return fmt.Errorf(errInvalidChangeAddr, err)
		}
	}

	ins, outs, _, err := s.vm.txBuilder.Lock(
		keys.Keys,
		args.AmountToLock,
		args.AmountToBurn,
		locked.State(args.LockMode),
		changeAddr,
	)
	if err != nil {
		return fmt.Errorf("%w: %s", errCreateTransferables, err)
	}

	bytes, err := txs.Codec.Marshal(txs.Version, ins)
	if err != nil {
		return fmt.Errorf("%w: %s", errSerializeTransferables, err)
	}

	if response.Ins, err = formatting.Encode(args.Encoding, bytes); err != nil {
		return fmt.Errorf("%w: %s", errEncodeTransferables, err)
	}

	bytes, err = txs.Codec.Marshal(txs.Version, outs)
	if err != nil {
		return fmt.Errorf("%w: %s", errSerializeTransferables, err)
	}

	if response.Outs, err = formatting.Encode(args.Encoding, bytes); err != nil {
		return fmt.Errorf("%w: %s", errEncodeTransferables, err)
	}

	return nil
}

func (s *Service) getKeystoreKeys(args *api.JSONSpendHeader) (*secp256k1fx.Keychain, error) {
	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return nil, err
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
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

func (s *Service) getFakeKeys(args *api.JSONFromAddrs) (*secp256k1fx.Keychain, error) {
	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return nil, err
	}

	privKeys := secp256k1fx.NewKeychain()
	for fromAddr := range fromAddrs {
		privKeys.Add(crypto.FakePrivateKey(fromAddr))
	}
	return privKeys, nil
}

func (s *Service) buildAddressStateTx(args *SetAddressStateArgs, keys *secp256k1fx.Keychain) (*txs.Tx, error) {
	var changeAddr ids.ShortID
	if len(args.ChangeAddr) > 0 {
		var err error
		if changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr); err != nil {
			return nil, fmt.Errorf(errInvalidChangeAddr, err)
		}
	}

	targetAddr, err := avax.ParseServiceAddress(s.addrManager, args.Address)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse param Address: %w", err)
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewAddAddressStateTx(
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
