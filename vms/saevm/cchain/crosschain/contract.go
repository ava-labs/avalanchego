// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crosschain

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"

	_ "embed"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"

	ethparams "github.com/ava-labs/libevm/params"
)

// Gas charges. Import covers one shared memory read and one consumption write
// per UTXO. Export covers the UTXO write and the shared memory index update.
const (
	ImportBaseGas      uint64 = 20_000
	ImportUTXOGas      uint64 = 5_000
	SetRemoteImportGas uint64 = 20_000
	ExportGas          uint64 = 40_000
)

//go:embed ICrossChainTransfer.abi
var rawABI string

var (
	ABI        = contract.ParseABI(rawABI)
	Precompile = createPrecompile()

	errNotDirectCall    = errors.New("importUTXOs must be called directly by the transaction sender")
	errUnverifiedImport = errors.New("UTXO was not verified for this block")
	errDuplicateUTXO    = errors.New("duplicate UTXO in import")
	errNotOwner         = errors.New("caller is not the UTXO owner and the owner did not allow remote imports")
	errBadAmount        = errors.New("export value must be a positive whole number of nAVAX below 2^64")
	errBadDestination   = errors.New("export destination must be the P-Chain or X-Chain")
)

// UTXOID mirrors the ABI struct.
type UTXOID struct {
	TxID        [32]byte
	OutputIndex uint32
}

// blockImports holds the verified imports of the blocks being executed, keyed
// by height. StartExecutingBlock fills it from the block's extra data.
//
// ponytail: an LRU instead of explicit cleanup, because historical and
// canonical execution of the same height can overlap.
var blockImports = lru.NewCache[uint64, map[avax.UTXOID]tx.ImportRecord](1024)

// SetBlockImports records the verified imports of the block at height.
func SetBlockImports(height uint64, records []tx.ImportRecord) {
	m := make(map[avax.UTXOID]tx.ImportRecord, len(records))
	for _, r := range records {
		m[r.UTXOID] = r
	}
	blockImports.Put(height, m)
}

// Resolver looks UTXOs up in live shared memory. It only serves RPC
// simulations (eth_call, eth_estimateGas), which run without a verified block.
type Resolver func(utxos []avax.UTXOID, blockTime uint64) ([]tx.ImportRecord, error)

var resolver Resolver

func SetResolver(r Resolver) { resolver = r }

// ImportCalldata reports whether t is a direct importUTXOs call and returns
// the UTXO IDs it names.
func ImportCalldata(t *types.Transaction) ([]avax.UTXOID, bool) {
	if to := t.To(); to == nil || *to != ContractAddress {
		return nil, false
	}
	ids, err := unpackImport(t.Data())
	return ids, err == nil
}

// unpackImport parses full calldata, selector included.
func unpackImport(calldata []byte) ([]avax.UTXOID, error) {
	method := ABI.Methods["importUTXOs"]
	if len(calldata) < contract.SelectorLen || string(calldata[:contract.SelectorLen]) != string(method.ID) {
		return nil, errors.New("not an importUTXOs call")
	}
	return unpackImportArgs(calldata[contract.SelectorLen:])
}

// unpackImportArgs parses the arguments that follow the selector, which is
// what the precompile framework hands to each function.
func unpackImportArgs(args []byte) ([]avax.UTXOID, error) {
	var utxos []UTXOID
	if err := ABI.UnpackInputIntoInterface(&utxos, "importUTXOs", args); err != nil {
		return nil, err
	}
	out := make([]avax.UTXOID, len(utxos))
	for i, u := range utxos {
		out[i] = avax.UTXOID{TxID: ids.ID(u.TxID), OutputIndex: u.OutputIndex}
	}
	return out, nil
}

// RemoteImportKey is the storage slot of owner's remote import flag.
func RemoteImportKey(owner common.Address) common.Hash {
	return common.BytesToHash(owner[:])
}

func importUTXOs(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, ImportBaseGas); err != nil {
		return nil, 0, err
	}
	env := accessibleState.GetPrecompileEnv()
	if addrs := env.Addresses(); env.IncomingCallType() != vm.Call || addrs.EVMSemantic.Caller != addrs.Origin {
		return nil, remainingGas, errNotDirectCall
	}
	utxos, err := unpackImportArgs(input)
	if err != nil {
		return nil, remainingGas, err
	}
	utxoGas, overflow := math.SafeMul(ImportUTXOGas, uint64(len(utxos)))
	if overflow {
		return nil, 0, vm.ErrOutOfGas
	}
	if remainingGas, err = contract.DeductGas(remainingGas, utxoGas); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}

	records, err := verifiedImports(env.BlockNumber(), env.BlockTime(), utxos)
	if err != nil {
		return nil, remainingGas, err
	}
	statedb := accessibleState.GetStateDB()
	seen := make(map[avax.UTXOID]struct{}, len(utxos))
	for _, id := range utxos {
		if _, ok := seen[id]; ok {
			return nil, remainingGas, errDuplicateUTXO
		}
		seen[id] = struct{}{}
		r, ok := records[id]
		if !ok {
			return nil, remainingGas, fmt.Errorf("%w: %s", errUnverifiedImport, id.InputID())
		}
		owner := common.Address(r.Owner)
		if owner != caller && statedb.GetState(ContractAddress, RemoteImportKey(owner)) == (common.Hash{}) {
			return nil, remainingGas, fmt.Errorf("%w: %s", errNotOwner, id.InputID())
		}
		statedb.AddBalance(owner, new(uint256.Int).Mul(uint256.NewInt(r.Amount), uint256.NewInt(ethparams.GWei)))
		topics, data, err := ABI.PackEvent("Imported", owner, r.UTXOID.TxID, r.UTXOID.OutputIndex, r.Amount)
		if err != nil {
			return nil, remainingGas, err
		}
		statedb.AddLog(&types.Log{Address: ContractAddress, Topics: topics, Data: data, BlockNumber: env.BlockNumber().Uint64()})
	}
	return nil, remainingGas, nil
}

// verifiedImports returns the block's verified imports, or resolves them from
// live shared memory when no block was verified (RPC simulation).
func verifiedImports(number *big.Int, blockTime uint64, utxos []avax.UTXOID) (map[avax.UTXOID]tx.ImportRecord, error) {
	if m, ok := blockImports.Get(number.Uint64()); ok {
		return m, nil
	}
	if resolver == nil {
		return nil, errUnverifiedImport
	}
	records, err := resolver(utxos, blockTime)
	if err != nil {
		return nil, err
	}
	m := make(map[avax.UTXOID]tx.ImportRecord, len(records))
	for _, r := range records {
		m[r.UTXOID] = r
	}
	return m, nil
}

func setRemoteImport(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, SetRemoteImportGas); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	var allowed bool
	if err := ABI.UnpackInputIntoInterface(&allowed, "setRemoteImport", input); err != nil {
		return nil, remainingGas, err
	}
	var value common.Hash
	if allowed {
		value = common.Hash{31: 1}
	}
	statedb := accessibleState.GetStateDB()
	statedb.SetState(ContractAddress, RemoteImportKey(caller), value)
	topics, data, err := ABI.PackEvent("RemoteImportSet", caller, allowed)
	if err != nil {
		return nil, remainingGas, err
	}
	statedb.AddLog(&types.Log{Address: ContractAddress, Topics: topics, Data: data, BlockNumber: accessibleState.GetBlockContext().Number().Uint64()})
	return nil, remainingGas, nil
}

type exportInput struct {
	DestinationChainID [32]byte
	To                 common.Address
}

func exportAVAX(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, ExportGas); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	var in exportInput
	if err := ABI.UnpackInputIntoInterface(&in, "exportAVAX", input); err != nil {
		return nil, remainingGas, err
	}
	env := accessibleState.GetPrecompileEnv()
	value := env.Value()
	var nAVAX, rem uint256.Int
	nAVAX.DivMod(value, uint256.NewInt(ethparams.GWei), &rem)
	if value.IsZero() || !rem.IsZero() || !nAVAX.IsUint64() {
		return nil, remainingGas, errBadAmount
	}
	dest := ids.ID(in.DestinationChainID)
	if dest != constants.PlatformChainID && dest != accessibleState.GetSnowContext().XChainID {
		return nil, remainingGas, errBadDestination
	}
	topics, data, err := ABI.PackEvent("Exported", caller, in.DestinationChainID, in.To, nAVAX.Uint64())
	if err != nil {
		return nil, remainingGas, err
	}
	accessibleState.GetStateDB().AddLog(&types.Log{Address: ContractAddress, Topics: topics, Data: data, BlockNumber: env.BlockNumber().Uint64()})
	return nil, remainingGas, nil
}

// Export is one exportAVAX call parsed from a receipt log.
type Export struct {
	From        common.Address
	Destination ids.ID
	To          ids.ShortID
	Amount      uint64
	UTXOID      avax.UTXOID
}

// Import is one credited UTXO parsed from a receipt log.
type Import struct {
	Owner  common.Address
	UTXOID avax.UTXOID
}

// FromReceipts returns the exports and imports the precompile logged in
// successful transactions.
func FromReceipts(receipts types.Receipts) ([]Export, []Import, error) {
	var (
		exports  []Export
		imports  []Import
		exportID = ABI.Events["Exported"].ID
		importID = ABI.Events["Imported"].ID
	)
	for _, r := range receipts {
		if r.Status != types.ReceiptStatusSuccessful {
			continue
		}
		for _, l := range r.Logs {
			if l.Address != ContractAddress || len(l.Topics) == 0 {
				continue
			}
			switch l.Topics[0] {
			case exportID:
				if len(l.Topics) != 3 {
					return nil, nil, fmt.Errorf("Exported log %s/%d has %d topics", l.TxHash, l.Index, len(l.Topics))
				}
				var out struct {
					To          common.Address
					AmountNAVAX uint64
				}
				if err := ABI.UnpackIntoInterface(&out, "Exported", l.Data); err != nil {
					return nil, nil, fmt.Errorf("parsing Exported log %s/%d: %w", l.TxHash, l.Index, err)
				}
				exports = append(exports, Export{
					From:        common.BytesToAddress(l.Topics[1][:]),
					Destination: ids.ID(l.Topics[2]),
					To:          ids.ShortID(out.To),
					Amount:      out.AmountNAVAX,
					UTXOID:      avax.UTXOID{TxID: ids.ID(l.TxHash), OutputIndex: uint32(l.Index)}, //#nosec G115 -- Won't overflow
				})
			case importID:
				if len(l.Topics) != 2 {
					return nil, nil, fmt.Errorf("Imported log %s/%d has %d topics", l.TxHash, l.Index, len(l.Topics))
				}
				var out struct {
					TxID        [32]byte
					OutputIndex uint32
					AmountNAVAX uint64
				}
				if err := ABI.UnpackIntoInterface(&out, "Imported", l.Data); err != nil {
					return nil, nil, fmt.Errorf("parsing Imported log %s/%d: %w", l.TxHash, l.Index, err)
				}
				imports = append(imports, Import{
					Owner:  common.BytesToAddress(l.Topics[1][:]),
					UTXOID: avax.UTXOID{TxID: ids.ID(out.TxID), OutputIndex: out.OutputIndex},
				})
			}
		}
	}
	return exports, imports, nil
}

func createPrecompile() contract.StatefulPrecompiledContract {
	fns := map[string]contract.RunStatefulPrecompileFunc{
		"importUTXOs":     importUTXOs,
		"setRemoteImport": setRemoteImport,
		"exportAVAX":      exportAVAX,
	}
	functions := make([]*contract.StatefulPrecompileFunction, 0, len(fns))
	for name, fn := range fns {
		method, ok := ABI.Methods[name]
		if !ok {
			panic(fmt.Errorf("method %s not in ABI", name))
		}
		functions = append(functions, contract.NewStatefulPrecompileFunction(method.ID, fn))
	}
	c, err := contract.NewStatefulPrecompileContract(nil, functions)
	if err != nil {
		panic(err)
	}
	return c
}
