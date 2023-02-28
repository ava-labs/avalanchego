// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	stdmath "math"

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of addresses that can be passed in as argument to GetStake
	maxGetStakeAddrs = 256

	// Minimum amount of delay to allow a transaction to be issued through the
	// API
	minAddStakerDelay = 2 * executor.SyncBound

	// Note: Staker attributes cache should be large enough so that no evictions
	// happen when the API loops through all stakers.
	stakerAttributesCacheSize = 100_000
)

var (
	errMissingDecisionBlock     = errors.New("should have a decision block within the past two blocks")
	errNoSubnetID               = errors.New("argument 'subnetID' not provided")
	errNoRewardAddress          = errors.New("argument 'rewardAddress' not provided")
	errInvalidDelegationRate    = errors.New("argument 'delegationFeeRate' must be between 0 and 100, inclusive")
	errNoAddresses              = errors.New("no addresses provided")
	errNoKeys                   = errors.New("user has no keys or funds")
	errStartTimeTooSoon         = fmt.Errorf("start time must be at least %s in the future", minAddStakerDelay)
	errStartTimeTooLate         = errors.New("start time is too far in the future")
	errNamedSubnetCantBePrimary = errors.New("subnet validator attempts to validate primary network")
	errNoAmount                 = errors.New("argument 'amount' must be > 0")
	errMissingName              = errors.New("argument 'name' not given")
	errMissingVMID              = errors.New("argument 'vmID' not given")
	errMissingBlockchainID      = errors.New("argument 'blockchainID' not given")
	errMissingPrivateKey        = errors.New("argument 'privateKey' not given")
	errStartAfterEndTime        = errors.New("start time must be before end time")
	errStartTimeInThePast       = errors.New("start time in the past")
)

// Service defines the API calls that can be made to the platform chain
type Service struct {
	vm                    *VM
	addrManager           avax.AddressManager
	stakerAttributesCache *cache.LRU[ids.ID, *stakerAttributes]
}

// All attributes are optional and may not be filled for each stakerTx.
type stakerAttributes struct {
	shares                 uint32
	rewardsOwner           fx.Owner
	validationRewardsOwner fx.Owner
	delegationRewardsOwner fx.Owner
	proofOfPossession      *signer.ProofOfPossession
}

type GetHeightResponse struct {
	Height json.Uint64 `json:"height"`
}

// GetHeight returns the height of the last accepted block
func (s *Service) GetHeight(r *http.Request, _ *struct{}, response *GetHeightResponse) error {
	ctx := r.Context()
	lastAcceptedID, err := s.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block ID: %w", err)
	}
	lastAccepted, err := s.vm.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block: %w", err)
	}
	response.Height = json.Uint64(lastAccepted.Height())
	return nil
}

// ExportKeyArgs are arguments for ExportKey
type ExportKeyArgs struct {
	api.UserPass
	Address string `json:"address"`
}

// ExportKeyReply is the response for ExportKey
type ExportKeyReply struct {
	// The decrypted PrivateKey for the Address provided in the arguments
	PrivateKey *secp256k1.PrivateKey `json:"privateKey"`
}

// ExportKey returns a private key from the provided user
func (s *Service) ExportKey(_ *http.Request, args *ExportKeyArgs, reply *ExportKeyReply) error {
	s.vm.ctx.Log.Debug("Platform: ExportKey called")

	address, err := avax.ParseServiceAddress(s.addrManager, args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse %s to address: %w", args.Address, err)
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}

	reply.PrivateKey, err = user.GetKey(address)
	if err != nil {
		// Drop any potential error closing the user to report the original
		// error
		_ = user.Close()
		return fmt.Errorf("problem retrieving private key: %w", err)
	}
	return user.Close()
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	api.UserPass
	PrivateKey *secp256k1.PrivateKey `json:"privateKey"`
}

// ImportKey adds a private key to the provided user
func (s *Service) ImportKey(_ *http.Request, args *ImportKeyArgs, reply *api.JSONAddress) error {
	s.vm.ctx.Log.Debug("Platform: ImportKey called",
		logging.UserString("username", args.Username),
	)

	if args.PrivateKey == nil {
		return errMissingPrivateKey
	}

	var err error
	reply.Address, err = s.addrManager.FormatLocalAddress(args.PrivateKey.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	if err := user.PutKeys(args.PrivateKey); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}
	return user.Close()
}

/*
 ******************************************************
 *************  Balances / Addresses ******************
 ******************************************************
 */

type GetBalanceRequest struct {
	Addresses []string `json:"addresses"`
}

// Note: We explicitly duplicate AVAX out of the maps to ensure backwards
// compatibility.
type GetBalanceResponse struct {
	// Balance, in nAVAX, of the address
	Balance             json.Uint64            `json:"balance"`
	Unlocked            json.Uint64            `json:"unlocked"`
	LockedStakeable     json.Uint64            `json:"lockedStakeable"`
	LockedNotStakeable  json.Uint64            `json:"lockedNotStakeable"`
	Balances            map[ids.ID]json.Uint64 `json:"balances"`
	Unlockeds           map[ids.ID]json.Uint64 `json:"unlockeds"`
	LockedStakeables    map[ids.ID]json.Uint64 `json:"lockedStakeables"`
	LockedNotStakeables map[ids.ID]json.Uint64 `json:"lockedNotStakeables"`
	UTXOIDs             []*avax.UTXOID         `json:"utxoIDs"`
}

// GetBalance gets the balance of an address
func (s *Service) GetBalance(_ *http.Request, args *GetBalanceRequest, response *GetBalanceResponse) error {
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

	currentTime := s.vm.clock.Unix()

	unlockeds := map[ids.ID]uint64{}
	lockedStakeables := map[ids.ID]uint64{}
	lockedNotStakeables := map[ids.ID]uint64{}

utxoFor:
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		switch out := utxo.Out.(type) {
		case *secp256k1fx.TransferOutput:
			if out.Locktime <= currentTime {
				newBalance, err := math.Add64(unlockeds[assetID], out.Amount())
				if err != nil {
					unlockeds[assetID] = stdmath.MaxUint64
				} else {
					unlockeds[assetID] = newBalance
				}
			} else {
				newBalance, err := math.Add64(lockedNotStakeables[assetID], out.Amount())
				if err != nil {
					lockedNotStakeables[assetID] = stdmath.MaxUint64
				} else {
					lockedNotStakeables[assetID] = newBalance
				}
			}
		case *stakeable.LockOut:
			innerOut, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
			switch {
			case !ok:
				s.vm.ctx.Log.Warn("unexpected output type in UTXO",
					zap.String("type", fmt.Sprintf("%T", out.TransferableOut)),
				)
				continue utxoFor
			case innerOut.Locktime > currentTime:
				newBalance, err := math.Add64(lockedNotStakeables[assetID], out.Amount())
				if err != nil {
					lockedNotStakeables[assetID] = stdmath.MaxUint64
				} else {
					lockedNotStakeables[assetID] = newBalance
				}
			case out.Locktime <= currentTime:
				newBalance, err := math.Add64(unlockeds[assetID], out.Amount())
				if err != nil {
					unlockeds[assetID] = stdmath.MaxUint64
				} else {
					unlockeds[assetID] = newBalance
				}
			default:
				newBalance, err := math.Add64(lockedStakeables[assetID], out.Amount())
				if err != nil {
					lockedStakeables[assetID] = stdmath.MaxUint64
				} else {
					lockedStakeables[assetID] = newBalance
				}
			}
		default:
			continue utxoFor
		}

		response.UTXOIDs = append(response.UTXOIDs, &utxo.UTXOID)
	}

	balances := maps.Clone(lockedStakeables)
	for assetID, amount := range lockedNotStakeables {
		newBalance, err := math.Add64(balances[assetID], amount)
		if err != nil {
			balances[assetID] = stdmath.MaxUint64
		} else {
			balances[assetID] = newBalance
		}
	}
	for assetID, amount := range unlockeds {
		newBalance, err := math.Add64(balances[assetID], amount)
		if err != nil {
			balances[assetID] = stdmath.MaxUint64
		} else {
			balances[assetID] = newBalance
		}
	}

	response.Balances = newJSONBalanceMap(balances)
	response.Unlockeds = newJSONBalanceMap(unlockeds)
	response.LockedStakeables = newJSONBalanceMap(lockedStakeables)
	response.LockedNotStakeables = newJSONBalanceMap(lockedNotStakeables)
	response.Balance = response.Balances[s.vm.ctx.AVAXAssetID]
	response.Unlocked = response.Unlockeds[s.vm.ctx.AVAXAssetID]
	response.LockedStakeable = response.LockedStakeables[s.vm.ctx.AVAXAssetID]
	response.LockedNotStakeable = response.LockedNotStakeables[s.vm.ctx.AVAXAssetID]
	return nil
}

func newJSONBalanceMap(balanceMap map[ids.ID]uint64) map[ids.ID]json.Uint64 {
	jsonBalanceMap := make(map[ids.ID]json.Uint64, len(balanceMap))
	for assetID, amount := range balanceMap {
		jsonBalanceMap[assetID] = json.Uint64(amount)
	}
	return jsonBalanceMap
}

// CreateAddress creates an address controlled by [args.Username]
// Returns the newly created address
func (s *Service) CreateAddress(_ *http.Request, args *api.UserPass, response *api.JSONAddress) error {
	s.vm.ctx.Log.Debug("Platform: CreateAddress called")

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	key, err := keystore.NewKey(user)
	if err != nil {
		return err
	}

	response.Address, err = s.addrManager.FormatLocalAddress(key.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}
	return user.Close()
}

// ListAddresses returns the addresses controlled by [args.Username]
func (s *Service) ListAddresses(_ *http.Request, args *api.UserPass, response *api.JSONAddresses) error {
	s.vm.ctx.Log.Debug("Platform: ListAddresses called")

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	addresses, err := user.GetAddresses()
	if err != nil {
		return fmt.Errorf("couldn't get addresses: %w", err)
	}
	response.Addresses = make([]string, len(addresses))
	for i, addr := range addresses {
		response.Addresses[i], err = s.addrManager.FormatLocalAddress(addr)
		if err != nil {
			return fmt.Errorf("problem formatting address: %w", err)
		}
	}
	return user.Close()
}

// Index is an address and an associated UTXO.
// Marks a starting or stopping point when fetching UTXOs. Used for pagination.
type Index struct {
	Address string `json:"address"` // The address as a string
	UTXO    string `json:"utxo"`    // The UTXO ID as a string
}

// GetUTXOs returns the UTXOs controlled by the given addresses
func (s *Service) GetUTXOs(_ *http.Request, args *api.GetUTXOsArgs, response *api.GetUTXOsReply) error {
	s.vm.ctx.Log.Debug("Platform: GetUTXOs called")

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	var sourceChain ids.ID
	if args.SourceChain == "" {
		sourceChain = s.vm.ctx.ChainID
	} else {
		chainID, err := s.vm.ctx.BCLookup.Lookup(args.SourceChain)
		if err != nil {
			return fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
		}
		sourceChain = chainID
	}

	addrSet, err := avax.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return err
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		startAddr, err = avax.ParseServiceAddress(s.addrManager, args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		startUTXO, err = ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}
	}

	var (
		utxos     []*avax.UTXO
		endAddr   ids.ShortID
		endUTXOID ids.ID
	)
	limit := int(args.Limit)
	if limit <= 0 || builder.MaxPageSize < limit {
		limit = builder.MaxPageSize
	}
	if sourceChain == s.vm.ctx.ChainID {
		utxos, endAddr, endUTXOID, err = avax.GetPaginatedUTXOs(
			s.vm.state,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	} else {
		utxos, endAddr, endUTXOID, err = s.vm.atomicUtxosManager.GetAtomicUTXOs(
			sourceChain,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	}
	if err != nil {
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	response.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		bytes, err := txs.Codec.Marshal(txs.Version, utxo)
		if err != nil {
			return fmt.Errorf("couldn't serialize UTXO %q: %w", utxo.InputID(), err)
		}
		response.UTXOs[i], err = formatting.Encode(args.Encoding, bytes)
		if err != nil {
			return fmt.Errorf("couldn't encode UTXO %s as string: %w", utxo.InputID(), err)
		}
	}

	endAddress, err := s.addrManager.FormatLocalAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	response.EndIndex.Address = endAddress
	response.EndIndex.UTXO = endUTXOID.String()
	response.NumFetched = json.Uint64(len(utxos))
	response.Encoding = args.Encoding
	return nil
}

/*
 ******************************************************
 ******************* Get Subnets **********************
 ******************************************************
 */

// APISubnet is a representation of a subnet used in API calls
type APISubnet struct {
	// ID of the subnet
	ID ids.ID `json:"id"`

	// Each element of [ControlKeys] the address of a public key.
	// A transaction to add a validator to this subnet requires
	// signatures from [Threshold] of these keys to be valid.
	ControlKeys []string    `json:"controlKeys"`
	Threshold   json.Uint32 `json:"threshold"`
}

// GetSubnetsArgs are the arguments to GetSubnet
type GetSubnetsArgs struct {
	// IDs of the subnets to retrieve information about
	// If omitted, gets all subnets
	IDs []ids.ID `json:"ids"`
}

// GetSubnetsResponse is the response from calling GetSubnets
type GetSubnetsResponse struct {
	// Each element is a subnet that exists
	// Null if there are no subnets other than the primary network
	Subnets []APISubnet `json:"subnets"`
}

// GetSubnets returns the subnets whose ID are in [args.IDs]
// The response will include the primary network
func (s *Service) GetSubnets(_ *http.Request, args *GetSubnetsArgs, response *GetSubnetsResponse) error {
	s.vm.ctx.Log.Debug("Platform: GetSubnets called")

	getAll := len(args.IDs) == 0
	if getAll {
		subnets, err := s.vm.state.GetSubnets() // all subnets
		if err != nil {
			return fmt.Errorf("error getting subnets from database: %w", err)
		}

		response.Subnets = make([]APISubnet, len(subnets)+1)
		for i, subnet := range subnets {
			subnetID := subnet.ID()
			if _, err := s.vm.state.GetSubnetTransformation(subnetID); err == nil {
				response.Subnets[i] = APISubnet{
					ID:          subnetID,
					ControlKeys: []string{},
					Threshold:   json.Uint32(0),
				}
				continue
			}

			unsignedTx := subnet.Unsigned.(*txs.CreateSubnetTx)
			owner := unsignedTx.Owner.(*secp256k1fx.OutputOwners)
			controlAddrs := []string{}
			for _, controlKeyID := range owner.Addrs {
				addr, err := s.addrManager.FormatLocalAddress(controlKeyID)
				if err != nil {
					return fmt.Errorf("problem formatting address: %w", err)
				}
				controlAddrs = append(controlAddrs, addr)
			}
			response.Subnets[i] = APISubnet{
				ID:          subnetID,
				ControlKeys: controlAddrs,
				Threshold:   json.Uint32(owner.Threshold),
			}
		}
		// Include primary network
		response.Subnets[len(subnets)] = APISubnet{
			ID:          constants.PrimaryNetworkID,
			ControlKeys: []string{},
			Threshold:   json.Uint32(0),
		}
		return nil
	}

	subnetSet := set.NewSet[ids.ID](len(args.IDs))
	for _, subnetID := range args.IDs {
		if subnetSet.Contains(subnetID) {
			continue
		}
		subnetSet.Add(subnetID)

		if subnetID == constants.PrimaryNetworkID {
			response.Subnets = append(response.Subnets,
				APISubnet{
					ID:          constants.PrimaryNetworkID,
					ControlKeys: []string{},
					Threshold:   json.Uint32(0),
				},
			)
			continue
		}

		if _, err := s.vm.state.GetSubnetTransformation(subnetID); err == nil {
			response.Subnets = append(response.Subnets, APISubnet{
				ID:          subnetID,
				ControlKeys: []string{},
				Threshold:   json.Uint32(0),
			})
			continue
		}

		subnetTx, _, err := s.vm.state.GetTx(subnetID)
		if err == database.ErrNotFound {
			continue
		}
		if err != nil {
			return err
		}

		subnet, ok := subnetTx.Unsigned.(*txs.CreateSubnetTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateSubnetTx but got %T", subnetTx.Unsigned)
		}
		owner, ok := subnet.Owner.(*secp256k1fx.OutputOwners)
		if !ok {
			return fmt.Errorf("expected *secp256k1fx.OutputOwners but got %T", subnet.Owner)
		}

		controlAddrs := make([]string, len(owner.Addrs))
		for i, controlKeyID := range owner.Addrs {
			addr, err := s.addrManager.FormatLocalAddress(controlKeyID)
			if err != nil {
				return fmt.Errorf("problem formatting address: %w", err)
			}
			controlAddrs[i] = addr
		}

		response.Subnets = append(response.Subnets, APISubnet{
			ID:          subnetID,
			ControlKeys: controlAddrs,
			Threshold:   json.Uint32(owner.Threshold),
		})
	}
	return nil
}

// GetStakingAssetIDArgs are the arguments to GetStakingAssetID
type GetStakingAssetIDArgs struct {
	SubnetID ids.ID `json:"subnetID"`
}

// GetStakingAssetIDResponse is the response from calling GetStakingAssetID
type GetStakingAssetIDResponse struct {
	AssetID ids.ID `json:"assetID"`
}

// GetStakingAssetID returns the assetID of the token used to stake on the
// provided subnet
func (s *Service) GetStakingAssetID(_ *http.Request, args *GetStakingAssetIDArgs, response *GetStakingAssetIDResponse) error {
	s.vm.ctx.Log.Debug("Platform: GetStakingAssetID called")

	if args.SubnetID == constants.PrimaryNetworkID {
		response.AssetID = s.vm.ctx.AVAXAssetID
		return nil
	}

	transformSubnetIntf, err := s.vm.state.GetSubnetTransformation(args.SubnetID)
	if err != nil {
		return fmt.Errorf(
			"failed fetching subnet transformation for %s: %w",
			args.SubnetID,
			err,
		)
	}
	transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
	if !ok {
		return fmt.Errorf(
			"unexpected subnet transformation tx type fetched %T",
			transformSubnetIntf.Unsigned,
		)
	}

	response.AssetID = transformSubnet.AssetID
	return nil
}

/*
 ******************************************************
 **************** Get/Sample Validators ***************
 ******************************************************
 */

// GetCurrentValidatorsArgs are the arguments for calling GetCurrentValidators
type GetCurrentValidatorsArgs struct {
	// Subnet we're listing the validators of
	// If omitted, defaults to primary network
	SubnetID ids.ID `json:"subnetID"`
	// NodeIDs of validators to request. If [NodeIDs]
	// is empty, it fetches all current validators. If
	// some nodeIDs are not currently validators, they
	// will be omitted from the response.
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

// GetCurrentValidatorsReply are the results from calling GetCurrentValidators.
// Each validator contains a list of delegators to itself.
type GetCurrentValidatorsReply struct {
	Validators []interface{} `json:"validators"`
}

func (s *Service) loadStakerTxAttributes(txID ids.ID) (*stakerAttributes, error) {
	// Lookup tx from the cache first.
	attr, found := s.stakerAttributesCache.Get(txID)
	if found {
		return attr, nil
	}

	// Tx not available in cache; pull it from disk and populate the cache.
	tx, _, err := s.vm.state.GetTx(txID)
	if err != nil {
		return nil, err
	}

	switch stakerTx := tx.Unsigned.(type) {
	case txs.ValidatorTx:
		var pop *signer.ProofOfPossession
		if staker, ok := stakerTx.(*txs.AddPermissionlessValidatorTx); ok {
			if s, ok := staker.Signer.(*signer.ProofOfPossession); ok {
				pop = s
			}
		}

		attr = &stakerAttributes{
			shares:                 stakerTx.Shares(),
			validationRewardsOwner: stakerTx.ValidationRewardsOwner(),
			delegationRewardsOwner: stakerTx.DelegationRewardsOwner(),
			proofOfPossession:      pop,
		}

	case txs.DelegatorTx:
		attr = &stakerAttributes{
			rewardsOwner: stakerTx.RewardsOwner(),
		}

	default:
		return nil, fmt.Errorf("unexpected staker tx type %T", tx.Unsigned)
	}

	s.stakerAttributesCache.Put(txID, attr)
	return attr, nil
}

// GetCurrentValidators returns the current validators. If a single nodeID
// is provided, full delegators information is also returned. Otherwise only
// delegators' number and total weight is returned.
func (s *Service) GetCurrentValidators(_ *http.Request, args *GetCurrentValidatorsArgs, reply *GetCurrentValidatorsReply) error {
	s.vm.ctx.Log.Debug("Platform: GetCurrentValidators called")

	reply.Validators = []interface{}{}

	// Validator's node ID as string --> Delegators to them
	vdrToDelegators := map[ids.NodeID][]platformapi.PrimaryDelegator{}

	// Create set of nodeIDs
	nodeIDs := set.Set[ids.NodeID]{}
	nodeIDs.Add(args.NodeIDs...)

	numNodeIDs := nodeIDs.Len()
	targetStakers := make([]*state.Staker, 0, numNodeIDs)
	if numNodeIDs == 0 { // Include all nodes
		currentStakerIterator, err := s.vm.state.GetCurrentStakerIterator()
		if err != nil {
			return err
		}
		// TODO: avoid iterating over delegators here.
		for currentStakerIterator.Next() {
			staker := currentStakerIterator.Value()
			if args.SubnetID != staker.SubnetID {
				continue
			}
			targetStakers = append(targetStakers, staker)
		}
		currentStakerIterator.Release()
	} else {
		for nodeID := range nodeIDs {
			staker, err := s.vm.state.GetCurrentValidator(args.SubnetID, nodeID)
			switch err {
			case nil:
			case database.ErrNotFound:
				// nothing to do, continue
				continue
			default:
				return err
			}
			targetStakers = append(targetStakers, staker)

			// TODO: avoid iterating over delegators when numNodeIDs > 1.
			delegatorsIt, err := s.vm.state.GetCurrentDelegatorIterator(args.SubnetID, nodeID)
			if err != nil {
				return err
			}
			for delegatorsIt.Next() {
				staker := delegatorsIt.Value()
				targetStakers = append(targetStakers, staker)
			}
			delegatorsIt.Release()
		}
	}

	for _, currentStaker := range targetStakers {
		nodeID := currentStaker.NodeID
		weight := json.Uint64(currentStaker.Weight)
		apiStaker := platformapi.Staker{
			TxID:        currentStaker.TxID,
			StartTime:   json.Uint64(currentStaker.StartTime.Unix()),
			EndTime:     json.Uint64(currentStaker.EndTime.Unix()),
			Weight:      weight,
			StakeAmount: &weight,
			NodeID:      nodeID,
		}
		potentialReward := json.Uint64(currentStaker.PotentialReward)

		switch currentStaker.Priority {
		case txs.PrimaryNetworkValidatorCurrentPriority, txs.SubnetPermissionlessValidatorCurrentPriority:
			attr, err := s.loadStakerTxAttributes(currentStaker.TxID)
			if err != nil {
				return err
			}

			shares := attr.shares
			delegationFee := json.Float32(100 * float32(shares) / float32(reward.PercentDenominator))

			uptime, err := s.getAPIUptime(currentStaker)
			if err != nil {
				return err
			}

			connected := s.vm.uptimeManager.IsConnected(nodeID, args.SubnetID)
			var (
				validationRewardOwner *platformapi.Owner
				delegationRewardOwner *platformapi.Owner
			)
			validationOwner, ok := attr.validationRewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				validationRewardOwner, err = s.getAPIOwner(validationOwner)
				if err != nil {
					return err
				}
			}
			delegationOwner, ok := attr.delegationRewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				delegationRewardOwner, err = s.getAPIOwner(delegationOwner)
				if err != nil {
					return err
				}
			}

			vdr := platformapi.PermissionlessValidator{
				Staker:                apiStaker,
				Uptime:                uptime,
				Connected:             connected,
				PotentialReward:       &potentialReward,
				RewardOwner:           validationRewardOwner,
				ValidationRewardOwner: validationRewardOwner,
				DelegationRewardOwner: delegationRewardOwner,
				DelegationFee:         delegationFee,
				Signer:                attr.proofOfPossession,
			}
			reply.Validators = append(reply.Validators, vdr)

		case txs.PrimaryNetworkDelegatorCurrentPriority, txs.SubnetPermissionlessDelegatorCurrentPriority:
			var rewardOwner *platformapi.Owner
			// If we are handling multiple nodeIDs, we don't return the
			// delegator information.
			if numNodeIDs == 1 {
				attr, err := s.loadStakerTxAttributes(currentStaker.TxID)
				if err != nil {
					return err
				}
				owner, ok := attr.rewardsOwner.(*secp256k1fx.OutputOwners)
				if ok {
					rewardOwner, err = s.getAPIOwner(owner)
					if err != nil {
						return err
					}
				}
			}

			delegator := platformapi.PrimaryDelegator{
				Staker:          apiStaker,
				RewardOwner:     rewardOwner,
				PotentialReward: &potentialReward,
			}
			vdrToDelegators[delegator.NodeID] = append(vdrToDelegators[delegator.NodeID], delegator)

		case txs.SubnetPermissionedValidatorCurrentPriority:
			uptime, err := s.getAPIUptime(currentStaker)
			if err != nil {
				return err
			}
			connected := s.vm.uptimeManager.IsConnected(nodeID, args.SubnetID)
			reply.Validators = append(reply.Validators, platformapi.PermissionedValidator{
				Staker:    apiStaker,
				Connected: connected,
				Uptime:    uptime,
			})

		default:
			return fmt.Errorf("unexpected staker priority %d", currentStaker.Priority)
		}
	}

	// handle delegators' information
	for i, vdrIntf := range reply.Validators {
		vdr, ok := vdrIntf.(platformapi.PermissionlessValidator)
		if !ok {
			continue
		}
		delegators, ok := vdrToDelegators[vdr.NodeID]
		if !ok {
			// If we are expected to populate the delegators field, we should
			// always return a non-nil value.
			delegators = []platformapi.PrimaryDelegator{}
		}
		delegatorCount := json.Uint64(len(delegators))
		delegatorWeight := json.Uint64(0)
		for _, d := range delegators {
			delegatorWeight += d.Weight
		}

		vdr.DelegatorCount = &delegatorCount
		vdr.DelegatorWeight = &delegatorWeight

		if numNodeIDs == 1 {
			// queried a specific validator, load all of its delegators
			vdr.Delegators = &delegators
		}
		reply.Validators[i] = vdr
	}

	return nil
}

// GetPendingValidatorsArgs are the arguments for calling GetPendingValidators
type GetPendingValidatorsArgs struct {
	// Subnet we're getting the pending validators of
	// If omitted, defaults to primary network
	SubnetID ids.ID `json:"subnetID"`
	// NodeIDs of validators to request. If [NodeIDs]
	// is empty, it fetches all pending validators. If
	// some requested nodeIDs are not pending validators,
	// they are omitted from the response.
	NodeIDs []ids.NodeID `json:"nodeIDs"`
}

// GetPendingValidatorsReply are the results from calling GetPendingValidators.
type GetPendingValidatorsReply struct {
	Validators []interface{} `json:"validators"`
	Delegators []interface{} `json:"delegators"`
}

// GetPendingValidators returns the lists of pending validators and delegators.
func (s *Service) GetPendingValidators(_ *http.Request, args *GetPendingValidatorsArgs, reply *GetPendingValidatorsReply) error {
	s.vm.ctx.Log.Debug("Platform: GetPendingValidators called")

	reply.Validators = []interface{}{}
	reply.Delegators = []interface{}{}

	// Create set of nodeIDs
	nodeIDs := set.Set[ids.NodeID]{}
	nodeIDs.Add(args.NodeIDs...)

	numNodeIDs := nodeIDs.Len()
	targetStakers := make([]*state.Staker, 0, numNodeIDs)
	if numNodeIDs == 0 { // Include all nodes
		pendingStakerIterator, err := s.vm.state.GetPendingStakerIterator()
		if err != nil {
			return err
		}
		for pendingStakerIterator.Next() { // Iterates in order of increasing stop time
			staker := pendingStakerIterator.Value()
			if args.SubnetID != staker.SubnetID {
				continue
			}
			targetStakers = append(targetStakers, staker)
		}
		pendingStakerIterator.Release()
	} else {
		for nodeID := range nodeIDs {
			staker, err := s.vm.state.GetPendingValidator(args.SubnetID, nodeID)
			switch err {
			case nil:
			case database.ErrNotFound:
				// nothing to do, continue
				continue
			default:
				return err
			}
			targetStakers = append(targetStakers, staker)

			delegatorsIt, err := s.vm.state.GetPendingDelegatorIterator(args.SubnetID, nodeID)
			if err != nil {
				return err
			}
			for delegatorsIt.Next() {
				staker := delegatorsIt.Value()
				targetStakers = append(targetStakers, staker)
			}
			delegatorsIt.Release()
		}
	}

	for _, pendingStaker := range targetStakers {
		nodeID := pendingStaker.NodeID
		weight := json.Uint64(pendingStaker.Weight)
		apiStaker := platformapi.Staker{
			TxID:        pendingStaker.TxID,
			NodeID:      nodeID,
			StartTime:   json.Uint64(pendingStaker.StartTime.Unix()),
			EndTime:     json.Uint64(pendingStaker.EndTime.Unix()),
			Weight:      weight,
			StakeAmount: &weight,
		}

		switch pendingStaker.Priority {
		case txs.PrimaryNetworkValidatorPendingPriority, txs.SubnetPermissionlessValidatorPendingPriority:
			attr, err := s.loadStakerTxAttributes(pendingStaker.TxID)
			if err != nil {
				return err
			}

			shares := attr.shares
			delegationFee := json.Float32(100 * float32(shares) / float32(reward.PercentDenominator))

			connected := s.vm.uptimeManager.IsConnected(nodeID, args.SubnetID)
			vdr := platformapi.PermissionlessValidator{
				Staker:        apiStaker,
				DelegationFee: delegationFee,
				Connected:     connected,
				Signer:        attr.proofOfPossession,
			}
			reply.Validators = append(reply.Validators, vdr)

		case txs.PrimaryNetworkDelegatorApricotPendingPriority, txs.PrimaryNetworkDelegatorBanffPendingPriority, txs.SubnetPermissionlessDelegatorPendingPriority:
			reply.Delegators = append(reply.Delegators, apiStaker)

		case txs.SubnetPermissionedValidatorPendingPriority:
			connected := s.vm.uptimeManager.IsConnected(nodeID, args.SubnetID)
			reply.Validators = append(reply.Validators, platformapi.PermissionedValidator{
				Staker:    apiStaker,
				Connected: connected,
			})

		default:
			return fmt.Errorf("unexpected staker priority %d", pendingStaker.Priority)
		}
	}
	return nil
}

// GetCurrentSupplyArgs are the arguments for calling GetCurrentSupply
type GetCurrentSupplyArgs struct {
	SubnetID ids.ID `json:"subnetID"`
}

// GetCurrentSupplyReply are the results from calling GetCurrentSupply
type GetCurrentSupplyReply struct {
	Supply json.Uint64 `json:"supply"`
}

// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
func (s *Service) GetCurrentSupply(_ *http.Request, args *GetCurrentSupplyArgs, reply *GetCurrentSupplyReply) error {
	s.vm.ctx.Log.Debug("Platform: GetCurrentSupply called")

	supply, err := s.vm.state.GetCurrentSupply(args.SubnetID)
	reply.Supply = json.Uint64(supply)
	return err
}

// SampleValidatorsArgs are the arguments for calling SampleValidators
type SampleValidatorsArgs struct {
	// Number of validators in the sample
	Size json.Uint16 `json:"size"`

	// ID of subnet to sample validators from
	// If omitted, defaults to the primary network
	SubnetID ids.ID `json:"subnetID"`
}

// SampleValidatorsReply are the results from calling Sample
type SampleValidatorsReply struct {
	Validators []ids.NodeID `json:"validators"`
}

// SampleValidators returns a sampling of the list of current validators
func (s *Service) SampleValidators(_ *http.Request, args *SampleValidatorsArgs, reply *SampleValidatorsReply) error {
	s.vm.ctx.Log.Debug("Platform: SampleValidators called",
		zap.Uint16("size", uint16(args.Size)),
	)

	validators, ok := s.vm.Validators.Get(args.SubnetID)
	if !ok {
		return fmt.Errorf(
			"couldn't get validators of subnet %q. Is it being validated?",
			args.SubnetID,
		)
	}

	sample, err := validators.Sample(int(args.Size))
	if err != nil {
		return fmt.Errorf("sampling errored with %w", err)
	}

	if sample == nil {
		reply.Validators = []ids.NodeID{}
	} else {
		utils.Sort(sample)
		reply.Validators = sample
	}
	return nil
}

/*
 ******************************************************
 ************ Add Validators to Subnets ***************
 ******************************************************
 */

// AddValidatorArgs are the arguments to AddValidator
type AddValidatorArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	platformapi.Staker
	// The address the staking reward, if applicable, will go to
	RewardAddress     string       `json:"rewardAddress"`
	DelegationFeeRate json.Float32 `json:"delegationFeeRate"`
}

// AddValidator creates and signs and issues a transaction to add a validator to
// the primary network
func (s *Service) AddValidator(_ *http.Request, args *AddValidatorArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("Platform: AddValidator called")

	now := s.vm.clock.Time()
	minAddStakerTime := now.Add(minAddStakerDelay)
	minAddStakerUnix := json.Uint64(minAddStakerTime.Unix())
	maxAddStakerTime := now.Add(executor.MaxFutureStartTime)
	maxAddStakerUnix := json.Uint64(maxAddStakerTime.Unix())

	if args.StartTime == 0 {
		args.StartTime = minAddStakerUnix
	}

	switch {
	case args.RewardAddress == "":
		return errNoRewardAddress
	case args.StartTime < minAddStakerUnix:
		return errStartTimeTooSoon
	case args.StartTime > maxAddStakerUnix:
		return errStartTimeTooLate
	case args.DelegationFeeRate < 0 || args.DelegationFeeRate > 100:
		return errInvalidDelegationRate
	}

	// Parse the node ID
	var nodeID ids.NodeID
	if args.NodeID == ids.EmptyNodeID { // If ID unspecified, use this node's ID
		nodeID = s.vm.ctx.NodeID
	} else {
		nodeID = args.NodeID
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return err
	}

	// Parse the reward address
	rewardAddress, err := avax.ParseServiceAddress(s.addrManager, args.RewardAddress)
	if err != nil {
		return fmt.Errorf("problem while parsing reward address: %w", err)
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	// Get the user's keys
	privKeys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Parse the change address.
	if len(privKeys.Keys) == 0 {
		return errNoKeys
	}
	changeAddr := privKeys.Keys[0].PublicKey().Address() // By default, use a key controlled by the user
	if args.ChangeAddr != "" {
		changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// TODO: Remove after StakeAmount is removed from [args].
	if args.StakeAmount != nil {
		args.Weight = *args.StakeAmount
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewAddValidatorTx(
		uint64(args.Weight),                  // Stake amount
		uint64(args.StartTime),               // Start time
		uint64(args.EndTime),                 // End time
		nodeID,                               // Node ID
		rewardAddress,                        // Reward Address
		uint32(10000*args.DelegationFeeRate), // Shares
		privKeys.Keys,                        // Keys providing the staked tokens
		changeAddr,
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()
	reply.ChangeAddr, err = s.addrManager.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		s.vm.Builder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// AddDelegatorArgs are the arguments to AddDelegator
type AddDelegatorArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	platformapi.Staker
	RewardAddress string `json:"rewardAddress"`
}

// AddDelegator creates and signs and issues a transaction to add a delegator to
// the primary network
func (s *Service) AddDelegator(_ *http.Request, args *AddDelegatorArgs, reply *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("Platform: AddDelegator called")

	now := s.vm.clock.Time()
	minAddStakerTime := now.Add(minAddStakerDelay)
	minAddStakerUnix := json.Uint64(minAddStakerTime.Unix())
	maxAddStakerTime := now.Add(executor.MaxFutureStartTime)
	maxAddStakerUnix := json.Uint64(maxAddStakerTime.Unix())

	if args.StartTime == 0 {
		args.StartTime = minAddStakerUnix
	}

	switch {
	case args.RewardAddress == "":
		return errNoRewardAddress
	case args.StartTime < minAddStakerUnix:
		return errStartTimeTooSoon
	case args.StartTime > maxAddStakerUnix:
		return errStartTimeTooLate
	}

	var nodeID ids.NodeID
	if args.NodeID == ids.EmptyNodeID { // If ID unspecified, use this node's ID
		nodeID = s.vm.ctx.NodeID
	} else {
		nodeID = args.NodeID
	}

	// Parse the reward address
	rewardAddress, err := avax.ParseServiceAddress(s.addrManager, args.RewardAddress)
	if err != nil {
		return fmt.Errorf("problem parsing 'rewardAddress': %w", err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	privKeys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Parse the change address. Assumes that if the user has no keys,
	// this operation will fail so the change address can be anything.
	if len(privKeys.Keys) == 0 {
		return errNoKeys
	}
	changeAddr := privKeys.Keys[0].PublicKey().Address() // By default, use a key controlled by the user
	if args.ChangeAddr != "" {
		changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// TODO: Remove after StakeAmount is removed from [args].
	if args.StakeAmount != nil {
		args.Weight = *args.StakeAmount
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewAddDelegatorTx(
		uint64(args.Weight),    // Stake amount
		uint64(args.StartTime), // Start time
		uint64(args.EndTime),   // End time
		nodeID,                 // Node ID
		rewardAddress,          // Reward Address
		privKeys.Keys,          // Private keys
		changeAddr,             // Change address
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()
	reply.ChangeAddr, err = s.addrManager.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		s.vm.Builder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// AddSubnetValidatorArgs are the arguments to AddSubnetValidator
type AddSubnetValidatorArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	platformapi.Staker
	// ID of subnet to validate
	SubnetID string `json:"subnetID"`
}

// AddSubnetValidator creates and signs and issues a transaction to add a
// validator to a subnet other than the primary network
func (s *Service) AddSubnetValidator(_ *http.Request, args *AddSubnetValidatorArgs, response *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("Platform: AddSubnetValidator called")

	now := s.vm.clock.Time()
	minAddStakerTime := now.Add(minAddStakerDelay)
	minAddStakerUnix := json.Uint64(minAddStakerTime.Unix())
	maxAddStakerTime := now.Add(executor.MaxFutureStartTime)
	maxAddStakerUnix := json.Uint64(maxAddStakerTime.Unix())

	if args.StartTime == 0 {
		args.StartTime = minAddStakerUnix
	}

	switch {
	case args.SubnetID == "":
		return errNoSubnetID
	case args.StartTime < minAddStakerUnix:
		return errStartTimeTooSoon
	case args.StartTime > maxAddStakerUnix:
		return errStartTimeTooLate
	}

	// Parse the subnet ID
	subnetID, err := ids.FromString(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem parsing subnetID %q: %w", args.SubnetID, err)
	}
	if subnetID == constants.PrimaryNetworkID {
		return errNamedSubnetCantBePrimary
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	keys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Parse the change address.
	if len(keys.Keys) == 0 {
		return errNoKeys
	}
	changeAddr := keys.Keys[0].PublicKey().Address() // By default, use a key controlled by the user
	if args.ChangeAddr != "" {
		changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// TODO: Remove after StakeAmount is removed from [args].
	if args.StakeAmount != nil {
		args.Weight = *args.StakeAmount
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewAddSubnetValidatorTx(
		uint64(args.Weight),    // Stake amount
		uint64(args.StartTime), // Start time
		uint64(args.EndTime),   // End time
		args.NodeID,            // Node ID
		subnetID,               // Subnet ID
		keys.Keys,
		changeAddr,
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = s.addrManager.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		s.vm.Builder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// CreateSubnetArgs are the arguments to CreateSubnet
type CreateSubnetArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	// The ID member of APISubnet is ignored
	APISubnet
}

// CreateSubnet creates and signs and issues a transaction to create a new
// subnet
func (s *Service) CreateSubnet(_ *http.Request, args *CreateSubnetArgs, response *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("Platform: CreateSubnet called")

	// Parse the control keys
	controlKeys, err := avax.ParseServiceAddresses(s.addrManager, args.ControlKeys)
	if err != nil {
		return err
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	privKeys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Parse the change address. Assumes that if the user has no keys,
	// this operation will fail so the change address can be anything.
	if len(privKeys.Keys) == 0 {
		return errNoKeys
	}
	changeAddr := privKeys.Keys[0].PublicKey().Address() // By default, use a key controlled by the user
	if args.ChangeAddr != "" {
		changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewCreateSubnetTx(
		uint32(args.Threshold), // Threshold
		controlKeys.List(),     // Control Addresses
		privKeys.Keys,          // Private keys
		changeAddr,
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = s.addrManager.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		s.vm.Builder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// ExportAVAXArgs are the arguments to ExportAVAX
type ExportAVAXArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader

	// Amount of AVAX to send
	Amount json.Uint64 `json:"amount"`

	// Chain the funds are going to. Optional. Used if To address does not include the chainID.
	TargetChain string `json:"targetChain"`

	// ID of the address that will receive the AVAX. This address may include the
	// chainID, which is used to determine what the destination chain is.
	To string `json:"to"`
}

// ExportAVAX exports AVAX from the P-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (s *Service) ExportAVAX(_ *http.Request, args *ExportAVAXArgs, response *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("Platform: ExportAVAX called")

	if args.Amount == 0 {
		return errNoAmount
	}

	// Get the chainID and parse the to address
	chainID, to, err := s.addrManager.ParseAddress(args.To)
	if err != nil {
		chainID, err = s.vm.ctx.BCLookup.Lookup(args.TargetChain)
		if err != nil {
			return err
		}
		to, err = ids.ShortFromString(args.To)
		if err != nil {
			return err
		}
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	privKeys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Parse the change address. Assumes that if the user has no keys,
	// this operation will fail so the change address can be anything.
	if len(privKeys.Keys) == 0 {
		return errNoKeys
	}
	changeAddr := privKeys.Keys[0].PublicKey().Address() // By default, use a key controlled by the user
	if args.ChangeAddr != "" {
		changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewExportTx(
		uint64(args.Amount), // Amount
		chainID,             // ID of the chain to send the funds to
		to,                  // Address
		privKeys.Keys,       // Private keys
		changeAddr,          // Change address
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = s.addrManager.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		s.vm.Builder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// ImportAVAXArgs are the arguments to ImportAVAX
type ImportAVAXArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader

	// Chain the funds are coming from
	SourceChain string `json:"sourceChain"`

	// The address that will receive the imported funds
	To string `json:"to"`
}

// ImportAVAX issues a transaction to import AVAX from the X-chain. The AVAX
// must have already been exported from the X-Chain.
func (s *Service) ImportAVAX(_ *http.Request, args *ImportAVAXArgs, response *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("Platform: ImportAVAX called")

	// Parse the sourceCHain
	chainID, err := s.vm.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing chainID %q: %w", args.SourceChain, err)
	}

	// Parse the to address
	to, err := avax.ParseServiceAddress(s.addrManager, args.To)
	if err != nil { // Parse address
		return fmt.Errorf("couldn't parse argument 'to' to an address: %w", err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	privKeys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil { // Get keys
		return fmt.Errorf("couldn't get keys controlled by the user: %w", err)
	}

	// Parse the change address. Assumes that if the user has no keys,
	// this operation will fail so the change address can be anything.
	if len(privKeys.Keys) == 0 {
		return errNoKeys
	}
	changeAddr := privKeys.Keys[0].PublicKey().Address() // By default, use a key controlled by the user
	if args.ChangeAddr != "" {
		changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	tx, err := s.vm.txBuilder.NewImportTx(
		chainID,
		to,
		privKeys.Keys,
		changeAddr,
	)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = s.addrManager.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		s.vm.Builder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

/*
 ******************************************************
 ******** Create/get status of a blockchain ***********
 ******************************************************
 */

// CreateBlockchainArgs is the arguments for calling CreateBlockchain
type CreateBlockchainArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	// ID of Subnet that validates the new blockchain
	SubnetID ids.ID `json:"subnetID"`
	// ID of the VM the new blockchain is running
	VMID string `json:"vmID"`
	// IDs of the FXs the VM is running
	FxIDs []string `json:"fxIDs"`
	// Human-readable name for the new blockchain, not necessarily unique
	Name string `json:"name"`
	// Genesis state of the blockchain being created
	GenesisData string `json:"genesisData"`
	// Encoding format to use for genesis data
	Encoding formatting.Encoding `json:"encoding"`
}

// CreateBlockchain issues a transaction to create a new blockchain
func (s *Service) CreateBlockchain(_ *http.Request, args *CreateBlockchainArgs, response *api.JSONTxIDChangeAddr) error {
	s.vm.ctx.Log.Debug("Platform: CreateBlockchain called")

	switch {
	case args.Name == "":
		return errMissingName
	case args.VMID == "":
		return errMissingVMID
	}

	genesisBytes, err := formatting.Decode(args.Encoding, args.GenesisData)
	if err != nil {
		return fmt.Errorf("problem parsing genesis data: %w", err)
	}

	vmID, err := s.vm.Chains.LookupVM(args.VMID)
	if err != nil {
		return fmt.Errorf("no VM with ID '%s' found", args.VMID)
	}

	fxIDs := []ids.ID(nil)
	for _, fxIDStr := range args.FxIDs {
		fxID, err := s.vm.Chains.LookupVM(fxIDStr)
		if err != nil {
			return fmt.Errorf("no FX with ID '%s' found", fxIDStr)
		}
		fxIDs = append(fxIDs, fxID)
	}
	// If creating AVM instance, use secp256k1fx
	// TODO: Document FXs and have user specify them in API call
	fxIDsSet := set.Set[ids.ID]{}
	fxIDsSet.Add(fxIDs...)
	if vmID == constants.AVMID && !fxIDsSet.Contains(secp256k1fx.ID) {
		fxIDs = append(fxIDs, secp256k1fx.ID)
	}

	if args.SubnetID == constants.PrimaryNetworkID {
		return txs.ErrCantValidatePrimaryNetwork
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseServiceAddresses(s.addrManager, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(s.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	keys, err := keystore.GetKeychain(user, fromAddrs)
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Parse the change address. Assumes that if the user has no keys,
	// this operation will fail so the change address can be anything.
	if len(keys.Keys) == 0 {
		return errNoKeys
	}
	changeAddr := keys.Keys[0].PublicKey().Address() // By default, use a key controlled by the user
	if args.ChangeAddr != "" {
		changeAddr, err = avax.ParseServiceAddress(s.addrManager, args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := s.vm.txBuilder.NewCreateChainTx(
		args.SubnetID,
		genesisBytes,
		vmID,
		fxIDs,
		args.Name,
		keys.Keys,
		changeAddr, // Change address
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = s.addrManager.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		s.vm.Builder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// GetBlockchainStatusArgs is the arguments for calling GetBlockchainStatus
// [BlockchainID] is the ID of or an alias of the blockchain to get the status of.
type GetBlockchainStatusArgs struct {
	BlockchainID string `json:"blockchainID"`
}

// GetBlockchainStatusReply is the reply from calling GetBlockchainStatus
// [Status] is the blockchain's status.
type GetBlockchainStatusReply struct {
	Status status.BlockchainStatus `json:"status"`
}

// GetBlockchainStatus gets the status of a blockchain with the ID [args.BlockchainID].
func (s *Service) GetBlockchainStatus(r *http.Request, args *GetBlockchainStatusArgs, reply *GetBlockchainStatusReply) error {
	s.vm.ctx.Log.Debug("Platform: GetBlockchainStatus called")

	if args.BlockchainID == "" {
		return errMissingBlockchainID
	}

	// if its aliased then vm created this chain.
	if aliasedID, err := s.vm.Chains.Lookup(args.BlockchainID); err == nil {
		if s.nodeValidates(aliasedID) {
			reply.Status = status.Validating
			return nil
		}

		reply.Status = status.Syncing
		return nil
	}

	blockchainID, err := ids.FromString(args.BlockchainID)
	if err != nil {
		return fmt.Errorf("problem parsing blockchainID %q: %w", args.BlockchainID, err)
	}

	ctx := r.Context()
	lastAcceptedID, err := s.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("problem loading last accepted ID: %w", err)
	}

	exists, err := s.chainExists(ctx, lastAcceptedID, blockchainID)
	if err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	}
	if exists {
		reply.Status = status.Created
		return nil
	}

	preferredBlk, err := s.vm.Preferred()
	if err != nil {
		return fmt.Errorf("could not retrieve preferred block, err %w", err)
	}
	preferred, err := s.chainExists(ctx, preferredBlk.ID(), blockchainID)
	if err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	}
	if preferred {
		reply.Status = status.Preferred
	} else {
		reply.Status = status.UnknownChain
	}
	return nil
}

func (s *Service) nodeValidates(blockchainID ids.ID) bool {
	chainTx, _, err := s.vm.state.GetTx(blockchainID)
	if err != nil {
		return false
	}

	chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
	if !ok {
		return false
	}

	validators, ok := s.vm.Validators.Get(chain.SubnetID)
	if !ok {
		return false
	}

	return validators.Contains(s.vm.ctx.NodeID)
}

func (s *Service) chainExists(ctx context.Context, blockID ids.ID, chainID ids.ID) (bool, error) {
	state, ok := s.vm.manager.GetState(blockID)
	if !ok {
		block, err := s.vm.GetBlock(ctx, blockID)
		if err != nil {
			return false, err
		}
		state, ok = s.vm.manager.GetState(block.Parent())
		if !ok {
			return false, errMissingDecisionBlock
		}
	}

	tx, _, err := state.GetTx(chainID)
	if err == database.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, ok = tx.Unsigned.(*txs.CreateChainTx)
	return ok, nil
}

// ValidatedByArgs is the arguments for calling ValidatedBy
type ValidatedByArgs struct {
	// ValidatedBy returns the ID of the Subnet validating the blockchain with this ID
	BlockchainID ids.ID `json:"blockchainID"`
}

// ValidatedByResponse is the reply from calling ValidatedBy
type ValidatedByResponse struct {
	// ID of the Subnet validating the specified blockchain
	SubnetID ids.ID `json:"subnetID"`
}

// ValidatedBy returns the ID of the Subnet that validates [args.BlockchainID]
func (s *Service) ValidatedBy(r *http.Request, args *ValidatedByArgs, response *ValidatedByResponse) error {
	s.vm.ctx.Log.Debug("Platform: ValidatedBy called")

	var err error
	ctx := r.Context()
	response.SubnetID, err = s.vm.GetSubnetID(ctx, args.BlockchainID)
	return err
}

// ValidatesArgs are the arguments to Validates
type ValidatesArgs struct {
	SubnetID ids.ID `json:"subnetID"`
}

// ValidatesResponse is the response from calling Validates
type ValidatesResponse struct {
	BlockchainIDs []ids.ID `json:"blockchainIDs"`
}

// Validates returns the IDs of the blockchains validated by [args.SubnetID]
func (s *Service) Validates(_ *http.Request, args *ValidatesArgs, response *ValidatesResponse) error {
	s.vm.ctx.Log.Debug("Platform: Validates called")

	if args.SubnetID != constants.PrimaryNetworkID {
		subnetTx, _, err := s.vm.state.GetTx(args.SubnetID)
		if err != nil {
			return fmt.Errorf(
				"problem retrieving subnet %q: %w",
				args.SubnetID,
				err,
			)
		}
		_, ok := subnetTx.Unsigned.(*txs.CreateSubnetTx)
		if !ok {
			return fmt.Errorf("%q is not a subnet", args.SubnetID)
		}
	}

	// Get the chains that exist
	chains, err := s.vm.state.GetChains(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem retrieving chains for subnet %q: %w", args.SubnetID, err)
	}

	response.BlockchainIDs = make([]ids.ID, len(chains))
	for i, chain := range chains {
		response.BlockchainIDs[i] = chain.ID()
	}
	return nil
}

// APIBlockchain is the representation of a blockchain used in API calls
type APIBlockchain struct {
	// Blockchain's ID
	ID ids.ID `json:"id"`

	// Blockchain's (non-unique) human-readable name
	Name string `json:"name"`

	// Subnet that validates the blockchain
	SubnetID ids.ID `json:"subnetID"`

	// Virtual Machine the blockchain runs
	VMID ids.ID `json:"vmID"`
}

// GetBlockchainsResponse is the response from a call to GetBlockchains
type GetBlockchainsResponse struct {
	// blockchains that exist
	Blockchains []APIBlockchain `json:"blockchains"`
}

// GetBlockchains returns all of the blockchains that exist
func (s *Service) GetBlockchains(_ *http.Request, _ *struct{}, response *GetBlockchainsResponse) error {
	s.vm.ctx.Log.Debug("Platform: GetBlockchains called")

	subnets, err := s.vm.state.GetSubnets()
	if err != nil {
		return fmt.Errorf("couldn't retrieve subnets: %w", err)
	}

	response.Blockchains = []APIBlockchain{}
	for _, subnet := range subnets {
		subnetID := subnet.ID()
		chains, err := s.vm.state.GetChains(subnetID)
		if err != nil {
			return fmt.Errorf(
				"couldn't retrieve chains for subnet %q: %w",
				subnetID,
				err,
			)
		}

		for _, chainTx := range chains {
			chainID := chainTx.ID()
			chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
			if !ok {
				return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chainTx.Unsigned)
			}
			response.Blockchains = append(response.Blockchains, APIBlockchain{
				ID:       chainID,
				Name:     chain.ChainName,
				SubnetID: subnetID,
				VMID:     chain.VMID,
			})
		}
	}

	chains, err := s.vm.state.GetChains(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("couldn't retrieve subnets: %w", err)
	}
	for _, chainTx := range chains {
		chainID := chainTx.ID()
		chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chainTx.Unsigned)
		}
		response.Blockchains = append(response.Blockchains, APIBlockchain{
			ID:       chainID,
			Name:     chain.ChainName,
			SubnetID: constants.PrimaryNetworkID,
			VMID:     chain.VMID,
		})
	}

	return nil
}

// IssueTx issues a tx
func (s *Service) IssueTx(_ *http.Request, args *api.FormattedTx, response *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("Platform: IssueTx called")

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}
	tx, err := txs.Parse(txs.Codec, txBytes)
	if err != nil {
		return fmt.Errorf("couldn't parse tx: %w", err)
	}
	if err := s.vm.Builder.AddUnverifiedTx(tx); err != nil {
		return fmt.Errorf("couldn't issue tx: %w", err)
	}

	response.TxID = tx.ID()
	return nil
}

// GetTx gets a tx
func (s *Service) GetTx(_ *http.Request, args *api.GetTxArgs, response *api.GetTxReply) error {
	s.vm.ctx.Log.Debug("Platform: GetTx called")

	tx, _, err := s.vm.state.GetTx(args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get tx: %w", err)
	}
	txBytes := tx.Bytes()
	response.Encoding = args.Encoding

	if args.Encoding == formatting.JSON {
		tx.Unsigned.InitCtx(s.vm.ctx)
		response.Tx = tx
		return nil
	}

	response.Tx, err = formatting.Encode(args.Encoding, txBytes)
	if err != nil {
		return fmt.Errorf("couldn't encode tx as a string: %w", err)
	}
	return nil
}

type GetTxStatusArgs struct {
	TxID ids.ID `json:"txID"`
	// Returns a response that looks like this:
	// {
	// 	"jsonrpc": "2.0",
	// 	"result": {
	//     "status":"[Status]",
	//     "reason":"[Reason tx was dropped, if applicable]"
	//  },
	// 	"id": 1
	// }
	// "reason" is only present if the status is dropped
}

type GetTxStatusResponse struct {
	Status status.Status `json:"status"`
	// Reason this tx was dropped.
	// Only non-empty if Status is dropped
	Reason string `json:"reason,omitempty"`
}

// GetTxStatus gets a tx's status
func (s *Service) GetTxStatus(_ *http.Request, args *GetTxStatusArgs, response *GetTxStatusResponse) error {
	s.vm.ctx.Log.Debug("Platform: GetTxStatus called",
		zap.Stringer("txID", args.TxID),
	)

	_, txStatus, err := s.vm.state.GetTx(args.TxID)
	if err == nil { // Found the status. Report it.
		response.Status = txStatus
		return nil
	}
	if err != database.ErrNotFound {
		return err
	}

	// The status of this transaction is not in the database - check if the tx
	// is in the preferred block's db. If so, return that it's processing.
	prefBlk, err := s.vm.Preferred()
	if err != nil {
		return err
	}

	preferredID := prefBlk.ID()
	onAccept, ok := s.vm.manager.GetState(preferredID)
	if !ok {
		return fmt.Errorf("could not retrieve state for block %s", preferredID)
	}

	_, _, err = onAccept.GetTx(args.TxID)
	if err == nil {
		// Found the status in the preferred block's db. Report tx is processing.
		response.Status = status.Processing
		return nil
	}
	if err != database.ErrNotFound {
		return err
	}

	if s.vm.Builder.Has(args.TxID) {
		// Found the tx in the mempool. Report tx is processing.
		response.Status = status.Processing
		return nil
	}

	// Note: we check if tx is dropped only after having looked for it
	// in the database and the mempool, because dropped txs may be re-issued.
	reason, dropped := s.vm.Builder.GetDropReason(args.TxID)
	if !dropped {
		// The tx isn't being tracked by the node.
		response.Status = status.Unknown
		return nil
	}

	// The tx was recently dropped because it was invalid.
	response.Status = status.Dropped
	response.Reason = reason
	return nil
}

type GetStakeArgs struct {
	api.JSONAddresses
	Encoding formatting.Encoding `json:"encoding"`
}

// GetStakeReply is the response from calling GetStake.
type GetStakeReply struct {
	Staked  json.Uint64            `json:"staked"`
	Stakeds map[ids.ID]json.Uint64 `json:"stakeds"`
	// String representation of staked outputs
	// Each is of type avax.TransferableOutput
	Outputs []string `json:"stakedOutputs"`
	// Encoding of [Outputs]
	Encoding formatting.Encoding `json:"encoding"`
}

// GetStake returns the amount of nAVAX that [args.Addresses] have cumulatively
// staked on the Primary Network.
//
// This method assumes that each stake output has only owner
// This method assumes only AVAX can be staked
// This method only concerns itself with the Primary Network, not subnets
// TODO: Improve the performance of this method by maintaining this data
// in a data structure rather than re-calculating it by iterating over stakers
func (s *Service) GetStake(_ *http.Request, args *GetStakeArgs, response *GetStakeReply) error {
	s.vm.ctx.Log.Debug("Platform: GetStake called")

	if len(args.Addresses) > maxGetStakeAddrs {
		return fmt.Errorf("%d addresses provided but this method can take at most %d", len(args.Addresses), maxGetStakeAddrs)
	}

	addrs, err := avax.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return err
	}

	currentStakerIterator, err := s.vm.state.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	defer currentStakerIterator.Release()

	var (
		totalAmountStaked = make(map[ids.ID]uint64)
		stakedOuts        []avax.TransferableOutput
	)
	for currentStakerIterator.Next() { // Iterates over current stakers
		staker := currentStakerIterator.Value()

		tx, _, err := s.vm.state.GetTx(staker.TxID)
		if err != nil {
			return err
		}

		stakedOuts = append(stakedOuts, getStakeHelper(tx, addrs, totalAmountStaked)...)
	}

	pendingStakerIterator, err := s.vm.state.GetPendingStakerIterator()
	if err != nil {
		return err
	}
	defer pendingStakerIterator.Release()

	for pendingStakerIterator.Next() { // Iterates over pending stakers
		staker := pendingStakerIterator.Value()

		tx, _, err := s.vm.state.GetTx(staker.TxID)
		if err != nil {
			return err
		}

		stakedOuts = append(stakedOuts, getStakeHelper(tx, addrs, totalAmountStaked)...)
	}

	response.Stakeds = newJSONBalanceMap(totalAmountStaked)
	response.Staked = response.Stakeds[s.vm.ctx.AVAXAssetID]
	response.Outputs = make([]string, len(stakedOuts))
	for i, output := range stakedOuts {
		bytes, err := txs.Codec.Marshal(txs.Version, output)
		if err != nil {
			return fmt.Errorf("couldn't serialize output %s: %w", output.ID, err)
		}
		response.Outputs[i], err = formatting.Encode(args.Encoding, bytes)
		if err != nil {
			return fmt.Errorf("couldn't encode output %s as string: %w", output.ID, err)
		}
	}
	response.Encoding = args.Encoding

	return nil
}

// GetMinStakeArgs are the arguments for calling GetMinStake.
type GetMinStakeArgs struct {
	SubnetID ids.ID `json:"subnetID"`
}

// GetMinStakeReply is the response from calling GetMinStake.
type GetMinStakeReply struct {
	//  The minimum amount of tokens one must bond to be a validator
	MinValidatorStake json.Uint64 `json:"minValidatorStake"`
	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake json.Uint64 `json:"minDelegatorStake"`
}

// GetMinStake returns the minimum staking amount in nAVAX.
func (s *Service) GetMinStake(_ *http.Request, args *GetMinStakeArgs, reply *GetMinStakeReply) error {
	if args.SubnetID == constants.PrimaryNetworkID {
		reply.MinValidatorStake = json.Uint64(s.vm.MinValidatorStake)
		reply.MinDelegatorStake = json.Uint64(s.vm.MinDelegatorStake)
		return nil
	}

	transformSubnetIntf, err := s.vm.state.GetSubnetTransformation(args.SubnetID)
	if err != nil {
		return fmt.Errorf(
			"failed fetching subnet transformation for %s: %w",
			args.SubnetID,
			err,
		)
	}
	transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
	if !ok {
		return fmt.Errorf(
			"unexpected subnet transformation tx type fetched %T",
			transformSubnetIntf.Unsigned,
		)
	}

	reply.MinValidatorStake = json.Uint64(transformSubnet.MinValidatorStake)
	reply.MinDelegatorStake = json.Uint64(transformSubnet.MinDelegatorStake)

	return nil
}

// GetTotalStakeArgs are the arguments for calling GetTotalStake
type GetTotalStakeArgs struct {
	// Subnet we're getting the total stake
	// If omitted returns Primary network weight
	SubnetID ids.ID `json:"subnetID"`
}

// GetTotalStakeReply is the response from calling GetTotalStake.
type GetTotalStakeReply struct {
	// TODO: deprecate one of these fields
	Stake  json.Uint64 `json:"stake"`
	Weight json.Uint64 `json:"weight"`
}

// GetTotalStake returns the total amount staked on the Primary Network
func (s *Service) GetTotalStake(_ *http.Request, args *GetTotalStakeArgs, reply *GetTotalStakeReply) error {
	vdrs, ok := s.vm.Validators.Get(args.SubnetID)
	if !ok {
		return errMissingValidatorSet
	}
	weight := json.Uint64(vdrs.Weight())
	reply.Weight = weight
	reply.Stake = weight
	return nil
}

// GetMaxStakeAmountArgs is the request for calling GetMaxStakeAmount.
type GetMaxStakeAmountArgs struct {
	SubnetID  ids.ID      `json:"subnetID"`
	NodeID    ids.NodeID  `json:"nodeID"`
	StartTime json.Uint64 `json:"startTime"`
	EndTime   json.Uint64 `json:"endTime"`
}

// GetMaxStakeAmountReply is the response from calling GetMaxStakeAmount.
type GetMaxStakeAmountReply struct {
	Amount json.Uint64 `json:"amount"`
}

// GetMaxStakeAmount returns the maximum amount of nAVAX staking to the named
// node during the time period.
func (s *Service) GetMaxStakeAmount(_ *http.Request, args *GetMaxStakeAmountArgs, reply *GetMaxStakeAmountReply) error {
	startTime := time.Unix(int64(args.StartTime), 0)
	endTime := time.Unix(int64(args.EndTime), 0)

	if startTime.After(endTime) {
		return errStartAfterEndTime
	}
	now := s.vm.state.GetTimestamp()
	if startTime.Before(now) {
		return errStartTimeInThePast
	}

	staker, err := executor.GetValidator(s.vm.state, args.SubnetID, args.NodeID)
	if err == database.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	if startTime.After(staker.EndTime) {
		return nil
	}
	if endTime.Before(staker.StartTime) {
		return nil
	}

	maxStakeAmount, err := executor.GetMaxWeight(s.vm.state, staker, startTime, endTime)
	reply.Amount = json.Uint64(maxStakeAmount)
	return err
}

// GetRewardUTXOsReply defines the GetRewardUTXOs replies returned from the API
type GetRewardUTXOsReply struct {
	// Number of UTXOs returned
	NumFetched json.Uint64 `json:"numFetched"`
	// The UTXOs
	UTXOs []string `json:"utxos"`
	// Encoding specifies the encoding format the UTXOs are returned in
	Encoding formatting.Encoding `json:"encoding"`
}

// GetRewardUTXOs returns the UTXOs that were rewarded after the provided
// transaction's staking period ended.
func (s *Service) GetRewardUTXOs(_ *http.Request, args *api.GetTxArgs, reply *GetRewardUTXOsReply) error {
	s.vm.ctx.Log.Debug("Platform: GetRewardUTXOs called")

	utxos, err := s.vm.state.GetRewardUTXOs(args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get reward UTXOs: %w", err)
	}

	reply.NumFetched = json.Uint64(len(utxos))
	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		utxoBytes, err := txs.GenesisCodec.Marshal(txs.Version, utxo)
		if err != nil {
			return fmt.Errorf("failed to encode UTXO to bytes: %w", err)
		}

		utxoStr, err := formatting.Encode(args.Encoding, utxoBytes)
		if err != nil {
			return fmt.Errorf("couldn't encode utxo as a string: %w", err)
		}
		reply.UTXOs[i] = utxoStr
	}
	reply.Encoding = args.Encoding
	return nil
}

// GetTimestampReply is the response from GetTimestamp
type GetTimestampReply struct {
	// Current timestamp
	Timestamp time.Time `json:"timestamp"`
}

// GetTimestamp returns the current timestamp on chain.
func (s *Service) GetTimestamp(_ *http.Request, _ *struct{}, reply *GetTimestampReply) error {
	s.vm.ctx.Log.Debug("Platform: GetTimestamp called")

	reply.Timestamp = s.vm.state.GetTimestamp()
	return nil
}

// GetValidatorsAtArgs is the response from GetValidatorsAt
type GetValidatorsAtArgs struct {
	Height   json.Uint64 `json:"height"`
	SubnetID ids.ID      `json:"subnetID"`
}

// GetValidatorsAtReply is the response from GetValidatorsAt
type GetValidatorsAtReply struct {
	// TODO should we change this to map[ids.NodeID]*validators.Validator?
	// We'd have to add a MarshalJSON method to validators.Validator.
	Validators map[ids.NodeID]uint64 `json:"validators"`
}

// GetValidatorsAt returns the weights of the validator set of a provided subnet
// at the specified height.
func (s *Service) GetValidatorsAt(r *http.Request, args *GetValidatorsAtArgs, reply *GetValidatorsAtReply) error {
	height := uint64(args.Height)
	s.vm.ctx.Log.Debug("Platform: GetValidatorsAt called",
		zap.Uint64("height", height),
		zap.Stringer("subnetID", args.SubnetID),
	)

	ctx := r.Context()
	var err error
	vdrs, err := s.vm.GetValidatorSet(ctx, height, args.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get validator set: %w", err)
	}
	reply.Validators = make(map[ids.NodeID]uint64, len(vdrs))
	for _, vdr := range vdrs {
		reply.Validators[vdr.NodeID] = vdr.Weight
	}
	return nil
}

func (s *Service) GetBlock(_ *http.Request, args *api.GetBlockArgs, response *api.GetBlockResponse) error {
	s.vm.ctx.Log.Debug("Platform: GetBlock called",
		zap.Stringer("blkID", args.BlockID),
		zap.Stringer("encoding", args.Encoding),
	)

	block, err := s.vm.manager.GetStatelessBlock(args.BlockID)
	if err != nil {
		return fmt.Errorf("couldn't get block with id %s: %w", args.BlockID, err)
	}
	response.Encoding = args.Encoding

	if args.Encoding == formatting.JSON {
		block.InitCtx(s.vm.ctx)
		response.Block = block
		return nil
	}

	response.Block, err = formatting.Encode(args.Encoding, block.Bytes())
	if err != nil {
		return fmt.Errorf("couldn't encode block %s as string: %w", args.BlockID, err)
	}

	return nil
}

func (s *Service) getAPIUptime(staker *state.Staker) (*json.Float32, error) {
	// Only report uptimes that we have been actively tracking.
	if constants.PrimaryNetworkID != staker.SubnetID && !s.vm.TrackedSubnets.Contains(staker.SubnetID) {
		return nil, nil
	}

	rawUptime, err := s.vm.uptimeManager.CalculateUptimePercentFrom(staker.NodeID, staker.SubnetID, staker.StartTime)
	if err != nil {
		return nil, err
	}
	// Transform this to a percentage (0-100) to make it consistent
	// with observedUptime in info.peers API
	uptime := json.Float32(rawUptime * 100)
	return &uptime, nil
}

func (s *Service) getAPIOwner(owner *secp256k1fx.OutputOwners) (*platformapi.Owner, error) {
	apiOwner := &platformapi.Owner{
		Locktime:  json.Uint64(owner.Locktime),
		Threshold: json.Uint32(owner.Threshold),
	}
	for _, addr := range owner.Addrs {
		addrStr, err := s.addrManager.FormatLocalAddress(addr)
		if err != nil {
			return nil, err
		}
		apiOwner.Addresses = append(apiOwner.Addresses, addrStr)
	}
	return apiOwner, nil
}

// Takes in a staker and a set of addresses
// Returns:
// 1) The total amount staked by addresses in [addrs]
// 2) The staked outputs
func getStakeHelper(tx *txs.Tx, addrs set.Set[ids.ShortID], totalAmountStaked map[ids.ID]uint64) []avax.TransferableOutput {
	staker, ok := tx.Unsigned.(txs.PermissionlessStaker)
	if !ok {
		return nil
	}

	stake := staker.Stake()
	stakedOuts := make([]avax.TransferableOutput, 0, len(stake))
	// Go through all of the staked outputs
	for _, output := range stake {
		out := output.Out
		if lockedOut, ok := out.(*stakeable.LockOut); ok {
			// This output can only be used for staking until [stakeOnlyUntil]
			out = lockedOut.TransferableOut
		}
		secpOut, ok := out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}

		// Check whether this output is owned by one of the given addresses
		contains := false
		for _, addr := range secpOut.Addrs {
			if addrs.Contains(addr) {
				contains = true
				break
			}
		}
		if !contains {
			// This output isn't owned by one of the given addresses. Ignore.
			continue
		}

		assetID := output.AssetID()
		newAmount, err := math.Add64(totalAmountStaked[assetID], secpOut.Amt)
		if err != nil {
			newAmount = stdmath.MaxUint64
		}
		totalAmountStaked[assetID] = newAmount

		stakedOuts = append(
			stakedOuts,
			*output,
		)
	}
	return stakedOuts
}
