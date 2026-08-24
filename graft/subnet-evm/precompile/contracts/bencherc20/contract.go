// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package bencherc20 is a USDC-shaped ERC-20 implemented as a stateful
// precompile, for benchmarking against the same token written in Solidity.
// It keeps the same guards a fiat-backed token runs on every transfer: a
// pause switch, a blocklist on both parties, and an owner-gated mint.
//
// batchTransfer is the third benchmark level: one transaction carries many
// EIP-712-signed transfer authorizations (the "gasless" MetaMask flow), each
// verified with ecrecover and replay-protected by a per-authorization nonce.
package bencherc20

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
)

// EVMChainID is baked into the EIP-712 domain separator at compile time.
// The benchmark genesis must use this chainId. A production precompile would
// read the chain id from the EVM context instead.
const EVMChainID = 77777

// RecordLen is the packed size of one batchTransfer record:
// from(20) + to(20) + value(32) + nonce(32) + v(1) + r(32) + s(32).
const RecordLen = 169

// Gas costs are deliberately small, in line with what the native code
// actually does (the stock ecrecover precompile charges 3000): the benchmark
// must show the execution ceiling, so the 200M block gas limit must never be
// the binding constraint on the precompile levels.
const (
	BalanceOfGasCost        = 2_100
	TransferGasCost         = 5_000
	MintGasCost             = 30_000
	SetterGasCost           = 25_000
	BatchBaseGasCost        = 3_000
	BatchPerTransferGasCost = 6_000
)

const rawABI = `[
{"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"transfer","stateMutability":"nonpayable","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}]},
{"type":"function","name":"mint","stateMutability":"nonpayable","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[]},
{"type":"function","name":"setPaused","stateMutability":"nonpayable","inputs":[{"name":"paused","type":"bool"}],"outputs":[]},
{"type":"function","name":"setBlocklisted","stateMutability":"nonpayable","inputs":[{"name":"account","type":"address"},{"name":"blocked","type":"bool"}],"outputs":[]},
{"type":"function","name":"batchTransfer","stateMutability":"nonpayable","inputs":[{"name":"records","type":"bytes"}],"outputs":[]}
]`

var (
	BenchERC20Precompile contract.StatefulPrecompiledContract = createPrecompile()

	ABI = contract.ParseABI(rawABI)

	ErrPaused           = errors.New("token is paused")
	ErrBlocklisted      = errors.New("address is blocklisted")
	ErrInsufficient     = errors.New("insufficient balance")
	ErrNotOwner         = errors.New("caller is not the owner")
	ErrBadSignature     = errors.New("invalid transfer authorization signature")
	ErrNonceUsed        = errors.New("authorization nonce already used")
	ErrBadRecordLen     = errors.New("records length is not a multiple of RecordLen")
	ErrUnpackInput      = errors.New("failed to unpack input")
	ErrValueOutOfBounds = errors.New("value out of bounds")

	transferEventID = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

	ownerSlot  = common.BytesToHash([]byte("owner"))
	pausedSlot = common.BytesToHash([]byte("paused"))

	oneHash = common.BigToHash(common.Big1)
)

// DomainSeparator is the EIP-712 domain hash MetaMask would compute for this
// token: name "BenchToken", version "1", the chain id, and the precompile
// address as the verifying contract.
func DomainSeparator(chainID *big.Int, verifyingContract common.Address) common.Hash {
	return crypto.Keccak256Hash(
		crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")),
		crypto.Keccak256([]byte("BenchToken")),
		crypto.Keccak256([]byte("1")),
		common.BigToHash(chainID).Bytes(),
		common.BytesToHash(verifyingContract.Bytes()).Bytes(),
	)
}

var (
	authTypeHash    = crypto.Keccak256Hash([]byte("TransferWithAuthorization(address from,address to,uint256 value,bytes32 nonce)"))
	domainSeparator = DomainSeparator(big.NewInt(EVMChainID), ContractAddress)
)

// AuthDigest is the digest a user signs (eth_signTypedData_v4) to authorize
// one gasless transfer.
func AuthDigest(from, to common.Address, value *big.Int, nonce common.Hash) common.Hash {
	structHash := crypto.Keccak256(
		authTypeHash.Bytes(),
		common.BytesToHash(from.Bytes()).Bytes(),
		common.BytesToHash(to.Bytes()).Bytes(),
		common.BigToHash(value).Bytes(),
		nonce.Bytes(),
	)
	return crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSeparator.Bytes(), structHash)
}

// AppendRecord packs one signed authorization into the batchTransfer wire
// format. sig is the 65-byte [R || S || V] output of crypto.Sign over
// AuthDigest; V may be 0/1 or 27/28.
func AppendRecord(dst []byte, from, to common.Address, value *big.Int, nonce common.Hash, sig []byte) []byte {
	dst = append(dst, from.Bytes()...)
	dst = append(dst, to.Bytes()...)
	dst = append(dst, common.BigToHash(value).Bytes()...)
	dst = append(dst, nonce.Bytes()...)
	dst = append(dst, sig[64])
	dst = append(dst, sig[:64]...)
	return dst
}

func balanceSlot(addr common.Address) common.Hash {
	return crypto.Keccak256Hash(addr.Bytes(), []byte("balance"))
}

func blockedSlot(addr common.Address) common.Hash {
	return crypto.Keccak256Hash(addr.Bytes(), []byte("blocked"))
}

func authNonceSlot(from common.Address, nonce common.Hash) common.Hash {
	return crypto.Keccak256Hash(from.Bytes(), nonce.Bytes(), []byte("authnonce"))
}

func getBalance(stateDB contract.StateDB, addr common.Address) *big.Int {
	return stateDB.GetState(ContractAddress, balanceSlot(addr)).Big()
}

// checkGuards runs the USDC-style transfer guards: pause switch plus a
// blocklist read for both parties.
func checkGuards(stateDB contract.StateDB, from, to common.Address) error {
	if stateDB.GetState(ContractAddress, pausedSlot) != (common.Hash{}) {
		return ErrPaused
	}
	if stateDB.GetState(ContractAddress, blockedSlot(from)) != (common.Hash{}) {
		return fmt.Errorf("%w: %s", ErrBlocklisted, from)
	}
	if stateDB.GetState(ContractAddress, blockedSlot(to)) != (common.Hash{}) {
		return fmt.Errorf("%w: %s", ErrBlocklisted, to)
	}
	return nil
}

func moveBalance(stateDB contract.StateDB, from, to common.Address, value *big.Int) error {
	fromBalance := getBalance(stateDB, from)
	if fromBalance.Cmp(value) < 0 {
		return fmt.Errorf("%w: %s has %s, needs %s", ErrInsufficient, from, fromBalance, value)
	}
	stateDB.SetState(ContractAddress, balanceSlot(from), common.BigToHash(fromBalance.Sub(fromBalance, value)))
	toBalance := getBalance(stateDB, to)
	stateDB.SetState(ContractAddress, balanceSlot(to), common.BigToHash(toBalance.Add(toBalance, value)))
	return nil
}

func addTransferLog(accessibleState contract.AccessibleState, from, to common.Address, value *big.Int) {
	accessibleState.GetStateDB().AddLog(&types.Log{
		Address:     ContractAddress,
		Topics:      []common.Hash{transferEventID, common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
		Data:        common.BigToHash(value).Bytes(),
		BlockNumber: accessibleState.GetBlockContext().Number().Uint64(),
	})
}

func requireOwner(stateDB contract.StateDB, caller common.Address) error {
	if stateDB.GetState(ContractAddress, ownerSlot) != common.BytesToHash(caller.Bytes()) {
		return fmt.Errorf("%w: %s", ErrNotOwner, caller)
	}
	return nil
}

func balanceOf(accessibleState contract.AccessibleState, _ common.Address, _ common.Address, input []byte, suppliedGas uint64, _ bool) ([]byte, uint64, error) {
	remainingGas, err := contract.DeductGas(suppliedGas, BalanceOfGasCost)
	if err != nil {
		return nil, 0, err
	}
	var account common.Address
	if err := ABI.UnpackInputIntoInterface(&account, "balanceOf", input); err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %w", ErrUnpackInput, err)
	}
	return common.BigToHash(getBalance(accessibleState.GetStateDB(), account)).Bytes(), remainingGas, nil
}

type transferInput struct {
	To     common.Address
	Amount *big.Int
}

func transfer(accessibleState contract.AccessibleState, caller common.Address, _ common.Address, input []byte, suppliedGas uint64, readOnly bool) ([]byte, uint64, error) {
	remainingGas, err := contract.DeductGas(suppliedGas, TransferGasCost)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	var in transferInput
	if err := ABI.UnpackInputIntoInterface(&in, "transfer", input); err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %w", ErrUnpackInput, err)
	}
	stateDB := accessibleState.GetStateDB()
	if err := checkGuards(stateDB, caller, in.To); err != nil {
		return nil, remainingGas, err
	}
	if err := moveBalance(stateDB, caller, in.To, in.Amount); err != nil {
		return nil, remainingGas, err
	}
	addTransferLog(accessibleState, caller, in.To, in.Amount)
	return oneHash.Bytes(), remainingGas, nil
}

type mintInput struct {
	To     common.Address
	Amount *big.Int
}

func mint(accessibleState contract.AccessibleState, caller common.Address, _ common.Address, input []byte, suppliedGas uint64, readOnly bool) ([]byte, uint64, error) {
	remainingGas, err := contract.DeductGas(suppliedGas, MintGasCost)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	var in mintInput
	if err := ABI.UnpackInputIntoInterface(&in, "mint", input); err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %w", ErrUnpackInput, err)
	}
	stateDB := accessibleState.GetStateDB()
	if err := requireOwner(stateDB, caller); err != nil {
		return nil, remainingGas, err
	}
	toBalance := getBalance(stateDB, in.To)
	stateDB.SetState(ContractAddress, balanceSlot(in.To), common.BigToHash(toBalance.Add(toBalance, in.Amount)))
	addTransferLog(accessibleState, common.Address{}, in.To, in.Amount)
	return []byte{}, remainingGas, nil
}

func setPaused(accessibleState contract.AccessibleState, caller common.Address, _ common.Address, input []byte, suppliedGas uint64, readOnly bool) ([]byte, uint64, error) {
	remainingGas, err := contract.DeductGas(suppliedGas, SetterGasCost)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	var paused bool
	if err := ABI.UnpackInputIntoInterface(&paused, "setPaused", input); err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %w", ErrUnpackInput, err)
	}
	stateDB := accessibleState.GetStateDB()
	if err := requireOwner(stateDB, caller); err != nil {
		return nil, remainingGas, err
	}
	value := common.Hash{}
	if paused {
		value = oneHash
	}
	stateDB.SetState(ContractAddress, pausedSlot, value)
	return []byte{}, remainingGas, nil
}

type setBlocklistedInput struct {
	Account common.Address
	Blocked bool
}

func setBlocklisted(accessibleState contract.AccessibleState, caller common.Address, _ common.Address, input []byte, suppliedGas uint64, readOnly bool) ([]byte, uint64, error) {
	remainingGas, err := contract.DeductGas(suppliedGas, SetterGasCost)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	var in setBlocklistedInput
	if err := ABI.UnpackInputIntoInterface(&in, "setBlocklisted", input); err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %w", ErrUnpackInput, err)
	}
	stateDB := accessibleState.GetStateDB()
	if err := requireOwner(stateDB, caller); err != nil {
		return nil, remainingGas, err
	}
	value := common.Hash{}
	if in.Blocked {
		value = oneHash
	}
	stateDB.SetState(ContractAddress, blockedSlot(in.Account), value)
	return []byte{}, remainingGas, nil
}

// batchTransfer applies a batch of EIP-712-signed transfer authorizations.
// Anyone may submit the batch (the relayer pays gas); authority comes from
// each record's signature. Any invalid record fails the whole call.
func batchTransfer(accessibleState contract.AccessibleState, _ common.Address, _ common.Address, input []byte, suppliedGas uint64, readOnly bool) ([]byte, uint64, error) {
	remainingGas, err := contract.DeductGas(suppliedGas, BatchBaseGasCost)
	if err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	var records []byte
	if err := ABI.UnpackInputIntoInterface(&records, "batchTransfer", input); err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %w", ErrUnpackInput, err)
	}
	if len(records)%RecordLen != 0 {
		return nil, remainingGas, fmt.Errorf("%w: %d", ErrBadRecordLen, len(records))
	}
	count := len(records) / RecordLen
	if remainingGas, err = contract.DeductGas(remainingGas, uint64(count)*BatchPerTransferGasCost); err != nil {
		return nil, 0, err
	}

	// Verify every signature concurrently before touching state. Signature
	// checks read only the record bytes, so this is deterministic; it is also
	// where native code beats the EVM, which has no way to use more than one
	// core. State changes are applied serially below.
	type record struct {
		from, to common.Address
		value    *big.Int
		nonce    common.Hash
	}
	parsed := make([]record, count)
	badSig := make([]bool, count)
	var wg sync.WaitGroup
	workers := min(runtime.NumCPU(), 8)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := w; i < count; i += workers {
				raw := records[i*RecordLen : (i+1)*RecordLen]
				from := common.BytesToAddress(raw[0:20])
				to := common.BytesToAddress(raw[20:40])
				value := new(big.Int).SetBytes(raw[40:72])
				nonce := common.BytesToHash(raw[72:104])
				parsed[i] = record{from: from, to: to, value: value, nonce: nonce}

				sig := make([]byte, 65)
				copy(sig, raw[105:169])
				v := raw[104]
				if v >= 27 {
					v -= 27
				}
				sig[64] = v
				pubKey, err := crypto.SigToPub(AuthDigest(from, to, value, nonce).Bytes(), sig)
				badSig[i] = err != nil || crypto.PubkeyToAddress(*pubKey) != from
			}
		}(w)
	}
	wg.Wait()

	stateDB := accessibleState.GetStateDB()
	for i, rec := range parsed {
		if badSig[i] {
			return nil, remainingGas, fmt.Errorf("%w: record %d", ErrBadSignature, i)
		}
		nonceSlot := authNonceSlot(rec.from, rec.nonce)
		if stateDB.GetState(ContractAddress, nonceSlot) != (common.Hash{}) {
			return nil, remainingGas, fmt.Errorf("%w: %s", ErrNonceUsed, rec.nonce)
		}
		stateDB.SetState(ContractAddress, nonceSlot, oneHash)

		if err := checkGuards(stateDB, rec.from, rec.to); err != nil {
			return nil, remainingGas, err
		}
		if err := moveBalance(stateDB, rec.from, rec.to, rec.value); err != nil {
			return nil, remainingGas, err
		}
		addTransferLog(accessibleState, rec.from, rec.to, rec.value)
	}
	return []byte{}, remainingGas, nil
}

func createPrecompile() contract.StatefulPrecompiledContract {
	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"balanceOf":      balanceOf,
		"transfer":       transfer,
		"mint":           mint,
		"setPaused":      setPaused,
		"setBlocklisted": setBlocklisted,
		"batchTransfer":  batchTransfer,
	}
	functions := make([]*contract.StatefulPrecompileFunction, 0, len(abiFunctionMap))
	for name, function := range abiFunctionMap {
		method, ok := ABI.Methods[name]
		if !ok {
			panic(fmt.Errorf("given method (%s) does not exist in the ABI", name))
		}
		functions = append(functions, contract.NewStatefulPrecompileFunction(method.ID, function))
	}
	statefulContract, err := contract.NewStatefulPrecompileContract(nil, functions)
	if err != nil {
		panic(err)
	}
	return statefulContract
}
