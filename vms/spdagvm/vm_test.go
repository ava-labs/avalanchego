// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/units"
)

var keys []*crypto.PrivateKeySECP256K1R
var ctx *snow.Context
var avaxChainID = ids.NewID([32]byte{'y', 'e', 'e', 't'})
var defaultInitBalances = make(map[string]uint64)

const txFeeTest = 0 // Tx fee to use for tests

const (
	defaultInitBalance = uint64(5000000000) // Measured in NanoAvax
)

func init() {
	ctx = snow.DefaultContextTest()
	ctx.ChainID = avaxChainID
	cb58 := formatting.CB58{}
	factory := crypto.FactorySECP256K1R{}

	// String reprs. of private keys. Copy-pasted from genesis.go.
	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	} {
		ctx.Log.AssertNoError(cb58.FromString(key))
		pk, err := factory.ToPrivateKey(cb58.Bytes)
		ctx.Log.AssertNoError(err)
		keys = append(keys, pk.(*crypto.PrivateKeySECP256K1R))

		defaultInitBalances[pk.PublicKey().Address().String()] = defaultInitBalance
	}
}

// GenesisTx is the genesis transaction
// The amount given to each address is determined by [initBalances]
// [initBalances] keys are string reprs. of addresses
// [initBalances] values are the amount of NanoAvax they have at genesis
func GenesisTx(initBalances map[string]uint64) *Tx {
	builder := Builder{
		NetworkID: 0,
		ChainID:   avaxChainID,
	}

	outputs := []Output(nil)
	for _, key := range keys {
		addr := key.PublicKey().Address()
		if balance, ok := initBalances[addr.String()]; ok {
			outputs = append(outputs,
				builder.NewOutputPayment(
					/*amount=*/ balance,
					/*locktime=*/ 0,
					/*threshold=*/ 1,
					/*addresses=*/ []ids.ShortID{addr},
				),
			)
		}

	}

	result, _ := builder.NewTx(
		/*ins=*/ nil,
		/*outs=*/ outputs,
		/*signers=*/ nil,
	)
	return result
}

func TestAvax(t *testing.T) {
	// Give
	genesisTx := GenesisTx(defaultInitBalances)

	vmDB := memdb.New()

	msgChan := make(chan common.Message, 1)

	vm := &VM{}
	defer func() { vm.ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
	vm.Initialize(ctx, vmDB, genesisTx.Bytes(), msgChan, nil)
	vm.batchTimeout = 0

	builder := Builder{
		NetworkID: 0,
		ChainID:   avaxChainID,
	}
	tx1, err := builder.NewTx(
		/*ins=*/ []Input{
			builder.NewInputPayment(
				/*txID=*/ genesisTx.ID(),
				/*txIndex=*/ 0,
				/*amount=*/ 5*units.Avax,
				/*sigs=*/ []*Sig{builder.NewSig(0 /*=index*/)},
			),
		},
		/*outs=*/ []Output{
			builder.NewOutputPayment(
				/*amount=*/ 3*units.Avax,
				/*locktime=*/ 0,
				/*threshold=*/ 0,
				/*addresses=*/ nil,
			),
		},
		/*signers=*/ []*InputSigner{
			{Keys: []*crypto.PrivateKeySECP256K1R{
				keys[1],
			}},
		},
	)
	ctx.Log.AssertNoError(err)
	tx1Bytes := tx1.Bytes()

	ctx.Lock.Lock()
	vm.IssueTx(tx1Bytes, nil)
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()
	if txs := vm.PendingTxs(); len(txs) != 1 {
		t.Fatalf("Should have returned a tx")
	} else if tx := txs[0]; !tx.ID().Equals(tx1.ID()) {
		t.Fatalf("Should have returned %s", tx1.ID())
	}
	ctx.Lock.Unlock()

	tx2, err := builder.NewTx(
		/*ins=*/ []Input{
			builder.NewInputPayment(
				/*txID=*/ tx1.ID(),
				/*txIndex=*/ 0,
				/*amount=*/ 3*units.Avax,
				/*sigs=*/ []*Sig{},
			),
		},
		/*outs=*/ nil,
		/*signers=*/ []*InputSigner{{}},
	)
	ctx.Log.AssertNoError(err)
	tx2Bytes := tx2.Bytes()

	ctx.Lock.Lock()
	vm.IssueTx(tx2Bytes, nil)
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}
}

func TestInvalidSpentTx(t *testing.T) {
	genesisTx := GenesisTx(defaultInitBalances)

	vmDB := memdb.New()

	msgChan := make(chan common.Message, 1)

	vm := &VM{}
	defer func() { vm.ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()

	ctx.Lock.Lock()
	vm.Initialize(ctx, vmDB, genesisTx.Bytes(), msgChan, nil)
	vm.batchTimeout = 0

	builder := Builder{
		NetworkID: 0,
		ChainID:   avaxChainID,
	}
	tx1, _ := builder.NewTx(
		/*ins=*/ []Input{
			builder.NewInputPayment(
				/*txID=*/ genesisTx.ID(),
				/*txIndex=*/ 0,
				/*amount=*/ 5*units.Avax,
				/*sigs=*/ []*Sig{builder.NewSig(0 /*=index*/)},
			),
		},
		/*outs=*/ []Output{
			builder.NewOutputPayment(
				/*amount=*/ 3*units.Avax,
				/*locktime=*/ 0,
				/*threshold=*/ 0,
				/*addresses=*/ nil,
			),
		},
		/*signers=*/ []*InputSigner{
			{Keys: []*crypto.PrivateKeySECP256K1R{
				keys[1],
			}},
		},
	)
	tx2, _ := builder.NewTx(
		/*ins=*/ []Input{
			builder.NewInputPayment(
				/*txID=*/ genesisTx.ID(),
				/*txIndex=*/ 0,
				/*amount=*/ 5*units.Avax,
				/*sigs=*/ []*Sig{builder.NewSig(0 /*=index*/)},
			),
		},
		/*outs=*/ []Output{
			builder.NewOutputPayment(
				/*amount=*/ 2*units.Avax,
				/*locktime=*/ 0,
				/*threshold=*/ 0,
				/*addresses=*/ nil,
			),
		},
		/*signers=*/ []*InputSigner{
			{Keys: []*crypto.PrivateKeySECP256K1R{
				keys[1],
			}},
		},
	)

	wrappedTx1, err := vm.wrapTx(tx1, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := wrappedTx1.Verify(); err != nil {
		t.Fatal(err)
	}

	wrappedTx1.Accept()

	wrappedTx2, err := vm.wrapTx(tx2, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := wrappedTx2.Verify(); err == nil {
		t.Fatalf("Should have failed verification")
	}
	ctx.Lock.Unlock()
}

func TestInvalidTxVerification(t *testing.T) {
	genesisTx := GenesisTx(defaultInitBalances)

	vmDB := memdb.New()

	msgChan := make(chan common.Message, 1)

	vm := &VM{}
	defer func() { vm.ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()

	ctx.Lock.Lock()
	vm.Initialize(ctx, vmDB, genesisTx.Bytes(), msgChan, nil)
	vm.batchTimeout = 0

	builder := Builder{
		NetworkID: 0,
		ChainID:   avaxChainID,
	}
	tx, _ := builder.NewTx(
		/*ins=*/ []Input{
			builder.NewInputPayment(
				/*txID=*/ genesisTx.ID(),
				/*txIndex=*/ 2345,
				/*amount=*/ 50000+txFeeTest,
				/*sigs=*/ []*Sig{builder.NewSig(0 /*=index*/)},
			),
		},
		/*outs=*/ []Output{
			builder.NewOutputPayment(
				/*amount=*/ 50000,
				/*locktime=*/ 0,
				/*threshold=*/ 0,
				/*addresses=*/ nil,
			),
		},
		/*signers=*/ []*InputSigner{
			{Keys: []*crypto.PrivateKeySECP256K1R{
				keys[1],
			}},
		},
	)

	wrappedTx, err := vm.wrapTx(tx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := wrappedTx.Verify(); err == nil {
		t.Fatalf("Should have failed verification")
	}

	vm.state.uniqueTx.Flush()

	wrappedTx2, err := vm.wrapTx(tx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := wrappedTx2.Verify(); err == nil {
		t.Fatalf("Should have failed verification")
	}
	ctx.Lock.Unlock()
}

func TestRPCAPI(t *testing.T) {
	// Initialize avm vm with the genesis transaction
	genesisTx := GenesisTx(defaultInitBalances)
	vmDB := memdb.New()
	msgChan := make(chan common.Message, 1)
	vm := &VM{}
	defer func() { vm.ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
	vm.Initialize(ctx, vmDB, genesisTx.Bytes(), msgChan, nil)
	vm.batchTimeout = 0

	// Key: string repr. of an address
	// Value: string repr. of the private key that controls the address
	addrToPK := map[string]string{}

	// Inverse of the above map
	pkToAddr := map[string]string{}

	pks := []string{} // List of private keys

	// Populate the above data structures using [keys]
	for _, v := range keys {
		cb58 := formatting.CB58{Bytes: v.Bytes()}
		pk := cb58.String()

		address := v.PublicKey().Address().String()

		addrToPK[address] = pk
		pkToAddr[pk] = address

		pks = append(pks, pk)
	}

	// Ensure GetAddress and GetBalance return the correct values for the
	// addresses in the genesis transactions
	for addr, pk := range addrToPK {
		ctx.Lock.Lock()
		if a, err := vm.GetAddress(pk); err != nil {
			t.Fatalf("GetAddress(%q): %s", pk, err)
		} else if a != addr {
			t.Fatalf("GetAddress(%q): Addresses Not Equal(%q,%q)", pk, addr, a)
		} else if balance, err := vm.GetBalance(addr, ""); err != nil {
			t.Fatalf("GetBalance(%q): %s", addr, err)
		} else if balance != defaultInitBalance {
			t.Fatalf("GetBalance(%q,%q): Balance Not Equal(%d,%d)", addr, "", defaultInitBalance, balance)
		}
		ctx.Lock.Unlock()
	}

	// Create a new key
	ctx.Lock.Lock()
	addr1PrivKey, err := vm.CreateKey()
	if err != nil {
		t.Fatalf("CreateKey(): %s", err)
	}

	// The address of the key we just created
	addr1, err := vm.GetAddress(addr1PrivKey)
	if err != nil {
		t.Fatalf("GetAddress(%q): %s", addr1PrivKey, err)
	}

	send1Amt := uint64(10000)
	// Ensure the balance of the new address is 0
	if testbal, err := vm.GetBalance(addr1, ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", addr1, err)
	} else if testbal != 0 {
		t.Fatalf("GetBalance(%q,%q): Balance Not Equal(%d,%d)", addr1, "", 0, testbal)
		// The only valid asset ID is avax
	} else if _, err = vm.GetBalance(addr1, "thisshouldfail"); err == nil {
		t.Fatalf("GetBalance(%q): passed when it should have failed on bad assetID", addr1)
	} else if _, err = vm.Send(100, "thisshouldfail", addr1, pks); err == nil || err != errAsset {
		t.Fatalf("Send(%d,%q,%q,%v): passed when it should have failed on bad assetID", 100, "thisshouldfail", addr1, pks)
		// Ensure we can't send more funds from this address than the address has
	} else if _, err = vm.Send(4000000000000000, "", addr1, pks); err == nil || err != errInsufficientFunds {
		t.Fatalf("Send(%d,%q,%q,%v): passed when it should have failed on insufficient funds", 4000000000000000, "", addr1, pks)
		// Send [send1Amt] NanoAvax from [pks[0]] to [addr1]
	} else if _, err = vm.Send(send1Amt, "", addr1, []string{pks[0]}); err != nil {
		t.Fatalf("Send(%d,%q,%q,%v): failed with error - %s", send1Amt, "", addr1, []string{pks[0]}, err)
	}
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	// There should be one pending transaction (the send we just did).
	// Accept that transaction.
	ctx.Lock.Lock()
	if txs := vm.PendingTxs(); len(txs) != 1 {
		t.Fatalf("PendingTxs(): returned wrong number of transactions - expected: %d ; returned: %d", 1, len(txs))
	} else {
		txs[0].Accept()
	}
	if txs := vm.PendingTxs(); len(txs) != 0 {
		t.Fatalf("PendingTxs(): there should not have been any pending transactions")
	}

	send2Amt := uint64(10000)
	// Ensure that the balance of the address we sent [send1Amt] to is [send1Amt]
	if testbal, err := vm.GetBalance(addr1, ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", addr1, err)
	} else if testbal != send1Amt {
		t.Fatalf("GetBalance(%q): returned wrong balance - expected: %d ; returned: %d", addr1, send1Amt, testbal)
		// Send [send2Amt] from [pks[0]] to [addr1]
	} else if _, err = vm.Send(send1Amt, "", addr1, []string{pks[0]}); err != nil {
		t.Fatalf("Send(%d,%q,%q,%v): failed with error - %s", send2Amt, "", addr1, []string{pks[0]}, err)
	}
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	// There should be one pending transaction (the send we just did).
	// Accept that transaction.
	ctx.Lock.Lock()
	if txs := vm.PendingTxs(); len(txs) != 1 {
		t.Fatalf("PendingTxs: returned wrong number of transactions - expected: %d ; returned: %d", 1, len(txs))
	} else {
		txs[0].Accept()
	}
	if txs := vm.PendingTxs(); len(txs) != 0 {
		t.Fatalf("PendingTxs(): there should not have been any pending transactions")
	}

	// Ensure [addr1] has [send1Amt+send2Amt]
	if testbal, err := vm.GetBalance(addr1, ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", addr1, err)
	} else if testbal != send1Amt+send2Amt {
		t.Fatalf("GetBalance(%q): returned wrong balance - expected: %d; returned: %d", addr1, send1Amt+send2Amt, testbal)
	}

	send3Amt := uint64(10000)
	// Ensure the balance of the address controlled by [pks[0]] is the initial amount
	// it had (from genesis) minus the 2 amounts it sent to [addr1] minus 2 tx fees
	if testbal, err := vm.GetBalance(pkToAddr[pks[0]], ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", pkToAddr[pks[0]], err)
	} else if testbal != defaultInitBalance-send1Amt-send2Amt-2*txFeeTest {
		t.Fatalf("GetBalance(%q): returned wrong balance - expected: %d; returned: %d", pkToAddr[pks[0]], defaultInitBalance-send1Amt-send2Amt-2*txFeeTest, testbal)
		// Send [send3Amt] from [addr1] to the address controlled by [pks[0]]
	} else if _, err = vm.Send(send3Amt, "", pkToAddr[pks[0]], []string{addr1PrivKey}); err != nil {
		t.Fatalf("Send(%d,%q,%q,%v): failed with error - %s", send3Amt-txFeeTest, "", pkToAddr[pks[0]], []string{addr1PrivKey}, err)
	}
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()
	if txs := vm.PendingTxs(); len(txs) != 1 {
		t.Fatalf("PendingTxs(): returned wrong number of transactions - expected: %d; returned: %d", 1, len(txs))
	} else {
		txs[0].Accept()
	}
	if txs := vm.PendingTxs(); len(txs) != 0 {
		t.Fatalf("PendingTxs(): there should not have been any pending transactions")
	}

	send4Amt := uint64(30000)
	// Ensure the balance of the address controlled by [pk[0]] is:
	// [initial balance] - [send1Amt] - [send2Amt] + [send3Amt] - 2 * [txFeeTest]
	if testbal, err := vm.GetBalance(pkToAddr[pks[0]], ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", pkToAddr[pks[0]], err)
	} else if testbal != defaultInitBalance-send1Amt-send2Amt+send3Amt-2*txFeeTest {
		t.Fatalf("GetBalance(%q): returned wrong balance - expected: %d; returned: %d", pkToAddr[pks[0]], defaultInitBalance-send1Amt-send2Amt+send3Amt-2*txFeeTest, testbal)
		// Send [send4Amt] to [addr1] from addresses controlled by [pks[1]] and [pks[2]]
	} else if _, err = vm.Send(send4Amt, "", addr1, []string{pks[1], pks[2]}); err != nil {
		t.Fatalf("Send(%d,%q,%q,%v): failed with error - %s", send4Amt, "", addr1, []string{pks[1], pks[2]}, err)
	}
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()
	if txs := vm.PendingTxs(); len(txs) != 1 {
		t.Fatalf("<-txChan: returned wrong number of transactions - expected: %d ; returned: %d", 1, len(txs))
	} else {
		txs[0].Accept()
	}
	if txs := vm.PendingTxs(); len(txs) != 0 {
		t.Fatalf("PendingTxs(): there should not have been any pending transactions")
	}

	// Ensure the balance of [addr1] is:
	// [send1Amt] + [send2Amt] - [send3Amt] + [send4Amt] - [txFeeTest]
	if testbal, err := vm.GetBalance(addr1, ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", addr1, err)
	} else if testbal != send1Amt+send2Amt-send3Amt+send4Amt-txFeeTest {
		t.Fatalf("GetBalance(%q): returned wrong balance - expected: %d; returned: %d", addr1, send1Amt+send2Amt-send3Amt+send4Amt-txFeeTest, testbal)
	}

	// Ensure the sum of the balances of the addresses controlled by [pks[1]] and [pks[2]] is:
	// [sum of their initial balances] - [send4Amt] - [txFeeTest]
	if testbal1, err := vm.GetBalance(pkToAddr[pks[1]], ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", pkToAddr[pks[1]], err)
	} else if testbal2, err := vm.GetBalance(pkToAddr[pks[2]], ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", pkToAddr[pks[2]], err)
	} else if testbal1+testbal2 != defaultInitBalance*2-send4Amt-txFeeTest {
		t.Fatalf("GetBalance(%q) + GetBalance(%q): returned wrong balance - expected: %d ; returned: %d", pkToAddr[pks[1]], pkToAddr[pks[2]], defaultInitBalance*2-send4Amt-txFeeTest, testbal1+testbal2)
	}
	ctx.Lock.Unlock()
}

func TestMultipleSend(t *testing.T) {
	// Initialize the vm
	genesisTx := GenesisTx(defaultInitBalances)
	vmDB := memdb.New()
	msgChan := make(chan common.Message, 1)
	vm := &VM{}
	defer func() { vm.ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
	vm.Initialize(ctx, vmDB, genesisTx.Bytes(), msgChan, nil)

	// Initialize these data structures
	addrToPK := map[string]string{}
	pkToAddr := map[string]string{}
	pks := []string{}
	for _, v := range keys {
		cb58 := formatting.CB58{Bytes: v.Bytes()}
		pk := cb58.String()

		address := v.PublicKey().Address().String()

		addrToPK[address] = pk
		pkToAddr[pk] = address

		pks = append(pks, pk)
	}

	ctx.Lock.Lock()
	// Ensure GetAddress and GetBalance return the correct values for
	// the addresses mentioned in the genesis tx
	for addr, pk := range addrToPK {
		if a, err := vm.GetAddress(pk); err != nil {
			t.Fatalf("GetAddress(%q): %s", pk, err)
		} else if a != addr {
			t.Fatalf("GetAddress(%q): Addresses Not Equal(%q,%q)", pk, addr, a)
			// Ensure the balances of the addresses are [initAddrBalance]
		} else if balance, err := vm.GetBalance(addr, ""); err != nil {
			t.Fatalf("GetBalance(%q): %s", addr, err)
		} else if balance != defaultInitBalance {
			t.Fatalf("GetBalance(%q,%q): Balance Not Equal(%d,%d)", addr, "", defaultInitBalance, balance)
		}
	}

	// Create a new private key
	testPK, err := vm.CreateKey()
	if err != nil {
		t.Fatalf("CreateKey(): %s", err)
	}
	// Get the address controlled by the new private key
	testaddr, err := vm.GetAddress(testPK)
	if err != nil {
		t.Fatalf("GetAddress(%q): %s", testPK, err)
	}

	if testbal, err := vm.GetBalance(testaddr, ""); err != nil {
		t.Fatalf("GetBalance(%q): %s", testaddr, err)
	} else if testbal != 0 {
		// Balance of new address should be 0
		t.Fatalf("GetBalance(%q,%q): Balance Not Equal(%d,%d)", testaddr, "", 0, testbal)
	}
	if _, err = vm.GetBalance(testaddr, "thisshouldfail"); err == nil {
		t.Fatalf("GetBalance(%q): passed when it should have failed on bad assetID", testaddr)
	}
	if _, err = vm.Send(100, "thisshouldfail", testaddr, pks); err == nil || err != errAsset {
		t.Fatalf("Send(%d,%q,%q,%v): passed when it should have failed on bad assetID", 100, "thisshouldfail", testaddr, pks)
	}
	if _, err = vm.Send(4000000000000000, "", testaddr, pks); err == nil || err != errInsufficientFunds {
		t.Fatalf("Send(%d,%q,%q,%v): passed when it should have failed on insufficient funds", 4000000000000000, "", testaddr, pks)
	}

	// Send [send1Amt] and [send2Amt] from address controlled by [pks[0]] to [testAddr]
	send1Amt := uint64(10000)
	if _, err = vm.Send(send1Amt, "", testaddr, []string{pks[0]}); err != nil {
		t.Fatalf("Send(%d,%q,%q,%v): failed with error - %s", send1Amt, "", testaddr, []string{pks[0]}, err)
	}
	send2Amt := uint64(10000)
	if _, err = vm.Send(send2Amt, "", testaddr, []string{pks[0]}); err != nil {
		t.Fatalf("Send(%d,%q,%q,%v): failed with error - %s", send2Amt, "", testaddr, []string{pks[0]}, err)
	}
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()
	if txs := vm.PendingTxs(); len(txs) != 2 {
		t.Fatalf("PendingTxs(): returned wrong number of transactions - expected: %d ; returned: %d", 2, len(txs))
	} else if inputs1 := txs[0].InputIDs(); inputs1.Len() != 1 {
		t.Fatalf("inputs1: returned wrong number of inputs - expected: %d ; returned: %d", 1, inputs1.Len())
	} else if inputs2 := txs[1].InputIDs(); inputs2.Len() != 1 {
		t.Fatalf("inputs2: returned wrong number of inputs - expected: %d ; returned: %d", 1, inputs2.Len())
	} else if !inputs1.Overlaps(inputs2) {
		t.Fatalf("inputs1 doesn't conflict with inputs2 but it should")
	}

	_, _, _, _, err = vm.GetTxHistory(testaddr)
	if err != nil {
		t.Fatalf("GetTxHistory(%s): %s", testaddr, err)
	}

	_, _, _, _, err = vm.GetTxHistory(pkToAddr[pks[0]])
	if err != nil {
		t.Fatalf("GetTxHistory(%s): %s", pkToAddr[pks[0]], err)
	}
	ctx.Lock.Unlock()
}

func TestIssuePendingDependency(t *testing.T) {
	// Initialize vm with genesis info
	genesisTx := GenesisTx(defaultInitBalances)
	vmDB := memdb.New()
	msgChan := make(chan common.Message, 1)

	ctx.Lock.Lock()
	vm := &VM{}
	defer func() { vm.ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
	vm.Initialize(ctx, vmDB, genesisTx.Bytes(), msgChan, nil)
	vm.batchTimeout = 0

	builder := Builder{
		NetworkID: 0,
		ChainID:   avaxChainID,
	}
	tx1, _ := builder.NewTx(
		/*ins=*/ []Input{
			builder.NewInputPayment(
				/*txID=*/ genesisTx.ID(),
				/*txIndex=*/ 0,
				/*amount=*/ 5*units.Avax,
				/*sigs=*/ []*Sig{builder.NewSig(0 /*=index*/)},
			),
		},
		/*outs=*/ []Output{
			builder.NewOutputPayment(
				/*amount=*/ 3*units.Avax,
				/*locktime=*/ 0,
				/*threshold=*/ 0,
				/*addresses=*/ nil,
			),
		},
		/*signers=*/ []*InputSigner{
			{Keys: []*crypto.PrivateKeySECP256K1R{
				keys[1],
			}},
		},
	)
	tx1Bytes := tx1.Bytes()

	tx2, _ := builder.NewTx(
		/*ins=*/ []Input{
			builder.NewInputPayment(
				/*txID=*/ tx1.ID(),
				/*txIndex=*/ 0,
				/*amount=*/ 3*units.Avax,
				/*sigs=*/ nil,
			),
		},
		/*outs=*/ []Output{
			builder.NewOutputPayment(
				/*amount=*/ 1*units.Avax,
				/*locktime=*/ 0,
				/*threshold=*/ 0,
				/*addresses=*/ nil,
			),
		},
		/*signers=*/ []*InputSigner{
			{},
		},
	)
	tx2Bytes := tx2.Bytes()

	vm.IssueTx(tx1Bytes, nil)
	vm.IssueTx(tx2Bytes, nil)

	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	ctx.Lock.Lock()

	txs := vm.PendingTxs()

	var avlTx1 snowstorm.Tx
	var avlTx2 snowstorm.Tx

	if txs[0].ID().Equals(tx1.ID()) {
		avlTx1 = txs[0]
		avlTx2 = txs[1]
	} else {
		avlTx1 = txs[1]
		avlTx2 = txs[0]
	}

	if err := avlTx1.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := avlTx2.Verify(); err != nil {
		t.Fatal(err)
	}

	ctx.Lock.Unlock()
}
