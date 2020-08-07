// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ava-labs/gecko/api"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errMissingDecisionBlock = errors.New("should have a decision block within the past two blocks")
	errNoFunds              = errors.New("no spendable funds were found")
	errNoUsername           = errors.New("argument 'username' not provided")
	errNoPassword           = errors.New("argument 'password' not provided")
	errNoSubnetID           = errors.New("argument 'subnetID' not provided")
	errNoDestination        = errors.New("argument 'destination' not provided")
	errUnexpectedTxType     = errors.New("expected tx to be a DecisionTx, ProposalTx or AtomicTx but is not")
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
	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	if address, err := service.vm.ParseAddress(args.Address); err != nil {
		return fmt.Errorf("couldn't parse %s to address: %s", args.Address, err)
	} else if sk, err := user.getKey(address); err != nil {
		return fmt.Errorf("problem retrieving private key: %w", err)
	} else {
		reply.PrivateKey = constants.SecretKeyPrefix + formatting.CB58{Bytes: sk.Bytes()}.String()
		return nil
	}
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	api.UserPass
	PrivateKey string `json:"privateKey"`
}

// ImportKeyReply is the response for ImportKey
type ImportKeyReply struct {
	// The address controlled by the PrivateKey provided in the arguments
	Address string `json:"address"`
}

// ImportKey adds a private key to the provided user
func (service *Service) ImportKey(r *http.Request, args *ImportKeyArgs, reply *ImportKeyReply) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ImportKey called for user '%s'", args.Username)
	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving data: %w", err)
	}

	user := user{db: db}

	factory := crypto.FactorySECP256K1R{}

	if !strings.HasPrefix(args.PrivateKey, constants.SecretKeyPrefix) {
		return fmt.Errorf("private key missing %s prefix", constants.SecretKeyPrefix)
	}
	trimmedPrivateKey := strings.TrimPrefix(args.PrivateKey, constants.SecretKeyPrefix)
	formattedPrivateKey := formatting.CB58{}
	if err := formattedPrivateKey.FromString(trimmedPrivateKey); err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}

	skIntf, err := factory.ToPrivateKey(formattedPrivateKey.Bytes)
	if err != nil {
		return fmt.Errorf("problem parsing private key: %w", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	if err := user.putAddress(sk); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}

	reply.Address, err = service.vm.FormatAddress(sk.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}
	return nil
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
	Balance json.Uint64   `json:"balance"`
	UTXOIDs []*ava.UTXOID `json:"utxoIDs"`
}

// GetBalance gets the balance of an address
func (service *Service) GetBalance(_ *http.Request, args *GetBalanceArgs, response *GetBalanceResponse) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: GetBalance called for address %s", args.Address)

	// Parse to address
	address, err := service.vm.ParseAddress(args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse argument 'address' to address: %w", err)
	}

	addrs := [][]byte{address.Bytes()}
	utxos, err := service.vm.getUTXOs(service.vm.DB, addrs)
	if err != nil {
		addr, err2 := service.vm.FormatAddress(address)
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
func (service *Service) CreateAddress(_ *http.Request, args *api.UserPass, response *api.AddressResponse) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: CreateAddress called")

	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	factory := crypto.FactorySECP256K1R{}
	key, err := factory.NewPrivateKey()
	if err != nil {
		return fmt.Errorf("couldn't create key: %w", err)
	} else if err := user.putAddress(key.(*crypto.PrivateKeySECP256K1R)); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}
	response.Address, err = service.vm.FormatAddress(key.PublicKey().Address())
	if err != nil {
		return fmt.Errorf("problem formatting address: %w", err)
	}
	return nil
}

// ListAddresses returns the addresses controlled by [args.Username]
func (service *Service) ListAddresses(_ *http.Request, args *api.UserPass, response *api.AddressesResponse) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ListAddresses called")

	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	addresses, err := user.getAddresses()
	if err != nil {
		return fmt.Errorf("couldn't get addresses: %w", err)
	}
	response.Addresses = make([]string, len(addresses))
	for i, addr := range addresses {
		response.Addresses[i], err = service.vm.FormatAddress(addr)
		if err != nil {
			return fmt.Errorf("problem formatting address: %w", err)
		}
	}
	return nil
}

// GetUTXOsArgs ...
type GetUTXOsArgs struct {
	Addresses []string `json:"addresses"`
}

// GetUTXOsResponse ...
type GetUTXOsResponse struct {
	UTXOs []formatting.CB58 `json:"utxos"`
}

// GetUTXOs returns the UTXOs controlled by the given addresses
func (service *Service) GetUTXOs(_ *http.Request, args *GetUTXOsArgs, response *GetUTXOsResponse) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ListAddresses called")

	addrs := [][]byte{}
	for _, addrStr := range args.Addresses {
		addr, err := service.vm.ParseAddress(addrStr)
		if err != nil {
			return fmt.Errorf("can't parse %s to address: %w", addr, err)
		}
		addrs = append(addrs, addr.Bytes())
	}

	utxos, err := service.vm.getUTXOs(service.vm.DB, addrs)
	if err != nil {
		return fmt.Errorf("couldn't get UTXOs: %w", err)
	}

	response.UTXOs = make([]formatting.CB58, len(utxos))
	for i, utxo := range utxos {
		bytes, err := service.vm.codec.Marshal(utxo)
		if err != nil {
			return fmt.Errorf("couldn't serialize UTXO %s: %w", utxo.InputID(), err)
		}
		response.UTXOs[i] = formatting.CB58{Bytes: bytes}
	}
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
	// Null if there are no subnets other than the default subnet
	Subnets []APISubnet `json:"subnets"`
}

// GetSubnets returns the subnets whose ID are in [args.IDs]
// The response will include the default subnet
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
			unsignedTx := subnet.UnsignedDecisionTx.(*UnsignedCreateSubnetTx)
			owner := unsignedTx.Owner.(*secp256k1fx.OutputOwners)
			controlAddrs := []string{}
			for _, controlKeyID := range owner.Addrs {
				addr, err := service.vm.FormatAddress(controlKeyID)
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
		// Include Default Subnet
		response.Subnets[len(subnets)] = APISubnet{
			ID:          constants.DefaultSubnetID,
			ControlKeys: []string{},
			Threshold:   json.Uint32(0),
		}
		return nil
	}

	idsSet := ids.Set{}
	idsSet.Add(args.IDs...)
	for _, subnet := range subnets {
		if idsSet.Contains(subnet.ID()) {
			unsignedTx := subnet.UnsignedDecisionTx.(*UnsignedCreateSubnetTx)
			owner := unsignedTx.Owner.(*secp256k1fx.OutputOwners)
			controlAddrs := []string{}
			for _, controlKeyID := range owner.Addrs {
				addr, err := service.vm.FormatAddress(controlKeyID)
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
	if idsSet.Contains(constants.DefaultSubnetID) {
		response.Subnets = append(response.Subnets,
			APISubnet{
				ID:          constants.DefaultSubnetID,
				ControlKeys: []string{},
				Threshold:   json.Uint32(0),
			},
		)
	}
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
	// If omitted, defaults to default subnet
	SubnetID ids.ID `json:"subnetID"`
}

// GetCurrentValidatorsReply are the results from calling GetCurrentValidators
type GetCurrentValidatorsReply struct {
	Validators []FormattedAPIValidator `json:"validators"`
}

// GetCurrentValidators returns the list of current validators
func (service *Service) GetCurrentValidators(_ *http.Request, args *GetCurrentValidatorsArgs, reply *GetCurrentValidatorsReply) error {
	service.vm.Ctx.Log.Info("Platform: GetCurrentValidators called")
	if args.SubnetID.IsZero() {
		args.SubnetID = constants.DefaultSubnetID
	}

	validators, err := service.vm.getCurrentValidators(service.vm.DB, args.SubnetID)
	if err != nil {
		return fmt.Errorf("couldn't get validators of subnet with ID %s. Does it exist?", args.SubnetID)
	}

	reply.Validators = make([]FormattedAPIValidator, validators.Len())
	if args.SubnetID.Equals(constants.DefaultSubnetID) {
		for i, tx := range validators.Txs {
			switch tx := tx.UnsignedProposalTx.(type) {
			case *UnsignedAddDefaultSubnetValidatorTx:
				vdr := tx.Vdr()
				weight := json.Uint64(vdr.Weight())
				reply.Validators[i] = FormattedAPIValidator{
					ID:          vdr.ID().PrefixedString(constants.NodeIDPrefix),
					StartTime:   json.Uint64(tx.StartTime().Unix()),
					EndTime:     json.Uint64(tx.EndTime().Unix()),
					StakeAmount: &weight,
				}
			case *UnsignedAddDefaultSubnetDelegatorTx:
				vdr := tx.Vdr()
				weight := json.Uint64(vdr.Weight())
				reply.Validators[i] = FormattedAPIValidator{
					ID:          vdr.ID().PrefixedString(constants.NodeIDPrefix),
					StartTime:   json.Uint64(tx.StartTime().Unix()),
					EndTime:     json.Uint64(tx.EndTime().Unix()),
					StakeAmount: &weight,
				}
			default: // Shouldn't happen
				return fmt.Errorf("couldn't get the destination address of %s", tx.ID())
			}
		}
	} else {
		for i, tx := range validators.Txs {
			utx := tx.UnsignedProposalTx.(*UnsignedAddNonDefaultSubnetValidatorTx)
			vdr := utx.Vdr()
			weight := json.Uint64(vdr.Weight())
			reply.Validators[i] = FormattedAPIValidator{
				ID:        vdr.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime: json.Uint64(utx.StartTime().Unix()),
				EndTime:   json.Uint64(utx.EndTime().Unix()),
				Weight:    &weight,
			}
		}
	}

	return nil
}

// GetPendingValidatorsArgs are the arguments for calling GetPendingValidators
type GetPendingValidatorsArgs struct {
	// Subnet we're getting the pending validators of
	// If omitted, defaults to default subnet
	SubnetID ids.ID `json:"subnetID"`
}

// GetPendingValidatorsReply are the results from calling GetPendingValidators
type GetPendingValidatorsReply struct {
	Validators []FormattedAPIValidator `json:"validators"`
}

// GetPendingValidators returns the list of current validators
func (service *Service) GetPendingValidators(_ *http.Request, args *GetPendingValidatorsArgs, reply *GetPendingValidatorsReply) error {
	service.vm.Ctx.Log.Info("Platform: GetPendingValidators called")
	if args.SubnetID.IsZero() {
		args.SubnetID = constants.DefaultSubnetID
	}

	validators, err := service.vm.getPendingValidators(service.vm.DB, args.SubnetID)
	if err != nil {
		return fmt.Errorf("couldn't get validators of subnet with ID %s. Does it exist?", args.SubnetID)
	}

	reply.Validators = make([]FormattedAPIValidator, validators.Len())
	for i, tx := range validators.Txs {
		if args.SubnetID.Equals(constants.DefaultSubnetID) {
			switch tx := tx.UnsignedProposalTx.(type) {
			case *UnsignedAddDefaultSubnetValidatorTx:
				vdr := tx.Vdr()
				weight := json.Uint64(vdr.Weight())
				reply.Validators[i] = FormattedAPIValidator{
					ID:          vdr.ID().PrefixedString(constants.NodeIDPrefix),
					StartTime:   json.Uint64(tx.StartTime().Unix()),
					EndTime:     json.Uint64(tx.EndTime().Unix()),
					StakeAmount: &weight,
				}
			case *UnsignedAddDefaultSubnetDelegatorTx:
				vdr := tx.Vdr()
				weight := json.Uint64(vdr.Weight())
				reply.Validators[i] = FormattedAPIValidator{
					ID:          vdr.ID().PrefixedString(constants.NodeIDPrefix),
					StartTime:   json.Uint64(tx.StartTime().Unix()),
					EndTime:     json.Uint64(tx.EndTime().Unix()),
					StakeAmount: &weight,
				}
			default: // Shouldn't happen
				return fmt.Errorf("couldn't get the destination address of %s", tx.ID())
			}
		} else {
			utx := tx.UnsignedProposalTx.(*UnsignedAddNonDefaultSubnetValidatorTx)
			vdr := utx.Vdr()
			weight := json.Uint64(vdr.Weight())
			reply.Validators[i] = FormattedAPIValidator{
				ID:        vdr.ID().PrefixedString(constants.NodeIDPrefix),
				StartTime: json.Uint64(utx.StartTime().Unix()),
				EndTime:   json.Uint64(utx.EndTime().Unix()),
				Weight:    &weight,
			}
		}
	}

	return nil
}

// SampleValidatorsArgs are the arguments for calling SampleValidators
type SampleValidatorsArgs struct {
	// Number of validators in the sample
	Size json.Uint16 `json:"size"`

	// ID of subnet to sample validators from
	// If omitted, defaults to the default subnet
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
		args.SubnetID = constants.DefaultSubnetID
	}

	validators, ok := service.vm.validators.GetValidatorSet(args.SubnetID)
	if !ok {
		return fmt.Errorf("couldn't get validators of subnet with ID %s. Does it exist?", args.SubnetID)
	}

	sample := validators.Sample(int(args.Size))
	if setLen := len(sample); setLen != int(args.Size) {
		return fmt.Errorf("current number of validators (%d) is insufficient to sample %d validators", setLen, args.Size)
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

// AddDefaultSubnetValidatorArgs are the arguments to AddDefaultSubnetValidator
type AddDefaultSubnetValidatorArgs struct {
	FormattedAPIDefaultSubnetValidator

	api.UserPass
}

// AddDefaultSubnetValidator returns an unsigned transaction to add a validator to the default subnet
// The returned unsigned transaction should be signed using Sign()
func (service *Service) AddDefaultSubnetValidator(_ *http.Request, args *AddDefaultSubnetValidatorArgs, reply *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: AddDefaultSubnetValidator called")
	switch {
	case args.Destination == "":
		return errNoDestination
	case uint64(args.StartTime) < service.vm.clock.Unix():
		return fmt.Errorf("start time must be in the future")
	}

	var nodeID ids.ShortID
	if args.ID == "" {
		nodeID = service.vm.Ctx.NodeID
	} else {
		nID, err := ids.ShortFromPrefixedString(args.ID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeID = nID
	}

	destination, err := service.vm.ParseAddress(args.Destination)
	if err != nil {
		return fmt.Errorf("problem while parsing destination: %w", err)
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newAddDefaultSubnetValidatorTx(
		uint64(args.weight()),          // Stake amount
		uint64(args.StartTime),         // Start time
		uint64(args.EndTime),           // End time
		nodeID,                         // Node ID
		destination,                    // Destination
		uint32(args.DelegationFeeRate), // Shares
		privKeys,                       // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()
	return service.vm.issueTx(tx)
}

// AddDefaultSubnetDelegatorArgs are the arguments to AddDefaultSubnetDelegator
type AddDefaultSubnetDelegatorArgs struct {
	FormattedAPIValidator
	api.UserPass
	Destination string `json:"destination"`
}

// AddDefaultSubnetDelegator returns an unsigned transaction to add a delegator
// to the default subnet
// The returned unsigned transaction should be signed using Sign()
func (service *Service) AddDefaultSubnetDelegator(_ *http.Request, args *AddDefaultSubnetDelegatorArgs, reply *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: AddDefaultSubnetDelegator called")
	switch {
	case int64(args.StartTime) < time.Now().Unix():
		return fmt.Errorf("start time must be in the future")
	case args.Destination == "":
		return errNoDestination
	}

	var nodeID ids.ShortID
	if args.ID == "" { // If ID unspecified, use this node's ID as validator ID
		nodeID = service.vm.Ctx.NodeID
	} else {
		nID, err := ids.ShortFromPrefixedString(args.ID, constants.NodeIDPrefix)
		if err != nil {
			return err
		}
		nodeID = nID
	}

	destination, err := service.vm.ParseAddress(args.Destination)
	if err != nil {
		return fmt.Errorf("problem parsing 'destination': %w", err)
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newAddDefaultSubnetDelegatorTx(
		uint64(args.weight()),  // Stake amount
		uint64(args.StartTime), // Start time
		uint64(args.EndTime),   // End time
		nodeID,                 // Node ID
		destination,            // Destination
		privKeys,               // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()
	return service.vm.issueTx(tx)
}

// AddNonDefaultSubnetValidatorArgs are the arguments to AddNonDefaultSubnetValidator
type AddNonDefaultSubnetValidatorArgs struct {
	FormattedAPIValidator
	api.UserPass
	// ID of subnet to validate
	SubnetID string `json:"subnetID"`
}

// AddNonDefaultSubnetValidator adds a validator to a subnet other than the default subnet
// Returns the unsigned transaction, which must be signed using Sign
func (service *Service) AddNonDefaultSubnetValidator(_ *http.Request, args *AddNonDefaultSubnetValidatorArgs, response *api.TxIDResponse) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: AddNonDefaultSubnetValidator called")
	switch {
	case args.SubnetID == "":
		return errNoSubnetID
	}

	nodeID, err := ids.ShortFromPrefixedString(args.ID, constants.NodeIDPrefix)
	if err != nil {
		return fmt.Errorf("Error parsing nodeID: '%s': %w", args.ID, err)
	}

	subnetID, err := ids.FromString(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem parsing subnetID '%s': %w", args.SubnetID, err)
	}
	if subnetID.Equals(constants.DefaultSubnetID) {
		return errors.New("non-default subnet validator attempts to validate default subnet")
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	keys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newAddNonDefaultSubnetValidatorTx(
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
	return service.vm.issueTx(tx)
}

// CreateSubnetArgs are the arguments to CreateSubnet
type CreateSubnetArgs struct {
	// The ID member of APISubnet is ignored
	APISubnet
	api.UserPass
}

// CreateSubnet returns an unsigned transaction to create a new subnet.
// The unsigned transaction must be signed with the key of [args.Payer]
func (service *Service) CreateSubnet(_ *http.Request, args *CreateSubnetArgs, response *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: CreateSubnet called")

	controlKeys := []ids.ShortID{}
	for _, controlKey := range args.ControlKeys {
		controlKeyID, err := service.vm.ParseAddress(controlKey)
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
	return service.vm.issueTx(tx)
}

// ExportAVAArgs are the arguments to ExportAVA
type ExportAVAArgs struct {
	api.UserPass
	To string `json:"to"`
	// Amount of nAVA to send
	Amount json.Uint64 `json:"amount"`
}

// ExportAVA exports AVAX from the P-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (service *Service) ExportAVA(_ *http.Request, args *ExportAVAArgs, response *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: ExportAVA called")
	xchainID := service.vm.avm
	chainPrefixes := []string{xchainID.String()}
	if alias, err := service.vm.Ctx.BCLookup.PrimaryAlias(xchainID); err == nil {
		chainPrefixes = append(chainPrefixes, alias)
	}

	ToBytes, err := formatting.ParseAddress(args.To, chainPrefixes, addressSep, service.vm.GetHRP())
	if err != nil {
		return err
	}

	ToID, err := ids.ToShortID(ToBytes)
	if err != nil {
		return err
	}

	if args.Amount == 0 {
		return errors.New("argument 'amount' must be > 0")
	}

	// Get this user's data
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get addresses controlled by the user: %w", err)
	}

	// Create the transaction
	tx, err := service.vm.newExportTx(
		uint64(args.Amount), // Amount
		ToID,                // X-Chain address
		privKeys,            // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	return service.vm.issueTx(tx)
}

// ImportAVAArgs are the arguments to ImportAVA
type ImportAVAArgs struct {
	api.UserPass
	// The address that will receive the imported funds
	To string `json:"to"`
}

// ImportAVA returns an unsigned transaction to import AVA from the X-Chain.
// The AVA must have already been exported from the X-Chain.
func (service *Service) ImportAVA(_ *http.Request, args *ImportAVAArgs, response *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: ImportAVA called")

	// Get the user's info
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get user '%s': %w", args.Username, err)
	}
	user := user{db: db}

	to, err := service.vm.ParseAddress(args.To)
	if err != nil { // Parse address
		return fmt.Errorf("couldn't parse argument 'to' to an address: %w", err)
	}

	privKeys, err := user.getKeys()
	if err != nil { // Get keys
		return fmt.Errorf("couldn't get keys controlled by the user: %w", err)
	}

	tx, err := service.vm.newImportTx(to, privKeys)
	if err != nil {
		return err
	}

	response.TxID = tx.ID()
	return service.vm.issueTx(tx)
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

// CreateBlockchain returns an unsigned transaction to create a new blockchain
// Must be signed with the Subnet's control keys and with a key that pays the transaction fee before issuance
func (service *Service) CreateBlockchain(_ *http.Request, args *CreateBlockchainArgs, response *api.TxIDResponse) error {
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

	if args.SubnetID.Equals(constants.DefaultSubnetID) {
		return errDSCantValidate
	}

	// Get the keys controlled by the user
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user '%s': %w", args.Username, err)
	}
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
	return service.vm.issueTx(tx)
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
		block, ok = blockIntf.Parent().(decision)
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
	response.SubnetID = chain.UnsignedDecisionTx.(*UnsignedCreateChainTx).SubnetID
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
	// Ignore lookup error if it's the DefaultSubnetID
	if _, err := service.vm.getSubnet(service.vm.DB, args.SubnetID); err != nil && !args.SubnetID.Equals(constants.DefaultSubnetID) {
		return fmt.Errorf("problem retrieving subnet '%s': %w", args.SubnetID, err)
	}
	// Get the chains that exist
	chains, err := service.vm.getChains(service.vm.DB)
	if err != nil {
		return fmt.Errorf("problem retrieving chains for subnet '%s': %w", args.SubnetID, err)
	}
	// Filter to get the chains validated by the specified Subnet
	for _, chain := range chains {
		if chain.UnsignedDecisionTx.(*UnsignedCreateChainTx).SubnetID.Equals(args.SubnetID) {
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
		uChain := chain.UnsignedDecisionTx.(*UnsignedCreateChainTx)
		response.Blockchains = append(response.Blockchains, APIBlockchain{
			ID:       uChain.ID(),
			Name:     uChain.ChainName,
			SubnetID: uChain.SubnetID,
			VMID:     uChain.VMID,
		})
	}
	return nil
}

// GetTxArgs ...
type GetTxArgs struct {
	TxID ids.ID `json:"txID"`
}

// GetTxResponse ...
type GetTxResponse struct {
	// Raw byte representation of the transaction
	RawTx formatting.CB58 `json:"rawTx"`
	// JSON representation of the transaction
	JSON interface{} `json:"json"`
}

// GetTx gets a tx
func (service *Service) GetTx(_ *http.Request, args *GetTxArgs, response *GetTxResponse) error {
	service.vm.Ctx.Log.Info("Platform: GetTx called")
	txBytes, err := service.vm.getTx(service.vm.DB, args.TxID)
	if err != nil {
		return fmt.Errorf("couldn't get tx: %w", err)
	}
	response.RawTx.Bytes = txBytes

	// Parse the raw bytes to a struct so we can get the JSON representation
	// We don't know what kind of tx this is, so we go through the possibilities
	// until we find the right one
	var (
		proposalTx ProposalTx
		decisionTx DecisionTx
		atomicTx   AtomicTx
	)
	if err := service.vm.codec.Unmarshal(txBytes, &proposalTx); err == nil {
		response.JSON = &proposalTx
	} else if err := service.vm.codec.Unmarshal(txBytes, &decisionTx); err == nil {
		response.JSON = &decisionTx
	} else if err := service.vm.codec.Unmarshal(txBytes, &atomicTx); err == nil {
		response.JSON = &atomicTx
	} else {
		return errUnexpectedTxType
	}
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
	switch block := preferred.(type) {
	case *Abort:
		if _, err := service.vm.getStatus(block.onAccept(), args.TxID); err == nil {
			*response = Processing // Found the status in the preferred block's db. Report tx is processing.
			return nil
		}
	case *Commit:
		if _, err := service.vm.getStatus(block.onAccept(), args.TxID); err == nil {
			*response = Processing
			return nil
		}
	case *AtomicBlock:
		if _, err := service.vm.getStatus(block.onAccept(), args.TxID); err == nil {
			*response = Processing
			return nil
		}
	case *StandardBlock:
		if _, err := service.vm.getStatus(block.onAccept(), args.TxID); err == nil {
			*response = Processing
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
