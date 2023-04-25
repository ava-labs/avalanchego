// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
	"go.uber.org/zap"

	utilsjson "github.com/ava-labs/avalanchego/utils/json"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
)

var (
	errInvalidChangeAddr      = "couldn't parse changeAddr: %w"
	errCreateTx               = "couldn't create tx: %w"
	errCreateTransferables    = errors.New("can't create transferables")
	errSerializeTransferables = errors.New("can't serialize transferables")
	errEncodeTransferables    = errors.New("can't encode transferables as string")
	errWrongOwnerType         = errors.New("wrong owner type")
	errSerializeOwners        = errors.New("can't serialize owners")
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
	s.vm.ctx.Log.Debug("Platform: GetBalance called")

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
	api.UserPass
	api.JSONFromAddrs

	Change  platformapi.Owner `json:"change"`
	Address string            `json:"address"`
	State   uint8             `json:"state"`
	Remove  bool              `json:"remove"`
}

// AddAdressState issues an AddAdressStateTx
func (s *CaminoService) SetAddressState(_ *http.Request, args *SetAddressStateArgs, response *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("Platform: SetAddressState called")

	privKeys, err := s.getKeystoreKeys(&args.UserPass, &args.JSONFromAddrs)
	if err != nil {
		return err
	}

	change, err := s.secpOwnerFromAPI(&args.Change)
	if err != nil {
		return fmt.Errorf(errInvalidChangeAddr, err)
	}

	targetAddr, err := avax.ParseServiceAddress(s.addrManager, args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse param Address: %w", err)
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewAddressStateTx(
		targetAddr,  // Address to change state
		args.Remove, // Add or remove State
		args.State,  // The state to change
		privKeys,    // Keys providing the staked tokens
		change,
	)
	if err != nil {
		return fmt.Errorf(errCreateTx, err)
	}

	response.TxID = tx.ID()

	if err = s.vm.Builder.AddUnverifiedTx(tx); err != nil {
		return err
	}
	return nil
}

// GetAdressStates retrieves the state applied to an address (see setAddressState)
func (s *CaminoService) GetAddressStates(_ *http.Request, args *api.JSONAddress, response *utilsjson.Uint64) error {
	s.vm.ctx.Log.Debug("Platform: GetAddressStates called")

	addr, err := avax.ParseServiceAddress(s.addrManager, args.Address)
	if err != nil {
		return err
	}

	state, err := s.vm.state.GetAddressStates(addr)
	if err != nil {
		return err
	}

	*response = utilsjson.Uint64(state)

	return nil
}

type GetMultisigAliasReply struct {
	Memo types.JSONByteSlice `json:"memo"`
	platformapi.Owner
}

// GetMultisigAlias retrieves the owners and threshold for a given multisig alias
func (s *CaminoService) GetMultisigAlias(_ *http.Request, args *api.JSONAddress, response *GetMultisigAliasReply) error {
	s.vm.ctx.Log.Debug("Platform: GetMultisigAlias called")

	addr, err := avax.ParseServiceAddress(s.addrManager, args.Address)
	if err != nil {
		return err
	}

	alias, err := s.vm.state.GetMultisigAlias(addr)
	if err != nil {
		return err
	}
	owners, ok := alias.Owners.(*secp256k1fx.OutputOwners)
	if !ok {
		return errWrongOwnerType
	}

	response.Memo = alias.Memo
	response.Locktime = utilsjson.Uint64(owners.Locktime)
	response.Threshold = utilsjson.Uint32(owners.Threshold)
	response.Addresses = make([]string, len(owners.Addrs))

	for index, addr := range owners.Addrs {
		addrString, err := s.addrManager.FormatLocalAddress(addr)
		if err != nil {
			return err
		}
		response.Addresses[index] = addrString
	}

	return nil
}

type SpendArgs struct {
	api.JSONFromAddrs

	To           platformapi.Owner   `json:"to"`
	Change       platformapi.Owner   `json:"change"`
	LockMode     byte                `json:"lockMode"`
	AmountToLock utilsjson.Uint64    `json:"amountToLock"`
	AmountToBurn utilsjson.Uint64    `json:"amountToBurn"`
	AsOf         utilsjson.Uint64    `json:"asOf"`
	Encoding     formatting.Encoding `json:"encoding"`
}

type SpendReply struct {
	Ins     string          `json:"ins"`
	Outs    string          `json:"outs"`
	Signers [][]ids.ShortID `json:"signers"`
	Owners  string          `json:"owners"`
}

func (s *CaminoService) Spend(_ *http.Request, args *SpendArgs, response *SpendReply) error {
	s.vm.ctx.Log.Debug("Platform: Spend called")

	privKeys, err := s.getFakeKeys(&args.JSONFromAddrs)
	if err != nil {
		return err
	}
	if len(privKeys) == 0 {
		return errNoKeys
	}

	to, err := s.secpOwnerFromAPI(&args.To)
	if err != nil {
		return err
	}

	change, err := s.secpOwnerFromAPI(&args.Change)
	if err != nil {
		return fmt.Errorf(errInvalidChangeAddr, err)
	}

	ins, outs, signers, owners, err := s.vm.txBuilder.Lock(
		privKeys,
		uint64(args.AmountToLock),
		uint64(args.AmountToBurn),
		locked.State(args.LockMode),
		to,
		change,
		uint64(args.AsOf),
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

	response.Signers = make([][]ids.ShortID, len(signers))
	for i, cred := range signers {
		response.Signers[i] = make([]ids.ShortID, len(cred))
		for j, sig := range cred {
			response.Signers[i][j] = sig.Address()
		}
	}

	bytes, err = txs.Codec.Marshal(txs.Version, owners)
	if err != nil {
		return fmt.Errorf("%w: %s", errSerializeOwners, err)
	}
	if response.Owners, err = formatting.Encode(args.Encoding, bytes); err != nil {
		return fmt.Errorf("%w: %s", errSerializeOwners, err)
	}
	return nil
}

type RegisterNodeArgs struct {
	api.UserPass
	api.JSONFromAddrs

	Change                  platformapi.Owner `json:"change"`
	OldNodeID               ids.NodeID        `json:"oldNodeID"`
	NewNodeID               ids.NodeID        `json:"newNodeID"`
	ConsortiumMemberAddress string            `json:"consortiumMemberAddress"`
}

// RegisterNode issues an RegisterNodeTx
func (s *CaminoService) RegisterNode(_ *http.Request, args *RegisterNodeArgs, reply *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("Platform: RegisterNode called")

	privKeys, err := s.getKeystoreKeys(&args.UserPass, &args.JSONFromAddrs)
	if err != nil {
		return err
	}

	change, err := s.secpOwnerFromAPI(&args.Change)
	if err != nil {
		return fmt.Errorf(errInvalidChangeAddr, err)
	}

	// Parse the consortium member address.
	consortiumMemberAddress, err := avax.ParseServiceAddress(s.addrManager, args.ConsortiumMemberAddress)
	if err != nil {
		return fmt.Errorf("couldn't parse consortiumMemberAddress: %w", err)
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewRegisterNodeTx(
		args.OldNodeID,
		args.NewNodeID,
		consortiumMemberAddress,
		privKeys,
		change,
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()

	if err = s.vm.Builder.AddUnverifiedTx(tx); err != nil {
		return err
	}
	return nil
}

type ClaimedAmount struct {
	DepositTxID    ids.ID            `json:"depositTxID"`
	ClaimableOwner platformapi.Owner `json:"claimableOwner"`
	Amount         uint64            `json:"amount"`
	ClaimType      txs.ClaimType     `json:"claimType"`
}

type ClaimArgs struct {
	api.UserPass
	api.JSONFromAddrs

	Claimables []ClaimedAmount   `json:"claimables"`
	ClaimTo    platformapi.Owner `json:"claimTo"`
	Change     platformapi.Owner `json:"change"`
}

// Claim issues an ClaimTx
func (s *CaminoService) Claim(_ *http.Request, args *ClaimArgs, reply *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("Platform: Claim called")

	privKeys, err := s.getKeystoreKeys(&args.UserPass, &args.JSONFromAddrs)
	if err != nil {
		return err
	}

	change, err := s.secpOwnerFromAPI(&args.Change)
	if err != nil {
		return fmt.Errorf(errInvalidChangeAddr, err)
	}

	claimTo, err := s.secpOwnerFromAPI(&args.ClaimTo)
	if err != nil {
		return err
	}

	claimables := make([]txs.ClaimAmount, len(args.Claimables))
	for i := range args.Claimables {
		switch args.Claimables[i].ClaimType {
		case txs.ClaimTypeActiveDepositReward:
			claimables[i].ID = args.Claimables[i].DepositTxID
		case txs.ClaimTypeExpiredDepositReward, txs.ClaimTypeValidatorReward, txs.ClaimTypeAllTreasury:
			claimableOwner, err := s.secpOwnerFromAPI(&args.Claimables[i].ClaimableOwner)
			if err != nil {
				return fmt.Errorf("failed to parse api owner to secp owner: %w", err)
			}
			ownerID, err := txs.GetOwnerID(claimableOwner)
			if err != nil {
				return fmt.Errorf("failed to calculate ownerID from owner: %w", err)
			}
			claimables[i].ID = ownerID
		default:
			return txs.ErrWrongClaimType
		}
		claimables[i].Amount = args.Claimables[i].Amount
		claimables[i].Type = args.Claimables[i].ClaimType
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewClaimTx(
		claimables,
		claimTo,
		privKeys,
		change,
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()

	if err := s.vm.Builder.AddUnverifiedTx(tx); err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	return nil
}

type TransferArgs struct {
	api.UserPass
	api.JSONFromAddrs
	Change     platformapi.Owner `json:"change"`
	TransferTo platformapi.Owner `json:"transferTo"`
	Amount     uint64            `json:"amount"`
}

// Transfer issues an BaseTx
func (s *CaminoService) Transfer(_ *http.Request, args *TransferArgs, reply *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("Platform: Transfer called")

	privKeys, err := s.getKeystoreKeys(&args.UserPass, &args.JSONFromAddrs)
	if err != nil {
		return err
	}

	change, err := s.secpOwnerFromAPI(&args.Change)
	if err != nil {
		return fmt.Errorf(errInvalidChangeAddr, err)
	}

	transferTo, err := s.secpOwnerFromAPI(&args.TransferTo)
	if err != nil {
		return err
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewBaseTx(
		args.Amount,
		transferTo,
		privKeys,
		change,
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()

	if err := s.vm.Builder.AddUnverifiedTx(tx); err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	return nil
}

func (s *CaminoService) GetRegisteredShortIDLink(_ *http.Request, args *api.JSONAddress, response *api.JSONAddress) error {
	s.vm.ctx.Log.Debug("Platform: GetRegisteredShortIDLink called")

	var id ids.ShortID
	isNodeID := false
	if nodeID, err := ids.NodeIDFromString(args.Address); err == nil {
		id = ids.ShortID(nodeID)
		isNodeID = true
	} else {
		id, err = avax.ParseServiceAddress(s.addrManager, args.Address)
		if err != nil {
			return err
		}
	}

	link, err := s.vm.state.GetShortIDLink(id, state.ShortLinkKeyRegisterNode)
	if err != nil {
		return err
	}

	if isNodeID {
		response.Address, err = s.addrManager.FormatLocalAddress(link)
		if err != nil {
			return err
		}
	} else {
		response.Address = ids.NodeID(link).String()
	}
	return nil
}

type APIClaimable struct {
	ValidatorRewards      utilsjson.Uint64 `json:"validatorRewards"`
	ExpiredDepositRewards utilsjson.Uint64 `json:"expiredDepositRewards"`
}

type GetClaimablesArgs struct {
	Owners []platformapi.Owner `json:"owners"`
}

type GetClaimablesReply struct {
	Claimables []APIClaimable `json:"claimables"`
}

// GetClaimables returns the amount of claimable tokens for given owner
func (s *CaminoService) GetClaimables(_ *http.Request, args *GetClaimablesArgs, response *GetClaimablesReply) error {
	s.vm.ctx.Log.Debug("Platform: GetClaimables called")

	response.Claimables = make([]APIClaimable, len(args.Owners))
	for i := range args.Owners {
		claimableOwner, err := s.secpOwnerFromAPI(&args.Owners[i])
		if err != nil {
			return err
		}

		ownerID, err := txs.GetOwnerID(claimableOwner)
		if err != nil {
			return err
		}

		claimable, err := s.vm.state.GetClaimable(ownerID)
		if err == database.ErrNotFound {
			claimable = &state.Claimable{}
		} else if err != nil {
			return err
		}

		response.Claimables[i].ValidatorRewards = utilsjson.Uint64(claimable.ValidatorReward)
		response.Claimables[i].ExpiredDepositRewards = utilsjson.Uint64(claimable.ExpiredDepositReward)
	}

	return nil
}

type APIDeposit struct {
	DepositTxID         ids.ID            `json:"depositTxID"`
	DepositOfferID      ids.ID            `json:"depositOfferID"`
	UnlockedAmount      utilsjson.Uint64  `json:"unlockedAmount"`
	ClaimedRewardAmount utilsjson.Uint64  `json:"claimedRewardAmount"`
	Start               utilsjson.Uint64  `json:"start"`
	Duration            uint32            `json:"duration"`
	Amount              utilsjson.Uint64  `json:"amount"`
	RewardOwner         platformapi.Owner `json:"rewardOwner"`
}

func (s *CaminoService) APIDepositFromDeposit(depositTxID ids.ID, deposit *deposit.Deposit) (*APIDeposit, error) {
	rewardOwner, ok := deposit.RewardOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, errWrongOwnerType
	}
	apiOwner, err := s.apiOwnerFromSECP(rewardOwner)
	if err != nil {
		return nil, err
	}
	return &APIDeposit{
		DepositTxID:         depositTxID,
		DepositOfferID:      deposit.DepositOfferID,
		UnlockedAmount:      utilsjson.Uint64(deposit.UnlockedAmount),
		ClaimedRewardAmount: utilsjson.Uint64(deposit.ClaimedRewardAmount),
		Start:               utilsjson.Uint64(deposit.Start),
		Duration:            deposit.Duration,
		Amount:              utilsjson.Uint64(deposit.Amount),
		RewardOwner:         *apiOwner,
	}, nil
}

type GetDepositsArgs struct {
	DepositTxIDs []ids.ID `json:"depositTxIDs"`
}

type GetDepositsReply struct {
	Deposits         []*APIDeposit      `json:"deposits"`
	AvailableRewards []utilsjson.Uint64 `json:"availableRewards"`
	Timestamp        utilsjson.Uint64   `json:"timestamp"`
}

// GetDeposits returns deposits by IDs
func (s *CaminoService) GetDeposits(_ *http.Request, args *GetDepositsArgs, reply *GetDepositsReply) error {
	s.vm.ctx.Log.Debug("Platform: GetDeposits called")
	timestamp := s.vm.clock.Unix()
	reply.Deposits = make([]*APIDeposit, len(args.DepositTxIDs))
	reply.AvailableRewards = make([]utilsjson.Uint64, len(args.DepositTxIDs))
	reply.Timestamp = utilsjson.Uint64(timestamp)
	for i := range args.DepositTxIDs {
		deposit, err := s.vm.state.GetDeposit(args.DepositTxIDs[i])
		if err != nil {
			return fmt.Errorf("could't get deposit from state: %w", err)
		}
		offer, err := s.vm.state.GetDepositOffer(deposit.DepositOfferID)
		if err != nil {
			return err
		}
		reply.Deposits[i], err = s.APIDepositFromDeposit(args.DepositTxIDs[i], deposit)
		if err != nil {
			return err
		}
		reply.AvailableRewards[i] = utilsjson.Uint64(deposit.ClaimableReward(offer, timestamp))
	}
	return nil
}

// GetHeight returns the height of the last accepted block
func (s *CaminoService) GetLastAcceptedBlock(r *http.Request, _ *struct{}, reply *api.GetBlockResponse) error {
	s.vm.ctx.Log.Debug("Platform: GetLastAcceptedBlock called")

	ctx := r.Context()
	lastAcceptedID, err := s.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block ID: %w", err)
	}
	block, err := s.vm.manager.GetStatelessBlock(lastAcceptedID)
	if err != nil {
		return fmt.Errorf("couldn't get block with id %s: %w", lastAcceptedID, err)
	}

	block.InitCtx(s.vm.ctx)
	reply.Encoding = formatting.JSON
	reply.Block = block
	return nil
}

func (s *Service) getKeystoreKeys(creds *api.UserPass, from *api.JSONFromAddrs) ([]*crypto.PrivateKeySECP256K1R, error) {
	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, creds.Username, creds.Password)
	if err != nil {
		return nil, err
	}
	defer user.Close()

	var keys []*crypto.PrivateKeySECP256K1R
	if len(from.Signer) > 0 {
		if len(from.From) == 0 {
			return nil, errNoKeys
		}

		// Get fake keys for from addresses
		keys, err = s.getFakeKeys(&api.JSONFromAddrs{From: from.From})
		if err != nil {
			return nil, err
		}

		// Parse the signer addresses
		signerAddrs, err := avax.ParseServiceAddresses(s.addrManager, from.Signer)
		if err != nil {
			return nil, err
		}

		// Get keys for multiSig
		keyChain, err := keystore.GetKeychain(user, signerAddrs)
		if err != nil {
			return nil, fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
		}
		if len(keyChain.Keys) == 0 {
			return nil, errNoKeys
		}

		keys = append(keys, nil)
		keys = append(keys, keyChain.Keys...)
	} else {
		// Parse the from addresses
		fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, from.From)
		if err != nil {
			return nil, err
		}

		// Get the user's keys
		keyChain, err := keystore.GetKeychain(user, fromAddrs)
		if err != nil {
			return nil, fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
		}
		if len(keyChain.Keys) == 0 {
			return nil, errNoKeys
		}

		keys = keyChain.Keys
	}

	if err = user.Close(); err != nil {
		return nil, err
	}
	return keys, nil
}

// getFakeKeys creates SECP256K1 private keys which have only the purpose to provide the address.
// Used for calls like spend() which provide fromAddrs and signers required to get the correct UTXOs
// for the transaction. These private keys can never recover to the public address they contain.
func (s *Service) getFakeKeys(from *api.JSONFromAddrs) ([]*crypto.PrivateKeySECP256K1R, error) {
	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, from.From)
	if err != nil {
		return nil, err
	}

	keys := []*crypto.PrivateKeySECP256K1R{}
	for fromAddr := range fromAddrs {
		keys = append(keys, crypto.FakePrivateKey(fromAddr))
	}

	if len(from.Signer) > 0 {
		// Parse the signer addresses
		signerAddrs, err := avax.ParseServiceAddresses(s.addrManager, from.Signer)
		if err != nil {
			return nil, err
		}

		keys = append(keys, nil)
		for signerAddr := range signerAddrs {
			keys = append(keys, crypto.FakePrivateKey(signerAddr))
		}
	}

	return keys, nil
}

func (s *CaminoService) secpOwnerFromAPI(apiOwner *platformapi.Owner) (*secp256k1fx.OutputOwners, error) {
	if len(apiOwner.Addresses) > 0 {
		secpOwner := &secp256k1fx.OutputOwners{
			Locktime:  uint64(apiOwner.Locktime),
			Threshold: uint32(apiOwner.Threshold),
			Addrs:     make([]ids.ShortID, len(apiOwner.Addresses)),
		}
		for i := range apiOwner.Addresses {
			addr, err := avax.ParseServiceAddress(s.addrManager, apiOwner.Addresses[i])
			if err != nil {
				return nil, err
			}
			secpOwner.Addrs[i] = addr
		}
		secpOwner.Sort()
		return secpOwner, nil
	}
	return nil, nil
}

func (s *CaminoService) apiOwnerFromSECP(secpOwner *secp256k1fx.OutputOwners) (*platformapi.Owner, error) {
	apiOwner := &platformapi.Owner{
		Locktime:  utilsjson.Uint64(secpOwner.Locktime),
		Threshold: utilsjson.Uint32(secpOwner.Threshold),
		Addresses: make([]string, len(secpOwner.Addrs)),
	}
	for i := range secpOwner.Addrs {
		addr, err := s.addrManager.FormatLocalAddress(secpOwner.Addrs[i])
		if err != nil {
			return nil, err
		}
		apiOwner.Addresses[i] = addr
	}
	return apiOwner, nil
}

type GetAllDepositOffersArgs struct {
	Active bool `json:"active"`
}

type GetAllDepositOffersReply struct {
	DepositOffers []*deposit.Offer `json:"depositOffers"`
}

// GetAllDepositOffers returns an array of all deposit offers. The array can be filtered to only return active offers.
func (s *CaminoService) GetAllDepositOffers(_ *http.Request, args *GetAllDepositOffersArgs, response *GetAllDepositOffersReply) error {
	s.vm.ctx.Log.Debug("Platform: GetAllDepositOffers called")

	depositOffers, err := s.vm.state.GetAllDepositOffers()
	if err != nil {
		return err
	}

	if args.Active {
		var activeOffers []*deposit.Offer
		for _, offer := range depositOffers {
			if offer.Flags&deposit.OfferFlagLocked == 0 {
				activeOffers = append(activeOffers, offer)
			}
		}
		depositOffers = activeOffers
	}

	response.DepositOffers = depositOffers
	return nil
}
