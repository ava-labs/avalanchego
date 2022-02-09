// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/keystore"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of addresses that can be passed in as argument to GetStake
	maxGetStakeAddrs = 256

	// Minimum amount of delay to allow a transaction to be issued through the
	// API
	minAddStakerDelay = 2 * syncBound

	// Max number of items allowed in a page
	maxPageSize = 1024
)

var (
	errMissingDecisionBlock       = errors.New("should have a decision block within the past two blocks")
	errNoFunds                    = errors.New("no spendable funds were found")
	errNoSubnetID                 = errors.New("argument 'subnetID' not provided")
	errNoRewardAddress            = errors.New("argument 'rewardAddress' not provided")
	errInvalidDelegationRate      = errors.New("argument 'delegationFeeRate' must be between 0 and 100, inclusive")
	errNoAddresses                = errors.New("no addresses provided")
	errNoKeys                     = errors.New("user has no keys or funds")
	errNoPrimaryValidators        = errors.New("no default subnet validators")
	errCorruptedReason            = errors.New("tx validity corrupted")
	errStartTimeTooSoon           = fmt.Errorf("start time must be at least %s in the future", minAddStakerDelay)
	errStartTimeTooLate           = errors.New("start time is too far in the future")
	errTotalOverflow              = errors.New("overflow while calculating total balance")
	errUnlockedOverflow           = errors.New("overflow while calculating unlocked balance")
	errLockedOverflow             = errors.New("overflow while calculating locked balance")
	errNotStakeableOverflow       = errors.New("overflow while calculating locked not stakeable balance")
	errLockedNotStakeableOverflow = errors.New("overflow while calculating locked not stakeable balance")
	errUnlockedStakeableOverflow  = errors.New("overflow while calculating unlocked stakeable balance")
	errNamedSubnetCantBePrimary   = errors.New("subnet validator attempts to validate primary network")
	errNoAmount                   = errors.New("argument 'amount' must be > 0")
	errMissingName                = errors.New("argument 'name' not given")
	errMissingVMID                = errors.New("argument 'vmID' not given")
	errMissingBlockchainID        = errors.New("argument 'blockchainID' not given")
)

// Service defines the API calls that can be made to the platform chain
type Service struct{ vm *VM }

type GetHeightResponse struct {
	Height json.Uint64 `json:"height"`
}

// GetHeight returns the height of the last accepted block
func (service *Service) GetHeight(r *http.Request, args *struct{}, response *GetHeightResponse) error {
	lastAcceptedID, err := service.vm.LastAccepted()
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block ID: %w", err)
	}
	lastAccepted, err := service.vm.getBlock(lastAcceptedID)
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
	PrivateKey string `json:"privateKey"`
}

// ExportKey returns a private key from the provided user
func (service *Service) ExportKey(r *http.Request, args *ExportKeyArgs, reply *ExportKeyReply) error {
	service.vm.ctx.Log.Debug("Platform: ExportKey called")

	address, err := service.vm.ParseLocalAddress(args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse %s to address: %w", args.Address, err)
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}

	sk, err := user.GetKey(address)
	if err != nil {
		// Drop any potential error closing the user to report the original
		// error
		_ = user.Close()
		return fmt.Errorf("problem retrieving private key: %w", err)
	}

	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of a SECP256K1 private key
	privKeyStr, _ := formatting.EncodeWithChecksum(formatting.CB58, sk.Bytes())
	reply.PrivateKey = constants.SecretKeyPrefix + privKeyStr
	return user.Close()
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	api.UserPass
	PrivateKey string `json:"privateKey"`
}

// ImportKey adds a private key to the provided user
func (service *Service) ImportKey(r *http.Request, args *ImportKeyArgs, reply *api.JSONAddress) error {
	service.vm.ctx.Log.Debug("Platform: ImportKey called for user '%s'", args.Username)

	if !strings.HasPrefix(args.PrivateKey, constants.SecretKeyPrefix) {
		return fmt.Errorf("private key missing %s prefix", constants.SecretKeyPrefix)
	}

	trimmedPrivateKey := strings.TrimPrefix(args.PrivateKey, constants.SecretKeyPrefix)
	privKeyBytes, err := formatting.Decode(formatting.CB58, trimmedPrivateKey)
	if err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}

	skIntf, err := service.vm.factory.ToPrivateKey(privKeyBytes)
	if err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	reply.Address, err = service.vm.FormatLocalAddress(sk.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	if err := user.PutKeys(sk); err != nil {
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
	// TODO: remove Address
	Address   *string  `json:"address,omitempty"`
	Addresses []string `json:"addresses"`
}

type GetBalanceResponse struct {
	// Balance, in nAVAX, of the address
	Balance            json.Uint64    `json:"balance"`
	Unlocked           json.Uint64    `json:"unlocked"`
	LockedStakeable    json.Uint64    `json:"lockedStakeable"`
	LockedNotStakeable json.Uint64    `json:"lockedNotStakeable"`
	UTXOIDs            []*avax.UTXOID `json:"utxoIDs"`
}

// GetBalance gets the balance of an address
func (service *Service) GetBalance(_ *http.Request, args *GetBalanceRequest, response *GetBalanceResponse) error {
	if args.Address != nil {
		args.Addresses = append(args.Addresses, *args.Address)
	}

	service.vm.ctx.Log.Debug("Platform: GetBalance called for addresses %v", args.Addresses)

	addrs := ids.ShortSet{}
	for _, addrStr := range args.Addresses {
		// Parse to address
		addr, err := service.vm.ParseLocalAddress(addrStr)
		if err != nil {
			return fmt.Errorf("couldn't parse argument %q to address: %w", addrStr, err)
		}
		addrs.Add(addr)
	}

	utxos, err := avax.GetAllUTXOs(service.vm.internalState, addrs)
	if err != nil {
		return fmt.Errorf("couldn't get UTXO set of %v: %w", args.Addresses, err)
	}

	currentTime := service.vm.clock.Unix()

	unlocked := uint64(0)
	lockedStakeable := uint64(0)
	lockedNotStakeable := uint64(0)

utxoFor:
	for _, utxo := range utxos {
		switch out := utxo.Out.(type) {
		case *secp256k1fx.TransferOutput:
			if out.Locktime <= currentTime {
				newBalance, err := math.Add64(unlocked, out.Amount())
				if err != nil {
					return errUnlockedOverflow
				}
				unlocked = newBalance
			} else {
				newBalance, err := math.Add64(lockedNotStakeable, out.Amount())
				if err != nil {
					return errNotStakeableOverflow
				}
				lockedNotStakeable = newBalance
			}
		case *StakeableLockOut:
			innerOut, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
			switch {
			case !ok:
				service.vm.ctx.Log.Warn("Unexpected Output type in UTXO: %T",
					out.TransferableOut)
				continue utxoFor
			case innerOut.Locktime > currentTime:
				newBalance, err := math.Add64(lockedNotStakeable, out.Amount())
				if err != nil {
					return errLockedNotStakeableOverflow
				}
				lockedNotStakeable = newBalance
			case out.Locktime <= currentTime:
				newBalance, err := math.Add64(unlocked, out.Amount())
				if err != nil {
					return errUnlockedOverflow
				}
				unlocked = newBalance
			default:
				newBalance, err := math.Add64(lockedStakeable, out.Amount())
				if err != nil {
					return errUnlockedStakeableOverflow
				}
				lockedStakeable = newBalance
			}
		default:
			continue utxoFor
		}

		response.UTXOIDs = append(response.UTXOIDs, &utxo.UTXOID)
	}

	lockedBalance, err := math.Add64(lockedStakeable, lockedNotStakeable)
	if err != nil {
		return errLockedOverflow
	}
	balance, err := math.Add64(unlocked, lockedBalance)
	if err != nil {
		return errTotalOverflow
	}

	response.Balance = json.Uint64(balance)
	response.Unlocked = json.Uint64(unlocked)
	response.LockedStakeable = json.Uint64(lockedStakeable)
	response.LockedNotStakeable = json.Uint64(lockedNotStakeable)
	return nil
}

// CreateAddress creates an address controlled by [args.Username]
// Returns the newly created address
func (service *Service) CreateAddress(_ *http.Request, args *api.UserPass, response *api.JSONAddress) error {
	service.vm.ctx.Log.Debug("Platform: CreateAddress called")

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
	if err != nil {
		return err
	}
	defer user.Close()

	key, err := keystore.NewKey(user)
	if err != nil {
		return err
	}

	response.Address, err = service.vm.FormatLocalAddress(key.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}
	return user.Close()
}

// ListAddresses returns the addresses controlled by [args.Username]
func (service *Service) ListAddresses(_ *http.Request, args *api.UserPass, response *api.JSONAddresses) error {
	service.vm.ctx.Log.Debug("Platform: ListAddresses called")

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		response.Addresses[i], err = service.vm.FormatLocalAddress(addr)
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

// GetUTXOsArgs are arguments for passing into GetUTXOs.
// Gets the UTXOs that reference at least one address in [Addresses].
// If specified, [SourceChain] is the chain where the atomic UTXOs were exported from. If empty,
// or the Platform Chain ID is specified, then GetUTXOs fetches the native UTXOs.
// Returns at most [limit] addresses.
// If [limit] == 0 or > [maxUTXOsToFetch], fetches up to [maxUTXOsToFetch].
// [StartIndex] defines where to start fetching UTXOs (for pagination.)
// UTXOs fetched are from addresses equal to or greater than [StartIndex.Address]
// For address [StartIndex.Address], only UTXOs with IDs greater than [StartIndex.UTXO] will be returned.
// If [StartIndex] is omitted, gets all UTXOs.
// If GetUTXOs is called multiple times, with our without [StartIndex], it is not guaranteed
// that returned UTXOs are unique. That is, the same UTXO may appear in the response of multiple calls.
// [Encoding] defines the encoding format to use for the returned UTXOs. Can be either "cb58" or "hex"
type GetUTXOsArgs struct {
	Addresses   []string            `json:"addresses"`
	SourceChain string              `json:"sourceChain"`
	Limit       json.Uint32         `json:"limit"`
	StartIndex  Index               `json:"startIndex"`
	Encoding    formatting.Encoding `json:"encoding"`
}

// GetUTXOsResponse defines the GetUTXOs replies returned from the API
type GetUTXOsResponse struct {
	// Number of UTXOs returned
	NumFetched json.Uint64 `json:"numFetched"`
	// The UTXOs
	UTXOs []string `json:"utxos"`
	// The last UTXO that was returned, and the address it corresponds to.
	// Used for pagination. To get the rest of the UTXOs, call GetUTXOs
	// again and set [StartIndex] to this value.
	EndIndex Index `json:"endIndex"`
	// Encoding specifies the format the UTXOs are returned in
	Encoding formatting.Encoding `json:"encoding"`
}

// GetUTXOs returns the UTXOs controlled by the given addresses
func (service *Service) GetUTXOs(_ *http.Request, args *GetUTXOsArgs, response *GetUTXOsResponse) error {
	service.vm.ctx.Log.Debug("Platform: GetUTXOs called")

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	var sourceChain ids.ID
	if args.SourceChain == "" {
		sourceChain = service.vm.ctx.ChainID
	} else {
		chainID, err := service.vm.ctx.BCLookup.Lookup(args.SourceChain)
		if err != nil {
			return fmt.Errorf("problem parsing source chainID %q: %w", args.SourceChain, err)
		}
		sourceChain = chainID
	}

	addrSet := ids.ShortSet{}
	for _, addrStr := range args.Addresses {
		addr, err := service.vm.ParseLocalAddress(addrStr)
		if err != nil {
			return fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
		}
		addrSet.Add(addr)
	}

	startAddr := ids.ShortEmpty
	startUTXO := ids.Empty
	if args.StartIndex.Address != "" || args.StartIndex.UTXO != "" {
		var err error
		startAddr, err = service.vm.ParseLocalAddress(args.StartIndex.Address)
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
		err       error
	)
	limit := int(args.Limit)
	if limit <= 0 || maxPageSize < limit {
		limit = maxPageSize
	}
	if sourceChain == service.vm.ctx.ChainID {
		utxos, endAddr, endUTXOID, err = avax.GetPaginatedUTXOs(
			service.vm.internalState,
			addrSet,
			startAddr,
			startUTXO,
			limit,
		)
	} else {
		utxos, endAddr, endUTXOID, err = service.vm.GetAtomicUTXOs(
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
		bytes, err := Codec.Marshal(CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("couldn't serialize UTXO %q: %w", utxo.InputID(), err)
		}
		response.UTXOs[i], err = formatting.EncodeWithChecksum(args.Encoding, bytes)
		if err != nil {
			return fmt.Errorf("couldn't encode UTXO %s as string: %w", utxo.InputID(), err)
		}
	}

	endAddress, err := service.vm.FormatLocalAddress(endAddr)
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
func (service *Service) GetSubnets(_ *http.Request, args *GetSubnetsArgs, response *GetSubnetsResponse) error {
	service.vm.ctx.Log.Debug("Platform: GetSubnets called")

	getAll := len(args.IDs) == 0
	if getAll {
		subnets, err := service.vm.internalState.GetSubnets() // all subnets
		if err != nil {
			return fmt.Errorf("error getting subnets from database: %w", err)
		}

		response.Subnets = make([]APISubnet, len(subnets)+1)
		for i, subnet := range subnets {
			unsignedTx := subnet.UnsignedTx.(*UnsignedCreateSubnetTx)
			owner := unsignedTx.Owner.(*secp256k1fx.OutputOwners)
			controlAddrs := []string{}
			for _, controlKeyID := range owner.Addrs {
				addr, err := service.vm.FormatLocalAddress(controlKeyID)
				if err != nil {
					return fmt.Errorf("problem formatting address: %w", err)
				}
				controlAddrs = append(controlAddrs, addr)
			}
			response.Subnets[i] = APISubnet{
				ID:          subnet.ID(),
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

	subnetSet := ids.NewSet(len(args.IDs))
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

		subnetTx, _, err := service.vm.internalState.GetTx(subnetID)
		if err == database.ErrNotFound {
			continue
		}
		if err != nil {
			return err
		}

		subnet, ok := subnetTx.UnsignedTx.(*UnsignedCreateSubnetTx)
		if !ok {
			return errWrongTxType
		}
		owner, ok := subnet.Owner.(*secp256k1fx.OutputOwners)
		if !ok {
			return errUnknownOwners
		}

		controlAddrs := make([]string, len(owner.Addrs))
		for i, controlKeyID := range owner.Addrs {
			addr, err := service.vm.FormatLocalAddress(controlKeyID)
			if err != nil {
				return fmt.Errorf("problem formatting address: %w", err)
			}
			controlAddrs[i] = addr
		}

		response.Subnets = append(response.Subnets,
			APISubnet{
				ID:          subnet.ID(),
				ControlKeys: controlAddrs,
				Threshold:   json.Uint32(owner.Threshold),
			},
		)
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
func (service *Service) GetStakingAssetID(_ *http.Request, args *GetStakingAssetIDArgs, response *GetStakingAssetIDResponse) error {
	service.vm.ctx.Log.Debug("Platform: GetStakingAssetID called")

	if args.SubnetID != constants.PrimaryNetworkID {
		return fmt.Errorf("Subnet %s doesn't have a valid staking token",
			args.SubnetID)
	}

	response.AssetID = service.vm.ctx.AVAXAssetID
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
	NodeIDs []string `json:"nodeIDs"`
}

// GetCurrentValidatorsReply are the results from calling GetCurrentValidators.
// Each validator contains a list of delegators to itself.
type GetCurrentValidatorsReply struct {
	Validators []interface{} `json:"validators"`
}

// GetCurrentValidators returns current validators and delegators
func (service *Service) GetCurrentValidators(_ *http.Request, args *GetCurrentValidatorsArgs, reply *GetCurrentValidatorsReply) error {
	service.vm.ctx.Log.Debug("Platform: GetCurrentValidators called")

	reply.Validators = []interface{}{}

	// Validator's node ID as string --> Delegators to them
	vdrToDelegators := map[string][]APIPrimaryDelegator{}

	// Create set of nodeIDs
	nodeIDs := ids.ShortSet{}
	for _, nodeID := range args.NodeIDs {
		nID, err := ids.ShortFromPrefixedString(nodeID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeIDs.Add(nID)
	}
	includeAllNodes := nodeIDs.Len() == 0

	currentValidators := service.vm.internalState.CurrentStakerChainState()

	for _, tx := range currentValidators.Stakers() { // Iterates in order of increasing stop time
		_, rewardAmount, err := currentValidators.GetStaker(tx.ID())
		if err != nil {
			return err
		}
		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
			if args.SubnetID != constants.PrimaryNetworkID {
				continue
			}
			if !includeAllNodes && !nodeIDs.Contains(staker.Validator.ID()) {
				continue
			}

			weight := json.Uint64(staker.Validator.Weight())

			var rewardOwner *APIOwner
			owner, ok := staker.RewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				rewardOwner = &APIOwner{
					Locktime:  json.Uint64(owner.Locktime),
					Threshold: json.Uint32(owner.Threshold),
				}
				for _, addr := range owner.Addrs {
					addrStr, err := service.vm.FormatLocalAddress(addr)
					if err != nil {
						return err
					}
					rewardOwner.Addresses = append(rewardOwner.Addresses, addrStr)
				}
			}

			potentialReward := json.Uint64(rewardAmount)
			delegator := APIPrimaryDelegator{
				APIStaker: APIStaker{
					TxID:        tx.ID(),
					StartTime:   json.Uint64(staker.StartTime().Unix()),
					EndTime:     json.Uint64(staker.EndTime().Unix()),
					StakeAmount: &weight,
					NodeID:      staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				},
				RewardOwner:     rewardOwner,
				PotentialReward: &potentialReward,
			}
			vdrToDelegators[delegator.NodeID] = append(vdrToDelegators[delegator.NodeID], delegator)
		case *UnsignedAddValidatorTx:
			if args.SubnetID != constants.PrimaryNetworkID {
				continue
			}
			if !includeAllNodes && !nodeIDs.Contains(staker.Validator.ID()) {
				continue
			}

			nodeID := staker.Validator.ID()
			startTime := staker.StartTime()
			weight := json.Uint64(staker.Validator.Weight())
			potentialReward := json.Uint64(rewardAmount)
			delegationFee := json.Float32(100 * float32(staker.Shares) / float32(reward.PercentDenominator))
			rawUptime, err := service.vm.uptimeManager.CalculateUptimePercentFrom(nodeID, startTime)
			if err != nil {
				return err
			}
			uptime := json.Float32(rawUptime)

			connected := service.vm.uptimeManager.IsConnected(nodeID)

			var rewardOwner *APIOwner
			owner, ok := staker.RewardsOwner.(*secp256k1fx.OutputOwners)
			if ok {
				rewardOwner = &APIOwner{
					Locktime:  json.Uint64(owner.Locktime),
					Threshold: json.Uint32(owner.Threshold),
				}
				for _, addr := range owner.Addrs {
					addrStr, err := service.vm.FormatLocalAddress(addr)
					if err != nil {
						return err
					}
					rewardOwner.Addresses = append(rewardOwner.Addresses, addrStr)
				}
			}

			reply.Validators = append(reply.Validators, APIPrimaryValidator{
				APIStaker: APIStaker{
					TxID:        tx.ID(),
					NodeID:      nodeID.PrefixedString(constants.NodeIDPrefix),
					StartTime:   json.Uint64(startTime.Unix()),
					EndTime:     json.Uint64(staker.EndTime().Unix()),
					StakeAmount: &weight,
				},
				Uptime:          &uptime,
				Connected:       &connected,
				PotentialReward: &potentialReward,
				RewardOwner:     rewardOwner,
				DelegationFee:   delegationFee,
			})
		case *UnsignedAddSubnetValidatorTx:
			if args.SubnetID != staker.Validator.Subnet {
				continue
			}
			if !includeAllNodes && !nodeIDs.Contains(staker.Validator.ID()) {
				continue
			}

			weight := json.Uint64(staker.Validator.Weight())
			reply.Validators = append(reply.Validators, APIStaker{
				TxID:      tx.ID(),
				NodeID:    staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime: json.Uint64(staker.StartTime().Unix()),
				EndTime:   json.Uint64(staker.EndTime().Unix()),
				Weight:    &weight,
			})
		default:
			return fmt.Errorf("expected validator but got %T", tx.UnsignedTx)
		}
	}

	for i, vdrIntf := range reply.Validators {
		vdr, ok := vdrIntf.(APIPrimaryValidator)
		if !ok {
			continue
		}
		if delegators, ok := vdrToDelegators[vdr.NodeID]; ok {
			vdr.Delegators = delegators
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
	NodeIDs []string `json:"nodeIDs"`
}

// GetPendingValidatorsReply are the results from calling GetPendingValidators.
// Unlike GetCurrentValidatorsReply, each validator has a null delegator list.
type GetPendingValidatorsReply struct {
	Validators []interface{} `json:"validators"`
	Delegators []interface{} `json:"delegators"`
}

// GetPendingValidators returns the list of pending validators
func (service *Service) GetPendingValidators(_ *http.Request, args *GetPendingValidatorsArgs, reply *GetPendingValidatorsReply) error {
	service.vm.ctx.Log.Debug("Platform: GetPendingValidators called")

	reply.Validators = []interface{}{}
	reply.Delegators = []interface{}{}

	// Create set of nodeIDs
	nodeIDs := ids.ShortSet{}
	for _, nodeID := range args.NodeIDs {
		nID, err := ids.ShortFromPrefixedString(nodeID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeIDs.Add(nID)
	}
	includeAllNodes := nodeIDs.Len() == 0

	pendingValidators := service.vm.internalState.PendingStakerChainState()

	for _, tx := range pendingValidators.Stakers() { // Iterates in order of increasing start time
		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
			if args.SubnetID != constants.PrimaryNetworkID {
				continue
			}
			if !includeAllNodes && !nodeIDs.Contains(staker.Validator.ID()) {
				continue
			}

			weight := json.Uint64(staker.Validator.Weight())
			reply.Delegators = append(reply.Delegators, APIStaker{
				TxID:        tx.ID(),
				NodeID:      staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime:   json.Uint64(staker.StartTime().Unix()),
				EndTime:     json.Uint64(staker.EndTime().Unix()),
				StakeAmount: &weight,
			})
		case *UnsignedAddValidatorTx:
			if args.SubnetID != constants.PrimaryNetworkID {
				continue
			}
			if !includeAllNodes && !nodeIDs.Contains(staker.Validator.ID()) {
				continue
			}

			nodeID := staker.Validator.ID()
			weight := json.Uint64(staker.Validator.Weight())
			delegationFee := json.Float32(100 * float32(staker.Shares) / float32(reward.PercentDenominator))

			connected := service.vm.uptimeManager.IsConnected(nodeID)
			reply.Validators = append(reply.Validators, APIPrimaryValidator{
				APIStaker: APIStaker{
					TxID:        tx.ID(),
					NodeID:      staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
					StartTime:   json.Uint64(staker.StartTime().Unix()),
					EndTime:     json.Uint64(staker.EndTime().Unix()),
					StakeAmount: &weight,
				},
				DelegationFee: delegationFee,
				Connected:     &connected,
			})
		case *UnsignedAddSubnetValidatorTx:
			if args.SubnetID != staker.Validator.Subnet {
				continue
			}
			if !includeAllNodes && !nodeIDs.Contains(staker.Validator.ID()) {
				continue
			}

			weight := json.Uint64(staker.Validator.Weight())
			reply.Validators = append(reply.Validators, APIStaker{
				TxID:      tx.ID(),
				NodeID:    staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime: json.Uint64(staker.StartTime().Unix()),
				EndTime:   json.Uint64(staker.EndTime().Unix()),
				Weight:    &weight,
			})
		default:
			return fmt.Errorf("expected validator but got %T", tx.UnsignedTx)
		}
	}
	return nil
}

// GetCurrentSupplyReply are the results from calling GetCurrentSupply
type GetCurrentSupplyReply struct {
	Supply json.Uint64 `json:"supply"`
}

// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
func (service *Service) GetCurrentSupply(_ *http.Request, _ *struct{}, reply *GetCurrentSupplyReply) error {
	service.vm.ctx.Log.Debug("Platform: GetCurrentSupply called")

	reply.Supply = json.Uint64(service.vm.internalState.GetCurrentSupply())
	return nil
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
	Validators []string `json:"validators"`
}

// SampleValidators returns a sampling of the list of current validators
func (service *Service) SampleValidators(_ *http.Request, args *SampleValidatorsArgs, reply *SampleValidatorsReply) error {
	service.vm.ctx.Log.Debug("Platform: SampleValidators called with Size = %d", args.Size)

	validators, ok := service.vm.Validators.GetValidators(args.SubnetID)
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

	validatorIDs := make([]ids.ShortID, int(args.Size))
	for i, vdr := range sample {
		validatorIDs[i] = vdr.ID()
	}
	ids.SortShortIDs(validatorIDs)

	reply.Validators = make([]string, int(args.Size))
	for i, vdrID := range validatorIDs {
		reply.Validators[i] = vdrID.PrefixedString(constants.NodeIDPrefix)
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
	APIStaker
	// The address the staking reward, if applicable, will go to
	RewardAddress     string       `json:"rewardAddress"`
	DelegationFeeRate json.Float32 `json:"delegationFeeRate"`
}

// AddValidator creates and signs and issues a transaction to add a validator to
// the primary network
func (service *Service) AddValidator(_ *http.Request, args *AddValidatorArgs, reply *api.JSONTxIDChangeAddr) error {
	service.vm.ctx.Log.Debug("Platform: AddValidator called")

	now := service.vm.clock.Time()
	minAddStakerTime := now.Add(minAddStakerDelay)
	minAddStakerUnix := json.Uint64(minAddStakerTime.Unix())
	maxAddStakerTime := now.Add(maxFutureStartTime)
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
	var nodeID ids.ShortID
	if args.NodeID == "" {
		nodeID = service.vm.ctx.NodeID // If omitted, use this node's ID
	} else {
		nID, err := ids.ShortFromPrefixedString(args.NodeID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeID = nID
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseLocalAddresses(service.vm, args.From)
	if err != nil {
		return err
	}

	// Parse the reward address
	rewardAddress, err := service.vm.ParseLocalAddress(args.RewardAddress)
	if err != nil {
		return fmt.Errorf("problem while parsing reward address: %w", err)
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		changeAddr, err = service.vm.ParseLocalAddress(args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := service.vm.newAddValidatorTx(
		args.weight(),                        // Stake amount
		uint64(args.StartTime),               // Start time
		uint64(args.EndTime),                 // End time
		nodeID,                               // Node ID
		rewardAddress,                        // Reward Address
		uint32(10000*args.DelegationFeeRate), // Shares
		privKeys.Keys,                        // Private keys
		changeAddr,                           // Change address
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()
	reply.ChangeAddr, err = service.vm.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		service.vm.blockBuilder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// AddDelegatorArgs are the arguments to AddDelegator
type AddDelegatorArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	APIStaker
	RewardAddress string `json:"rewardAddress"`
}

// AddDelegator creates and signs and issues a transaction to add a delegator to
// the primary network
func (service *Service) AddDelegator(_ *http.Request, args *AddDelegatorArgs, reply *api.JSONTxIDChangeAddr) error {
	service.vm.ctx.Log.Debug("Platform: AddDelegator called")

	now := service.vm.clock.Time()
	minAddStakerTime := now.Add(minAddStakerDelay)
	minAddStakerUnix := json.Uint64(minAddStakerTime.Unix())
	maxAddStakerTime := now.Add(maxFutureStartTime)
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

	// Parse the node ID
	var nodeID ids.ShortID
	if args.NodeID == "" { // If ID unspecified, use this node's ID
		nodeID = service.vm.ctx.NodeID
	} else {
		nID, err := ids.ShortFromPrefixedString(args.NodeID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeID = nID
	}

	// Parse the reward address
	rewardAddress, err := service.vm.ParseLocalAddress(args.RewardAddress)
	if err != nil {
		return fmt.Errorf("problem parsing 'rewardAddress': %w", err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseLocalAddresses(service.vm, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		changeAddr, err = service.vm.ParseLocalAddress(args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := service.vm.newAddDelegatorTx(
		args.weight(),          // Stake amount
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
	reply.ChangeAddr, err = service.vm.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		service.vm.blockBuilder.AddUnverifiedTx(tx),
		user.Close(),
	)
	return errs.Err
}

// AddSubnetValidatorArgs are the arguments to AddSubnetValidator
type AddSubnetValidatorArgs struct {
	// User, password, from addrs, change addr
	api.JSONSpendHeader
	APIStaker
	// ID of subnet to validate
	SubnetID string `json:"subnetID"`
}

// AddSubnetValidator creates and signs and issues a transaction to add a
// validator to a subnet other than the primary network
func (service *Service) AddSubnetValidator(_ *http.Request, args *AddSubnetValidatorArgs, response *api.JSONTxIDChangeAddr) error {
	service.vm.ctx.Log.Debug("Platform: AddSubnetValidator called")

	now := service.vm.clock.Time()
	minAddStakerTime := now.Add(minAddStakerDelay)
	minAddStakerUnix := json.Uint64(minAddStakerTime.Unix())
	maxAddStakerTime := now.Add(maxFutureStartTime)
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

	// Parse the node ID
	nodeID, err := ids.ShortFromPrefixedString(args.NodeID, constants.NodeIDPrefix)
	if err != nil {
		return fmt.Errorf("error parsing nodeID: %q: %w", args.NodeID, err)
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
	fromAddrs, err := avax.ParseLocalAddresses(service.vm, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		changeAddr, err = service.vm.ParseLocalAddress(args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := service.vm.newAddSubnetValidatorTx(
		args.weight(),          // Stake amount
		uint64(args.StartTime), // Start time
		uint64(args.EndTime),   // End time
		nodeID,                 // Node ID
		subnetID,               // Subnet ID
		keys.Keys,              // Keys
		changeAddr,             // Change address
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = service.vm.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		service.vm.blockBuilder.AddUnverifiedTx(tx),
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
func (service *Service) CreateSubnet(_ *http.Request, args *CreateSubnetArgs, response *api.JSONTxIDChangeAddr) error {
	service.vm.ctx.Log.Debug("Platform: CreateSubnet called")

	// Parse the control keys
	controlKeys, err := avax.ParseLocalAddresses(service.vm, args.ControlKeys)
	if err != nil {
		return err
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseLocalAddresses(service.vm, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		changeAddr, err = service.vm.ParseLocalAddress(args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := service.vm.newCreateSubnetTx(
		uint32(args.Threshold), // Threshold
		controlKeys.List(),     // Control Addresses
		privKeys.Keys,          // Private keys
		changeAddr,             // Change address
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = service.vm.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		service.vm.blockBuilder.AddUnverifiedTx(tx),
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

	// ID of the address that will receive the AVAX. This address includes the
	// chainID, which is used to determine what the destination chain is.
	To string `json:"to"`
}

// ExportAVAX exports AVAX from the P-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (service *Service) ExportAVAX(_ *http.Request, args *ExportAVAXArgs, response *api.JSONTxIDChangeAddr) error {
	service.vm.ctx.Log.Debug("Platform: ExportAVAX called")

	if args.Amount == 0 {
		return errNoAmount
	}

	// Parse the to address
	chainID, to, err := service.vm.ParseAddress(args.To)
	if err != nil {
		return err
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseLocalAddresses(service.vm, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		changeAddr, err = service.vm.ParseLocalAddress(args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := service.vm.newExportTx(
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
	response.ChangeAddr, err = service.vm.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		service.vm.blockBuilder.AddUnverifiedTx(tx),
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
func (service *Service) ImportAVAX(_ *http.Request, args *ImportAVAXArgs, response *api.JSONTxIDChangeAddr) error {
	service.vm.ctx.Log.Debug("Platform: ImportAVAX called")

	// Parse the sourceCHain
	chainID, err := service.vm.ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing chainID %q: %w", args.SourceChain, err)
	}

	// Parse the to address
	to, err := service.vm.ParseLocalAddress(args.To)
	if err != nil { // Parse address
		return fmt.Errorf("couldn't parse argument 'to' to an address: %w", err)
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseLocalAddresses(service.vm, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		changeAddr, err = service.vm.ParseLocalAddress(args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	tx, err := service.vm.newImportTx(chainID, to, privKeys.Keys, changeAddr)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()
	response.ChangeAddr, err = service.vm.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		service.vm.blockBuilder.AddUnverifiedTx(tx),
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
func (service *Service) CreateBlockchain(_ *http.Request, args *CreateBlockchainArgs, response *api.JSONTxIDChangeAddr) error {
	service.vm.ctx.Log.Debug("Platform: CreateBlockchain called")

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

	vmID, err := service.vm.Chains.LookupVM(args.VMID)
	if err != nil {
		return fmt.Errorf("no VM with ID '%s' found", args.VMID)
	}

	fxIDs := []ids.ID(nil)
	for _, fxIDStr := range args.FxIDs {
		fxID, err := service.vm.Chains.LookupVM(fxIDStr)
		if err != nil {
			return fmt.Errorf("no FX with ID '%s' found", fxIDStr)
		}
		fxIDs = append(fxIDs, fxID)
	}
	// If creating AVM instance, use secp256k1fx
	// TODO: Document FXs and have user specify them in API call
	fxIDsSet := ids.Set{}
	fxIDsSet.Add(fxIDs...)
	if vmID == constants.AVMID && !fxIDsSet.Contains(secp256k1fx.ID) {
		fxIDs = append(fxIDs, secp256k1fx.ID)
	}

	if args.SubnetID == constants.PrimaryNetworkID {
		return errDSCantValidate
	}

	// Parse the from addresses
	fromAddrs, err := avax.ParseLocalAddresses(service.vm, args.From)
	if err != nil {
		return err
	}

	user, err := keystore.NewUserFromKeystore(service.vm.ctx.Keystore, args.Username, args.Password)
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
		changeAddr, err = service.vm.ParseLocalAddress(args.ChangeAddr)
		if err != nil {
			return fmt.Errorf("couldn't parse changeAddr: %w", err)
		}
	}

	// Create the transaction
	tx, err := service.vm.newCreateChainTx(
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
	response.ChangeAddr, err = service.vm.FormatLocalAddress(changeAddr)

	errs := wrappers.Errs{}
	errs.Add(
		err,
		service.vm.blockBuilder.AddUnverifiedTx(tx),
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
func (service *Service) GetBlockchainStatus(_ *http.Request, args *GetBlockchainStatusArgs, reply *GetBlockchainStatusReply) error {
	service.vm.ctx.Log.Debug("Platform: GetBlockchainStatus called")

	if args.BlockchainID == "" {
		return errMissingBlockchainID
	}

	// if its aliased then vm created this chain.
	if aliasedID, err := service.vm.Chains.Lookup(args.BlockchainID); err == nil {
		if service.nodeValidates(aliasedID) {
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

	lastAcceptedID, err := service.vm.LastAccepted()
	if err != nil {
		return fmt.Errorf("problem loading last accepted ID: %w", err)
	}

	exists, err := service.chainExists(lastAcceptedID, blockchainID)
	if err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	}
	if exists {
		reply.Status = status.Created
		return nil
	}

	preferred, err := service.chainExists(service.vm.preferred, blockchainID)
	if err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	}
	if preferred {
		reply.Status = status.Preferred
	}
	return nil
}

func (service *Service) nodeValidates(blockchainID ids.ID) bool {
	chainTx, _, err := service.vm.internalState.GetTx(blockchainID)
	if err != nil {
		return false
	}

	chain, ok := chainTx.UnsignedTx.(*UnsignedCreateChainTx)
	if !ok {
		return false
	}

	validators, ok := service.vm.Validators.GetValidators(chain.SubnetID)
	if !ok {
		return false
	}

	return validators.Contains(service.vm.ctx.NodeID)
}

func (service *Service) chainExists(blockID ids.ID, chainID ids.ID) (bool, error) {
	blockIntf, err := service.vm.getBlock(blockID)
	if err != nil {
		return false, err
	}

	block, ok := blockIntf.(decision)
	if !ok {
		parentBlockIntf, err := blockIntf.parentBlock()
		if err != nil {
			return false, err
		}
		block, ok = parentBlockIntf.(decision)
		if !ok {
			return false, errMissingDecisionBlock
		}
	}
	state := block.onAccept()

	tx, _, err := state.GetTx(chainID)
	if err == database.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, ok = tx.UnsignedTx.(*UnsignedCreateChainTx)
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
func (service *Service) ValidatedBy(_ *http.Request, args *ValidatedByArgs, response *ValidatedByResponse) error {
	service.vm.ctx.Log.Debug("Platform: ValidatedBy called")

	chainTx, _, err := service.vm.internalState.GetTx(args.BlockchainID)
	if err != nil {
		return fmt.Errorf(
			"problem retrieving blockchain %q: %w",
			args.BlockchainID,
			err,
		)
	}
	chain, ok := chainTx.UnsignedTx.(*UnsignedCreateChainTx)
	if !ok {
		return fmt.Errorf("%q is not a blockchain", args.BlockchainID)
	}
	response.SubnetID = chain.SubnetID
	return nil
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
func (service *Service) Validates(_ *http.Request, args *ValidatesArgs, response *ValidatesResponse) error {
	service.vm.ctx.Log.Debug("Platform: Validates called")

	if args.SubnetID != constants.PrimaryNetworkID {
		subnetTx, _, err := service.vm.internalState.GetTx(args.SubnetID)
		if err != nil {
			return fmt.Errorf(
				"problem retrieving subnet %q: %w",
				args.SubnetID,
				err,
			)
		}
		_, ok := subnetTx.UnsignedTx.(*UnsignedCreateSubnetTx)
		if !ok {
			return fmt.Errorf("%q is not a subnet", args.SubnetID)
		}
	}

	// Get the chains that exist
	chains, err := service.vm.internalState.GetChains(args.SubnetID)
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
func (service *Service) GetBlockchains(_ *http.Request, args *struct{}, response *GetBlockchainsResponse) error {
	service.vm.ctx.Log.Debug("Platform: GetBlockchains called")

	subnets, err := service.vm.internalState.GetSubnets()
	if err != nil {
		return fmt.Errorf("couldn't retrieve subnets: %w", err)
	}

	response.Blockchains = []APIBlockchain{}
	for _, subnet := range subnets {
		subnetID := subnet.ID()
		chains, err := service.vm.internalState.GetChains(subnetID)
		if err != nil {
			return fmt.Errorf(
				"couldn't retrieve chains for subnet %q: %w",
				subnetID,
				err,
			)
		}

		for _, chainTx := range chains {
			chain, ok := chainTx.UnsignedTx.(*UnsignedCreateChainTx)
			if !ok {
				return errWrongTxType
			}
			response.Blockchains = append(response.Blockchains, APIBlockchain{
				ID:       chain.ID(),
				Name:     chain.ChainName,
				SubnetID: subnetID,
				VMID:     chain.VMID,
			})
		}
	}

	chains, err := service.vm.internalState.GetChains(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("couldn't retrieve subnets: %w", err)
	}
	for _, chainTx := range chains {
		chain, ok := chainTx.UnsignedTx.(*UnsignedCreateChainTx)
		if !ok {
			return errWrongTxType
		}
		response.Blockchains = append(response.Blockchains, APIBlockchain{
			ID:       chain.ID(),
			Name:     chain.ChainName,
			SubnetID: constants.PrimaryNetworkID,
			VMID:     chain.VMID,
		})
	}

	return nil
}

// IssueTx issues a tx
func (service *Service) IssueTx(_ *http.Request, args *api.FormattedTx, response *api.JSONTxID) error {
	service.vm.ctx.Log.Debug("Platform: IssueTx called")

	txBytes, err := formatting.Decode(args.Encoding, args.Tx)
	if err != nil {
		return fmt.Errorf("problem decoding transaction: %w", err)
	}
	tx := &Tx{}
	if _, err := Codec.Unmarshal(txBytes, tx); err != nil {
		return fmt.Errorf("couldn't parse tx: %w", err)
	}
	if err := service.vm.blockBuilder.AddUnverifiedTx(tx); err != nil {
		return fmt.Errorf("couldn't issue tx: %w", err)
	}

	response.TxID = tx.ID()
	return nil
}

// GetTx gets a tx
func (service *Service) GetTx(_ *http.Request, args *api.GetTxArgs, response *api.FormattedTx) error {
	service.vm.ctx.Log.Debug("Platform: GetTx called")

	tx, _, err := service.vm.internalState.GetTx(args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get tx: %w", err)
	}

	txBytes := tx.Bytes()
	response.Tx, err = formatting.EncodeWithChecksum(args.Encoding, txBytes)
	if err != nil {
		return fmt.Errorf("couldn't encode tx as a string: %w", err)
	}
	response.Encoding = args.Encoding
	return nil
}

type GetTxStatusArgs struct {
	TxID ids.ID `json:"txID"`
	// If IncludeReason is false returns a response that looks like:
	// {
	// 	"jsonrpc": "2.0",
	// 	"result": "Dropped",
	// 	"id": 1
	// }
	// If IncludeReason is true returns a response that looks like this:
	// {
	// 	"jsonrpc": "2.0",
	// 	"result": {
	//     "status":"[Status]",
	//     "reason":"[Reason tx was dropped, if applicable]"
	//  },
	// 	"id": 1
	// }
	// In the latter, "reason" is only present if the status is dropped
	IncludeReason bool `json:"includeReason"`
}

type GetTxStatusResponse struct {
	Status status.Status `json:"status"`
	// Reason this tx was dropped.
	// Only non-empty if Status is dropped
	Reason string `json:"reason,omitempty"`
}

// GetTxStatus gets a tx's status
func (service *Service) GetTxStatus(_ *http.Request, args *GetTxStatusArgs, response *GetTxStatusResponse) error {
	service.vm.ctx.Log.Debug("Platform: GetTxStatus called with txID: %s", args.TxID)

	_, txStatus, err := service.vm.internalState.GetTx(args.TxID)
	if err == nil { // Found the status. Report it.
		response.Status = txStatus
		return nil
	}
	if err != database.ErrNotFound {
		return err
	}

	// The status of this transaction is not in the database - check if the tx
	// is in the preferred block's db. If so, return that it's processing.
	preferred, err := service.vm.Preferred()
	if err != nil {
		return err
	}

	block, ok := preferred.(decision)
	if !ok {
		return errInvalidBlockType
	}

	onAccept := block.onAccept()
	_, _, err = onAccept.GetTx(args.TxID)
	if err == nil {
		// Found the status in the preferred block's db. Report tx is processing.
		response.Status = status.Processing
		return nil
	}
	if err != database.ErrNotFound {
		return err
	}

	if service.vm.blockBuilder.Has(args.TxID) {
		// Found the tx in the mempool. Report tx is processing.
		response.Status = status.Processing
		return nil
	}

	reason, ok := service.vm.droppedTxCache.Get(args.TxID)
	if !ok {
		// The tx isn't being tracked by the node.
		response.Status = status.Unknown
		return nil
	}

	// The tx was recently dropped because it was invalid.
	response.Status = status.Dropped
	if !args.IncludeReason {
		return nil
	}

	reasonStr, ok := reason.(string)
	if !ok {
		return errCorruptedReason
	}

	response.Reason = reasonStr
	return nil
}

type GetStakeArgs struct {
	api.JSONAddresses
	Encoding formatting.Encoding `json:"encoding"`
}

// GetStakeReply is the response from calling GetStake.
type GetStakeReply struct {
	Staked json.Uint64 `json:"staked"`
	// String representation of staked outputs
	// Each is of type avax.TransferableOutput
	Outputs []string `json:"stakedOutputs"`
	// Encoding of [Outputs]
	Encoding formatting.Encoding `json:"encoding"`
}

// Takes in a staker and a set of addresses
// Returns:
// 1) The total amount staked by addresses in [addrs]
// 2) The staked outputs
func (service *Service) getStakeHelper(tx *Tx, addrs ids.ShortSet) (uint64, []avax.TransferableOutput, error) {
	var outs []*avax.TransferableOutput
	switch staker := tx.UnsignedTx.(type) {
	case *UnsignedAddDelegatorTx:
		outs = staker.Stake
	case *UnsignedAddValidatorTx:
		outs = staker.Stake
	case *UnsignedAddSubnetValidatorTx:
		return 0, nil, nil
	default:
		err := fmt.Errorf("expected *UnsignedAddDelegatorTx, *UnsignedAddValidatorTx or *UnsignedAddSubnetValidatorTx but got %T", tx.UnsignedTx)
		service.vm.ctx.Log.Error("invalid tx type provided from validator set %s", err)
		return 0, nil, err
	}

	var (
		totalAmountStaked uint64
		err               error
		stakedOuts        = make([]avax.TransferableOutput, 0, len(outs))
	)
	// Go through all of the staked outputs
	for _, stake := range outs {
		// This output isn't AVAX. Ignore.
		if stake.AssetID() != service.vm.ctx.AVAXAssetID {
			continue
		}
		out := stake.Out
		if lockedOut, ok := out.(*StakeableLockOut); ok {
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
		totalAmountStaked, err = math.Add64(totalAmountStaked, stake.Out.Amount())
		if err != nil {
			return 0, stakedOuts, err
		}
		stakedOuts = append(
			stakedOuts,
			*stake,
		)
	}
	return totalAmountStaked, stakedOuts, nil
}

// GetStake returns the amount of nAVAX that [args.Addresses] have cumulatively
// staked on the Primary Network.
//
// This method assumes that each stake output has only owner
// This method assumes only AVAX can be staked
// This method only concerns itself with the Primary Network, not subnets
// TODO: Improve the performance of this method by maintaining this data
// in a data structure rather than re-calculating it by iterating over stakers
func (service *Service) GetStake(_ *http.Request, args *GetStakeArgs, response *GetStakeReply) error {
	service.vm.ctx.Log.Debug("Platform: GetStake called")

	if len(args.Addresses) > maxGetStakeAddrs {
		return fmt.Errorf("%d addresses provided but this method can take at most %d", len(args.Addresses), maxGetStakeAddrs)
	}

	addrs := ids.ShortSet{}
	for _, addrStr := range args.Addresses { // Parse addresses from string
		addr, err := service.vm.ParseLocalAddress(addrStr)
		if err != nil {
			return fmt.Errorf("couldn't parse address %s: %w", addrStr, err)
		}
		addrs.Add(addr)
	}

	currentStakers := service.vm.internalState.CurrentStakerChainState()
	stakers := currentStakers.Stakers()

	var (
		totalStake uint64
		stakedOuts = make([]avax.TransferableOutput, 0, len(stakers))
	)
	for _, tx := range stakers { // Iterates over current stakers
		stakedAmt, outs, err := service.getStakeHelper(tx, addrs)
		if err != nil {
			return err
		}
		totalStake, err = math.Add64(totalStake, stakedAmt)
		if err != nil {
			return err
		}
		stakedOuts = append(stakedOuts, outs...)
	}

	pendingStakers := service.vm.internalState.PendingStakerChainState()
	for _, tx := range pendingStakers.Stakers() { // Iterates over pending stakers
		stakedAmt, outs, err := service.getStakeHelper(tx, addrs)
		if err != nil {
			return err
		}
		totalStake, err = math.Add64(totalStake, stakedAmt)
		if err != nil {
			return err
		}
		stakedOuts = append(stakedOuts, outs...)
	}

	response.Staked = json.Uint64(totalStake)
	response.Outputs = make([]string, len(stakedOuts))
	for i, output := range stakedOuts {
		bytes, err := Codec.Marshal(CodecVersion, output)
		if err != nil {
			return fmt.Errorf("couldn't serialize output %s: %w", output.ID, err)
		}
		response.Outputs[i], err = formatting.EncodeWithChecksum(args.Encoding, bytes)
		if err != nil {
			return fmt.Errorf("couldn't encode output %s as string: %w", output.ID, err)
		}
	}
	response.Encoding = args.Encoding

	return nil
}

// GetMinStakeReply is the response from calling GetMinStake.
type GetMinStakeReply struct {
	//  The minimum amount of tokens one must bond to be a validator
	MinValidatorStake json.Uint64 `json:"minValidatorStake"`
	// Minimum stake, in nAVAX, that can be delegated on the primary network
	MinDelegatorStake json.Uint64 `json:"minDelegatorStake"`
}

// GetMinStake returns the minimum staking amount in nAVAX.
func (service *Service) GetMinStake(_ *http.Request, _ *struct{}, reply *GetMinStakeReply) error {
	reply.MinValidatorStake = json.Uint64(service.vm.MinValidatorStake)
	reply.MinDelegatorStake = json.Uint64(service.vm.MinDelegatorStake)
	return nil
}

// GetTotalStakeReply is the response from calling GetTotalStake.
type GetTotalStakeReply struct {
	Stake json.Uint64 `json:"stake"`
}

// GetTotalStake returns the total amount staked on the Primary Network
func (service *Service) GetTotalStake(_ *http.Request, _ *struct{}, reply *GetTotalStakeReply) error {
	vdrs, ok := service.vm.Validators.GetValidators(constants.PrimaryNetworkID)
	if !ok {
		return errNoPrimaryValidators
	}
	reply.Stake = json.Uint64(vdrs.Weight())
	return nil
}

// GetMaxStakeAmountArgs is the request for calling GetMaxStakeAmount.
type GetMaxStakeAmountArgs struct {
	SubnetID  ids.ID      `json:"subnetID"`
	NodeID    string      `json:"nodeID"`
	StartTime json.Uint64 `json:"startTime"`
	EndTime   json.Uint64 `json:"endTime"`
}

// GetMaxStakeAmountReply is the response from calling GetMaxStakeAmount.
type GetMaxStakeAmountReply struct {
	Amount json.Uint64 `json:"amount"`
}

// GetMaxStakeAmount returns the maximum amount of nAVAX staking to the named
// node during the time period.
func (service *Service) GetMaxStakeAmount(_ *http.Request, args *GetMaxStakeAmountArgs, reply *GetMaxStakeAmountReply) error {
	nodeID, err := ids.ShortFromPrefixedString(args.NodeID, constants.NodeIDPrefix)
	if err != nil {
		return fmt.Errorf("failed to parse nodeID %q due to: %w", args.NodeID, err)
	}
	startTime := time.Unix(int64(args.StartTime), 0)
	endTime := time.Unix(int64(args.EndTime), 0)

	maxStakeAmount, err := service.vm.maxStakeAmount(
		args.SubnetID,
		nodeID,
		startTime,
		endTime,
	)

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
func (service *Service) GetRewardUTXOs(_ *http.Request, args *api.GetTxArgs, reply *GetRewardUTXOsReply) error {
	service.vm.ctx.Log.Debug("Platform: GetRewardUTXOs called")

	utxos, err := service.vm.internalState.GetRewardUTXOs(args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get reward UTXOs: %w", err)
	}

	reply.NumFetched = json.Uint64(len(utxos))
	reply.UTXOs = make([]string, len(utxos))
	for i, utxo := range utxos {
		utxoBytes, err := GenesisCodec.Marshal(CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("failed to encode UTXO to bytes: %w", err)
		}

		utxoStr, err := formatting.EncodeWithChecksum(args.Encoding, utxoBytes)
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
func (service *Service) GetTimestamp(_ *http.Request, args *struct{}, reply *GetTimestampReply) error {
	service.vm.ctx.Log.Debug("Platform: GetTimestamp called")

	reply.Timestamp = service.vm.internalState.GetTimestamp()
	return nil
}

// GetValidatorsAtArgs is the response from GetValidatorsAt
type GetValidatorsAtArgs struct {
	Height   json.Uint64 `json:"height"`
	SubnetID ids.ID      `json:"subnetID"`
}

// GetValidatorsAtReply is the response from GetValidatorsAt
type GetValidatorsAtReply struct {
	Validators map[string]uint64 `json:"validators"`
}

// GetValidatorsAt returns the weights of the validator set of a provided subnet
// at the specified height.
func (service *Service) GetValidatorsAt(_ *http.Request, args *GetValidatorsAtArgs, reply *GetValidatorsAtReply) error {
	service.vm.ctx.Log.Info(
		"Platform: GetValidatorsAt called with Height %d and SubnetID %s",
		args.Height,
		args.SubnetID,
	)

	validators, err := service.vm.GetValidatorSet(uint64(args.Height), args.SubnetID)
	if err != nil {
		return fmt.Errorf("couldn't get validator set: %w", err)
	}

	reply.Validators = make(map[string]uint64, len(validators))
	for nodeID, weight := range validators {
		reply.Validators[nodeID.PrefixedString(constants.NodeIDPrefix)] = weight
	}
	return nil
}
