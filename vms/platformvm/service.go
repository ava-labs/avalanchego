// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"

	avajson "github.com/ava-labs/avalanchego/utils/json"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	platformapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of addresses that can be passed in as argument to GetStake
	maxGetStakeAddrs = 256

	// Max number of items allowed in a page
	maxPageSize = 1024

	// Note: Staker attributes cache should be large enough so that no evictions
	// happen when the API loops through all stakers.
	stakerAttributesCacheSize = 100_000
)

var (
	errMissingDecisionBlock       = errors.New("should have a decision block within the past two blocks")
	errPrimaryNetworkIsNotASubnet = errors.New("the primary network isn't a subnet")
	errNoAddresses                = errors.New("no addresses provided")
	errMissingBlockchainID        = errors.New("argument 'blockchainID' not given")
)

// Service defines the API calls that can be made to the platform chain
type Service struct {
	vm                    *VM
	addrManager           avax.AddressManager
	stakerAttributesCache *lru.Cache[ids.ID, *stakerAttributes]
}

// All attributes are optional and may not be filled for each stakerTx.
type stakerAttributes struct {
	shares                 uint32
	rewardsOwner           fx.Owner
	validationRewardsOwner fx.Owner
	delegationRewardsOwner fx.Owner
	proofOfPossession      *signer.ProofOfPossession
}

// GetHeight returns the height of the last accepted block
func (s *Service) GetHeight(r *http.Request, _ *struct{}, response *api.GetHeightResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getHeight"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	ctx := r.Context()
	height, err := s.vm.GetCurrentHeight(ctx)
	response.Height = avajson.Uint64(height)
	return err
}

// GetProposedHeight returns the current ProposerVM height
func (s *Service) GetProposedHeight(r *http.Request, _ *struct{}, reply *api.GetHeightResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getProposedHeight"),
	)
	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	ctx := r.Context()
	proposerHeight, err := s.vm.GetMinimumHeight(ctx)
	reply.Height = avajson.Uint64(proposerHeight)
	return err
}

type GetBalanceRequest struct {
	Addresses []string `json:"addresses"`
}

// Note: We explicitly duplicate AVAX out of the maps to ensure backwards
// compatibility.
type GetBalanceResponse struct {
	// Balance, in nAVAX, of the address
	Balance             avajson.Uint64            `json:"balance"`
	Unlocked            avajson.Uint64            `json:"unlocked"`
	LockedStakeable     avajson.Uint64            `json:"lockedStakeable"`
	LockedNotStakeable  avajson.Uint64            `json:"lockedNotStakeable"`
	Balances            map[ids.ID]avajson.Uint64 `json:"balances"`
	Unlockeds           map[ids.ID]avajson.Uint64 `json:"unlockeds"`
	LockedStakeables    map[ids.ID]avajson.Uint64 `json:"lockedStakeables"`
	LockedNotStakeables map[ids.ID]avajson.Uint64 `json:"lockedNotStakeables"`
	UTXOIDs             []*avax.UTXOID            `json:"utxoIDs"`
}

// GetBalance gets the balance of an address
func (s *Service) GetBalance(_ *http.Request, args *GetBalanceRequest, response *GetBalanceResponse) error {
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "platform"),
		zap.String("method", "getBalance"),
		logging.UserStrings("addresses", args.Addresses),
	)

	addrs, err := avax.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
				newBalance, err := safemath.Add(unlockeds[assetID], out.Amount())
				if err != nil {
					unlockeds[assetID] = math.MaxUint64
				} else {
					unlockeds[assetID] = newBalance
				}
			} else {
				newBalance, err := safemath.Add(lockedNotStakeables[assetID], out.Amount())
				if err != nil {
					lockedNotStakeables[assetID] = math.MaxUint64
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
				newBalance, err := safemath.Add(lockedNotStakeables[assetID], out.Amount())
				if err != nil {
					lockedNotStakeables[assetID] = math.MaxUint64
				} else {
					lockedNotStakeables[assetID] = newBalance
				}
			case out.Locktime <= currentTime:
				newBalance, err := safemath.Add(unlockeds[assetID], out.Amount())
				if err != nil {
					unlockeds[assetID] = math.MaxUint64
				} else {
					unlockeds[assetID] = newBalance
				}
			default:
				newBalance, err := safemath.Add(lockedStakeables[assetID], out.Amount())
				if err != nil {
					lockedStakeables[assetID] = math.MaxUint64
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
		newBalance, err := safemath.Add(balances[assetID], amount)
		if err != nil {
			balances[assetID] = math.MaxUint64
		} else {
			balances[assetID] = newBalance
		}
	}
	for assetID, amount := range unlockeds {
		newBalance, err := safemath.Add(balances[assetID], amount)
		if err != nil {
			balances[assetID] = math.MaxUint64
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

func newJSONBalanceMap(balanceMap map[ids.ID]uint64) map[ids.ID]avajson.Uint64 {
	jsonBalanceMap := make(map[ids.ID]avajson.Uint64, len(balanceMap))
	for assetID, amount := range balanceMap {
		jsonBalanceMap[assetID] = avajson.Uint64(amount)
	}
	return jsonBalanceMap
}

// Index is an address and an associated UTXO.
// Marks a starting or stopping point when fetching UTXOs. Used for pagination.
type Index struct {
	Address string `json:"address"` // The address as a string
	UTXO    string `json:"utxo"`    // The UTXO ID as a string
}

// GetUTXOs returns the UTXOs controlled by the given addresses
func (s *Service) GetUTXOs(_ *http.Request, args *api.GetUTXOsArgs, response *api.GetUTXOsReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getUTXOs"),
	)

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
	if limit <= 0 || maxPageSize < limit {
		limit = maxPageSize
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	if sourceChain == s.vm.ctx.ChainID {
		utxos, endAddr, endUTXOID, err = avax.GetPaginatedUTXOs(
			s.vm.state,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	} else {
		utxos, endAddr, endUTXOID, err = avax.GetAtomicUTXOs(
			s.vm.ctx.SharedMemory,
			txs.Codec,
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
		bytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("couldn't serialize UTXO %q: %w", utxo.InputID(), err)
		}
		response.UTXOs[i], err = formatting.Encode(args.Encoding, bytes)
		if err != nil {
			return fmt.Errorf("couldn't encode UTXO %s as %s: %w", utxo.InputID(), args.Encoding, err)
		}
	}

	endAddress, err := s.addrManager.FormatLocalAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	response.EndIndex.Address = endAddress
	response.EndIndex.UTXO = endUTXOID.String()
	response.NumFetched = avajson.Uint64(len(utxos))
	response.Encoding = args.Encoding
	return nil
}

// GetSubnetArgs are the arguments to GetSubnet
type GetSubnetArgs struct {
	// ID of the subnet to retrieve information about
	SubnetID ids.ID `json:"subnetID"`
}

// GetSubnetResponse is the response from calling GetSubnet
type GetSubnetResponse struct {
	// whether it is permissioned or not
	IsPermissioned bool `json:"isPermissioned"`
	// subnet auth information for a permissioned subnet
	ControlKeys []string       `json:"controlKeys"`
	Threshold   avajson.Uint32 `json:"threshold"`
	Locktime    avajson.Uint64 `json:"locktime"`
	// subnet transformation tx ID for an elastic subnet
	SubnetTransformationTxID ids.ID `json:"subnetTransformationTxID"`
	// subnet conversion information for an L1
	ConversionID   ids.ID              `json:"conversionID"`
	ManagerChainID ids.ID              `json:"managerChainID"`
	ManagerAddress types.JSONByteSlice `json:"managerAddress"`
}

func (s *Service) GetSubnet(_ *http.Request, args *GetSubnetArgs, response *GetSubnetResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getSubnet"),
		zap.Stringer("subnetID", args.SubnetID),
	)

	if args.SubnetID == constants.PrimaryNetworkID {
		return errPrimaryNetworkIsNotASubnet
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	subnetOwner, err := s.vm.state.GetSubnetOwner(args.SubnetID)
	if err != nil {
		return err
	}
	owner, ok := subnetOwner.(*secp256k1fx.OutputOwners)
	if !ok {
		return fmt.Errorf("expected *secp256k1fx.OutputOwners but got %T", subnetOwner)
	}
	controlAddrs := make([]string, len(owner.Addrs))
	for i, controlKeyID := range owner.Addrs {
		addr, err := s.addrManager.FormatLocalAddress(controlKeyID)
		if err != nil {
			return fmt.Errorf("problem formatting address: %w", err)
		}
		controlAddrs[i] = addr
	}

	response.ControlKeys = controlAddrs
	response.Threshold = avajson.Uint32(owner.Threshold)
	response.Locktime = avajson.Uint64(owner.Locktime)

	switch subnetTransformationTx, err := s.vm.state.GetSubnetTransformation(args.SubnetID); err {
	case nil:
		response.IsPermissioned = false
		response.SubnetTransformationTxID = subnetTransformationTx.ID()
	case database.ErrNotFound:
		response.IsPermissioned = true
		response.SubnetTransformationTxID = ids.Empty
	default:
		return err
	}

	switch c, err := s.vm.state.GetSubnetToL1Conversion(args.SubnetID); err {
	case nil:
		response.IsPermissioned = false
		response.ConversionID = c.ConversionID
		response.ManagerChainID = c.ChainID
		response.ManagerAddress = c.Addr
	case database.ErrNotFound:
		response.ConversionID = ids.Empty
		response.ManagerChainID = ids.Empty
		response.ManagerAddress = []byte(nil)
	default:
		return err
	}

	return nil
}

// APISubnet is a representation of a subnet used in API calls
type APISubnet struct {
	// ID of the subnet
	ID ids.ID `json:"id"`

	// Each element of [ControlKeys] the address of a public key.
	// A transaction to add a validator to this subnet requires
	// signatures from [Threshold] of these keys to be valid.
	ControlKeys []string       `json:"controlKeys"`
	Threshold   avajson.Uint32 `json:"threshold"`
}

// GetSubnetsArgs are the arguments to GetSubnets
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
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "platform"),
		zap.String("method", "getSubnets"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	getAll := len(args.IDs) == 0
	if getAll {
		subnetIDs, err := s.vm.state.GetSubnetIDs() // all subnets
		if err != nil {
			return fmt.Errorf("error getting subnets from database: %w", err)
		}

		response.Subnets = make([]APISubnet, len(subnetIDs)+1)
		for i, subnetID := range subnetIDs {
			if _, err := s.vm.state.GetSubnetTransformation(subnetID); err == nil {
				response.Subnets[i] = APISubnet{
					ID:          subnetID,
					ControlKeys: []string{},
					Threshold:   avajson.Uint32(0),
				}
				continue
			}

			subnetOwner, err := s.vm.state.GetSubnetOwner(subnetID)
			if err != nil {
				return err
			}

			owner, ok := subnetOwner.(*secp256k1fx.OutputOwners)
			if !ok {
				return fmt.Errorf("expected *secp256k1fx.OutputOwners but got %T", subnetOwner)
			}

			controlAddrs := make([]string, len(owner.Addrs))
			for i, controlKeyID := range owner.Addrs {
				addr, err := s.addrManager.FormatLocalAddress(controlKeyID)
				if err != nil {
					return fmt.Errorf("problem formatting address: %w", err)
				}
				controlAddrs[i] = addr
			}
			response.Subnets[i] = APISubnet{
				ID:          subnetID,
				ControlKeys: controlAddrs,
				Threshold:   avajson.Uint32(owner.Threshold),
			}
		}
		// Include primary network
		response.Subnets[len(subnetIDs)] = APISubnet{
			ID:          constants.PrimaryNetworkID,
			ControlKeys: []string{},
			Threshold:   avajson.Uint32(0),
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
					Threshold:   avajson.Uint32(0),
				},
			)
			continue
		}

		if _, err := s.vm.state.GetSubnetTransformation(subnetID); err == nil {
			response.Subnets = append(response.Subnets, APISubnet{
				ID:          subnetID,
				ControlKeys: []string{},
				Threshold:   avajson.Uint32(0),
			})
			continue
		}

		subnetOwner, err := s.vm.state.GetSubnetOwner(subnetID)
		if err == database.ErrNotFound {
			continue
		}
		if err != nil {
			return err
		}

		owner, ok := subnetOwner.(*secp256k1fx.OutputOwners)
		if !ok {
			return fmt.Errorf("expected *secp256k1fx.OutputOwners but got %T", subnetOwner)
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
			Threshold:   avajson.Uint32(owner.Threshold),
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
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getStakingAssetID"),
	)

	if args.SubnetID == constants.PrimaryNetworkID {
		response.AssetID = s.vm.ctx.AVAXAssetID
		return nil
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	Validators []any `json:"validators"`
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
func (s *Service) GetCurrentValidators(request *http.Request, args *GetCurrentValidatorsArgs, reply *GetCurrentValidatorsReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getCurrentValidators"),
	)

	// Create set of nodeIDs
	nodeIDs := set.Of(args.NodeIDs...)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	// Check if subnet is L1
	_, err := s.vm.state.GetSubnetToL1Conversion(args.SubnetID)
	if errors.Is(err, database.ErrNotFound) {
		// Subnet is not L1, get validators for the subnet
		reply.Validators, err = s.getPrimaryOrSubnetValidators(
			args.SubnetID,
			nodeIDs,
		)
		if err != nil {
			return fmt.Errorf("failed to get primary or subnet validators: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get subnet to L1 conversion: %w", err)
	}

	// Subnet is L1, get validators for L1
	reply.Validators, err = s.getL1Validators(
		request.Context(),
		args.SubnetID,
		nodeIDs,
	)
	if err != nil {
		return fmt.Errorf("failed to get L1 validators: %w", err)
	}
	return nil
}

func (s *Service) getL1Validators(
	ctx context.Context,
	subnetID ids.ID,
	nodeIDs set.Set[ids.NodeID],
) ([]any, error) {
	validators := []any{}
	baseStakers, l1Validators, _, err := s.vm.state.GetCurrentValidators(ctx, subnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current validators: %w", err)
	}

	fetchAll := nodeIDs.Len() == 0

	for _, staker := range baseStakers {
		if !fetchAll && !nodeIDs.Contains(staker.NodeID) {
			continue
		}

		apiStaker := toPlatformStaker(staker)
		validators = append(validators, apiStaker)
	}

	for _, l1Validator := range l1Validators {
		if !fetchAll && !nodeIDs.Contains(l1Validator.NodeID) {
			continue
		}

		apiL1Vdr, err := s.convertL1ValidatorToAPI(l1Validator)
		if err != nil {
			return nil, fmt.Errorf("converting L1 validator to API format: %w", err)
		}

		validators = append(validators, apiL1Vdr)
	}

	return validators, nil
}

func (s *Service) getPrimaryOrSubnetValidators(subnetID ids.ID, nodeIDs set.Set[ids.NodeID]) ([]any, error) {
	numNodeIDs := nodeIDs.Len()

	targetStakers := make([]*state.Staker, 0, numNodeIDs)

	// Validator's node ID as string --> Delegators to them
	vdrToDelegators := map[ids.NodeID][]platformapi.PrimaryDelegator{}

	validators := []any{}

	if numNodeIDs == 0 { // Include all nodes
		currentStakerIterator, err := s.vm.state.GetCurrentStakerIterator()
		if err != nil {
			return nil, err
		}
		// TODO: avoid iterating over delegators here.
		for currentStakerIterator.Next() {
			staker := currentStakerIterator.Value()
			if subnetID != staker.SubnetID {
				continue
			}
			targetStakers = append(targetStakers, staker)
		}
		currentStakerIterator.Release()
	} else {
		for nodeID := range nodeIDs {
			staker, err := s.vm.state.GetCurrentValidator(subnetID, nodeID)
			switch err {
			case nil:
			case database.ErrNotFound:
				// nothing to do, continue
				continue
			default:
				return nil, err
			}
			targetStakers = append(targetStakers, staker)

			// TODO: avoid iterating over delegators when numNodeIDs > 1.
			delegatorsIt, err := s.vm.state.GetCurrentDelegatorIterator(subnetID, nodeID)
			if err != nil {
				return nil, err
			}
			for delegatorsIt.Next() {
				staker := delegatorsIt.Value()
				targetStakers = append(targetStakers, staker)
			}
			delegatorsIt.Release()
		}
	}

	for _, currentStaker := range targetStakers {
		apiStaker := toPlatformStaker(currentStaker)
		potentialReward := avajson.Uint64(currentStaker.PotentialReward)

		delegateeReward, err := s.vm.state.GetDelegateeReward(currentStaker.SubnetID, currentStaker.NodeID)
		if err != nil {
			return nil, err
		}
		jsonDelegateeReward := avajson.Uint64(delegateeReward)

		switch currentStaker.Priority {
		case txs.PrimaryNetworkValidatorCurrentPriority, txs.SubnetPermissionlessValidatorCurrentPriority:
			attr, err := s.loadStakerTxAttributes(currentStaker.TxID)
			if err != nil {
				return nil, err
			}

			shares := attr.shares
			delegationFee := avajson.Float32(100 * float32(shares) / float32(reward.PercentDenominator))
			var (
				uptime    *avajson.Float32
				connected *bool
			)
			if subnetID == constants.PrimaryNetworkID {
				rawUptime, err := s.vm.uptimeManager.CalculateUptimePercentFrom(currentStaker.NodeID, currentStaker.StartTime)
				if err != nil {
					return nil, err
				}
				// Transform this to a percentage (0-100) to make it consistent
				// with observedUptime in info.peers API
				currentUptime := avajson.Float32(rawUptime * 100)
				if err != nil {
					return nil, err
				}
				isConnected := s.vm.uptimeManager.IsConnected(currentStaker.NodeID)
				connected = &isConnected
				uptime = &currentUptime
			}

			var (
				validationRewardOwner *platformapi.Owner
				delegationRewardOwner *platformapi.Owner
			)
			validationOwner, ok := attr.validationRewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				validationRewardOwner, err = s.getAPIOwner(validationOwner)
				if err != nil {
					return nil, err
				}
			}
			delegationOwner, ok := attr.delegationRewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				delegationRewardOwner, err = s.getAPIOwner(delegationOwner)
				if err != nil {
					return nil, err
				}
			}

			vdr := platformapi.PermissionlessValidator{
				Staker:                 apiStaker,
				Uptime:                 uptime,
				Connected:              connected,
				PotentialReward:        &potentialReward,
				AccruedDelegateeReward: &jsonDelegateeReward,
				ValidationRewardOwner:  validationRewardOwner,
				DelegationRewardOwner:  delegationRewardOwner,
				DelegationFee:          delegationFee,
				Signer:                 attr.proofOfPossession,
			}
			validators = append(validators, vdr)

		case txs.PrimaryNetworkDelegatorCurrentPriority, txs.SubnetPermissionlessDelegatorCurrentPriority:
			var rewardOwner *platformapi.Owner
			// If we are handling multiple nodeIDs, we don't return the
			// delegator information.
			if numNodeIDs == 1 {
				attr, err := s.loadStakerTxAttributes(currentStaker.TxID)
				if err != nil {
					return nil, err
				}
				owner, ok := attr.rewardsOwner.(*secp256k1fx.OutputOwners)
				if ok {
					rewardOwner, err = s.getAPIOwner(owner)
					if err != nil {
						return nil, err
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
			validators = append(validators, apiStaker)

		default:
			return nil, fmt.Errorf("unexpected staker priority %d", currentStaker.Priority)
		}
	}

	// handle delegators' information
	for i, vdrIntf := range validators {
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
		delegatorCount := avajson.Uint64(len(delegators))
		delegatorWeight := avajson.Uint64(0)
		for _, d := range delegators {
			delegatorWeight += d.Weight
		}

		vdr.DelegatorCount = &delegatorCount
		vdr.DelegatorWeight = &delegatorWeight

		if numNodeIDs == 1 {
			// queried a specific validator, load all of its delegators
			vdr.Delegators = &delegators
		}
		validators[i] = vdr
	}

	return validators, nil
}

type GetL1ValidatorArgs struct {
	ValidationID ids.ID `json:"validationID"`
}

type GetL1ValidatorReply struct {
	platformapi.APIL1Validator
	SubnetID ids.ID `json:"subnetID"`
	// Height is the height of the last accepted block
	Height avajson.Uint64 `json:"height"`
}

// GetL1Validator returns the L1 validator if it exists
func (s *Service) GetL1Validator(r *http.Request, args *GetL1ValidatorArgs, reply *GetL1ValidatorReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getL1Validator"),
		zap.Stringer("validationID", args.ValidationID),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	l1Validator, err := s.vm.state.GetL1Validator(args.ValidationID)
	if err != nil {
		return fmt.Errorf("fetching L1 validator %q failed: %w", args.ValidationID, err)
	}

	ctx := r.Context()
	height, err := s.vm.GetCurrentHeight(ctx)
	if err != nil {
		return fmt.Errorf("failed to get the current height: %w", err)
	}
	apiVdr, err := s.convertL1ValidatorToAPI(l1Validator)
	if err != nil {
		return fmt.Errorf("failed to convert L1 validator to API format: %w", err)
	}

	reply.APIL1Validator = apiVdr
	reply.SubnetID = l1Validator.SubnetID
	reply.Height = avajson.Uint64(height)
	return nil
}

func (s *Service) convertL1ValidatorToAPI(vdr state.L1Validator) (platformapi.APIL1Validator, error) {
	var remainingBalanceOwner message.PChainOwner
	if _, err := txs.Codec.Unmarshal(vdr.RemainingBalanceOwner, &remainingBalanceOwner); err != nil {
		return platformapi.APIL1Validator{}, fmt.Errorf("failed unmarshalling remaining balance owner: %w", err)
	}
	remainingBalanceAPIOwner, err := s.getAPIOwner(&secp256k1fx.OutputOwners{
		Threshold: remainingBalanceOwner.Threshold,
		Addrs:     remainingBalanceOwner.Addresses,
	})
	if err != nil {
		return platformapi.APIL1Validator{}, fmt.Errorf("failed formatting remaining balance owner: %w", err)
	}

	var deactivationOwner message.PChainOwner
	if _, err := txs.Codec.Unmarshal(vdr.DeactivationOwner, &deactivationOwner); err != nil {
		return platformapi.APIL1Validator{}, fmt.Errorf("failed unmarshalling deactivation owner: %w", err)
	}
	deactivationAPIOwner, err := s.getAPIOwner(&secp256k1fx.OutputOwners{
		Threshold: deactivationOwner.Threshold,
		Addrs:     deactivationOwner.Addresses,
	})
	if err != nil {
		return platformapi.APIL1Validator{}, fmt.Errorf("failed formatting deactivation owner: %w", err)
	}

	pubKey := types.JSONByteSlice(bls.PublicKeyToCompressedBytes(
		bls.PublicKeyFromValidUncompressedBytes(vdr.PublicKey),
	))
	minNonce := avajson.Uint64(vdr.MinNonce)

	apiVdr := platformapi.APIL1Validator{
		NodeID:    vdr.NodeID,
		StartTime: avajson.Uint64(vdr.StartTime),
		Weight:    avajson.Uint64(vdr.Weight),
		BaseL1Validator: platformapi.BaseL1Validator{
			ValidationID:          &vdr.ValidationID,
			PublicKey:             &pubKey,
			RemainingBalanceOwner: remainingBalanceAPIOwner,
			DeactivationOwner:     deactivationAPIOwner,
			MinNonce:              &minNonce,
		},
	}
	zero := avajson.Uint64(0)
	apiVdr.Balance = &zero
	if vdr.EndAccumulatedFee != 0 {
		accruedFees := s.vm.state.GetAccruedFees()
		balance := avajson.Uint64(vdr.EndAccumulatedFee - accruedFees)
		apiVdr.Balance = &balance
	}
	return apiVdr, nil
}

// GetCurrentSupplyArgs are the arguments for calling GetCurrentSupply
type GetCurrentSupplyArgs struct {
	SubnetID ids.ID `json:"subnetID"`
}

// GetCurrentSupplyReply are the results from calling GetCurrentSupply
type GetCurrentSupplyReply struct {
	Supply avajson.Uint64 `json:"supply"`
	Height avajson.Uint64 `json:"height"`
}

// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
func (s *Service) GetCurrentSupply(r *http.Request, args *GetCurrentSupplyArgs, reply *GetCurrentSupplyReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getCurrentSupply"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	supply, err := s.vm.state.GetCurrentSupply(args.SubnetID)
	if err != nil {
		return fmt.Errorf("fetching current supply failed: %w", err)
	}
	reply.Supply = avajson.Uint64(supply)

	ctx := r.Context()
	height, err := s.vm.GetCurrentHeight(ctx)
	if err != nil {
		return fmt.Errorf("fetching current height failed: %w", err)
	}
	reply.Height = avajson.Uint64(height)

	return nil
}

// SampleValidatorsArgs are the arguments for calling SampleValidators
type SampleValidatorsArgs struct {
	// Number of validators in the sample
	Size avajson.Uint16 `json:"size"`

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
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "sampleValidators"),
		zap.Uint16("size", uint16(args.Size)),
	)

	sample, err := s.vm.Validators.Sample(args.SubnetID, int(args.Size))
	if err != nil {
		return fmt.Errorf("sampling %s errored with %w", args.SubnetID, err)
	}

	if sample == nil {
		reply.Validators = []ids.NodeID{}
	} else {
		utils.Sort(sample)
		reply.Validators = sample
	}
	return nil
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
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getBlockchainStatus"),
	)

	if args.BlockchainID == "" {
		return errMissingBlockchainID
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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

	preferredBlkID := s.vm.manager.Preferred()
	preferred, err := s.chainExists(ctx, preferredBlkID, blockchainID)
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

	_, isValidator := s.vm.Validators.GetValidator(chain.SubnetID, s.vm.ctx.NodeID)
	return isValidator
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
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "validatedBy"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "validates"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "platform"),
		zap.String("method", "getBlockchains"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	subnetIDs, err := s.vm.state.GetSubnetIDs()
	if err != nil {
		return fmt.Errorf("couldn't retrieve subnets: %w", err)
	}

	response.Blockchains = []APIBlockchain{}
	for _, subnetID := range subnetIDs {
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

func (s *Service) IssueTx(_ *http.Request, args *api.FormattedTx, response *api.JSONTxID) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "issueTx"),
	)

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}
	tx, err := txs.Parse(txs.Codec, txBytes)
	if err != nil {
		return fmt.Errorf("couldn't parse tx: %w", err)
	}

	if err := s.vm.issueTxFromRPC(tx); err != nil {
		return fmt.Errorf("couldn't issue tx: %w", err)
	}

	response.TxID = tx.ID()
	return nil
}

func (s *Service) GetTx(_ *http.Request, args *api.GetTxArgs, response *api.GetTxReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getTx"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	tx, _, err := s.vm.state.GetTx(args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get tx: %w", err)
	}
	response.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		tx.Unsigned.InitCtx(s.vm.ctx)
		result = tx
	} else {
		result, err = formatting.Encode(args.Encoding, tx.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode tx as %s: %w", args.Encoding, err)
		}
	}

	response.Tx, err = json.Marshal(result)
	return err
}

type GetTxStatusArgs struct {
	TxID ids.ID `json:"txID"`
}

type GetTxStatusResponse struct {
	Status status.Status `json:"status"`
	// Reason this tx was dropped.
	// Only non-empty if Status is dropped
	Reason string `json:"reason,omitempty"`
}

// GetTxStatus gets a tx's status
func (s *Service) GetTxStatus(_ *http.Request, args *GetTxStatusArgs, response *GetTxStatusResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getTxStatus"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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
	preferredID := s.vm.manager.Preferred()
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

	if _, ok := s.vm.mempool.Get(args.TxID); ok {
		// Found the tx in the mempool. Report tx is processing.
		response.Status = status.Processing
		return nil
	}

	// Note: we check if tx is dropped only after having looked for it
	// in the database and the mempool, because dropped txs may be re-issued.
	reason := s.vm.mempool.GetDropReason(args.TxID)
	if reason == nil {
		// The tx isn't being tracked by the node.
		response.Status = status.Unknown
		return nil
	}

	// The tx was recently dropped because it was invalid.
	response.Status = status.Dropped
	response.Reason = reason.Error()
	return nil
}

type GetStakeArgs struct {
	api.JSONAddresses
	ValidatorsOnly bool                `json:"validatorsOnly"`
	Encoding       formatting.Encoding `json:"encoding"`
}

// GetStakeReply is the response from calling GetStake.
type GetStakeReply struct {
	Staked  avajson.Uint64            `json:"staked"`
	Stakeds map[ids.ID]avajson.Uint64 `json:"stakeds"`
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
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "platform"),
		zap.String("method", "getStake"),
	)

	if len(args.Addresses) > maxGetStakeAddrs {
		return fmt.Errorf("%d addresses provided but this method can take at most %d", len(args.Addresses), maxGetStakeAddrs)
	}

	addrs, err := avax.ParseServiceAddresses(s.addrManager, args.Addresses)
	if err != nil {
		return err
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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

		if args.ValidatorsOnly && !staker.Priority.IsValidator() {
			continue
		}

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

		if args.ValidatorsOnly && !staker.Priority.IsValidator() {
			continue
		}

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
		bytes, err := txs.Codec.Marshal(txs.CodecVersion, output)
		if err != nil {
			return fmt.Errorf("couldn't serialize output %s: %w", output.ID, err)
		}
		response.Outputs[i], err = formatting.Encode(args.Encoding, bytes)
		if err != nil {
			return fmt.Errorf("couldn't encode output %s as %s: %w", output.ID, args.Encoding, err)
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
	MinValidatorStake avajson.Uint64 `json:"minValidatorStake"`
	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake avajson.Uint64 `json:"minDelegatorStake"`
}

// GetMinStake returns the minimum staking amount in nAVAX.
func (s *Service) GetMinStake(_ *http.Request, args *GetMinStakeArgs, reply *GetMinStakeReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getMinStake"),
	)

	if args.SubnetID == constants.PrimaryNetworkID {
		reply.MinValidatorStake = avajson.Uint64(s.vm.MinValidatorStake)
		reply.MinDelegatorStake = avajson.Uint64(s.vm.MinDelegatorStake)
		return nil
	}

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

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

	reply.MinValidatorStake = avajson.Uint64(transformSubnet.MinValidatorStake)
	reply.MinDelegatorStake = avajson.Uint64(transformSubnet.MinDelegatorStake)

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
	// Deprecated: Use Weight instead.
	Stake avajson.Uint64 `json:"stake"`

	Weight avajson.Uint64 `json:"weight"`
}

// GetTotalStake returns the total amount staked on the Primary Network
func (s *Service) GetTotalStake(_ *http.Request, args *GetTotalStakeArgs, reply *GetTotalStakeReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getTotalStake"),
	)

	totalWeight, err := s.vm.Validators.TotalWeight(args.SubnetID)
	if err != nil {
		return fmt.Errorf("couldn't get total weight: %w", err)
	}
	weight := avajson.Uint64(totalWeight)
	reply.Weight = weight
	reply.Stake = weight
	return nil
}

// GetRewardUTXOsReply defines the GetRewardUTXOs replies returned from the API
type GetRewardUTXOsReply struct {
	// Number of UTXOs returned
	NumFetched avajson.Uint64 `json:"numFetched"`
	// The UTXOs
	UTXOs []string `json:"utxos"`
	// Encoding specifies the encoding format the UTXOs are returned in
	Encoding formatting.Encoding `json:"encoding"`
}

// GetRewardUTXOs returns the UTXOs that were rewarded after the provided
// transaction's staking period ended.
func (s *Service) GetRewardUTXOs(_ *http.Request, args *api.GetTxArgs, reply *GetRewardUTXOsReply) error {
	s.vm.ctx.Log.Debug("deprecated API called",
		zap.String("service", "platform"),
		zap.String("method", "getRewardUTXOs"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	utxos, err := s.vm.state.GetRewardUTXOs(args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get reward UTXOs: %w", err)
	}

	reply.NumFetched = avajson.Uint64(len(utxos))
	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		utxoBytes, err := txs.GenesisCodec.Marshal(txs.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("couldn't encode UTXO to bytes: %w", err)
		}

		utxoStr, err := formatting.Encode(args.Encoding, utxoBytes)
		if err != nil {
			return fmt.Errorf("couldn't encode utxo as %s: %w", args.Encoding, err)
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
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getTimestamp"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	reply.Timestamp = s.vm.state.GetTimestamp()
	return nil
}

// GetValidatorsAtArgs is the response from GetValidatorsAt
type GetValidatorsAtArgs struct {
	Height   platformapi.Height `json:"height"`
	SubnetID ids.ID             `json:"subnetID"`
}

type jsonGetValidatorOutput struct {
	PublicKey *string        `json:"publicKey"`
	Weight    avajson.Uint64 `json:"weight"`
}

func (v *GetValidatorsAtReply) MarshalJSON() ([]byte, error) {
	m := make(map[ids.NodeID]*jsonGetValidatorOutput, len(v.Validators))
	for _, vdr := range v.Validators {
		vdrJSON := &jsonGetValidatorOutput{
			Weight: avajson.Uint64(vdr.Weight),
		}

		if vdr.PublicKey != nil {
			pk, err := formatting.Encode(formatting.HexNC, bls.PublicKeyToCompressedBytes(vdr.PublicKey))
			if err != nil {
				return nil, err
			}
			vdrJSON.PublicKey = &pk
		}

		m[vdr.NodeID] = vdrJSON
	}
	return json.Marshal(m)
}

func (v *GetValidatorsAtReply) UnmarshalJSON(b []byte) error {
	var m map[ids.NodeID]*jsonGetValidatorOutput
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}

	if m == nil {
		v.Validators = nil
		return nil
	}

	v.Validators = make(map[ids.NodeID]*validators.GetValidatorOutput, len(m))
	for nodeID, vdrJSON := range m {
		vdr := &validators.GetValidatorOutput{
			NodeID: nodeID,
			Weight: uint64(vdrJSON.Weight),
		}

		if vdrJSON.PublicKey != nil {
			pkBytes, err := formatting.Decode(formatting.HexNC, *vdrJSON.PublicKey)
			if err != nil {
				return err
			}
			vdr.PublicKey, err = bls.PublicKeyFromCompressedBytes(pkBytes)
			if err != nil {
				return err
			}
		}

		v.Validators[nodeID] = vdr
	}
	return nil
}

// GetValidatorsAtReply is the response from GetValidatorsAt
type GetValidatorsAtReply struct {
	Validators map[ids.NodeID]*validators.GetValidatorOutput
}

// GetValidatorsAt returns the weights of the validator set of a provided subnet
// at the specified height.
func (s *Service) GetValidatorsAt(r *http.Request, args *GetValidatorsAtArgs, reply *GetValidatorsAtReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getValidatorsAt"),
		zap.Uint64("height", uint64(args.Height)),
		zap.Bool("isProposed", args.Height.IsProposed()),
		zap.Stringer("subnetID", args.SubnetID),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	ctx := r.Context()
	var err error
	height := uint64(args.Height)
	if args.Height.IsProposed() {
		height, err = s.vm.GetMinimumHeight(ctx)
		if err != nil {
			return fmt.Errorf("failed to get proposed height: %w", err)
		}
	}

	reply.Validators, err = s.vm.GetValidatorSet(ctx, height, args.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get validator set: %w", err)
	}
	return nil
}

func (s *Service) GetBlock(_ *http.Request, args *api.GetBlockArgs, response *api.GetBlockResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getBlock"),
		zap.Stringer("blkID", args.BlockID),
		zap.Stringer("encoding", args.Encoding),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	block, err := s.vm.manager.GetStatelessBlock(args.BlockID)
	if err != nil {
		return fmt.Errorf("couldn't get block with id %s: %w", args.BlockID, err)
	}
	response.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		block.InitCtx(s.vm.ctx)
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode block %s as %s: %w", args.BlockID, args.Encoding, err)
		}
	}

	response.Block, err = json.Marshal(result)
	return err
}

// GetBlockByHeight returns the block at the given height.
func (s *Service) GetBlockByHeight(_ *http.Request, args *api.GetBlockByHeightArgs, response *api.GetBlockResponse) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getBlockByHeight"),
		zap.Uint64("height", uint64(args.Height)),
		zap.Stringer("encoding", args.Encoding),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	blockID, err := s.vm.state.GetBlockIDAtHeight(uint64(args.Height))
	if err != nil {
		return fmt.Errorf("couldn't get block at height %d: %w", args.Height, err)
	}

	block, err := s.vm.manager.GetStatelessBlock(blockID)
	if err != nil {
		s.vm.ctx.Log.Error("couldn't get accepted block",
			zap.Stringer("blkID", blockID),
			zap.Error(err),
		)
		return fmt.Errorf("couldn't get block with id %s: %w", blockID, err)
	}
	response.Encoding = args.Encoding

	var result any
	if args.Encoding == formatting.JSON {
		block.InitCtx(s.vm.ctx)
		result = block
	} else {
		result, err = formatting.Encode(args.Encoding, block.Bytes())
		if err != nil {
			return fmt.Errorf("couldn't encode block %s as %s: %w", blockID, args.Encoding, err)
		}
	}

	response.Block, err = json.Marshal(result)
	return err
}

// GetFeeConfig returns the dynamic fee config of the chain.
func (s *Service) GetFeeConfig(_ *http.Request, _ *struct{}, reply *gas.Config) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getFeeConfig"),
	)

	*reply = s.vm.DynamicFeeConfig
	return nil
}

type GetFeeStateReply struct {
	gas.State
	Price gas.Price `json:"price"`
	Time  time.Time `json:"timestamp"`
}

// GetFeeState returns the current fee state of the chain.
func (s *Service) GetFeeState(_ *http.Request, _ *struct{}, reply *GetFeeStateReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getFeeState"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	reply.State = s.vm.state.GetFeeState()
	reply.Price = gas.CalculatePrice(
		s.vm.DynamicFeeConfig.MinPrice,
		reply.State.Excess,
		s.vm.DynamicFeeConfig.ExcessConversionConstant,
	)
	reply.Time = s.vm.state.GetTimestamp()
	return nil
}

// GetValidatorFeeConfig returns the validator fee config of the chain.
func (s *Service) GetValidatorFeeConfig(_ *http.Request, _ *struct{}, reply *fee.Config) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getValidatorFeeConfig"),
	)

	*reply = s.vm.ValidatorFeeConfig
	return nil
}

type GetValidatorFeeStateReply struct {
	Excess gas.Gas   `json:"excess"`
	Price  gas.Price `json:"price"`
	Time   time.Time `json:"timestamp"`
}

// GetValidatorFeeState returns the current validator fee state of the chain.
func (s *Service) GetValidatorFeeState(_ *http.Request, _ *struct{}, reply *GetValidatorFeeStateReply) error {
	s.vm.ctx.Log.Debug("API called",
		zap.String("service", "platform"),
		zap.String("method", "getValidatorFeeState"),
	)

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	reply.Excess = s.vm.state.GetL1ValidatorExcess()
	reply.Price = gas.CalculatePrice(
		s.vm.ValidatorFeeConfig.MinPrice,
		reply.Excess,
		s.vm.ValidatorFeeConfig.ExcessConversionConstant,
	)
	reply.Time = s.vm.state.GetTimestamp()
	return nil
}

func (s *Service) getAPIOwner(owner *secp256k1fx.OutputOwners) (*platformapi.Owner, error) {
	apiOwner := &platformapi.Owner{
		Locktime:  avajson.Uint64(owner.Locktime),
		Threshold: avajson.Uint32(owner.Threshold),
		Addresses: make([]string, 0, len(owner.Addrs)),
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
		contains := slices.ContainsFunc(secpOut.Addrs, addrs.Contains)
		if !contains {
			// This output isn't owned by one of the given addresses. Ignore.
			continue
		}

		assetID := output.AssetID()
		newAmount, err := safemath.Add(totalAmountStaked[assetID], secpOut.Amt)
		if err != nil {
			newAmount = math.MaxUint64
		}
		totalAmountStaked[assetID] = newAmount

		stakedOuts = append(
			stakedOuts,
			*output,
		)
	}
	return stakedOuts
}

func toPlatformStaker(staker *state.Staker) platformapi.Staker {
	return platformapi.Staker{
		TxID:      staker.TxID,
		StartTime: avajson.Uint64(staker.StartTime.Unix()),
		EndTime:   avajson.Uint64(staker.EndTime.Unix()),
		Weight:    avajson.Uint64(staker.Weight),
		NodeID:    staker.NodeID,
	}
}
