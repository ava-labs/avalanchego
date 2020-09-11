// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ava-labs/avalanche-go/api"
	"github.com/ava-labs/avalanche-go/database/prefixdb"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/constants"
	"github.com/ava-labs/avalanche-go/utils/crypto"
	"github.com/ava-labs/avalanche-go/utils/formatting"
	"github.com/ava-labs/avalanche-go/utils/json"
	"github.com/ava-labs/avalanche-go/utils/math"
	"github.com/ava-labs/avalanche-go/utils/wrappers"
	"github.com/ava-labs/avalanche-go/vms/avm"
	"github.com/ava-labs/avalanche-go/vms/components/avax"
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

const (
	// Max number of addresses that can be passed in as argument to GetUTXOs
	maxGetUTXOsAddrs = 1024

	// Max number of addresses that can be passed in as argument to GetStake
	maxGetStakeAddrs = 256
)

var (
	errMissingDecisionBlock  = errors.New("should have a decision block within the past two blocks")
	errNoFunds               = errors.New("no spendable funds were found")
	errNoSubnetID            = errors.New("argument 'subnetID' not provided")
	errNoRewardAddress       = errors.New("argument 'rewardAddress' not provided")
	errInvalidDelegationRate = errors.New("argument 'delegationFeeRate' must be between 0 and 100, inclusive")
	errNoAddresses           = errors.New("no addresses provided")
)

// Service defines the API calls that can be made to the platform chain
type Service struct{ vm *VM }

// GetHeightResponse ...
type GetHeightResponse struct {
	Height json.Uint64 `json:"height"`
}

// GetHeight returns the height of the last accepted block
func (service *Service) GetHeight(r *http.Request, args *struct{}, response *GetHeightResponse) error {
	lastAccepted, err := service.vm.getBlock(service.vm.LastAccepted())
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
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ExportKey called")

	address, err := service.vm.ParseLocalAddress(args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse %s to address: %s", args.Address, err)
	}

	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	user := user{db: db}

	sk, err := user.getKey(address)
	if err != nil {
		// Drop any potential error closing the database to report the original
		// error
		_ = db.Close()
		return fmt.Errorf("problem retrieving private key: %w", err)
	}

	reply.PrivateKey = constants.SecretKeyPrefix + formatting.CB58{Bytes: sk.Bytes()}.String()
	return db.Close()
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	api.UserPass
	PrivateKey string `json:"privateKey"`
}

// ImportKey adds a private key to the provided user
func (service *Service) ImportKey(r *http.Request, args *ImportKeyArgs, reply *api.JsonAddress) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ImportKey called for user '%s'", args.Username)

	if !strings.HasPrefix(args.PrivateKey, constants.SecretKeyPrefix) {
		return fmt.Errorf("private key missing %s prefix", constants.SecretKeyPrefix)
	}

	trimmedPrivateKey := strings.TrimPrefix(args.PrivateKey, constants.SecretKeyPrefix)
	formattedPrivateKey := formatting.CB58{}
	if err := formattedPrivateKey.FromString(trimmedPrivateKey); err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.ToPrivateKey(formattedPrivateKey.Bytes)
	if err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	reply.Address, err = service.vm.FormatLocalAddress(sk.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving data: %w", err)
	}

	user := user{db: db}
	if err := user.putAddress(sk); err != nil {
		// Drop any potential error closing the database to report the original
		// error
		_ = db.Close()

		return fmt.Errorf("problem saving key %w", err)
	}
	return db.Close()
}

/*
 ******************************************************
 *************  Balances / Addresses ******************
 ******************************************************
 */

// GetBalanceArgs ...
type GetBalanceArgs struct {
	// Address to get the balance of
	Address string `json:"address"`
}

// GetBalanceResponse ...
type GetBalanceResponse struct {
	// Balance, in nAVAX, of the address
	Balance json.Uint64    `json:"balance"`
	UTXOIDs []*avax.UTXOID `json:"utxoIDs"`
}

// GetBalance gets the balance of an address
func (service *Service) GetBalance(_ *http.Request, args *GetBalanceArgs, response *GetBalanceResponse) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: GetBalance called for address %s", args.Address)

	// Parse to address
	addr, err := service.vm.ParseLocalAddress(args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse argument 'address' to address: %w", err)
	}

	addrs := ids.ShortSet{}
	addrs.Add(addr)
	utxos, _, _, err := service.vm.GetUTXOs(service.vm.DB, addrs, ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		addr, err2 := service.vm.FormatLocalAddress(addr)
		if err2 != nil {
			return fmt.Errorf("problem formatting address: %w", err2)
		}
		return fmt.Errorf("couldn't get UTXO set of %s: %w", addr, err)
	}

	balance := uint64(0)
	for _, utxo := range utxos {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			service.vm.Ctx.Log.Warn("expected UTXO output to be type *secp256k1fx.TransferOutput but is type %T", utxo.Out)
			continue
		}
		balance, err = math.Add64(balance, out.Amount())
		if err != nil {
			return errors.New("overflow while calculating balance")
		}
		response.UTXOIDs = append(response.UTXOIDs, &utxo.UTXOID)
	}
	response.Balance = json.Uint64(balance)
	return nil
}

// CreateAddress creates an address controlled by [args.Username]
// Returns the newly created address
func (service *Service) CreateAddress(_ *http.Request, args *api.UserPass, response *api.JsonAddress) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: CreateAddress called")

	factory := crypto.FactorySECP256K1R{}
	key, err := factory.NewPrivateKey()
	if err != nil {
		return fmt.Errorf("couldn't create key: %w", err)
	}

	response.Address, err = service.vm.FormatLocalAddress(key.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	user := user{db: db}
	if err := user.putAddress(key.(*crypto.PrivateKeySECP256K1R)); err != nil {
		// Drop any potential error closing the database to report the original
		// error
		_ = db.Close()

		return fmt.Errorf("problem saving key %w", err)
	}
	return db.Close()
}

// ListAddresses returns the addresses controlled by [args.Username]
func (service *Service) ListAddresses(_ *http.Request, args *api.UserPass, response *api.JsonAddresses) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ListAddresses called")

	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	addresses, err := user.getAddresses()
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
	return db.Close()
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
type GetUTXOsArgs struct {
	Addresses   []string    `json:"addresses"`
	SourceChain string      `json:"sourceChain"`
	Limit       json.Uint32 `json:"limit"`
	StartIndex  Index       `json:"startIndex"`
}

// GetUTXOsResponse defines the GetUTXOs replies returned from the API
type GetUTXOsResponse struct {
	// Number of UTXOs returned
	NumFetched json.Uint64 `json:"numFetched"`
	// The UTXOs
	UTXOs []formatting.CB58 `json:"utxos"`
	// The last UTXO that was returned, and the address it corresponds to.
	// Used for pagination. To get the rest of the UTXOs, call GetUTXOs
	// again and set [StartIndex] to this value.
	EndIndex Index `json:"endIndex"`
}

// GetUTXOs returns the UTXOs controlled by the given addresses
func (service *Service) GetUTXOs(_ *http.Request, args *GetUTXOsArgs, response *GetUTXOsResponse) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ListAddresses called")

	if len(args.Addresses) == 0 {
		return errNoAddresses
	}
	if len(args.Addresses) > maxGetUTXOsAddrs {
		return fmt.Errorf("number of addresses given, %d, exceeds maximum, %d", len(args.Addresses), maxGetUTXOsAddrs)
	}

	sourceChain := ids.ID{}
	if args.SourceChain == "" {
		sourceChain = service.vm.Ctx.ChainID
	} else {
		chainID, err := service.vm.Ctx.BCLookup.Lookup(args.SourceChain)
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
		addr, err := service.vm.ParseLocalAddress(args.StartIndex.Address)
		if err != nil {
			return fmt.Errorf("couldn't parse start index address %q: %w", args.StartIndex.Address, err)
		}
		utxo, err := ids.FromString(args.StartIndex.UTXO)
		if err != nil {
			return fmt.Errorf("couldn't parse start index utxo: %w", err)
		}

		startAddr = addr
		startUTXO = utxo
	}

	var (
		utxos     []*avax.UTXO
		endAddr   ids.ShortID
		endUTXOID ids.ID
		err       error
	)
	if sourceChain.Equals(service.vm.Ctx.ChainID) {
		utxos, endAddr, endUTXOID, err = service.vm.GetUTXOs(
			service.vm.DB,
			addrSet,
			startAddr,
			startUTXO,
			int(args.Limit),
		)
	} else {
		utxos, endAddr, endUTXOID, err = service.vm.GetAtomicUTXOs(
			sourceChain,
			addrSet,
			startAddr,
			startUTXO,
			int(args.Limit),
		)
	}
	if err != nil {
		return fmt.Errorf("problem retrieving UTXOs: %w", err)
	}

	response.UTXOs = make([]formatting.CB58, len(utxos))
	for i, utxo := range utxos {
		bytes, err := service.vm.codec.Marshal(utxo)
		if err != nil {
			return fmt.Errorf("couldn't serialize UTXO %q: %w", utxo.InputID(), err)
		}
		response.UTXOs[i] = formatting.CB58{Bytes: bytes}
	}

	endAddress, err := service.vm.FormatLocalAddress(endAddr)
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}

	response.EndIndex.Address = endAddress
	response.EndIndex.UTXO = endUTXOID.String()
	response.NumFetched = json.Uint64(len(utxos))
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
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: GetSubnets called")
	subnets, err := service.vm.getSubnets(service.vm.DB) // all subnets
	if err != nil {
		return fmt.Errorf("error getting subnets from database: %w", err)
	}

	getAll := len(args.IDs) == 0

	if getAll {
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

	idsSet := ids.Set{}
	idsSet.Add(args.IDs...)
	for _, subnet := range subnets {
		if idsSet.Contains(subnet.ID()) {
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
			response.Subnets = append(response.Subnets,
				APISubnet{
					ID:          subnet.ID(),
					ControlKeys: controlAddrs,
					Threshold:   json.Uint32(owner.Threshold),
				},
			)
		}
	}
	if idsSet.Contains(constants.PrimaryNetworkID) {
		response.Subnets = append(response.Subnets,
			APISubnet{
				ID:          constants.PrimaryNetworkID,
				ControlKeys: []string{},
				Threshold:   json.Uint32(0),
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
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: GetStakingAssetID called")

	if args.SubnetID.IsZero() {
		args.SubnetID = constants.PrimaryNetworkID
	}

	if !args.SubnetID.Equals(constants.PrimaryNetworkID) {
		return fmt.Errorf("Subnet %s doesn't have a valid staking token",
			args.SubnetID)
	}

	response.AssetID = service.vm.Ctx.AVAXAssetID
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
}

// GetCurrentValidatorsReply are the results from calling GetCurrentValidators
type GetCurrentValidatorsReply struct {
	Validators []interface{} `json:"validators"`
	Delegators []interface{} `json:"delegators"`
}

// GetCurrentValidators returns the list of current validators
func (service *Service) GetCurrentValidators(_ *http.Request, args *GetCurrentValidatorsArgs, reply *GetCurrentValidatorsReply) error {
	service.vm.Ctx.Log.Info("Platform: GetCurrentValidators called")
	if args.SubnetID.IsZero() {
		args.SubnetID = constants.PrimaryNetworkID
	}

	reply.Validators = []interface{}{}
	reply.Delegators = []interface{}{}

	stopPrefix := []byte(fmt.Sprintf("%s%s", args.SubnetID, stopDBPrefix))
	stopDB := prefixdb.NewNested(stopPrefix, service.vm.DB)
	defer stopDB.Close()

	stopIter := stopDB.NewIterator()
	defer stopIter.Release()

	for stopIter.Next() { // Iterates in order of increasing start time
		txBytes := stopIter.Value()

		tx := rewardTx{}
		if err := service.vm.codec.Unmarshal(txBytes, &tx); err != nil {
			return fmt.Errorf("couldn't unmarshal validator tx: %w", err)
		}
		if err := tx.Tx.Sign(service.vm.codec, nil); err != nil {
			return err
		}

		switch staker := tx.Tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
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

			potentialReward := json.Uint64(tx.Reward)
			reply.Delegators = append(reply.Delegators, APIPrimaryDelegator{
				APIStaker: APIStaker{
					StartTime:   json.Uint64(staker.StartTime().Unix()),
					EndTime:     json.Uint64(staker.EndTime().Unix()),
					StakeAmount: &weight,
					NodeID:      staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				},
				RewardOwner:     rewardOwner,
				PotentialReward: &potentialReward,
			})
		case *UnsignedAddValidatorTx:
			nodeID := staker.Validator.ID()
			startTime := staker.StartTime()
			weight := json.Uint64(staker.Validator.Weight())
			potentialReward := json.Uint64(tx.Reward)
			delegationFee := json.Float32(100 * float32(staker.Shares) / float32(PercentDenominator))
			rawUptime, err := service.vm.calculateUptime(service.vm.DB, nodeID, startTime)
			if err != nil {
				return err
			}
			uptime := json.Float32(rawUptime)

			service.vm.connLock.Lock()
			_, connected := service.vm.connections[nodeID.Key()]
			service.vm.connLock.Unlock()

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
			weight := json.Uint64(staker.Validator.Weight())
			reply.Validators = append(reply.Validators, APIStaker{
				NodeID:    staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime: json.Uint64(staker.StartTime().Unix()),
				EndTime:   json.Uint64(staker.EndTime().Unix()),
				Weight:    &weight,
			})
		default:
			return fmt.Errorf("expected validator but got %T", tx.Tx.UnsignedTx)
		}
	}
	return stopIter.Error()
}

// GetPendingValidatorsArgs are the arguments for calling GetPendingValidators
type GetPendingValidatorsArgs struct {
	// Subnet we're getting the pending validators of
	// If omitted, defaults to primary network
	SubnetID ids.ID `json:"subnetID"`
}

// GetPendingValidatorsReply are the results from calling GetPendingValidators
type GetPendingValidatorsReply struct {
	Validators []interface{} `json:"validators"`
	Delegators []interface{} `json:"delegators"`
}

// GetPendingValidators returns the list of pending validators
func (service *Service) GetPendingValidators(_ *http.Request, args *GetPendingValidatorsArgs, reply *GetPendingValidatorsReply) error {
	service.vm.Ctx.Log.Info("Platform: GetPendingValidators called")
	if args.SubnetID.IsZero() {
		args.SubnetID = constants.PrimaryNetworkID
	}

	reply.Validators = []interface{}{}
	reply.Delegators = []interface{}{}

	startPrefix := []byte(fmt.Sprintf("%s%s", args.SubnetID, startDBPrefix))
	startDB := prefixdb.NewNested(startPrefix, service.vm.DB)
	defer startDB.Close()

	startIter := startDB.NewIterator()
	defer startIter.Release()

	for startIter.Next() { // Iterates in order of increasing start time
		txBytes := startIter.Value()

		tx := Tx{}
		if err := service.vm.codec.Unmarshal(txBytes, &tx); err != nil {
			return fmt.Errorf("couldn't unmarshal validator tx: %w", err)
		}
		if err := tx.Sign(service.vm.codec, nil); err != nil {
			return err
		}

		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
			weight := json.Uint64(staker.Validator.Weight())
			reply.Delegators = append(reply.Delegators, APIStaker{
				NodeID:      staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime:   json.Uint64(staker.StartTime().Unix()),
				EndTime:     json.Uint64(staker.EndTime().Unix()),
				StakeAmount: &weight,
			})
		case *UnsignedAddValidatorTx:
			nodeID := staker.Validator.ID()
			weight := json.Uint64(staker.Validator.Weight())
			delegationFee := json.Float32(100 * float32(staker.Shares) / float32(PercentDenominator))

			service.vm.connLock.Lock()
			_, connected := service.vm.connections[nodeID.Key()]
			service.vm.connLock.Unlock()
			reply.Validators = append(reply.Validators, APIPrimaryValidator{
				APIStaker: APIStaker{
					NodeID:      staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
					StartTime:   json.Uint64(staker.StartTime().Unix()),
					EndTime:     json.Uint64(staker.EndTime().Unix()),
					StakeAmount: &weight,
				},
				DelegationFee: delegationFee,
				Connected:     &connected,
			})
		case *UnsignedAddSubnetValidatorTx:
			weight := json.Uint64(staker.Validator.Weight())
			reply.Validators = append(reply.Validators, APIStaker{
				NodeID:    staker.Validator.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime: json.Uint64(staker.StartTime().Unix()),
				EndTime:   json.Uint64(staker.EndTime().Unix()),
				Weight:    &weight,
			})
		default:
			return fmt.Errorf("expected validator but got %T", tx.UnsignedTx)
		}
	}
	return startIter.Error()
}

// GetCurrentSupplyReply are the results from calling GetCurrentSupply
type GetCurrentSupplyReply struct {
	Supply json.Uint64 `json:"supply"`
}

// GetCurrentSupply returns an upper bound on the supply of AVAX in the system
func (service *Service) GetCurrentSupply(_ *http.Request, _ *struct{}, reply *GetCurrentSupplyReply) error {
	supply, err := service.vm.getCurrentSupply(service.vm.DB)
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
	Validators []string `json:"validators"`
}

// SampleValidators returns a sampling of the list of current validators
func (service *Service) SampleValidators(_ *http.Request, args *SampleValidatorsArgs, reply *SampleValidatorsReply) error {
	service.vm.Ctx.Log.Info("Platform: SampleValidators called with Size = %d", args.Size)
	if args.SubnetID.IsZero() {
		args.SubnetID = constants.PrimaryNetworkID
	}

	validators, ok := service.vm.vdrMgr.GetValidators(args.SubnetID)
	if !ok {
		return fmt.Errorf("couldn't get validators of subnet with ID %s. Does it exist?", args.SubnetID)
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
	api.UserPass

	APIStaker
	// The address the staking reward, if applicable, will go to
	RewardAddress     string       `json:"rewardAddress"`
	DelegationFeeRate json.Float32 `json:"delegationFeeRate"`
}

// AddValidator creates and signs and issues a transaction to add a
// validator to the primary network
func (service *Service) AddValidator(_ *http.Request, args *AddValidatorArgs, reply *api.JsonTxID) error {
	service.vm.Ctx.Log.Info("Platform: AddValidator called")
	switch {
	case args.RewardAddress == "":
		return errNoRewardAddress
	case uint64(args.StartTime) < service.vm.clock.Unix():
		return fmt.Errorf("start time must be in the future")
	case args.DelegationFeeRate < 0 || args.DelegationFeeRate > 100:
		return errInvalidDelegationRate
	}

	var nodeID ids.ShortID
	if args.NodeID == "" {
		nodeID = service.vm.Ctx.NodeID
	} else {
		nID, err := ids.ShortFromPrefixedString(args.NodeID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeID = nID
	}

	rewardAddress, err := service.vm.ParseLocalAddress(args.RewardAddress)
	if err != nil {
		return fmt.Errorf("problem while parsing reward address: %w", err)
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newAddValidatorTx(
		uint64(args.weight()),                // Stake amount
		uint64(args.StartTime),               // Start time
		uint64(args.EndTime),                 // End time
		nodeID,                               // Node ID
		rewardAddress,                        // Reward Address
		uint32(10000*args.DelegationFeeRate), // Shares
		privKeys,                             // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()

	errs := wrappers.Errs{}
	errs.Add(
		service.vm.issueTx(tx),
		db.Close(),
	)
	return errs.Err
}

// AddDelegatorArgs are the arguments to AddDelegator
type AddDelegatorArgs struct {
	api.UserPass
	APIStaker
	RewardAddress string `json:"rewardAddress"`
}

// AddDelegator creates and signs and issues a transaction to add a
// delegator to the primary network
func (service *Service) AddDelegator(_ *http.Request, args *AddDelegatorArgs, reply *api.JsonTxID) error {
	service.vm.Ctx.Log.Info("Platform: AddDelegator called")
	switch {
	case int64(args.StartTime) < time.Now().Unix():
		return fmt.Errorf("start time must be in the future")
	case args.RewardAddress == "":
		return errNoRewardAddress
	}

	var nodeID ids.ShortID
	if args.NodeID == "" { // If ID unspecified, use this node's ID as validator ID
		nodeID = service.vm.Ctx.NodeID
	} else {
		nID, err := ids.ShortFromPrefixedString(args.NodeID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeID = nID
	}

	rewardAddress, err := service.vm.ParseLocalAddress(args.RewardAddress)
	if err != nil {
		return fmt.Errorf("problem parsing 'rewardAddress': %w", err)
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newAddDelegatorTx(
		uint64(args.weight()),  // Stake amount
		uint64(args.StartTime), // Start time
		uint64(args.EndTime),   // End time
		nodeID,                 // Node ID
		rewardAddress,          // Reward Address
		privKeys,               // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()

	errs := wrappers.Errs{}
	errs.Add(
		service.vm.issueTx(tx),
		db.Close(),
	)
	return errs.Err
}

// AddSubnetValidatorArgs are the arguments to AddSubnetValidator
type AddSubnetValidatorArgs struct {
	APIStaker
	api.UserPass
	// ID of subnet to validate
	SubnetID string `json:"subnetID"`
}

// AddSubnetValidator creates and signs and issues a transaction to
// add a validator to a subnet other than the primary network
func (service *Service) AddSubnetValidator(_ *http.Request, args *AddSubnetValidatorArgs, response *api.JsonTxID) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: AddSubnetValidator called")
	switch {
	case args.SubnetID == "":
		return errNoSubnetID
	}

	nodeID, err := ids.ShortFromPrefixedString(args.NodeID, constants.NodeIDPrefix)
	if err != nil {
		return fmt.Errorf("error parsing nodeID: '%s': %w", args.NodeID, err)
	}

	subnetID, err := ids.FromString(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem parsing subnetID '%s': %w", args.SubnetID, err)
	}
	if subnetID.Equals(constants.PrimaryNetworkID) {
		return errors.New("subnet validator attempts to validate primary network")
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	keys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newAddSubnetValidatorTx(
		uint64(args.weight()),  // Stake amount
		uint64(args.StartTime), // Start time
		uint64(args.EndTime),   // End time
		nodeID,                 // Node ID
		subnetID,               // Subnet ID
		keys,                   // Keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()

	errs := wrappers.Errs{}
	errs.Add(
		service.vm.issueTx(tx),
		db.Close(),
	)
	return errs.Err
}

// CreateSubnetArgs are the arguments to CreateSubnet
type CreateSubnetArgs struct {
	// The ID member of APISubnet is ignored
	APISubnet
	api.UserPass
}

// CreateSubnet creates and signs and issues a transaction to create a new
// subnet
func (service *Service) CreateSubnet(_ *http.Request, args *CreateSubnetArgs, response *api.JsonTxID) error {
	service.vm.Ctx.Log.Info("Platform: CreateSubnet called")

	controlKeys := []ids.ShortID{}
	for _, controlKey := range args.ControlKeys {
		controlKeyID, err := service.vm.ParseLocalAddress(controlKey)
		if err != nil {
			return fmt.Errorf("problem parsing control key '%s': %w", controlKey, err)
		}
		controlKeys = append(controlKeys, controlKeyID)
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newCreateSubnetTx(
		uint32(args.Threshold), // Threshold
		controlKeys,            // Control Addresses
		privKeys,               // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()

	errs := wrappers.Errs{}
	errs.Add(
		service.vm.issueTx(tx),
		db.Close(),
	)
	return errs.Err
}

// ExportAVAXArgs are the arguments to ExportAVAX
type ExportAVAXArgs struct {
	api.UserPass

	// Amount of AVAX to send
	Amount json.Uint64 `json:"amount"`

	// ID of the address that will receive the AVAX. This address includes the
	// chainID, which is used to determine what the destination chain is.
	To string `json:"to"`
}

// ExportAVAX exports AVAX from the P-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (service *Service) ExportAVAX(_ *http.Request, args *ExportAVAXArgs, response *api.JsonTxID) error {
	service.vm.Ctx.Log.Info("Platform: ExportAVAX called")

	if args.Amount == 0 {
		return errors.New("argument 'amount' must be > 0")
	}

	chainID, to, err := service.vm.ParseAddress(args.To)
	if err != nil {
		return err
	}

	// Get this user's data
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newExportTx(
		uint64(args.Amount), // Amount
		chainID,             // ID of the chain to send the funds to
		to,                  // Address
		privKeys,            // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()

	errs := wrappers.Errs{}
	errs.Add(
		service.vm.issueTx(tx),
		db.Close(),
	)
	return errs.Err
}

// ImportAVAXArgs are the arguments to ImportAVAX
type ImportAVAXArgs struct {
	api.UserPass

	// Chain the funds are coming from
	SourceChain string `json:"sourceChain"`

	// The address that will receive the imported funds
	To string `json:"to"`
}

// ImportAVAX issues a transaction to import AVAX from the X-chain. The AVAX
// must have already been exported from the X-Chain.
func (service *Service) ImportAVAX(_ *http.Request, args *ImportAVAXArgs, response *api.JsonTxID) error {
	service.vm.Ctx.Log.Info("Platform: ImportAVAX called")

	chainID, err := service.vm.Ctx.BCLookup.Lookup(args.SourceChain)
	if err != nil {
		return fmt.Errorf("problem parsing chainID %q: %w", args.SourceChain, err)
	}

	to, err := service.vm.ParseLocalAddress(args.To)
	if err != nil { // Parse address
		return fmt.Errorf("couldn't parse argument 'to' to an address: %w", err)
	}

	// Get the user's info
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil { // Get keys
		return fmt.Errorf("couldn't get keys controlled by the user: %w", err)
	}

	tx, err := service.vm.newImportTx(chainID, to, privKeys)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()

	errs := wrappers.Errs{}
	errs.Add(
		service.vm.issueTx(tx),
		db.Close(),
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
	api.UserPass
	// ID of Subnet that validates the new blockchain
	SubnetID ids.ID `json:"subnetID"`
	// ID of the VM the new blockchain is running
	VMID string `json:"vmID"`
	// IDs of the FXs the VM is running
	FxIDs []string `json:"fxIDs"`
	// Human-readable name for the new blockchain, not necessarily unique
	Name string `json:"name"`
	// Genesis state of the blockchain being created
	GenesisData formatting.CB58 `json:"genesisData"`
}

// CreateBlockchain issues a transaction to create a new blockchain
func (service *Service) CreateBlockchain(_ *http.Request, args *CreateBlockchainArgs, response *api.JsonTxID) error {
	service.vm.Ctx.Log.Info("Platform: CreateBlockchain called")
	switch {
	case args.Name == "":
		return errors.New("argument 'name' not given")
	case args.VMID == "":
		return errors.New("argument 'vmID' not given")
	}

	vmID, err := service.vm.chainManager.LookupVM(args.VMID)
	if err != nil {
		return fmt.Errorf("no VM with ID '%s' found", args.VMID)
	}

	fxIDs := []ids.ID(nil)
	for _, fxIDStr := range args.FxIDs {
		fxID, err := service.vm.chainManager.LookupVM(fxIDStr)
		if err != nil {
			return fmt.Errorf("no FX with ID '%s' found", fxIDStr)
		}
		fxIDs = append(fxIDs, fxID)
	}
	// If creating AVM instance, use secp256k1fx
	// TODO: Document FXs and have user specify them in API call
	fxIDsSet := ids.Set{}
	fxIDsSet.Add(fxIDs...)
	if vmID.Equals(avm.ID) && !fxIDsSet.Contains(secp256k1fx.ID) {
		fxIDs = append(fxIDs, secp256k1fx.ID)
	}

	if args.SubnetID.Equals(constants.PrimaryNetworkID) {
		return errDSCantValidate
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}

	// Drop any potential error closing the database to report the original
	// error
	defer db.Close()

	user := user{db: db}
	keys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newCreateChainTx(
		args.SubnetID,
		args.GenesisData.Bytes,
		vmID,
		fxIDs,
		args.Name,
		keys,
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()

	errs := wrappers.Errs{}
	errs.Add(
		service.vm.issueTx(tx),
		db.Close(),
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
	Status Status `json:"status"`
}

// GetBlockchainStatus gets the status of a blockchain with the ID [args.BlockchainID].
func (service *Service) GetBlockchainStatus(_ *http.Request, args *GetBlockchainStatusArgs, reply *GetBlockchainStatusReply) error {
	service.vm.Ctx.Log.Info("Platform: GetBlockchainStatus called")
	switch {
	case args.BlockchainID == "":
		return errors.New("argument 'blockchainID' not given")
	}

	if _, err := service.vm.chainManager.Lookup(args.BlockchainID); err == nil {
		reply.Status = Validating
		return nil
	} else if blockchainID, err := ids.FromString(args.BlockchainID); err != nil {
		return fmt.Errorf("problem parsing blockchainID '%s': %w", args.BlockchainID, err)
	} else if exists, err := service.chainExists(service.vm.LastAccepted(), blockchainID); err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	} else if exists {
		reply.Status = Created
		return nil
	} else if exists, err := service.chainExists(service.vm.Preferred(), blockchainID); err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	} else if exists {
		reply.Status = Preferred
		return nil
	}
	return nil
}

func (service *Service) chainExists(blockID ids.ID, chainID ids.ID) (bool, error) {
	blockIntf, err := service.vm.getBlock(blockID)
	if err != nil {
		return false, err
	}

	block, ok := blockIntf.(decision)
	if !ok {
		block, ok = blockIntf.parentBlock().(decision)
		if !ok {
			return false, errMissingDecisionBlock
		}
	}
	db := block.onAccept()

	chains, err := service.vm.getChains(db)
	if err != nil {
		return false, err
	}

	for _, chain := range chains {
		if chain.ID().Equals(chainID) {
			return true, nil
		}
	}
	return false, nil
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
	service.vm.Ctx.Log.Info("Platform: ValidatedBy called")
	chain, err := service.vm.getChain(service.vm.DB, args.BlockchainID)
	if err != nil {
		return fmt.Errorf("problem retrieving blockchain '%s': %w", args.BlockchainID, err)
	}
	response.SubnetID = chain.UnsignedTx.(*UnsignedCreateChainTx).SubnetID
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
	service.vm.Ctx.Log.Info("Platform: Validates called")
	// Verify that the Subnet exists
	// Ignore lookup error if it's the PrimaryNetworkID
	if _, err := service.vm.getSubnet(service.vm.DB, args.SubnetID); err != nil && !args.SubnetID.Equals(constants.PrimaryNetworkID) {
		return fmt.Errorf("problem retrieving subnet '%s': %w", args.SubnetID, err)
	}
	// Get the chains that exist
	chains, err := service.vm.getChains(service.vm.DB)
	if err != nil {
		return fmt.Errorf("problem retrieving chains for subnet '%s': %w", args.SubnetID, err)
	}
	// Filter to get the chains validated by the specified Subnet
	for _, chain := range chains {
		if chain.UnsignedTx.(*UnsignedCreateChainTx).SubnetID.Equals(args.SubnetID) {
			response.BlockchainIDs = append(response.BlockchainIDs, chain.ID())
		}
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
	service.vm.Ctx.Log.Info("Platform: GetBlockchains called")
	chains, err := service.vm.getChains(service.vm.DB)
	if err != nil {
		return fmt.Errorf("couldn't retrieve blockchains: %w", err)
	}

	for _, chain := range chains {
		uChain := chain.UnsignedTx.(*UnsignedCreateChainTx)
		response.Blockchains = append(response.Blockchains, APIBlockchain{
			ID:       uChain.ID(),
			Name:     uChain.ChainName,
			SubnetID: uChain.SubnetID,
			VMID:     uChain.VMID,
		})
	}
	return nil
}

// IssueTxArgs ...
type IssueTxArgs struct {
	// Raw byte representation of the transaction
	Tx formatting.CB58 `json:"tx"`
}

// IssueTxResponse ...
type IssueTxResponse struct {
	TxID ids.ID `json:"txID"`
}

// IssueTx issues a tx
func (service *Service) IssueTx(_ *http.Request, args *IssueTxArgs, response *IssueTxResponse) error {
	service.vm.Ctx.Log.Info("Platform: IssueTx called")

	tx := &Tx{}
	if err := service.vm.codec.Unmarshal(args.Tx.Bytes, tx); err != nil {
		return fmt.Errorf("couldn't parse tx: %w", err)
	}
	if err := service.vm.issueTx(tx); err != nil {
		return fmt.Errorf("couldn't issue tx: %w", err)
	}

	response.TxID = tx.ID()
	return nil
}

// GetTxArgs ...
type GetTxArgs struct {
	TxID ids.ID `json:"txID"`
}

// GetTxResponse ...
type GetTxResponse struct {
	// Raw byte representation of the transaction
	Tx formatting.CB58 `json:"tx"`
}

// GetTx gets a tx
func (service *Service) GetTx(_ *http.Request, args *GetTxArgs, response *GetTxResponse) error {
	service.vm.Ctx.Log.Info("Platform: GetTx called")
	txBytes, err := service.vm.getTx(service.vm.DB, args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get tx: %w", err)
	}
	response.Tx.Bytes = txBytes
	return nil
}

// GetTxStatusArgs ...
type GetTxStatusArgs struct {
	TxID ids.ID `json:"txID"`
}

// GetTxStatus gets a tx's status
func (service *Service) GetTxStatus(_ *http.Request, args *GetTxStatusArgs, response *Status) error {
	service.vm.Ctx.Log.Info("Platform: GetTxStatus called")
	status, err := service.vm.getStatus(service.vm.DB, args.TxID)
	if err == nil { // Found the status. Report it.
		*response = status
		return nil
	}
	// The status of this transaction is not in the database.
	// Check if the tx is in the preferred block's db. If so, return that it's processing.
	preferred, err := service.vm.getBlock(service.vm.Preferred())
	if err != nil {
		service.vm.Ctx.Log.Error("couldn't get preferred block: %s", err)
		*response = Unknown
		return nil
	}
	if block, ok := preferred.(decision); ok {
		if _, err := service.vm.getStatus(block.onAccept(), args.TxID); err == nil {
			*response = Processing // Found the status in the preferred block's db. Report tx is processing.
			return nil
		}
	}
	if _, ok := service.vm.droppedTxCache.Get(args.TxID); ok {
		*response = Dropped
	} else {
		*response = Unknown
	}
	return nil
}

// GetStakeReply is the response from calling GetStake.
type GetStakeReply struct {
	Staked json.Uint64 `json:"staked"`
}

// GetStake returns the amount of nAVAX that [args.Addresses] have cumulatively
// staked on the Primary Network.
//
// This method assumes that each stake output has only owner
// This method assumes only AVAX can be staked
// This method only concerns itself with the Primary Network, not subnets
// TODO: Improve the performance of this method by maintaining this data
// in a data structure rather than re-calculating it by iterating over stakers
func (service *Service) GetStake(_ *http.Request, args *api.JsonAddresses, response *GetStakeReply) error {
	service.vm.Ctx.Log.Info("Platform: GetStake called")

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

	// Takes in byte repr. of a staker.
	// Returns the amount staked that belongs to an address in [addrs]
	helper := func(tx *Tx) (uint64, error) {
		var outs []*avax.TransferableOutput
		switch staker := tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
			outs = staker.Stake
		case *UnsignedAddValidatorTx:
			outs = staker.Stake
		}

		var (
			amount uint64
			err    error
		)
		for _, stake := range outs {
			if !stake.AssetID().Equals(service.vm.Ctx.AVAXAssetID) {
				continue
			}
			secpOut, ok := stake.Out.(*secp256k1fx.TransferOutput)
			if !ok {
				continue
			}
			contains := false
			for _, addr := range secpOut.Addrs {
				if addrs.Contains(addr) {
					contains = true
					break
				}
			}
			if !contains {
				continue
			}
			amount, err = math.Add64(amount, stake.Out.Amount())
			if err != nil {
				return 0, err
			}
		}
		return amount, nil
	}

	var totalStake uint64

	stopPrefix := []byte(fmt.Sprintf("%s%s", constants.PrimaryNetworkID, stopDBPrefix))
	stopDB := prefixdb.NewNested(stopPrefix, service.vm.DB)
	defer stopDB.Close()
	stopIter := stopDB.NewIterator()
	defer stopIter.Release()

	for stopIter.Next() { // Iterates over current stakers
		stakerBytes := stopIter.Value()

		tx := rewardTx{}
		if err := service.vm.codec.Unmarshal(stakerBytes, &tx); err != nil {
			return fmt.Errorf("couldn't unmarshal validator tx: %w", err)
		}
		if err := tx.Tx.Sign(service.vm.codec, nil); err != nil {
			return err
		}

		staked, err := helper(&tx.Tx)
		if err != nil {
			return err
		}
		totalStake, err = math.Add64(totalStake, staked)
		if err != nil {
			return err
		}
	}
	if err := stopIter.Error(); err != nil {
		return fmt.Errorf("iterator errored: %w", err)
	}

	// Iterate over pending validators
	startPrefix := []byte(fmt.Sprintf("%s%s", constants.PrimaryNetworkID, startDBPrefix))
	startDB := prefixdb.NewNested(startPrefix, service.vm.DB)
	defer startDB.Close()
	startIter := startDB.NewIterator()
	defer startIter.Release()

	for startIter.Next() { // Iterates over current stakers
		stakerBytes := startIter.Value()

		tx := Tx{}
		if err := service.vm.codec.Unmarshal(stakerBytes, &tx); err != nil {
			return fmt.Errorf("couldn't unmarshal validator tx: %w", err)
		}
		if err := tx.Sign(service.vm.codec, nil); err != nil {
			return err
		}

		staked, err := helper(&tx)
		if err != nil {
			return err
		}
		totalStake, err = math.Add64(totalStake, staked)
		if err != nil {
			return err
		}
	}
	if err := stopIter.Error(); err != nil {
		return fmt.Errorf("iterator errored: %w", err)
	}

	response.Staked = json.Uint64(totalStake)
	return nil
}

// GetMinStakeReply is the response from calling GetMinStake.
type GetMinStakeReply struct {
	MinStake json.Uint64 `json:"minStake"`
}

// GetMinStake returns the minimum staking amount in nAVAX.
func (service *Service) GetMinStake(_ *http.Request, _ *struct{}, reply *GetMinStakeReply) error {
	reply.MinStake = json.Uint64(service.vm.minStake)
	return nil
}
