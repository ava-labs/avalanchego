// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"testing"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/nftfx"
	"github.com/ava-labs/gecko/vms/propertyfx"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var networkID uint32 = 43110
var chainID = ids.NewID([32]byte{5, 4, 3, 2, 1})

var keys []*crypto.PrivateKeySECP256K1R
var ctx *snow.Context
var asset = ids.NewID([32]byte{1, 2, 3})

func init() {
	ctx = snow.DefaultContextTest()
	ctx.NetworkID = networkID
	ctx.ChainID = chainID
	cb58 := formatting.CB58{}
	factory := crypto.FactorySECP256K1R{}

	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	} {
		ctx.Log.AssertNoError(cb58.FromString(key))
		pk, err := factory.ToPrivateKey(cb58.Bytes)
		ctx.Log.AssertNoError(err)
		keys = append(keys, pk.(*crypto.PrivateKeySECP256K1R))
	}
}

func GetFirstTxFromGenesisTest(genesisBytes []byte, t *testing.T) *Tx {
	c := setupCodec()
	genesis := Genesis{}
	if err := c.Unmarshal(genesisBytes, &genesis); err != nil {
		t.Fatal(err)
	}

	for _, genesisTx := range genesis.Txs {
		if len(genesisTx.Outs) != 0 {
			t.Fatal("genesis tx can't have non-new assets")
		}

		tx := Tx{
			UnsignedTx: &genesisTx.CreateAssetTx,
		}
		txBytes, err := c.Marshal(&tx)
		if err != nil {
			t.Fatal(err)
		}
		tx.Initialize(txBytes)

		return &tx
	}

	t.Fatal("genesis tx didn't have any txs")
	return nil
}

func BuildGenesisTest(t *testing.T) []byte {
	ss := StaticService{}

	addr0 := keys[0].PublicKey().Address()
	addr1 := keys[1].PublicKey().Address()
	addr2 := keys[2].PublicKey().Address()

	args := BuildGenesisArgs{GenesisData: map[string]AssetDefinition{
		"asset1": {
			Name:   "myFixedCapAsset",
			Symbol: "MFCA",
			InitialState: map[string][]interface{}{
				"fixedCap": {
					Holder{
						Amount:  100000,
						Address: addr0.String(),
					},
					Holder{
						Amount:  100000,
						Address: addr0.String(),
					},
					Holder{
						Amount:  50000,
						Address: addr0.String(),
					},
					Holder{
						Amount:  50000,
						Address: addr0.String(),
					},
				},
			},
		},
		"asset2": {
			Name:   "myVarCapAsset",
			Symbol: "MVCA",
			InitialState: map[string][]interface{}{
				"variableCap": {
					Owners{
						Threshold: 1,
						Minters: []string{
							addr0.String(),
							addr1.String(),
						},
					},
					Owners{
						Threshold: 2,
						Minters: []string{
							addr0.String(),
							addr1.String(),
							addr2.String(),
						},
					},
				},
			},
		},
		"asset3": {
			Name: "myOtherVarCapAsset",
			InitialState: map[string][]interface{}{
				"variableCap": {
					Owners{
						Threshold: 1,
						Minters: []string{
							addr0.String(),
						},
					},
				},
			},
		},
	}}
	reply := BuildGenesisReply{}
	err := ss.BuildGenesis(nil, &args, &reply)
	if err != nil {
		t.Fatal(err)
	}

	return reply.Bytes.Bytes
}

func GenesisVM(t *testing.T) ([]byte, chan common.Message, *VM) {
	genesisBytes := BuildGenesisTest(t)

	baseDB := memdb.New()

	sm := &atomic.SharedMemory{}
	sm.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDB))

	ctx.NetworkID = networkID
	ctx.ChainID = chainID
	ctx.SharedMemory = sm.NewBlockchainSharedMemory(chainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	issuer := make(chan common.Message, 1)
	vm := &VM{
		ava:      ids.Empty,
		platform: ids.Empty,
	}
	err := vm.Initialize(
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		issuer,
		[]*common.Fx{{
			ID: ids.Empty,
			Fx: &secp256k1fx.Fx{},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	if err := vm.Bootstrapping(); err != nil {
		t.Fatal(err)
	}

	if err := vm.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	return genesisBytes, issuer, vm
}

func NewTx(t *testing.T, genesisBytes []byte, vm *VM) *Tx {
	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	newTx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&newTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	newTx.Creds = append(newTx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(newTx)
	if err != nil {
		t.Fatal(err)
	}
	newTx.Initialize(b)
	return newTx
}

func TestTxSerialization(t *testing.T) {
	expected := []byte{
		// txID:
		0x00, 0x00, 0x00, 0x01,
		// networkID:
		0x00, 0x00, 0xa8, 0x66,
		// chainID:
		0x05, 0x04, 0x03, 0x02, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// number of outs:
		0x00, 0x00, 0x00, 0x03,
		// output[0]:
		// assetID:
		0x01, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// fxID:
		0x00, 0x00, 0x00, 0x07,
		// secp256k1 Transferable Output:
		// amount:
		0x00, 0x00, 0x12, 0x30, 0x9c, 0xe5, 0x40, 0x00,
		// locktime:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold:
		0x00, 0x00, 0x00, 0x01,
		// number of addresses
		0x00, 0x00, 0x00, 0x01,
		// address[0]
		0xfc, 0xed, 0xa8, 0xf9, 0x0f, 0xcb, 0x5d, 0x30,
		0x61, 0x4b, 0x99, 0xd7, 0x9f, 0xc4, 0xba, 0xa2,
		0x93, 0x07, 0x76, 0x26,
		// output[1]:
		// assetID:
		0x01, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// fxID:
		0x00, 0x00, 0x00, 0x07,
		// secp256k1 Transferable Output:
		// amount:
		0x00, 0x00, 0x12, 0x30, 0x9c, 0xe5, 0x40, 0x00,
		// locktime:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold:
		0x00, 0x00, 0x00, 0x01,
		// number of addresses:
		0x00, 0x00, 0x00, 0x01,
		// address[0]:
		0x6e, 0xad, 0x69, 0x3c, 0x17, 0xab, 0xb1, 0xbe,
		0x42, 0x2b, 0xb5, 0x0b, 0x30, 0xb9, 0x71, 0x1f,
		0xf9, 0x8d, 0x66, 0x7e,
		// output[2]:
		// assetID:
		0x01, 0x02, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// fxID:
		0x00, 0x00, 0x00, 0x07,
		// secp256k1 Transferable Output:
		// amount:
		0x00, 0x00, 0x12, 0x30, 0x9c, 0xe5, 0x40, 0x00,
		// locktime:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold:
		0x00, 0x00, 0x00, 0x01,
		// number of addresses:
		0x00, 0x00, 0x00, 0x01,
		// address[0]:
		0xf2, 0x42, 0x08, 0x46, 0x87, 0x6e, 0x69, 0xf4,
		0x73, 0xdd, 0xa2, 0x56, 0x17, 0x29, 0x67, 0xe9,
		0x92, 0xf0, 0xee, 0x31,
		// number of inputs:
		0x00, 0x00, 0x00, 0x00,
		// name length:
		0x00, 0x04,
		// name:
		'n', 'a', 'm', 'e',
		// symbol length:
		0x00, 0x04,
		// symbol:
		's', 'y', 'm', 'b',
		// denomination
		0x00,
		// number of initial states:
		0x00, 0x00, 0x00, 0x01,
		// fx index:
		0x00, 0x00, 0x00, 0x00,
		// number of outputs:
		0x00, 0x00, 0x00, 0x01,
		// fxID:
		0x00, 0x00, 0x00, 0x06,
		// secp256k1 Mint Output:
		// locktime:
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// threshold:
		0x00, 0x00, 0x00, 0x01,
		// number of addresses:
		0x00, 0x00, 0x00, 0x01,
		// address[0]:
		0xfc, 0xed, 0xa8, 0xf9, 0x0f, 0xcb, 0x5d, 0x30,
		0x61, 0x4b, 0x99, 0xd7, 0x9f, 0xc4, 0xba, 0xa2,
		0x93, 0x07, 0x76, 0x26,
		// number of credentials:
		0x00, 0x00, 0x00, 0x00,
	}

	unsignedTx := &CreateAssetTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
		},
		Name:         "name",
		Symbol:       "symb",
		Denomination: 0,
		States: []*InitialState{
			{
				FxID: 0,
				Outs: []verify.Verifiable{
					&secp256k1fx.MintOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
				},
			},
		},
	}
	tx := &Tx{UnsignedTx: unsignedTx}
	for _, key := range keys {
		addr := key.PublicKey().Address()

		unsignedTx.Outs = append(unsignedTx.Outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 20 * units.KiloAva,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		})
	}

	c := setupCodec()
	b, err := c.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Initialize(b)

	result := tx.Bytes()
	if !bytes.Equal(expected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
	}
}

func TestInvalidGenesis(t *testing.T) {
	vm := &VM{}
	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	err := vm.Initialize(
		/*context=*/ ctx,
		/*db=*/ memdb.New(),
		/*genesisState=*/ nil,
		/*engineMessenger=*/ make(chan common.Message, 1),
		/*fxs=*/ nil,
	)
	if err == nil {
		t.Fatalf("Should have errored due to an invalid genesis")
	}
}

func TestInvalidFx(t *testing.T) {
	vm := &VM{}
	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	err := vm.Initialize(
		/*context=*/ ctx,
		/*db=*/ memdb.New(),
		/*genesisState=*/ genesisBytes,
		/*engineMessenger=*/ make(chan common.Message, 1),
		/*fxs=*/ []*common.Fx{
			nil,
		},
	)
	if err == nil {
		t.Fatalf("Should have errored due to an invalid interface")
	}
}

func TestFxInitializationFailure(t *testing.T) {
	vm := &VM{}
	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	err := vm.Initialize(
		/*context=*/ ctx,
		/*db=*/ memdb.New(),
		/*genesisState=*/ genesisBytes,
		/*engineMessenger=*/ make(chan common.Message, 1),
		/*fxs=*/ []*common.Fx{{
			ID: ids.Empty,
			Fx: &testFx{initialize: errUnknownFx},
		}},
	)
	if err == nil {
		t.Fatalf("Should have errored due to an invalid fx initialization")
	}
}

type testTxBytes struct{ unsignedBytes []byte }

func (tx *testTxBytes) UnsignedBytes() []byte { return tx.unsignedBytes }

func TestIssueTx(t *testing.T) {
	genesisBytes, issuer, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	newTx := NewTx(t, genesisBytes, vm)

	txID, err := vm.IssueTx(newTx.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !txID.Equals(newTx.ID()) {
		t.Fatalf("Issue Tx returned wrong TxID")
	}
	ctx.Lock.Unlock()

	msg := <-issuer
	if msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}
	ctx.Lock.Lock()

	if txs := vm.PendingTxs(); len(txs) != 1 {
		t.Fatalf("Should have returned %d tx(s)", 1)
	}
}

func TestGenesisGetUTXOs(t *testing.T) {
	_, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	shortAddr := keys[0].PublicKey().Address()
	addr := ids.NewID(hashing.ComputeHash256Array(shortAddr.Bytes()))

	addrs := ids.Set{}
	addrs.Add(addr)
	utxos, err := vm.GetUTXOs(addrs)
	if err != nil {
		t.Fatal(err)
	}

	if len(utxos) != 7 {
		t.Fatalf("Wrong number of utxos. Expected (%d) returned (%d)", 7, len(utxos))
	}
}

// Test issuing a transaction that consumes a currently pending UTXO. The
// transaction should be issued successfully.
func TestIssueDependentTx(t *testing.T) {
	genesisBytes, issuer, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	key := keys[0]

	firstTx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*ava.TransferableOutput{{
			Asset: ava.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&firstTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	firstTx.Creds = append(firstTx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err := vm.codec.Marshal(firstTx)
	if err != nil {
		t.Fatal(err)
	}
	firstTx.Initialize(b)

	_, err = vm.IssueTx(firstTx.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}

	secondTx := &Tx{UnsignedTx: &BaseTx{
		NetID: networkID,
		BCID:  chainID,
		Ins: []*ava.TransferableInput{{
			UTXOID: ava.UTXOID{
				TxID:        firstTx.ID(),
				OutputIndex: 0,
			},
			Asset: ava.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}

	unsignedBytes, err = vm.codec.Marshal(&secondTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	sig, err = key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig = [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	secondTx.Creds = append(secondTx.Creds, &secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		},
	})

	b, err = vm.codec.Marshal(secondTx)
	if err != nil {
		t.Fatal(err)
	}
	secondTx.Initialize(b)

	_, err = vm.IssueTx(secondTx.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx.Lock.Unlock()

	msg := <-issuer
	if msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}
	ctx.Lock.Lock()

	if txs := vm.PendingTxs(); len(txs) != 2 {
		t.Fatalf("Should have returned %d tx(s)", 2)
	}
}

// Test issuing a transaction that creates an NFT family
func TestIssueNFT(t *testing.T) {
	vm := &VM{}
	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	err := vm.Initialize(
		ctx,
		memdb.New(),
		genesisBytes,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty.Prefix(0),
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(1),
				Fx: &nftfx.Fx{},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	err = vm.Bootstrapping()
	if err != nil {
		t.Fatal(err)
	}

	err = vm.Bootstrapped()
	if err != nil {
		t.Fatal(err)
	}

	createAssetTx := &Tx{UnsignedTx: &CreateAssetTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
		},
		Name:         "Team Rocket",
		Symbol:       "TR",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 1,
			Outs: []verify.Verifiable{
				&nftfx.MintOutput{
					GroupID: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
				&nftfx.MintOutput{
					GroupID: 2,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		}},
	}}

	b, err := vm.codec.Marshal(createAssetTx)
	if err != nil {
		t.Fatal(err)
	}
	createAssetTx.Initialize(b)

	if _, err = vm.IssueTx(createAssetTx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}

	mintNFTTx := &Tx{UnsignedTx: &OperationTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
		},
		Ops: []*Operation{{
			Asset: ava.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*ava.UTXOID{{
				TxID:        createAssetTx.ID(),
				OutputIndex: 0,
			}},
			Op: &nftfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
				GroupID: 1,
				Payload: []byte{'h', 'e', 'l', 'l', 'o'},
				Outputs: []*secp256k1fx.OutputOwners{{}},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&mintNFTTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	mintNFTTx.Creds = append(mintNFTTx.Creds, &nftfx.Credential{Credential: secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		}},
	})

	b, err = vm.codec.Marshal(mintNFTTx)
	if err != nil {
		t.Fatal(err)
	}
	mintNFTTx.Initialize(b)

	if _, err = vm.IssueTx(mintNFTTx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}

	transferNFTTx := &Tx{UnsignedTx: &OperationTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
		},
		Ops: []*Operation{{
			Asset: ava.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*ava.UTXOID{{
				TxID:        mintNFTTx.ID(),
				OutputIndex: 0,
			}},
			Op: &nftfx.TransferOperation{
				Input: secp256k1fx.Input{},
				Output: nftfx.TransferOutput{
					GroupID:      1,
					Payload:      []byte{'h', 'e', 'l', 'l', 'o'},
					OutputOwners: secp256k1fx.OutputOwners{},
				},
			},
		}},
	}}

	transferNFTTx.Creds = append(transferNFTTx.Creds, &nftfx.Credential{})

	b, err = vm.codec.Marshal(transferNFTTx)
	if err != nil {
		t.Fatal(err)
	}
	transferNFTTx.Initialize(b)

	if _, err = vm.IssueTx(transferNFTTx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}
}

// Test issuing a transaction that creates an Property family
func TestIssueProperty(t *testing.T) {
	vm := &VM{}
	ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisBytes := BuildGenesisTest(t)
	issuer := make(chan common.Message, 1)
	err := vm.Initialize(
		ctx,
		memdb.New(),
		genesisBytes,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty.Prefix(0),
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(1),
				Fx: &nftfx.Fx{},
			},
			{
				ID: ids.Empty.Prefix(2),
				Fx: &propertyfx.Fx{},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	vm.batchTimeout = 0

	err = vm.Bootstrapping()
	if err != nil {
		t.Fatal(err)
	}

	err = vm.Bootstrapped()
	if err != nil {
		t.Fatal(err)
	}

	createAssetTx := &Tx{UnsignedTx: &CreateAssetTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
		},
		Name:         "Team Rocket",
		Symbol:       "TR",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 2,
			Outs: []verify.Verifiable{
				&propertyfx.MintOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		}},
	}}

	b, err := vm.codec.Marshal(createAssetTx)
	if err != nil {
		t.Fatal(err)
	}
	createAssetTx.Initialize(b)

	if _, err = vm.IssueTx(createAssetTx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}

	mintPropertyTx := &Tx{UnsignedTx: &OperationTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
		},
		Ops: []*Operation{{
			Asset: ava.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*ava.UTXOID{{
				TxID:        createAssetTx.ID(),
				OutputIndex: 0,
			}},
			Op: &propertyfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
				MintOutput: propertyfx.MintOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
				OwnedOutput: propertyfx.OwnedOutput{},
			},
		}},
	}}

	unsignedBytes, err := vm.codec.Marshal(&mintPropertyTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}

	key := keys[0]
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixedSig := [crypto.SECP256K1RSigLen]byte{}
	copy(fixedSig[:], sig)

	mintPropertyTx.Creds = append(mintPropertyTx.Creds, &propertyfx.Credential{Credential: secp256k1fx.Credential{
		Sigs: [][crypto.SECP256K1RSigLen]byte{
			fixedSig,
		}},
	})

	b, err = vm.codec.Marshal(mintPropertyTx)
	if err != nil {
		t.Fatal(err)
	}
	mintPropertyTx.Initialize(b)

	if _, err = vm.IssueTx(mintPropertyTx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}

	burnPropertyTx := &Tx{UnsignedTx: &OperationTx{
		BaseTx: BaseTx{
			NetID: networkID,
			BCID:  chainID,
		},
		Ops: []*Operation{{
			Asset: ava.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*ava.UTXOID{{
				TxID:        mintPropertyTx.ID(),
				OutputIndex: 1,
			}},
			Op: &propertyfx.BurnOperation{Input: secp256k1fx.Input{}},
		}},
	}}

	burnPropertyTx.Creds = append(burnPropertyTx.Creds, &propertyfx.Credential{})

	b, err = vm.codec.Marshal(burnPropertyTx)
	if err != nil {
		t.Fatal(err)
	}
	burnPropertyTx.Initialize(b)

	if _, err = vm.IssueTx(burnPropertyTx.Bytes(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestVMFormat(t *testing.T) {
	_, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	tests := []struct {
		in       string
		expected string
	}{
		{"", "3D7sudhzUKTYFkYj4Zoe7GgSKhuyP9bYwXunHwhZsmQe1z9Mp-45PJLL"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if res := vm.Format([]byte(tt.in)); tt.expected != res {
				t.Errorf("Expected %q, got %q", tt.expected, res)
			}
		})
	}
}

func TestVMFormatAliased(t *testing.T) {
	_, _, vm := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	origAliases := ctx.BCLookup
	defer func() { ctx.BCLookup = origAliases }()

	tmpAliases := &ids.Aliaser{}
	tmpAliases.Initialize()
	tmpAliases.Alias(ctx.ChainID, "X")
	ctx.BCLookup = tmpAliases

	tests := []struct {
		in       string
		expected string
	}{
		{"", "X-45PJLL"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if res := vm.Format([]byte(tt.in)); tt.expected != res {
				t.Errorf("Expected %q, got %q", tt.expected, res)
			}
		})
	}
}
