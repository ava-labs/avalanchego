// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errMissingDecisionBlock  = errors.New("should have a decision block within the past two blocks")
	errParsingID             = errors.New("error parsing ID")
	errGetAccount            = errors.New("error retrieving account information")
	errGetAccounts           = errors.New("error getting accounts controlled by specified user")
	errGetUser               = errors.New("error while getting user. Does user exist?")
	errNoMethodWithGenesis   = errors.New("no method was provided but genesis data was provided")
	errCreatingTransaction   = errors.New("problem while creating transaction")
	errNoDestination         = errors.New("call is missing field 'stakeDestination'")
	errNoSource              = errors.New("call is missing field 'stakeSource'")
	errGetStakeSource        = errors.New("couldn't get account specified in 'stakeSource'")
	errNoBlockchainWithAlias = errors.New("there is no blockchain with the specified alias")
	errDSCantValidate        = errors.New("new blockchain can't be validated by default Subnet")
	errNonDSUsesDS           = errors.New("add non default subnet validator attempts to use default Subnet ID")
	errNilSigner             = errors.New("nil ShortID 'signer' is not valid")
	errNilTo                 = errors.New("nil ShortID 'to' is not valid")
	errNoFunds               = errors.New("no spendable funds were found")
)

// Service defines the API calls that can be made to the platform chain
type Service struct{ vm *VM }

// ExportKeyArgs are arguments for ExportKey
type ExportKeyArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Address  string `json:"address"`
}

// ExportKeyReply is the response for ExportKey
type ExportKeyReply struct {
	// The decrypted PrivateKey for the Address provided in the arguments
	PrivateKey formatting.CB58 `json:"privateKey"`
}

// ExportKey returns a private key from the provided user
func (service *Service) ExportKey(r *http.Request, args *ExportKeyArgs, reply *ExportKeyReply) error {
	service.vm.SnowmanVM.Ctx.Log.Verbo("ExportKey called for user '%s'", args.Username)

	addr, err := service.vm.ParseAddress(args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address: %w", err)
	}

	db, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := user{db: db}

	sk, err := user.getKey(addr)
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
	service.vm.SnowmanVM.Ctx.Log.Verbo("ImportKey called for user '%s'", args.Username)

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

	if err := user.putAccount(sk); err != nil {
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
	service.vm.Ctx.Log.Debug("GetCurrentValidators called")

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
	service.vm.Ctx.Log.Debug("GetPendingValidators called")

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
	service.vm.Ctx.Log.Debug("Sample called with {Size = %d}", args.Size)

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

/*
 ******************************************************
 *************** Get/Create Accounts ******************
 ******************************************************
 */

// GetAccountArgs are the arguments for calling GetAccount
type GetAccountArgs struct {
	// Address of the account we want the information about
	Address string `json:"address"`
}

// GetAccountReply is the response from calling GetAccount
type GetAccountReply struct {
	Address string      `json:"address"`
	Nonce   json.Uint64 `json:"nonce"`
	Balance json.Uint64 `json:"balance"`
}

// GetAccount details given account ID
func (service *Service) GetAccount(_ *http.Request, args *GetAccountArgs, reply *GetAccountReply) error {
	address, err := service.vm.ParseAddress(args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address: %w", err)
	}
	account, err := service.vm.getAccount(service.vm.DB, address)
	if err != nil && err != database.ErrNotFound {
		return fmt.Errorf("couldn't get account: %w", err)
	} else if err == database.ErrNotFound {
		account = newAccount(address, 0, 0)
	}

	reply.Address = service.vm.FormatAddress(account.Address)
	reply.Balance = json.Uint64(account.Balance)
	reply.Nonce = json.Uint64(account.Nonce)
	return nil
}

// ListAccountsArgs are the arguments to ListAccounts
type ListAccountsArgs struct {
	// List all of the accounts controlled by this user
	Username string `json:"username"`
	Password string `json:"password"`
}

// ListAccountsReply is the reply from ListAccounts
type ListAccountsReply struct {
	Accounts []FormattedAPIAccount `json:"accounts"`
}

// ListAccounts lists all of the accounts controlled by [args.Username]
func (service *Service) ListAccounts(_ *http.Request, args *ListAccountsArgs, reply *ListAccountsReply) error {
	service.vm.Ctx.Log.Debug("listAccounts called for user '%s'", args.Username)

	// db holds the user's info that pertains to the Platform Chain
	userDB, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get user: %w", err)
	}

	// The user
	user := user{
		db: userDB,
	}

	// IDs of accounts controlled by this user
	accountIDs, err := user.getAccountIDs()
	if err != nil {
		return fmt.Errorf("couldn't get accounts held by user: %w", err)
	}

	reply.Accounts = []FormattedAPIAccount{}
	for _, accountID := range accountIDs {
		account, err := service.vm.getAccount(service.vm.DB, accountID) // Get account whose ID is [accountID]
		if err != nil && err != database.ErrNotFound {
			service.vm.Ctx.Log.Error("couldn't get account from database: %w", err)
			continue
		} else if err == database.ErrNotFound {
			account = newAccount(accountID, 0, 0)
		}
		reply.Accounts = append(reply.Accounts, FormattedAPIAccount{
			Address: service.vm.FormatAddress(accountID),
			Nonce:   json.Uint64(account.Nonce),
			Balance: json.Uint64(account.Balance),
		})
	}
	return nil
}

// CreateAccountArgs are the arguments for calling CreateAccount
type CreateAccountArgs struct {
	// User that will control the newly created account
	Username string `json:"username"`

	// That user's password
	Password string `json:"password"`

	// The private key that controls the new account.
	// If omitted, will generate a new private key belonging
	// to the user.
	PrivateKey string `json:"privateKey"`
}

// CreateAccountReply are the response from calling CreateAccount
type CreateAccountReply struct {
	// Address of the newly created account
	Address string `json:"address"`
}

// CreateAccount creates a new account on the Platform Chain
// The account is controlled by [args.Username]
// The account's ID is [privKey].PublicKey().Address(), where [privKey] is a
// private key controlled by the user.
func (service *Service) CreateAccount(_ *http.Request, args *CreateAccountArgs, reply *CreateAccountReply) error {
	service.vm.Ctx.Log.Debug("createAccount called for user '%s'", args.Username)

	// userDB holds the user's info that pertains to the Platform Chain
	userDB, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get user: %w", err)
	}

	// The user creating a new account
	user := user{
		db: userDB,
	}

	// private key that controls the new account
	var privKey *crypto.PrivateKeySECP256K1R
	// If no private key supplied in args, create a new one
	if args.PrivateKey == "" {
		privKeyInt, err := service.vm.factory.NewPrivateKey() // The private key that controls the new account
		if err != nil {                                       // The account ID is [private key].PublicKey().Address()
			return errors.New("problem generating private key")
		}
		privKey = privKeyInt.(*crypto.PrivateKeySECP256K1R)
	} else { // parse provided private key
		byteFormatter := formatting.CB58{}
		err := byteFormatter.FromString(args.PrivateKey)
		if err != nil {
			return errors.New("problem while parsing privateKey")
		}
		pk, err := service.vm.factory.ToPrivateKey(byteFormatter.Bytes)
		if err != nil {
			return errors.New("problem while parsing privateKey")
		}
		privKey = pk.(*crypto.PrivateKeySECP256K1R)
	}

	if err := user.putAccount(privKey); err != nil { // Save the private key
		return errors.New("problem saving account")
	}

	reply.Address = service.vm.FormatAddress(privKey.PublicKey().Address())

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

// CreateTxResponse is the response from calls to create a transaction
type CreateTxResponse struct {
	UnsignedTx formatting.CB58 `json:"unsignedTx"`
}

// AddDefaultSubnetValidatorArgs are the arguments to AddDefaultSubnetValidator
type AddDefaultSubnetValidatorArgs struct {
	FormattedAPIDefaultSubnetValidator

	// Next nonce of the sender
	PayerNonce json.Uint64 `json:"payerNonce"`
}

// AddDefaultSubnetValidator returns an unsigned transaction to add a validator to the default subnet
// The returned unsigned transaction should be signed using Sign()
func (service *Service) AddDefaultSubnetValidator(_ *http.Request, args *AddDefaultSubnetValidatorArgs, reply *CreateTxResponse) error {
	service.vm.Ctx.Log.Debug("AddDefaultSubnetValidator called")

	switch {
	case args.ID.IsZero(): // If ID unspecified, use this node's ID as validator ID
		args.ID = service.vm.Ctx.NodeID
	case args.PayerNonce == 0:
		return fmt.Errorf("sender's next nonce not specified")
	case int64(args.StartTime) < time.Now().Unix():
		return fmt.Errorf("start time must be in the future")
	case args.Destination == "":
		return fmt.Errorf("destination not specified")
	}

	destination, err := service.vm.ParseAddress(args.Destination)
	if err != nil {
		return fmt.Errorf("problem while parsing destination: %w", err)
	}

	// Create the transaction
	tx := addDefaultSubnetValidatorTx{UnsignedAddDefaultSubnetValidatorTx: UnsignedAddDefaultSubnetValidatorTx{
		DurationValidator: DurationValidator{
			Validator: Validator{
				NodeID: args.ID,
				Wght:   args.weight(),
			},
			Start: uint64(args.StartTime),
			End:   uint64(args.EndTime),
		},
		Nonce:       uint64(args.PayerNonce),
		Destination: destination,
		NetworkID:   service.vm.Ctx.NetworkID,
		Shares:      uint32(args.DelegationFeeRate),
	}}

	txBytes, err := Codec.Marshal(genericTx{Tx: &tx})
	if err != nil {
		return fmt.Errorf("problem while creating transaction: %w", err)
	}

	reply.UnsignedTx.Bytes = txBytes
	return nil
}

// AddDefaultSubnetDelegatorArgs are the arguments to AddDefaultSubnetDelegator
type AddDefaultSubnetDelegatorArgs struct {
	APIValidator

	Destination string `json:"destination"`

	// Next unused nonce of the account the staked $AVA and tx fee are paid from
	PayerNonce json.Uint64 `json:"payerNonce"`
}

// AddDefaultSubnetDelegator returns an unsigned transaction to add a delegator
// to the default subnet
// The returned unsigned transaction should be signed using Sign()
func (service *Service) AddDefaultSubnetDelegator(_ *http.Request, args *AddDefaultSubnetDelegatorArgs, reply *CreateTxResponse) error {
	service.vm.Ctx.Log.Debug("AddDefaultSubnetDelegator called")

	switch {
	case args.ID.IsZero(): // If ID unspecified, use this node's ID as validator ID
		args.ID = service.vm.Ctx.NodeID
	case args.PayerNonce == 0:
		return fmt.Errorf("sender's next unused nonce not specified")
	case int64(args.StartTime) < time.Now().Unix():
		return fmt.Errorf("start time must be in the future")
	case args.Destination == "":
		return fmt.Errorf("destination must be non-empty string")
	}

	destination, err := service.vm.ParseAddress(args.Destination)
	if err != nil {
		return fmt.Errorf("problem parsing destination address: %w", err)
	}

	// Create the transaction
	tx := addDefaultSubnetDelegatorTx{UnsignedAddDefaultSubnetDelegatorTx: UnsignedAddDefaultSubnetDelegatorTx{
		DurationValidator: DurationValidator{
			Validator: Validator{
				NodeID: args.ID,
				Wght:   args.weight(),
			},
			Start: uint64(args.StartTime),
			End:   uint64(args.EndTime),
		},
		NetworkID:   service.vm.Ctx.NetworkID,
		Nonce:       uint64(args.PayerNonce),
		Destination: destination,
	}}

	txBytes, err := Codec.Marshal(genericTx{Tx: &tx})
	if err != nil {
		return fmt.Errorf("problem while creating transaction: %w", err)
	}

	reply.UnsignedTx.Bytes = txBytes
	return nil
}

// AddNonDefaultSubnetValidatorArgs are the arguments to AddNonDefaultSubnetValidator
type AddNonDefaultSubnetValidatorArgs struct {
	APIValidator

	// ID of subnet to validate
	SubnetID string `json:"subnetID"`

	// Next unused nonce of the account the tx fee is paid from
	PayerNonce json.Uint64 `json:"payerNonce"`
}

// AddNonDefaultSubnetValidator adds a validator to a subnet other than the default subnet
// Returns the unsigned transaction, which must be signed using Sign
func (service *Service) AddNonDefaultSubnetValidator(_ *http.Request, args *AddNonDefaultSubnetValidatorArgs, response *CreateTxResponse) error {
	switch {
	case args.SubnetID == "":
		return errors.New("'subnetID' not given")
	}

	subnetID, err := ids.FromString(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem parsing subnetID '%s': %w", args.SubnetID, err)
	}

	if subnetID.Equals(DefaultSubnetID) {
		return errNonDSUsesDS
	}

	tx := addNonDefaultSubnetValidatorTx{
		UnsignedAddNonDefaultSubnetValidatorTx: UnsignedAddNonDefaultSubnetValidatorTx{
			SubnetValidator: SubnetValidator{
				DurationValidator: DurationValidator{
					Validator: Validator{
						NodeID: args.APIValidator.ID,
						Wght:   args.weight(),
					},
					Start: uint64(args.StartTime),
					End:   uint64(args.EndTime),
				},
				Subnet: subnetID,
			},
			NetworkID: service.vm.Ctx.NetworkID,
			Nonce:     uint64(args.PayerNonce),
		},
		ControlSigs: nil,
		PayerSig:    [crypto.SECP256K1RSigLen]byte{},
		vm:          nil,
		id:          ids.ID{},
		senderID:    ids.ShortID{},
		bytes:       nil,
	}

	txBytes, err := Codec.Marshal(genericTx{Tx: &tx})
	if err != nil {
		return errCreatingTransaction
	}

	response.UnsignedTx.Bytes = txBytes
	return nil
}

// CreateSubnetArgs are the arguments to CreateSubnet
type CreateSubnetArgs struct {
	// The ID member of APISubnet is ignored
	APISubnet

	// Nonce of the account that pays the transaction fee
	PayerNonce json.Uint64 `json:"payerNonce"`
}

// CreateSubnet returns an unsigned transaction to create a new subnet.
// The unsigned transaction must be signed with the key of [args.Payer]
func (service *Service) CreateSubnet(_ *http.Request, args *CreateSubnetArgs, response *CreateTxResponse) error {
	service.vm.Ctx.Log.Debug("platform.createSubnet called")

	switch {
	case args.PayerNonce == 0:
		return fmt.Errorf("sender's next nonce not specified")
	}

	controlKeys := []ids.ShortID{}
	for _, controlKey := range args.ControlKeys {
		controlKeyID, err := service.vm.ParseAddress(controlKey)
		if err != nil {
			return fmt.Errorf("problem parsing control key: %w", err)
		}
		controlKeys = append(controlKeys, controlKeyID)
	}

	// Create the transaction
	tx := CreateSubnetTx{
		UnsignedCreateSubnetTx: UnsignedCreateSubnetTx{
			NetworkID:   service.vm.Ctx.NetworkID,
			Nonce:       uint64(args.PayerNonce),
			ControlKeys: controlKeys,
			Threshold:   uint16(args.Threshold),
		},
		key:   nil,
		Sig:   [65]byte{},
		bytes: nil,
	}

	txBytes, err := Codec.Marshal(genericTx{Tx: &tx})
	if err != nil {
		return errCreatingTransaction
	}

	response.UnsignedTx.Bytes = txBytes
	return nil
}

// ExportAVAArgs are the arguments to ExportAVA
type ExportAVAArgs struct {
	// X-Chain address (without prepended X-) that will receive the exported AVA
	To ids.ShortID `json:"to"`

	// Nonce of the account that pays the transaction fee and provides the export AVA
	PayerNonce json.Uint64 `json:"payerNonce"`

	// Amount of nAVA to send
	Amount json.Uint64 `json:"amount"`
}

// ExportAVA returns an unsigned transaction to export AVA from the P-Chain to the X-Chain.
// After this tx is accepted, the AVA must be imported on the X-Chain side.
// The unsigned transaction must be signed with the key of the account exporting the AVA
// and paying the transaction fee
func (service *Service) ExportAVA(_ *http.Request, args *ExportAVAArgs, response *CreateTxResponse) error {
	service.vm.Ctx.Log.Debug("platform.ExportAVA called")

	switch {
	case args.PayerNonce == 0:
		return fmt.Errorf("sender's next nonce not specified")
	case uint64(args.Amount) == 0:
		return fmt.Errorf("amount must be >0")
	}

	// Create the transaction
	tx := ExportTx{UnsignedExportTx: UnsignedExportTx{
		NetworkID: service.vm.Ctx.NetworkID,
		Nonce:     uint64(args.PayerNonce),
		Outs: []*ava.TransferableOutput{&ava.TransferableOutput{
			Asset: ava.Asset{ID: service.vm.ava},
			Out: &secp256k1fx.TransferOutput{
				Amt: uint64(args.Amount),
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{args.To},
				},
			},
		}},
	}}

	txBytes, err := Codec.Marshal(genericTx{Tx: &tx})
	if err != nil {
		return errCreatingTransaction
	}

	response.UnsignedTx.Bytes = txBytes
	return nil
}

/*
 ******************************************************
 **************** Sign/Issue Txs **********************
 ******************************************************
 */

// SignArgs are the arguments to Sign
type SignArgs struct {
	// The bytes to sign
	// Must be the output of AddDefaultSubnetValidator
	Tx formatting.CB58 `json:"tx"`

	// The address of the key signing the bytes
	Signer string `json:"signer"`

	// User that controls Signer
	Username string `json:"username"`
	Password string `json:"password"`
}

// SignResponse is the response from Sign
type SignResponse struct {
	// The signed bytes
	Tx formatting.CB58 `json:"tx"`
}

// Sign [args.bytes]
func (service *Service) Sign(_ *http.Request, args *SignArgs, reply *SignResponse) error {
	service.vm.Ctx.Log.Debug("sign called")

	if args.Signer == "" {
		return errNilSigner
	}

	signer, err := service.vm.ParseAddress(args.Signer)
	if err != nil {
		return fmt.Errorf("problem parsing address %w", err)
	}

	// Get the key of the Signer
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get data for user '%s'. Does user exist?", args.Username)
	}
	user := user{db: db}

	key, err := user.getKey(signer) // Key of [args.Signer]
	if err != nil {
		return errDB
	}
	if !bytes.Equal(key.PublicKey().Address().Bytes(), signer.Bytes()) { // sanity check
		return errors.New("got unexpected key from database")
	}

	genTx := genericTx{}
	if err := Codec.Unmarshal(args.Tx.Bytes, &genTx); err != nil {
		return err
	}

	switch tx := genTx.Tx.(type) {
	case *addDefaultSubnetValidatorTx:
		genTx.Tx, err = service.signAddDefaultSubnetValidatorTx(tx, key)
	case *addDefaultSubnetDelegatorTx:
		genTx.Tx, err = service.signAddDefaultSubnetDelegatorTx(tx, key)
	case *addNonDefaultSubnetValidatorTx:
		genTx.Tx, err = service.signAddNonDefaultSubnetValidatorTx(tx, key)
	case *CreateSubnetTx:
		genTx.Tx, err = service.signCreateSubnetTx(tx, key)
	case *CreateChainTx:
		genTx.Tx, err = service.signCreateChainTx(tx, key)
	case *ExportTx:
		genTx.Tx, err = service.signExportTx(tx, key)
	default:
		err = errors.New("Could not parse given tx")
	}
	if err != nil {
		return err
	}

	reply.Tx.Bytes, err = Codec.Marshal(genTx)
	return err
}

// Sign [unsigned] with [key]
func (service *Service) signAddDefaultSubnetValidatorTx(tx *addDefaultSubnetValidatorTx, key *crypto.PrivateKeySECP256K1R) (*addDefaultSubnetValidatorTx, error) {
	service.vm.Ctx.Log.Debug("signAddDefaultSubnetValidatorTx called")

	// TODO: Should we check if tx is already signed?
	unsignedIntf := interface{}(&tx.UnsignedAddDefaultSubnetValidatorTx)
	unsignedTxBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, fmt.Errorf("error serializing unsigned tx: %w", err)
	}

	sig, err := key.Sign(unsignedTxBytes)
	if err != nil {
		return nil, errors.New("error while signing")
	}
	if len(sig) != crypto.SECP256K1RSigLen {
		return nil, fmt.Errorf("expected signature to be length %d but was length %d", crypto.SECP256K1RSigLen, len(sig))
	}
	copy(tx.Sig[:], sig)

	return tx, nil
}

// Sign [unsigned] with [key]
func (service *Service) signAddDefaultSubnetDelegatorTx(tx *addDefaultSubnetDelegatorTx, key *crypto.PrivateKeySECP256K1R) (*addDefaultSubnetDelegatorTx, error) {
	service.vm.Ctx.Log.Debug("signAddDefaultSubnetValidatorTx called")

	// TODO: Should we check if tx is already signed?
	unsignedIntf := interface{}(&tx.UnsignedAddDefaultSubnetDelegatorTx)
	unsignedTxBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, fmt.Errorf("error serializing unsigned tx: %w", err)
	}

	sig, err := key.Sign(unsignedTxBytes)
	if err != nil {
		return nil, errors.New("error while signing")
	}
	if len(sig) != crypto.SECP256K1RSigLen {
		return nil, fmt.Errorf("expected signature to be length %d but was length %d", crypto.SECP256K1RSigLen, len(sig))
	}
	copy(tx.Sig[:], sig)

	return tx, nil
}

// Sign [xt] with [key]
func (service *Service) signCreateSubnetTx(tx *CreateSubnetTx, key *crypto.PrivateKeySECP256K1R) (*CreateSubnetTx, error) {
	service.vm.Ctx.Log.Debug("signAddDefaultSubnetValidatorTx called")

	// TODO: Should we check if tx is already signed?
	unsignedIntf := interface{}(&tx.UnsignedCreateSubnetTx)
	unsignedTxBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, fmt.Errorf("error serializing unsigned tx: %w", err)
	}

	sig, err := key.Sign(unsignedTxBytes)
	if err != nil {
		return nil, errors.New("error while signing")
	}
	if len(sig) != crypto.SECP256K1RSigLen {
		return nil, fmt.Errorf("expected signature to be length %d but was length %d", crypto.SECP256K1RSigLen, len(sig))
	}
	copy(tx.Sig[:], sig)

	return tx, nil
}

// Sign [tx] with [key]
func (service *Service) signExportTx(tx *ExportTx, key *crypto.PrivateKeySECP256K1R) (*ExportTx, error) {
	service.vm.Ctx.Log.Debug("platform.signAddDefaultSubnetValidatorTx called")

	// TODO: Should we check if tx is already signed?
	unsignedIntf := interface{}(&tx.UnsignedExportTx)
	unsignedTxBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, fmt.Errorf("error serializing unsigned tx: %w", err)
	}

	sig, err := key.Sign(unsignedTxBytes)
	if err != nil {
		return nil, errors.New("error while signing")
	}
	if len(sig) != crypto.SECP256K1RSigLen {
		return nil, fmt.Errorf("expected signature to be length %d but was length %d", crypto.SECP256K1RSigLen, len(sig))
	}
	copy(tx.Sig[:], sig)

	return tx, nil
}

// Signs an unsigned or partially signed addNonDefaultSubnetValidatorTx with [key]
// If [key] is a control key for the subnet and there is an empty spot in tx.ControlSigs, signs there
// If [key] is a control key for the subnet and there is no empty spot in tx.ControlSigs, signs as payer
// If [key] is not a control key, sign as payer (account controlled by [key] pays the tx fee)
// Sorts tx.ControlSigs before returning
// Assumes each element of tx.ControlSigs is actually a signature, not just empty bytes
func (service *Service) signAddNonDefaultSubnetValidatorTx(tx *addNonDefaultSubnetValidatorTx, key *crypto.PrivateKeySECP256K1R) (*addNonDefaultSubnetValidatorTx, error) {
	service.vm.Ctx.Log.Debug("signAddNonDefaultSubnetValidatorTx called")

	// Compute the byte repr. of the unsigned tx and the signature of [key] over it
	unsignedIntf := interface{}(&tx.UnsignedAddNonDefaultSubnetValidatorTx)
	unsignedTxBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, fmt.Errorf("error serializing unsigned tx: %w", err)
	}
	sig, err := key.Sign(unsignedTxBytes)
	if err != nil {
		return nil, errors.New("error while signing")
	}
	if len(sig) != crypto.SECP256K1RSigLen {
		return nil, fmt.Errorf("expected signature to be length %d but was length %d", crypto.SECP256K1RSigLen, len(sig))
	}

	// Get information about the subnet
	subnet, err := service.vm.getSubnet(service.vm.DB, tx.SubnetID())
	if err != nil {
		return nil, fmt.Errorf("problem getting subnet information: %w", err)
	}

	// Find the location at which [key] should put its signature.
	// If [key] is a control key for this subnet and there is an empty spot in tx.ControlSigs, sign there
	// If [key] is a control key for this subnet and there is no empty spot in tx.ControlSigs, sign as payer
	// If [key] is not a control key, sign as payer (account controlled by [key] pays the tx fee)
	controlKeySet := ids.ShortSet{}
	controlKeySet.Add(subnet.ControlKeys...)
	isControlKey := controlKeySet.Contains(key.PublicKey().Address())

	payerSigEmpty := tx.PayerSig == [crypto.SECP256K1RSigLen]byte{} // true if no key has signed to pay the tx fee

	if isControlKey && len(tx.ControlSigs) != int(subnet.Threshold) { // Sign as controlSig
		tx.ControlSigs = append(tx.ControlSigs, [crypto.SECP256K1RSigLen]byte{})
		copy(tx.ControlSigs[len(tx.ControlSigs)-1][:], sig)
	} else if payerSigEmpty { // sign as payer
		copy(tx.PayerSig[:], sig)
	} else {
		return nil, errors.New("no place for key to sign")
	}

	crypto.SortSECP2561RSigs(tx.ControlSigs)

	return tx, nil
}

// ImportAVAArgs are the arguments to ImportAVA
type ImportAVAArgs struct {
	// ID of the account that will receive the imported funds, and pay the transaction fee
	To string `json:"to"`

	// Next nonce of the sender
	PayerNonce json.Uint64 `json:"payerNonce"`

	// User that controls the account
	Username string `json:"username"`
	Password string `json:"password"`
}

// ImportAVA returns an unsigned transaction to import AVA from the X-Chain.
// The AVA must have already been exported from the X-Chain.
// The unsigned transaction must be signed with the key of the tx fee payer.
func (service *Service) ImportAVA(_ *http.Request, args *ImportAVAArgs, response *SignResponse) error {
	service.vm.Ctx.Log.Debug("platform.ImportAVA called")

	switch {
	case args.To == "":
		return errNilTo
	case args.PayerNonce == 0:
		return fmt.Errorf("sender's next nonce not specified")
	}

	toID, err := service.vm.ParseAddress(args.To)
	if err != nil {
		return fmt.Errorf("problem parsing address in 'to' field %w", err)
	}

	// Get the key of the Signer
	db, err := service.vm.Ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("couldn't get user: %w", err)
	}
	user := user{db: db}

	kc := secp256k1fx.NewKeychain()
	key, err := user.getKey(toID)
	if err != nil {
		return errDB
	}
	kc.Add(key)

	addrSet := ids.Set{}
	addrSet.Add(ids.NewID(hashing.ComputeHash256Array(toID.Bytes())))

	utxos, err := service.vm.GetAtomicUTXOs(addrSet)
	if err != nil {
		return fmt.Errorf("problem retrieving user's atomic UTXOs: %w", err)
	}

	amount := uint64(0)
	time := service.vm.clock.Unix()

	ins := []*ava.TransferableInput{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}
	for _, utxo := range utxos {
		if !utxo.AssetID().Equals(service.vm.ava) {
			continue
		}
		inputIntf, signers, err := kc.Spend(utxo.Out, time)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(ava.Transferable)
		if !ok {
			continue
		}
		spent, err := math.Add64(amount, input.Amount())
		if err != nil {
			return err
		}
		amount = spent

		in := &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: service.vm.ava},
			In:     input,
		}

		ins = append(ins, in)
		keys = append(keys, signers)
	}

	if amount == 0 {
		return errNoFunds
	}

	ava.SortTransferableInputsWithSigners(ins, keys)

	// Create the transaction
	tx := ImportTx{UnsignedImportTx: UnsignedImportTx{
		NetworkID: service.vm.Ctx.NetworkID,
		Nonce:     uint64(args.PayerNonce),
		Account:   toID,
		Ins:       ins,
	}}

	// TODO: Should we check if tx is already signed?
	unsignedIntf := interface{}(&tx.UnsignedImportTx)
	unsignedTxBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return fmt.Errorf("error serializing unsigned tx: %w", err)
	}
	hash := hashing.ComputeHash256(unsignedTxBytes)

	sig, err := key.SignHash(hash)
	if err != nil {
		return errors.New("error while signing")
	}
	copy(tx.Sig[:], sig)

	for _, credKeys := range keys {
		cred := &secp256k1fx.Credential{}
		for _, key := range credKeys {
			sig, err := key.SignHash(hash)
			if err != nil {
				return fmt.Errorf("problem creating transaction: %w", err)
			}
			fixedSig := [crypto.SECP256K1RSigLen]byte{}
			copy(fixedSig[:], sig)

			cred.Sigs = append(cred.Sigs, fixedSig)
		}
		tx.Creds = append(tx.Creds, cred)
	}

	txBytes, err := Codec.Marshal(genericTx{Tx: &tx})
	if err != nil {
		return errCreatingTransaction
	}

	response.Tx.Bytes = txBytes
	return nil
}

// Signs an unsigned or partially signed CreateChainTx with [key]
// If [key] is a control key for the subnet and there is an empty spot in tx.ControlSigs, signs there
// If [key] is a control key for the subnet and there is no empty spot in tx.ControlSigs, signs as payer
// If [key] is not a control key, sign as payer (account controlled by [key] pays the tx fee)
// Sorts tx.ControlSigs before returning
// Assumes each element of tx.ControlSigs is actually a signature, not just empty bytes
func (service *Service) signCreateChainTx(tx *CreateChainTx, key *crypto.PrivateKeySECP256K1R) (*CreateChainTx, error) {
	service.vm.Ctx.Log.Debug("signCreateChainTx called")

	// Compute the byte repr. of the unsigned tx and the signature of [key] over it
	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
	unsignedTxBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, fmt.Errorf("error serializing unsigned tx: %w", err)
	}
	sig, err := key.Sign(unsignedTxBytes)
	if err != nil {
		return nil, errors.New("error while signing")
	}
	if len(sig) != crypto.SECP256K1RSigLen {
		return nil, fmt.Errorf("expected signature to be length %d but was length %d", crypto.SECP256K1RSigLen, len(sig))
	}

	// Get information about the subnet
	subnet, err := service.vm.getSubnet(service.vm.DB, tx.SubnetID)
	if err != nil {
		return nil, fmt.Errorf("problem getting subnet information: %w", err)
	}

	// Find the location at which [key] should put its signature.
	// If [key] is a control key for this subnet and there is an empty spot in tx.ControlSigs, sign there
	// If [key] is a control key for this subnet and there is no empty spot in tx.ControlSigs, sign as payer
	// If [key] is not a control key, sign as payer (account controlled by [key] pays the tx fee)
	controlKeySet := ids.ShortSet{}
	controlKeySet.Add(subnet.ControlKeys...)
	isControlKey := controlKeySet.Contains(key.PublicKey().Address())

	payerSigEmpty := tx.PayerSig == [crypto.SECP256K1RSigLen]byte{} // true if no key has signed to pay the tx fee

	if isControlKey && len(tx.ControlSigs) != int(subnet.Threshold) { // Sign as controlSig
		tx.ControlSigs = append(tx.ControlSigs, [crypto.SECP256K1RSigLen]byte{})
		copy(tx.ControlSigs[len(tx.ControlSigs)-1][:], sig)
	} else if payerSigEmpty { // sign as payer
		copy(tx.PayerSig[:], sig)
	} else {
		return nil, errors.New("no place for key to sign")
	}

	crypto.SortSECP2561RSigs(tx.ControlSigs)

	return tx, nil
}

// IssueTxArgs are the arguments to IssueTx
type IssueTxArgs struct {
	// Tx being sent to the network
	Tx formatting.CB58 `json:"tx"`
}

// IssueTxResponse is the response from IssueTx
type IssueTxResponse struct {
	// ID of transaction being sent to network
	TxID ids.ID `json:"txID"`
}

// IssueTx issues the transaction [args.Tx] to the network
func (service *Service) IssueTx(_ *http.Request, args *IssueTxArgs, response *IssueTxResponse) error {
	service.vm.Ctx.Log.Debug("issueTx called")

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
		return errors.New("Could not parse given tx. Must be a TimedTx, DecisionTx, or AtomicTx")
	}

	service.vm.resetTimer()
	return nil
}

/*
 ******************************************************
 ******** Create/get status of a blockchain ***********
 ******************************************************
 */

// CreateBlockchainArgs is the arguments for calling CreateBlockchain
type CreateBlockchainArgs struct {
	// ID of Subnet that validates the new blockchain
	SubnetID string `json:"subnetID"`

	// ID of the VM the new blockchain is running
	VMID string `json:"vmID"`

	// IDs of the FXs the VM is running
	FxIDs []string `json:"fxIDs"`

	// Human-readable name for the new blockchain, not necessarily unique
	Name string `json:"name"`

	// Next nonce of the sender
	PayerNonce json.Uint64 `json:"payerNonce"`

	// Genesis state of the blockchain being created
	GenesisData formatting.CB58 `json:"genesisData"`
}

// CreateBlockchain returns an unsigned transaction to create a new blockchain
// Must be signed with the Subnet's control keys and with a key that pays the transaction fee before issuance
func (service *Service) CreateBlockchain(_ *http.Request, args *CreateBlockchainArgs, response *CreateTxResponse) error {
	service.vm.Ctx.Log.Debug("createBlockchain called")

	switch {
	case args.PayerNonce == 0:
		return errors.New("sender's next nonce not specified")
	case args.VMID == "":
		return errors.New("VM not specified")
	case args.SubnetID == "":
		return errors.New("subnet not specified")
	}

	subnetID, err := ids.FromString(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem parsing subnetID %s, %w", args.SubnetID, err)
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

	if subnetID.Equals(DefaultSubnetID) {
		return errDSCantValidate
	}

	tx := CreateChainTx{
		UnsignedCreateChainTx: UnsignedCreateChainTx{
			NetworkID:   service.vm.Ctx.NetworkID,
			SubnetID:    subnetID,
			Nonce:       uint64(args.PayerNonce),
			ChainName:   args.Name,
			VMID:        vmID,
			FxIDs:       fxIDs,
			GenesisData: args.GenesisData.Bytes,
		},
		PayerAddress: ids.ShortID{},
		PayerSig:     [crypto.SECP256K1RSigLen]byte{},
		ControlSigs:  nil,
		vm:           nil,
		id:           ids.ID{},
		bytes:        nil,
	}

	txBytes, err := Codec.Marshal(genericTx{Tx: &tx})
	if err != nil {
		service.vm.Ctx.Log.Error("problem marshaling createChainTx: %w", err)
		return errCreatingTransaction
	}

	response.UnsignedTx.Bytes = txBytes
	return nil
}

// GetBlockchainStatusArgs is the arguments for calling GetBlockchainStatus
// [BlockchainID] is the blockchain to get the status of.
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
	service.vm.Ctx.Log.Debug("getBlockchainStatus called")

	switch {
	case args.BlockchainID == "":
		return errors.New("'blockchainID' not given")
	}

	_, err := service.vm.chainManager.Lookup(args.BlockchainID)
	if err == nil {
		reply.Status = Validating
		return nil
	}

	bID, err := ids.FromString(args.BlockchainID)
	if err != nil {
		return fmt.Errorf("problem parsing blockchainID '%s': %w", args.BlockchainID, err)
	}

	lastAcceptedID := service.vm.LastAccepted()
	if exists, err := service.chainExists(lastAcceptedID, bID); err != nil {
		return fmt.Errorf("problem looking up blockchain: %w", err)
	} else if exists {
		reply.Status = Created
		return nil
	}

	preferred := service.vm.Preferred()
	if exists, err := service.chainExists(preferred, bID); err != nil {
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
	BlockchainID string `json:"blockchainID"`
}

// ValidatedByResponse is the reply from calling ValidatedBy
type ValidatedByResponse struct {
	// ID of the Subnet validating the specified blockchain
	SubnetID ids.ID `json:"subnetID"`
}

// ValidatedBy returns the ID of the Subnet that validates [args.BlockchainID]
func (service *Service) ValidatedBy(_ *http.Request, args *ValidatedByArgs, response *ValidatedByResponse) error {
	service.vm.Ctx.Log.Debug("validatedBy called")

	switch {
	case args.BlockchainID == "":
		return errors.New("'blockchainID' not given")
	}

	blockchainID, err := ids.FromString(args.BlockchainID)
	if err != nil {
		return fmt.Errorf("problem parsing blockchainID '%s': %w", args.BlockchainID, err)
	}

	chain, err := service.vm.getChain(service.vm.DB, blockchainID)
	if err != nil {
		return err
	}
	response.SubnetID = chain.SubnetID
	return nil
}

// ValidatesArgs are the arguments to Validates
type ValidatesArgs struct {
	SubnetID string `json:"subnetID"`
}

// ValidatesResponse is the response from calling Validates
type ValidatesResponse struct {
	BlockchainIDs []ids.ID `json:"blockchainIDs"`
}

// Validates returns the IDs of the blockchains validated by [args.SubnetID]
func (service *Service) Validates(_ *http.Request, args *ValidatesArgs, response *ValidatesResponse) error {
	service.vm.Ctx.Log.Debug("validates called")

	switch {
	case args.SubnetID == "":
		return errors.New("'subnetID' not given")
	}

	subnetID, err := ids.FromString(args.SubnetID)
	if err != nil {
		return fmt.Errorf("problem parsing subnetID '%s': %w", args.SubnetID, err)
	}

	// Verify that the Subnet exists
	// Ignore lookup error if it's the DefaultSubnetID
	if _, err := service.vm.getSubnet(service.vm.DB, subnetID); err != nil && !subnetID.Equals(DefaultSubnetID) {
		return err
	}
	// Get the chains that exist
	chains, err := service.vm.getChains(service.vm.DB)
	if err != nil {
		return err
	}
	// Filter to get the chains validated by the specified Subnet
	for _, chain := range chains {
		if chain.SubnetID.Equals(subnetID) {
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
	service.vm.Ctx.Log.Debug("getBlockchains called")

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
