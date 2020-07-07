// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ava-labs/gecko/api"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errMissingDecisionBlock = errors.New("should have a decision block within the past two blocks")
	errNoFunds              = errors.New("no spendable funds were found")
	errNoUsername           = errors.New("argument 'username' not provided")
	errNoPassword           = errors.New("argument 'password' not provided")
	errNoSubnetID           = errors.New("argument 'subnetID' not provided")
	errNoDestination        = errors.New("argument 'destination' not provided")
)

// Service defines the API calls that can be made to the platform chain
type Service struct{ vm *VM }

// ExportKeyArgs are arguments for ExportKey
type ExportKeyArgs struct {
	api.UserPass
	Address ids.ShortID `json:"address"`
}

// ExportKeyReply is the response for ExportKey
type ExportKeyReply struct {
	// The decrypted PrivateKey for the Address provided in the arguments
	PrivateKey formatting.CB58 `json:"privateKey"`
}

// ExportKey returns a private key from the provided user
func (service *Service) ExportKey(r *http.Request, args *ExportKeyArgs, reply *ExportKeyReply) error {
	service.vm.SnowmanVM.Ctx.Log.Info("Platform: ExportKey called")
	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := user{db: db}

	sk, err := user.getKey(args.Address)
	if err != nil {
		return fmt.Errorf("problem retrieving private key: %w", err)
	}

	reply.PrivateKey.Bytes = sk.Bytes()
	return nil
}

// ImportKeyArgs are arguments for ImportKey
type ImportKeyArgs struct {
	Username   string          `json:"username"`
	Password   string          `json:"password"`
	PrivateKey formatting.CB58 `json:"privateKey"`
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
	skIntf, err := factory.ToPrivateKey(args.PrivateKey.Bytes)
	if err != nil {
		return fmt.Errorf("problem parsing private key %s: %w", args.PrivateKey, err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	if err := user.putAddress(sk); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}

	reply.Address = service.vm.FormatAddress(sk.PublicKey().Address())
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
	Threshold   json.Uint16 `json:"threshold"`
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
			controlAddrs := []string{}
			for _, controlKeyID := range subnet.ControlKeys {
				controlAddrs = append(controlAddrs, service.vm.FormatAddress(controlKeyID))
			}
			response.Subnets[i] = APISubnet{
				ID:          subnet.id,
				ControlKeys: controlAddrs,
				Threshold:   json.Uint16(subnet.Threshold),
			}
		}
		// Include Default Subnet
		response.Subnets[len(subnets)] = APISubnet{
			ID:          DefaultSubnetID,
			ControlKeys: []string{},
			Threshold:   json.Uint16(0),
		}
		return nil
	}

	idsSet := ids.Set{}
	idsSet.Add(args.IDs...)
	for _, subnet := range subnets {
		if idsSet.Contains(subnet.id) {
			controlAddrs := []string{}
			for _, controlKeyID := range subnet.ControlKeys {
				controlAddrs = append(controlAddrs, service.vm.FormatAddress(controlKeyID))
			}
			response.Subnets = append(response.Subnets,
				APISubnet{
					ID:          subnet.id,
					ControlKeys: controlAddrs,
					Threshold:   json.Uint16(subnet.Threshold),
				},
			)
		}
	}
	if idsSet.Contains(DefaultSubnetID) {
		response.Subnets = append(response.Subnets,
			APISubnet{
				ID:          DefaultSubnetID,
				ControlKeys: []string{},
				Threshold:   json.Uint16(0),
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
		args.SubnetID = DefaultSubnetID
	}

	validators, err := service.vm.getCurrentValidators(service.vm.DB, args.SubnetID)
	if err != nil {
		return fmt.Errorf("couldn't get validators of subnet with ID %s. Does it exist?", args.SubnetID)
	}

	reply.Validators = make([]FormattedAPIValidator, validators.Len())
	if args.SubnetID.Equals(DefaultSubnetID) {
		for i, tx := range validators.Txs {
			vdr := tx.Vdr()
			weight := json.Uint64(vdr.Weight())
			var address ids.ShortID
			switch tx := tx.(type) {
			case *addDefaultSubnetValidatorTx:
				address = tx.Destination
			case *addDefaultSubnetDelegatorTx:
				address = tx.Destination
			default: // Shouldn't happen
				return fmt.Errorf("couldn't get the destination address of %s", tx.ID())
			}

			reply.Validators[i] = FormattedAPIValidator{
				ID:          vdr.ID(),
				StartTime:   json.Uint64(tx.StartTime().Unix()),
				EndTime:     json.Uint64(tx.EndTime().Unix()),
				StakeAmount: &weight,
				Address:     service.vm.FormatAddress(address),
			}
		}
	} else {
		for i, tx := range validators.Txs {
			vdr := tx.Vdr()
			weight := json.Uint64(vdr.Weight())
			reply.Validators[i] = FormattedAPIValidator{
				ID:        vdr.ID(),
				StartTime: json.Uint64(tx.StartTime().Unix()),
				EndTime:   json.Uint64(tx.EndTime().Unix()),
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
		args.SubnetID = DefaultSubnetID
	}

	validators, err := service.vm.getPendingValidators(service.vm.DB, args.SubnetID)
	if err != nil {
		return fmt.Errorf("couldn't get validators of subnet with ID %s. Does it exist?", args.SubnetID)
	}

	reply.Validators = make([]FormattedAPIValidator, validators.Len())
	for i, tx := range validators.Txs {
		vdr := tx.Vdr()
		weight := json.Uint64(vdr.Weight())
		if args.SubnetID.Equals(DefaultSubnetID) {
			var address ids.ShortID
			switch tx := tx.(type) {
			case *addDefaultSubnetValidatorTx:
				address = tx.Destination
			case *addDefaultSubnetDelegatorTx:
				address = tx.Destination
			default: // Shouldn't happen
				return fmt.Errorf("couldn't get the destination address of %s", tx.ID())
			}
			reply.Validators[i] = FormattedAPIValidator{
				ID:          vdr.ID(),
				StartTime:   json.Uint64(tx.StartTime().Unix()),
				EndTime:     json.Uint64(tx.EndTime().Unix()),
				StakeAmount: &weight,
				Address:     service.vm.FormatAddress(address),
			}
		} else {
			reply.Validators[i] = FormattedAPIValidator{
				ID:        vdr.ID(),
				StartTime: json.Uint64(tx.StartTime().Unix()),
				EndTime:   json.Uint64(tx.EndTime().Unix()),
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
	Validators []ids.ShortID `json:"validators"`
}

// SampleValidators returns a sampling of the list of current validators
func (service *Service) SampleValidators(_ *http.Request, args *SampleValidatorsArgs, reply *SampleValidatorsReply) error {
	service.vm.Ctx.Log.Info("Platform: SampleValidators called with Size = %d", args.Size)
	if args.SubnetID.IsZero() {
		args.SubnetID = DefaultSubnetID
	}

	validators, ok := service.vm.validators.GetValidatorSet(args.SubnetID)
	if !ok {
		return fmt.Errorf("couldn't get validators of subnet with ID %s. Does it exist?", args.SubnetID)
	}

	sample := validators.Sample(int(args.Size))
	if setLen := len(sample); setLen != int(args.Size) {
		return fmt.Errorf("current number of validators (%d) is insufficient to sample %d validators", setLen, args.Size)
	}

	reply.Validators = make([]ids.ShortID, int(args.Size))
	for i, vdr := range sample {
		reply.Validators[i] = vdr.ID()
	}
	ids.SortShortIDs(reply.Validators)

	return nil
}

type genericTx struct {
	Tx interface{} `serialize:"true"`
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
	case args.ID.IsZero(): // If ID unspecified, use this node's ID as validator ID
		args.ID = service.vm.Ctx.NodeID
	case args.Username == "":
		return errNoUsername
	case args.Password == "":
		return errNoPassword
	case args.Destination == "":
		return errNoDestination
	case int64(args.StartTime) < time.Now().Unix():
		return fmt.Errorf("start time must be in the future")
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
		args.ID,                        // Node ID
		destination,                    // Destination
		uint32(args.DelegationFeeRate), // Shares
		privKeys,                       // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()
	return errors.New("TODO issue the tx")
}

// AddDefaultSubnetDelegatorArgs are the arguments to AddDefaultSubnetDelegator
type AddDefaultSubnetDelegatorArgs struct {
	APIValidator
	api.UserPass
	Destination string `json:"destination"`
}

// AddDefaultSubnetDelegator returns an unsigned transaction to add a delegator
// to the default subnet
// The returned unsigned transaction should be signed using Sign()
func (service *Service) AddDefaultSubnetDelegator(_ *http.Request, args *AddDefaultSubnetDelegatorArgs, reply *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: AddDefaultSubnetDelegator called")
	switch {
	case args.ID.IsZero(): // If ID unspecified, use this node's ID as validator ID
		args.ID = service.vm.Ctx.NodeID
	case int64(args.StartTime) < time.Now().Unix():
		return fmt.Errorf("start time must be in the future")
	case args.Username == "":
		return errNoUsername
	case args.Password == "":
		return errNoPassword
	case args.Destination == "":
		return errNoDestination
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
		args.ID,                // Node ID
		destination,            // Destination
		privKeys,               // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	reply.TxID = tx.ID()
	return errors.New("TODO issue the tx")
}

// AddNonDefaultSubnetValidatorArgs are the arguments to AddNonDefaultSubnetValidator
type AddNonDefaultSubnetValidatorArgs struct {
	APIValidator
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
	case args.Username == "":
		return errNoUsername
	case args.Password == "":
		return errNoPassword
	}

	subnetID, err := ids.FromString(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem parsing subnetID '%s': %w", args.SubnetID, err)
	}
	if subnetID.Equals(DefaultSubnetID) {
		return errors.New("non-default subnet validator attempts to validate default subnet")
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
	tx, err := service.vm.newAddNonDefaultSubnetValidatorTx(
		uint64(args.weight()),  // Stake amount
		uint64(args.StartTime), // Start time
		uint64(args.EndTime),   // End time
		args.ID,                // Node ID
		subnetID,               // Subnet ID
		nil,                    // TODO how to get control keys?
		privKeys,               // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	return errors.New("TODO issue the tx and add control keys")
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
	switch {
	case args.Username == "":
		return errNoUsername
	case args.Password == "":
		return errNoPassword
	}

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
		controlKeys,            // Control Keys
		uint16(args.Threshold), // Threshold
		privKeys,               // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	return errors.New("TODO issue the tx")
}

// ExportAVAArgs are the arguments to ExportAVA
type ExportAVAArgs struct {
	api.UserPass
	// X-Chain address (without prepended X-) that will receive the exported AVA
	To ids.ShortID `json:"to"`
	// Amount of nAVA to send
	Amount json.Uint64 `json:"amount"`
}

// ExportAVA exports AVAX from the P-Chain to the X-Chain
// It must be imported on the X-Chain to complete the transfer
func (service *Service) ExportAVA(_ *http.Request, args *ExportAVAArgs, response *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: ExportAVA called")
	switch {
	case args.Username == "":
		return errNoUsername
	case args.Password == "":
		return errNoPassword
	case uint64(args.Amount) == 0:
		return errors.New("argument 'amount' must be > 0")
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
	tx, err := service.vm.newExportTx(
		uint64(args.Amount), // Amount
		args.To,             // X-Chain address
		privKeys,            // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	return errors.New("TODO issue the tx and fill in the output field")
}

// ImportAVAArgs are the arguments to ImportAVA
type ImportAVAArgs struct {
	api.UserPass
	// The address that will receive the imported funds
	To ids.ShortID `json:"to"`
}

// ImportAVA returns an unsigned transaction to import AVA from the X-Chain.
// The AVA must have already been exported from the X-Chain.
func (service *Service) ImportAVA(_ *http.Request, args *ImportAVAArgs, response *api.TxIDResponse) error {
	service.vm.Ctx.Log.Info("Platform: ImportAVA called")
	switch {
	case args.Username == "":
		return errNoUsername
	case args.Password == "":
		return errNoPassword
	}

	// Get the keys controlled by this user.
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get user '%s': %w", args.Username, err)
	}
	user := user{db: db}
	privKeys, err := user.getKeys()
	if err != nil {
		return fmt.Errorf("couldn't get keys controlled by the user: %w", err)
	}
	recipientKey, err := user.getKey(args.To)
	if err != nil {
		return fmt.Errorf("user does not have the key that controls address %s", args.To)
	}

	tx, err := service.vm.newImportTx(
		privKeys,
		recipientKey,
	)

	response.TxID = tx.ID()
	return errors.New("TODO issue the tx")
}

/* TODO remove
// IssueTx issues the transaction [args.Tx] to the network
func (service *Service) IssueTx(_ *http.Request, args *IssueTxArgs, response *IssueTxResponse) error {
	service.vm.Ctx.Log.Info("Platform: IssueTx called")

	genTx := genericTx{}
	if err := Codec.Unmarshal(args.Tx.Bytes, &genTx); err != nil {
		return err
	}

	switch tx := genTx.Tx.(type) {
	case TimedTx:
		if err := tx.initialize(service.vm); err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		service.vm.unissuedEvents.Add(tx)
		response.TxID = tx.ID()
	case DecisionTx:
		if err := tx.initialize(service.vm); err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		service.vm.unissuedDecisionTxs = append(service.vm.unissuedDecisionTxs, tx)
		response.TxID = tx.ID()
	case AtomicTx:
		if err := tx.initialize(service.vm); err != nil {
			return fmt.Errorf("error initializing tx: %s", err)
		}
		service.vm.unissuedAtomicTxs = append(service.vm.unissuedAtomicTxs, tx)
		response.TxID = tx.ID()
	default:
		return errors.New("Could not parse given tx. Provided tx needs to be a TimedTx, DecisionTx, or AtomicTx")
	}

	service.vm.resetTimer()
	return nil
}
*/

/*
 ******************************************************
 ******** Create/get status of a blockchain ***********
 ******************************************************
 */

// CreateBlockchainArgs is the arguments for calling CreateBlockchain
// TODO: add control keys
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
	case args.Username == "":
		return errNoUsername
	case args.Password == "":
		return errNoPassword
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

	if args.SubnetID.Equals(DefaultSubnetID) {
		return errDSCantValidate
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
	tx, err := service.vm.newCreateChainTx(
		args.SubnetID,
		args.GenesisData.Bytes,
		vmID,
		fxIDs,
		args.Name,
		nil,      // Control Keys. // TODO: Fill this in
		privKeys, // Private keys
	)
	if err != nil {
		return fmt.Errorf("couldn't create tx: %w", err)
	}

	response.TxID = tx.ID()
	return errors.New("TODO issue the tx and fill in the control keys field")
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

	_, err := service.vm.chainManager.Lookup(args.BlockchainID)
	if err == nil {
		reply.Status = Validating
		return nil
	}

	blockchainID, err := ids.FromString(args.BlockchainID)
	if err != nil {
		return fmt.Errorf("problem parsing blockchainID '%s': %w", args.BlockchainID, err)
	}

	lastAcceptedID := service.vm.LastAccepted()
	if exists, err := service.chainExists(lastAcceptedID, blockchainID); err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	} else if exists {
		reply.Status = Created
		return nil
	}

	preferred := service.vm.Preferred()
	if exists, err := service.chainExists(preferred, blockchainID); err != nil {
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
		return err
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
	service.vm.Ctx.Log.Info("Platform: Validates called")
	// Verify that the Subnet exists
	// Ignore lookup error if it's the DefaultSubnetID
	if _, err := service.vm.getSubnet(service.vm.DB, args.SubnetID); err != nil && !args.SubnetID.Equals(DefaultSubnetID) {
		return err
	}
	// Get the chains that exist
	chains, err := service.vm.getChains(service.vm.DB)
	if err != nil {
		return err
	}
	// Filter to get the chains validated by the specified Subnet
	for _, chain := range chains {
		if chain.SubnetID.Equals(args.SubnetID) {
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
		response.Blockchains = append(response.Blockchains, APIBlockchain{
			ID:       chain.ID(),
			Name:     chain.ChainName,
			SubnetID: chain.SubnetID,
			VMID:     chain.VMID,
		})
	}
	return nil
}
