// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/json"
	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errUnknownAssetID            = errors.New("unknown asset ID")
	errTxNotCreateAsset          = errors.New("transaction doesn't create an asset")
	errNoHolders                 = errors.New("initialHolders must not be empty")
	errNoMinters                 = errors.New("no minters provided")
	errInvalidAmount             = errors.New("amount must be positive")
	errSpendOverflow             = errors.New("spent amount overflows uint64")
	errInvalidMintAmount         = errors.New("amount minted must be positive")
	errAddressesCantMintAsset    = errors.New("provided addresses don't have the authority to mint the provided asset")
	errCanOnlySignSingleInputTxs = errors.New("can only sign transactions with one input")
	errUnknownUTXO               = errors.New("unknown utxo")
	errInvalidUTXO               = errors.New("invalid utxo")
	errUnknownOutputType         = errors.New("unknown output type")
	errUnneededAddress           = errors.New("address not required to sign")
	errUnknownCredentialType     = errors.New("unknown credential type")
	errNoMatchingAddress         = errors.New("the user has no from address")
)

// Service defines the base service for the asset vm
type Service struct{ vm *VM }

// IssueTxArgs are arguments for passing into IssueTx requests
type IssueTxArgs struct {
	Tx formatting.CB58 `json:"tx"`
}

// IssueTxReply defines the IssueTx replies returned from the API
type IssueTxReply struct {
	TxID ids.ID `json:"txID"`
}

// IssueTx attempts to issue a transaction into consensus
func (service *Service) IssueTx(r *http.Request, args *IssueTxArgs, reply *IssueTxReply) error {
	service.vm.ctx.Log.Verbo("IssueTx called with %s", args.Tx)

	txID, err := service.vm.IssueTx(args.Tx.Bytes, nil)
	if err != nil {
		return err
	}

	reply.TxID = txID
	return nil
}

// GetTxStatusArgs are arguments for passing into GetTxStatus requests
type GetTxStatusArgs struct {
	TxID ids.ID `json:"txID"`
}

// GetTxStatusReply defines the GetTxStatus replies returned from the API
type GetTxStatusReply struct {
	Status choices.Status `json:"status"`
}

var (
	errNilTxID = errors.New("nil transaction ID")
)

// GetTxStatus returns the status of the specified transaction
func (service *Service) GetTxStatus(r *http.Request, args *GetTxStatusArgs, reply *GetTxStatusReply) error {
	service.vm.ctx.Log.Verbo("GetTxStatus called with %s", args.TxID)

	if args.TxID.IsZero() {
		return errNilTxID
	}

	tx := UniqueTx{
		vm:   service.vm,
		txID: args.TxID,
	}

	reply.Status = tx.Status()
	return nil
}

// GetUTXOsArgs are arguments for passing into GetUTXOs requests
type GetUTXOsArgs struct {
	Addresses []string `json:"addresses"`
}

// GetUTXOsReply defines the GetUTXOs replies returned from the API
type GetUTXOsReply struct {
	UTXOs []formatting.CB58 `json:"utxos"`
}

// GetUTXOs creates an empty account with the name passed in
func (service *Service) GetUTXOs(r *http.Request, args *GetUTXOsArgs, reply *GetUTXOsReply) error {
	service.vm.ctx.Log.Verbo("GetUTXOs called with %s", args.Addresses)

	addrSet := ids.Set{}
	for _, addr := range args.Addresses {
		addrBytes, err := service.vm.Parse(addr)
		if err != nil {
			return err
		}
		addrSet.Add(ids.NewID(hashing.ComputeHash256Array(addrBytes)))
	}

	utxos, err := service.vm.GetUTXOs(addrSet)
	if err != nil {
		return err
	}

	reply.UTXOs = []formatting.CB58{}
	for _, utxo := range utxos {
		b, err := service.vm.codec.Marshal(utxo)
		if err != nil {
			return err
		}
		reply.UTXOs = append(reply.UTXOs, formatting.CB58{Bytes: b})
	}
	return nil
}

// GetAssetDescriptionArgs are arguments for passing into GetAssetDescription requests
type GetAssetDescriptionArgs struct {
	AssetID string `json:"assetID"`
}

// GetAssetDescriptionReply defines the GetAssetDescription replies returned from the API
type GetAssetDescriptionReply struct {
	AssetID      ids.ID     `json:"assetID"`
	Name         string     `json:"name"`
	Symbol       string     `json:"symbol"`
	Denomination json.Uint8 `json:"denomination"`
}

// GetAssetDescription creates an empty account with the name passed in
func (service *Service) GetAssetDescription(_ *http.Request, args *GetAssetDescriptionArgs, reply *GetAssetDescriptionReply) error {
	service.vm.ctx.Log.Verbo("GetAssetDescription called with %s", args.AssetID)

	assetID, err := service.vm.Lookup(args.AssetID)
	if err != nil {
		assetID, err = ids.FromString(args.AssetID)
		if err != nil {
			return err
		}
	}

	tx := &UniqueTx{
		vm:   service.vm,
		txID: assetID,
	}
	if status := tx.Status(); !status.Fetched() {
		return errUnknownAssetID
	}
	createAssetTx, ok := tx.UnsignedTx.(*CreateAssetTx)
	if !ok {
		return errTxNotCreateAsset
	}

	reply.AssetID = assetID
	reply.Name = createAssetTx.Name
	reply.Symbol = createAssetTx.Symbol
	reply.Denomination = json.Uint8(createAssetTx.Denomination)

	return nil
}

// GetBalanceArgs are arguments for passing into GetBalance requests
type GetBalanceArgs struct {
	Address string `json:"address"`
	AssetID string `json:"assetID"`
}

// GetBalanceReply defines the GetBalance replies returned from the API
type GetBalanceReply struct {
	Balance json.Uint64 `json:"balance"`
}

// GetBalance returns the amount of an asset that an address at least partially owns
func (service *Service) GetBalance(r *http.Request, args *GetBalanceArgs, reply *GetBalanceReply) error {
	service.vm.ctx.Log.Verbo("GetBalance called with address: %s assetID: %s", args.Address, args.AssetID)

	address, err := service.vm.Parse(args.Address)
	if err != nil {
		return err
	}

	assetID, err := service.vm.Lookup(args.AssetID)
	if err != nil {
		assetID, err = ids.FromString(args.AssetID)
		if err != nil {
			return err
		}
	}

	addrSet := ids.Set{}
	addrSet.Add(ids.NewID(hashing.ComputeHash256Array(address)))

	utxos, err := service.vm.GetUTXOs(addrSet)
	if err != nil {
		return err
	}

	for _, utxo := range utxos {
		if utxo.AssetID().Equals(assetID) {
			transferable, ok := utxo.Out.(ava.Transferable)
			if !ok {
				continue
			}
			amt, err := safemath.Add64(transferable.Amount(), uint64(reply.Balance))
			if err != nil {
				return err
			}
			reply.Balance = json.Uint64(amt)
		}
	}
	return nil
}

// Balance ...
type Balance struct {
	AssetID string      `json:"asset"`
	Balance json.Uint64 `json:"balance"`
}

// GetAllBalancesArgs are arguments for calling into GetAllBalances
type GetAllBalancesArgs struct {
	Address string `json:"address"`
}

// GetAllBalancesReply is the response from a call to GetAllBalances
type GetAllBalancesReply struct {
	Balances []Balance `json:"balances"`
}

// GetAllBalances returns a map where:
//   Key: ID of an asset such that [args.Address] has a non-zero balance of the asset
//   Value: The balance of the asset held by the address
// Note that balances include assets that the address only _partially_ owns
// (ie is one of several addresses specified in a multi-sig)
func (service *Service) GetAllBalances(r *http.Request, args *GetAllBalancesArgs, reply *GetAllBalancesReply) error {
	service.vm.ctx.Log.Verbo("GetAllBalances called with address: %s", args.Address)

	address, err := service.vm.Parse(args.Address)
	if err != nil {
		return fmt.Errorf("couldn't parse given address: %s", err)
	}
	addrAsSet := ids.Set{}
	addrAsSet.Add(ids.NewID(hashing.ComputeHash256Array(address)))

	utxos, err := service.vm.GetUTXOs(addrAsSet)
	if err != nil {
		return fmt.Errorf("couldn't get address's UTXOs: %s", err)
	}

	assetIDs := ids.Set{}                    // IDs of assets the address has a non-zero balance of
	balances := make(map[[32]byte]uint64, 0) // key: ID (as bytes). value: balance of that asset
	for _, utxo := range utxos {
		transferable, ok := utxo.Out.(ava.Transferable)
		if !ok {
			continue
		}
		assetID := utxo.AssetID()
		assetIDs.Add(assetID)
		balance := balances[assetID.Key()] // 0 if key doesn't exist
		balance, err := safemath.Add64(transferable.Amount(), balance)
		if err != nil {
			balances[assetID.Key()] = math.MaxUint64
		} else {
			balances[assetID.Key()] = balance
		}
	}

	reply.Balances = make([]Balance, assetIDs.Len())
	for i, assetID := range assetIDs.List() {
		if alias, err := service.vm.PrimaryAlias(assetID); err == nil {
			reply.Balances[i] = Balance{
				AssetID: alias,
				Balance: json.Uint64(balances[assetID.Key()]),
			}
		} else {
			reply.Balances[i] = Balance{
				AssetID: assetID.String(),
				Balance: json.Uint64(balances[assetID.Key()]),
			}
		}
	}

	return nil
}

// CreateFixedCapAssetArgs are arguments for passing into CreateFixedCapAsset requests
type CreateFixedCapAssetArgs struct {
	Username       string    `json:"username"`
	Password       string    `json:"password"`
	Name           string    `json:"name"`
	Symbol         string    `json:"symbol"`
	Denomination   byte      `json:"denomination"`
	InitialHolders []*Holder `json:"initialHolders"`
}

// Holder describes how much an address owns of an asset
type Holder struct {
	Amount  json.Uint64 `json:"amount"`
	Address string      `json:"address"`
}

// CreateFixedCapAssetReply defines the CreateFixedCapAsset replies returned from the API
type CreateFixedCapAssetReply struct {
	AssetID ids.ID `json:"assetID"`
}

// CreateFixedCapAsset returns ID of the newly created asset
func (service *Service) CreateFixedCapAsset(r *http.Request, args *CreateFixedCapAssetArgs, reply *CreateFixedCapAssetReply) error {
	service.vm.ctx.Log.Verbo("CreateFixedCapAsset called with name: %s symbol: %s number of holders: %d",
		args.Name,
		args.Symbol,
		len(args.InitialHolders),
	)

	if len(args.InitialHolders) == 0 {
		return errNoHolders
	}

	initialState := &InitialState{
		FxID: 0, // TODO: Should lookup secp256k1fx FxID
		Outs: []verify.Verifiable{},
	}

	tx := &Tx{UnsignedTx: &CreateAssetTx{
		BaseTx: BaseTx{
			NetID: service.vm.ctx.NetworkID,
			BCID:  service.vm.ctx.ChainID,
		},
		Name:         args.Name,
		Symbol:       args.Symbol,
		Denomination: args.Denomination,
		States: []*InitialState{
			initialState,
		},
	}}

	for _, holder := range args.InitialHolders {
		address, err := service.vm.Parse(holder.Address)
		if err != nil {
			return err
		}
		addr, err := ids.ToShortID(address)
		if err != nil {
			return err
		}
		initialState.Outs = append(initialState.Outs, &secp256k1fx.TransferOutput{
			Amt: uint64(holder.Amount),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		})
	}
	initialState.Sort(service.vm.codec)

	b, err := service.vm.codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	assetID, err := service.vm.IssueTx(b, nil)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.AssetID = assetID

	return nil
}

// CreateVariableCapAssetArgs are arguments for passing into CreateVariableCapAsset requests
type CreateVariableCapAssetArgs struct {
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	Name         string   `json:"name"`
	Symbol       string   `json:"symbol"`
	Denomination byte     `json:"denomination"`
	MinterSets   []Owners `json:"minterSets"`
}

// Owners describes who can perform an action
type Owners struct {
	Threshold json.Uint32 `json:"threshold"`
	Minters   []string    `json:"minters"`
}

// CreateVariableCapAssetReply defines the CreateVariableCapAsset replies returned from the API
type CreateVariableCapAssetReply struct {
	AssetID ids.ID `json:"assetID"`
}

// CreateVariableCapAsset returns ID of the newly created asset
func (service *Service) CreateVariableCapAsset(r *http.Request, args *CreateVariableCapAssetArgs, reply *CreateVariableCapAssetReply) error {
	service.vm.ctx.Log.Verbo("CreateFixedCapAsset called with name: %s symbol: %s number of minters: %d",
		args.Name,
		args.Symbol,
		len(args.MinterSets),
	)

	if len(args.MinterSets) == 0 {
		return errNoMinters
	}

	initialState := &InitialState{
		FxID: 0, // TODO: Should lookup secp256k1fx FxID
		Outs: []verify.Verifiable{},
	}

	tx := &Tx{UnsignedTx: &CreateAssetTx{
		BaseTx: BaseTx{
			NetID: service.vm.ctx.NetworkID,
			BCID:  service.vm.ctx.ChainID,
		},
		Name:         args.Name,
		Symbol:       args.Symbol,
		Denomination: args.Denomination,
		States: []*InitialState{
			initialState,
		},
	}}

	for _, owner := range args.MinterSets {
		minter := &secp256k1fx.MintOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: uint32(owner.Threshold),
			},
		}
		for _, address := range owner.Minters {
			addrBytes, err := service.vm.Parse(address)
			if err != nil {
				return err
			}
			addr, err := ids.ToShortID(addrBytes)
			if err != nil {
				return err
			}
			minter.Addrs = append(minter.Addrs, addr)
		}
		ids.SortShortIDs(minter.Addrs)
		initialState.Outs = append(initialState.Outs, minter)
	}
	initialState.Sort(service.vm.codec)

	b, err := service.vm.codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	assetID, err := service.vm.IssueTx(b, nil)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.AssetID = assetID

	return nil
}

// CreateAddressArgs are arguments for calling CreateAddress
type CreateAddressArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CreateAddressReply define the reply from a CreateAddress call
type CreateAddressReply struct {
	Address string `json:"address"`
}

// CreateAddress creates an address for the user [args.Username]
func (service *Service) CreateAddress(r *http.Request, args *CreateAddressArgs, reply *CreateAddressReply) error {
	service.vm.ctx.Log.Verbo("CreateAddress called for user '%s'", args.Username)

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := userState{vm: service.vm}

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.NewPrivateKey()
	if err != nil {
		return fmt.Errorf("problem generating private key: %w", err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	if err := user.SetKey(db, sk); err != nil {
		return fmt.Errorf("problem saving private key: %w", err)
	}

	addresses, _ := user.Addresses(db)
	addresses = append(addresses, ids.NewID(hashing.ComputeHash256Array(sk.PublicKey().Address().Bytes())))

	if err := user.SetAddresses(db, addresses); err != nil {
		return fmt.Errorf("problem saving address: %w", err)
	}

	reply.Address = service.vm.Format(sk.PublicKey().Address().Bytes())
	return nil
}

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
	service.vm.ctx.Log.Verbo("ExportKey called for user '%s'", args.Username)

	address, err := service.vm.Parse(args.Address)
	if err != nil {
		return fmt.Errorf("problem parsing address: %w", err)
	}

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := userState{vm: service.vm}

	sk, err := user.Key(db, ids.NewID(hashing.ComputeHash256Array(address)))
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
	service.vm.ctx.Log.Verbo("ImportKey called for user '%s'", args.Username)

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving data: %w", err)
	}

	user := userState{vm: service.vm}

	factory := crypto.FactorySECP256K1R{}
	skIntf, err := factory.ToPrivateKey(args.PrivateKey.Bytes)
	if err != nil {
		return fmt.Errorf("problem parsing private key %s: %w", args.PrivateKey, err)
	}
	sk := skIntf.(*crypto.PrivateKeySECP256K1R)

	if err := user.SetKey(db, sk); err != nil {
		return fmt.Errorf("problem saving key %w", err)
	}

	addresses, _ := user.Addresses(db)
	addresses = append(addresses, ids.NewID(hashing.ComputeHash256Array(sk.PublicKey().Address().Bytes())))

	if err := user.SetAddresses(db, addresses); err != nil {
		return fmt.Errorf("problem saving addresses: %w", err)
	}

	reply.Address = service.vm.Format(sk.PublicKey().Address().Bytes())
	return nil
}

// SendArgs are arguments for passing into Send requests
type SendArgs struct {
	Username string      `json:"username"`
	Password string      `json:"password"`
	Amount   json.Uint64 `json:"amount"`
	AssetID  string      `json:"assetID"`
	To       string      `json:"to"`
	From     string      `json:"from"`
}

// SendReply defines the Send replies returned from the API
type SendReply struct {
	TxID ids.ID `json:"txID"`
}

// Send returns the ID of the newly created transaction
func (service *Service) Send(r *http.Request, args *SendArgs, reply *SendReply) error {
	service.vm.ctx.Log.Verbo("Send called with username: %s", args.Username)

	if args.Amount == 0 {
		return errInvalidAmount
	}

	assetID, err := service.vm.Lookup(args.AssetID)
	if err != nil {
		assetID, err = ids.FromString(args.AssetID)
		if err != nil {
			return fmt.Errorf("asset '%s' not found", args.AssetID)
		}
	}

	toBytes, err := service.vm.Parse(args.To)
	if err != nil {
		return fmt.Errorf("problem parsing to address: %w", err)
	}
	to, err := ids.ToShortID(toBytes)
	if err != nil {
		return fmt.Errorf("problem parsing to address: %w", err)
	}

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := userState{vm: service.vm}

	addresses, _ := user.Addresses(db)

	addrs := ids.Set{}
	addrs.Add(addresses...)
	utxos, err := service.vm.GetUTXOs(addrs)
	if err != nil {
		return fmt.Errorf("problem retrieving user's UTXOs: %w", err)
	}

	kc := secp256k1fx.NewKeychain()
	for _, addr := range addresses {
		sk, err := user.Key(db, addr)
		if err != nil {
			return fmt.Errorf("problem retrieving private key: %w", err)
		}
		kc.Add(sk)
	}

	if args.From != "" {
		fromBytes, err := service.vm.Parse(args.From)
		if err != nil {
			return fmt.Errorf("problem parsing to address: %w", err)
		}
		from, err := ids.ToShortID(fromBytes)
		if err != nil {
			return fmt.Errorf("problem parsing to address: %w", err)
		}

		for _, sk := range kc.Keys {
			if sk.PublicKey().Address().String() == from.String() {
				kc = secp256k1fx.NewKeychain()
				kc.Add(sk)
				break
			}
		}
	}

	amountSpent := uint64(0)
	time := service.vm.clock.Unix()

	ins := []*ava.TransferableInput{}
	keys := [][]*crypto.PrivateKeySECP256K1R{}
	for _, utxo := range utxos {
		if !utxo.AssetID().Equals(assetID) {
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
		spent, err := safemath.Add64(amountSpent, input.Amount())
		if err != nil {
			return errSpendOverflow
		}
		amountSpent = spent

		in := &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: assetID},
			In:     input,
		}

		ins = append(ins, in)
		keys = append(keys, signers)

		if amountSpent >= uint64(args.Amount) {
			break
		}
	}

	if amountSpent < uint64(args.Amount) {
		return errInsufficientFunds
	}

	ava.SortTransferableInputsWithSigners(ins, keys)

	outs := []*ava.TransferableOutput{&ava.TransferableOutput{
		Asset: ava.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:      uint64(args.Amount),
			Locktime: 0,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	}}

	if amountSpent > uint64(args.Amount) {
		changeAddr := kc.Keys[0].PublicKey().Address()
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:      amountSpent - uint64(args.Amount),
				Locktime: 0,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	ava.SortTransferableOutputs(outs, service.vm.codec)

	tx := Tx{
		UnsignedTx: &BaseTx{
			NetID: service.vm.ctx.NetworkID,
			BCID:  service.vm.ctx.ChainID,
			Outs:  outs,
			Ins:   ins,
		},
	}

	unsignedBytes, err := service.vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	hash := hashing.ComputeHash256(unsignedBytes)

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

	b, err := service.vm.codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	txID, err := service.vm.IssueTx(b, nil)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	return nil
}

// CreateMintTxArgs are arguments for passing into CreateMintTx requests
type CreateMintTxArgs struct {
	Amount  json.Uint64 `json:"amount"`
	AssetID string      `json:"assetID"`
	To      string      `json:"to"`
	Minters []string    `json:"minters"`
}

// CreateMintTxReply defines the CreateMintTx replies returned from the API
type CreateMintTxReply struct {
	Tx formatting.CB58 `json:"tx"`
}

// CreateMintTx returns the newly created unsigned transaction
func (service *Service) CreateMintTx(r *http.Request, args *CreateMintTxArgs, reply *CreateMintTxReply) error {
	service.vm.ctx.Log.Verbo("CreateMintTx called")

	if args.Amount == 0 {
		return errInvalidMintAmount
	}

	assetID, err := service.vm.Lookup(args.AssetID)
	if err != nil {
		assetID, err = ids.FromString(args.AssetID)
		if err != nil {
			return fmt.Errorf("asset '%s' not found", args.AssetID)
		}
	}

	toBytes, err := service.vm.Parse(args.To)
	if err != nil {
		return fmt.Errorf("problem parsing to address '%s': %w", args.To, err)
	}
	to, err := ids.ToShortID(toBytes)
	if err != nil {
		return fmt.Errorf("problem parsing to address '%s': %w", args.To, err)
	}

	addrs := ids.Set{}
	minters := ids.ShortSet{}
	for _, minter := range args.Minters {
		addrBytes, err := service.vm.Parse(minter)
		if err != nil {
			return fmt.Errorf("problem parsing minter address '%s': %w", minter, err)
		}
		addr, err := ids.ToShortID(addrBytes)
		if err != nil {
			return fmt.Errorf("problem parsing minter address '%s': %w", minter, err)
		}
		addrs.Add(ids.NewID(hashing.ComputeHash256Array(addrBytes)))
		minters.Add(addr)
	}

	utxos, err := service.vm.GetUTXOs(addrs)
	if err != nil {
		return fmt.Errorf("problem getting user's UTXOs: %w", err)
	}

	for _, utxo := range utxos {
		switch out := utxo.Out.(type) {
		case *secp256k1fx.MintOutput:
			if !utxo.AssetID().Equals(assetID) {
				continue
			}
			sigs := []uint32{}
			for i := uint32(0); i < uint32(len(out.Addrs)) && uint32(len(sigs)) < out.Threshold; i++ {
				if minters.Contains(out.Addrs[i]) {
					sigs = append(sigs, i)
				}
			}

			if uint32(len(sigs)) != out.Threshold {
				continue
			}

			tx := Tx{UnsignedTx: &OperationTx{
				BaseTx: BaseTx{
					NetID: service.vm.ctx.NetworkID,
					BCID:  service.vm.ctx.ChainID,
				},
				Ops: []*Operation{
					&Operation{
						Asset: ava.Asset{ID: assetID},
						UTXOIDs: []*ava.UTXOID{
							&utxo.UTXOID,
						},
						Op: &secp256k1fx.MintOperation{
							MintInput: secp256k1fx.Input{
								SigIndices: sigs,
							},
							MintOutput: secp256k1fx.MintOutput{
								OutputOwners: out.OutputOwners,
							},
							TransferOutput: secp256k1fx.TransferOutput{
								Amt: uint64(args.Amount),
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{to},
								},
							},
						},
					},
				},
			}}

			txBytes, err := service.vm.codec.Marshal(&tx)
			if err != nil {
				return fmt.Errorf("problem creating transaction: %w", err)
			}
			reply.Tx.Bytes = txBytes
			return nil
		}
	}

	return errAddressesCantMintAsset
}

// SignMintTxArgs are arguments for passing into SignMintTx requests
type SignMintTxArgs struct {
	Username string          `json:"username"`
	Password string          `json:"password"`
	Minter   string          `json:"minter"`
	Tx       formatting.CB58 `json:"tx"`
}

// SignMintTxReply defines the SignMintTx replies returned from the API
type SignMintTxReply struct {
	Tx formatting.CB58 `json:"tx"`
}

// SignMintTx returns the newly signed transaction
func (service *Service) SignMintTx(r *http.Request, args *SignMintTxArgs, reply *SignMintTxReply) error {
	service.vm.ctx.Log.Verbo("SignMintTx called")

	minter, err := service.vm.Parse(args.Minter)
	if err != nil {
		return fmt.Errorf("problem parsing address '%s': %w", args.Minter, err)
	}

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := userState{vm: service.vm}

	addr := ids.NewID(hashing.ComputeHash256Array(minter))
	sk, err := user.Key(db, addr)
	if err != nil {
		return fmt.Errorf("problem retriving private key: %w", err)
	}

	tx := Tx{}
	if err := service.vm.codec.Unmarshal(args.Tx.Bytes, &tx); err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	opTx, ok := tx.UnsignedTx.(*OperationTx)
	if !ok {
		return errors.New("transaction must be a mint transaction")
	}
	if len(opTx.Ins) != 0 {
		return errCanOnlySignSingleInputTxs
	}
	if len(opTx.Ops) != 1 {
		return errCanOnlySignSingleInputTxs
	}
	op := opTx.Ops[0]

	if len(op.UTXOIDs) != 1 {
		return errCanOnlySignSingleInputTxs
	}
	inputUTXO := op.UTXOIDs[0]

	utxo, err := service.vm.getUTXO(inputUTXO)
	if err != nil {
		return err
	}

	out, ok := utxo.Out.(*secp256k1fx.MintOutput)
	if !ok {
		return errUnknownOutputType
	}

	secpOp, ok := op.Op.(*secp256k1fx.MintOperation)
	if !ok {
		return errors.New("unknown mint operation")
	}

	sigIndex := -1
	size := int(out.Threshold)
	for i, addrIndex := range secpOp.MintInput.SigIndices {
		if addrIndex >= uint32(len(out.Addrs)) {
			return errors.New("input output mismatch")
		}
		if bytes.Equal(out.Addrs[int(addrIndex)].Bytes(), minter) {
			sigIndex = i
			break
		}
	}
	if sigIndex == -1 {
		return errUnneededAddress
	}

	if len(tx.Creds) == 0 {
		tx.Creds = append(tx.Creds, &secp256k1fx.Credential{})
	}

	cred, ok := tx.Creds[0].(*secp256k1fx.Credential)
	if !ok {
		return errUnknownCredentialType
	}

	if len(cred.Sigs) != size {
		cred.Sigs = make([][crypto.SECP256K1RSigLen]byte, size)
	}

	unsignedBytes, err := service.vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	sig, err := sk.Sign(unsignedBytes)
	if err != nil {
		return fmt.Errorf("problem signing transaction: %w", err)
	}
	copy(cred.Sigs[sigIndex][:], sig)

	txBytes, err := service.vm.codec.Marshal(&tx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	reply.Tx.Bytes = txBytes
	return nil
}

// ImportAVAArgs are arguments for passing into ImportAVA requests
type ImportAVAArgs struct {
	// User that controls To
	Username string `json:"username"`
	Password string `json:"password"`

	// Address receiving the imported AVA
	To string `json:"to"`
}

// ImportAVAReply defines the ImportAVA replies returned from the API
type ImportAVAReply struct {
	TxID ids.ID `json:"txID"`
}

// ImportAVA imports AVA to this chain from the P-Chain.
// The AVA must have already been exported from the P-Chain.
// Returns the ID of the newly created atomic transaction
func (service *Service) ImportAVA(_ *http.Request, args *ImportAVAArgs, reply *ImportAVAReply) error {
	service.vm.ctx.Log.Verbo("ImportAVA called with username: %s", args.Username)

	toBytes, err := service.vm.Parse(args.To)
	if err != nil {
		return fmt.Errorf("problem parsing to address: %w", err)
	}
	to, err := ids.ToShortID(toBytes)
	if err != nil {
		return fmt.Errorf("problem parsing to address: %w", err)
	}

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := userState{vm: service.vm}

	addresses, _ := user.Addresses(db)

	addrs := ids.Set{}
	addrs.Add(addresses...)
	utxos, err := service.vm.GetAtomicUTXOs(addrs)
	if err != nil {
		return fmt.Errorf("problem retrieving user's atomic UTXOs: %w", err)
	}

	kc := secp256k1fx.NewKeychain()
	for _, addr := range addresses {
		sk, err := user.Key(db, addr)
		if err != nil {
			return fmt.Errorf("problem retrieving private key: %w", err)
		}
		kc.Add(sk)
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
		spent, err := safemath.Add64(amount, input.Amount())
		if err != nil {
			return errSpendOverflow
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

	ava.SortTransferableInputsWithSigners(ins, keys)

	outs := []*ava.TransferableOutput{&ava.TransferableOutput{
		Asset: ava.Asset{ID: service.vm.ava},
		Out: &secp256k1fx.TransferOutput{
			Amt:      amount,
			Locktime: 0,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	}}

	tx := Tx{UnsignedTx: &ImportTx{
		BaseTx: BaseTx{
			NetID: service.vm.ctx.NetworkID,
			BCID:  service.vm.ctx.ChainID,
			Outs:  outs,
		},
		Ins: ins,
	}}

	unsignedBytes, err := service.vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	hash := hashing.ComputeHash256(unsignedBytes)

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

	b, err := service.vm.codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	txID, err := service.vm.IssueTx(b, nil)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	return nil
}

// ExportAVAArgs are arguments for passing into ExportAVA requests
type ExportAVAArgs struct {
	// User providing exported AVA
	Username string `json:"username"`
	Password string `json:"password"`

	// Amount of nAVA to send
	Amount json.Uint64 `json:"amount"`

	// ID of P-Chain account that will receive the AVA
	To ids.ShortID `json:"to"`
}

// ExportAVAReply defines the Send replies returned from the API
type ExportAVAReply struct {
	TxID ids.ID `json:"txID"`
}

// ExportAVA sends AVA from this chain to the P-Chain.
// After this tx is accepted, the AVA must be imported to the P-chain with an importTx.
// Returns the ID of the newly created atomic transaction
func (service *Service) ExportAVA(_ *http.Request, args *ExportAVAArgs, reply *ExportAVAReply) error {
	service.vm.ctx.Log.Verbo("ExportAVA called with username: %s", args.Username)

	if args.Amount == 0 {
		return errInvalidAmount
	}

	db, err := service.vm.ctx.Keystore.GetDatabase(args.Username, args.Password)
	if err != nil {
		return fmt.Errorf("problem retrieving user: %w", err)
	}

	user := userState{vm: service.vm}

	addresses, _ := user.Addresses(db)

	addrs := ids.Set{}
	addrs.Add(addresses...)
	utxos, err := service.vm.GetUTXOs(addrs)
	if err != nil {
		return fmt.Errorf("problem retrieving user's UTXOs: %w", err)
	}

	kc := secp256k1fx.NewKeychain()
	for _, addr := range addresses {
		sk, err := user.Key(db, addr)
		if err != nil {
			return fmt.Errorf("problem retrieving private key: %w", err)
		}
		kc.Add(sk)
	}

	amountSpent := uint64(0)
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
		spent, err := safemath.Add64(amountSpent, input.Amount())
		if err != nil {
			return errSpendOverflow
		}
		amountSpent = spent

		in := &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: service.vm.ava},
			In:     input,
		}

		ins = append(ins, in)
		keys = append(keys, signers)

		if amountSpent >= uint64(args.Amount) {
			break
		}
	}

	if amountSpent < uint64(args.Amount) {
		return errInsufficientFunds
	}

	ava.SortTransferableInputsWithSigners(ins, keys)

	exportOuts := []*ava.TransferableOutput{&ava.TransferableOutput{
		Asset: ava.Asset{ID: service.vm.ava},
		Out: &secp256k1fx.TransferOutput{
			Amt:      uint64(args.Amount),
			Locktime: 0,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{args.To},
			},
		},
	}}

	outs := []*ava.TransferableOutput{}
	if amountSpent > uint64(args.Amount) {
		changeAddr := kc.Keys[0].PublicKey().Address()
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: service.vm.ava},
			Out: &secp256k1fx.TransferOutput{
				Amt:      amountSpent - uint64(args.Amount),
				Locktime: 0,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	ava.SortTransferableOutputs(outs, service.vm.codec)

	tx := Tx{UnsignedTx: &ExportTx{
		BaseTx: BaseTx{
			NetID: service.vm.ctx.NetworkID,
			BCID:  service.vm.ctx.ChainID,
			Outs:  outs,
			Ins:   ins,
		},
		Outs: exportOuts,
	}}

	unsignedBytes, err := service.vm.codec.Marshal(&tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}
	hash := hashing.ComputeHash256(unsignedBytes)

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

	b, err := service.vm.codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("problem creating transaction: %w", err)
	}

	txID, err := service.vm.IssueTx(b, nil)
	if err != nil {
		return fmt.Errorf("problem issuing transaction: %w", err)
	}

	reply.TxID = txID
	return nil
}
