// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanche-go/api/keystore"
	"github.com/ava-labs/avalanche-go/chains/atomic"
	"github.com/ava-labs/avalanche-go/database/memdb"
	"github.com/ava-labs/avalanche-go/database/mockdb"
	"github.com/ava-labs/avalanche-go/database/prefixdb"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow"
	"github.com/ava-labs/avalanche-go/snow/engine/common"
	"github.com/ava-labs/avalanche-go/utils/crypto"
	"github.com/ava-labs/avalanche-go/utils/formatting"
	"github.com/ava-labs/avalanche-go/utils/logging"
	"github.com/ava-labs/avalanche-go/utils/units"
	"github.com/ava-labs/avalanche-go/vms/components/avax"
	"github.com/ava-labs/avalanche-go/vms/components/verify"
	"github.com/ava-labs/avalanche-go/vms/nftfx"
	"github.com/ava-labs/avalanche-go/vms/propertyfx"
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

var networkID uint32 = 10
var chainID = ids.NewID([32]byte{5, 4, 3, 2, 1})
var platformChainID = ids.Empty.Prefix(0)

var keys []*crypto.PrivateKeySECP256K1R
var asset = ids.NewID([32]byte{1, 2, 3})
var username = "bobby"
var password = "StrnasfqewiurPasswdn56d"

func init() {
	cb58 := formatting.CB58{}
	factory := crypto.FactorySECP256K1R{}

	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
	} {
		_ = cb58.FromString(key)
		pk, _ := factory.ToPrivateKey(cb58.Bytes)
		keys = append(keys, pk.(*crypto.PrivateKeySECP256K1R))
	}
}

type snLookup struct {
	chainsToSubnet map[[32]byte]ids.ID
}

func (sn *snLookup) SubnetID(chainID ids.ID) (ids.ID, error) {
	subnetID, ok := sn.chainsToSubnet[chainID.Key()]
	if !ok {
		return ids.ID{}, errors.New("")
	}
	return subnetID, nil
}

func NewContext(t *testing.T) *snow.Context {
	genesisBytes := BuildGenesisTest(t)
	tx := GetFirstTxFromGenesisTest(genesisBytes, t)

	ctx := snow.DefaultContextTest()
	ctx.NetworkID = networkID
	ctx.ChainID = chainID
	ctx.AVAXAssetID = tx.ID()
	ctx.XChainID = ids.Empty.Prefix(0)
	aliaser := ctx.BCLookup.(*ids.Aliaser)
	aliaser.Alias(chainID, "X")
	aliaser.Alias(chainID, chainID.String())
	aliaser.Alias(platformChainID, "P")
	aliaser.Alias(platformChainID, platformChainID.String())

	sn := &snLookup{
		chainsToSubnet: make(map[[32]byte]ids.ID),
	}
	sn.chainsToSubnet[chainID.Key()] = ctx.SubnetID
	sn.chainsToSubnet[platformChainID.Key()] = ctx.SubnetID
	ctx.SNLookup = sn
	return ctx
}

func GetFirstTxFromGenesisTest(genesisBytes []byte, t *testing.T) *Tx {
	c := setupCodec()
	genesis := Genesis{}
	if err := c.Unmarshal(genesisBytes, &genesis); err != nil {
		t.Fatal(err)
	}

	if len(genesis.Txs) == 0 {
		t.Fatal("genesis tx didn't have any txs")
	}

	genesisTx := genesis.Txs[0]
	if len(genesisTx.Outs) != 0 {
		t.Fatal("genesis tx can't have non-new assets")
	}

	tx := Tx{
		UnsignedTx: &genesisTx.CreateAssetTx,
	}
	if err := tx.SignSECP256K1Fx(c, nil); err != nil {
		t.Fatal(err)
	}

	return &tx
}

func BuildGenesisTest(t *testing.T) []byte {
	ss := StaticService{}

	addr0, _ := formatting.FormatBech32(testHRP, keys[0].PublicKey().Address().Bytes())
	addr1, _ := formatting.FormatBech32(testHRP, keys[1].PublicKey().Address().Bytes())
	addr2, _ := formatting.FormatBech32(testHRP, keys[2].PublicKey().Address().Bytes())

	args := BuildGenesisArgs{GenesisData: map[string]AssetDefinition{
		"asset1": {
			Name:   "myFixedCapAsset",
			Symbol: "MFCA",
			InitialState: map[string][]interface{}{
				"fixedCap": {
					Holder{
						Amount:  100000,
						Address: addr0,
					},
					Holder{
						Amount:  100000,
						Address: addr0,
					},
					Holder{
						Amount:  50000,
						Address: addr0,
					},
					Holder{
						Amount:  50000,
						Address: addr0,
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
							addr0,
							addr1,
						},
					},
					Owners{
						Threshold: 2,
						Minters: []string{
							addr0,
							addr1,
							addr2,
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
							addr0,
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

func GenesisVM(t *testing.T) ([]byte, chan common.Message, *VM, *atomic.Memory) {
	genesisBytes := BuildGenesisTest(t)
	ctx := NewContext(t)

	baseDB := memdb.New()

	m := &atomic.Memory{}
	m.Initialize(logging.NoLog{}, prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	userKeystore := keystore.CreateTestKeystore()
	if err := userKeystore.AddUser(username, password); err != nil {
		t.Fatal(err)
	}
	ctx.Keystore = userKeystore.NewBlockchainKeyStore(ctx.ChainID)

	issuer := make(chan common.Message, 1)
	vm := &VM{}
	err := vm.Initialize(
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		issuer,
		[]*common.Fx{
			{
				ID: ids.Empty,
				Fx: &secp256k1fx.Fx{},
			},
			{
				ID: nftfx.ID,
				Fx: &nftfx.Fx{},
			},
		},
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

	return genesisBytes, issuer, vm, m
}

func NewTx(t *testing.T, genesisBytes []byte, vm *VM) *Tx {
	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	newTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}}
	if err := newTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}
	return newTx
}

func TestTxSerialization(t *testing.T) {
	expected := []byte{
		// Codec version:
		0x00, 0x00,
		// txID:
		0x00, 0x00, 0x00, 0x01,
		// networkID:
		0x00, 0x00, 0x00, 0x0a,
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
		// Memo length:
		0x00, 0x00, 0x00, 0x04,
		// Memo:
		0x00, 0x01, 0x02, 0x03,
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
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
			Memo:         []byte{0x00, 0x01, 0x02, 0x03},
		}},
		Name:         "name",
		Symbol:       "symb",
		Denomination: 0,
		States: []*InitialState{
			{
				FxID: 0,
				Outs: []verify.State{
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

		unsignedTx.Outs = append(unsignedTx.Outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: asset},
			Out: &secp256k1fx.TransferOutput{
				Amt: 20 * units.KiloAvax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			},
		})
	}

	c := setupCodec()
	if err := tx.SignSECP256K1Fx(c, nil); err != nil {
		t.Fatal(err)
	}

	result := tx.Bytes()
	if !bytes.Equal(expected, result) {
		t.Fatalf("\nExpected: 0x%x\nResult:   0x%x", expected, result)
	}
}

func TestInvalidGenesis(t *testing.T) {
	vm := &VM{}
	ctx := NewContext(t)
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
	ctx := NewContext(t)
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
	ctx := NewContext(t)
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
			Fx: &FxTest{
				InitializeF: func(interface{}) error {
					return errUnknownFx
				},
			},
		}},
	)
	if err == nil {
		t.Fatalf("Should have errored due to an invalid fx initialization")
	}
}

func TestIssueTx(t *testing.T) {
	genesisBytes, issuer, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	newTx := NewTx(t, genesisBytes, vm)

	txID, err := vm.IssueTx(newTx.Bytes())
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
	_, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	addr := keys[0].PublicKey().Address()

	addrs := ids.ShortSet{}
	addrs.Add(addr)
	utxos, _, _, err := vm.GetUTXOs(addrs, ids.ShortEmpty, ids.Empty, -1)
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
	genesisBytes, issuer, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)

	key := keys[0]

	firstTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := firstTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	if _, err := vm.IssueTx(firstTx.Bytes()); err != nil {
		t.Fatal(err)
	}

	secondTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        firstTx.ID(),
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}}
	if err := secondTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	if _, err := vm.IssueTx(secondTx.Bytes()); err != nil {
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
	ctx := NewContext(t)
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
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "Team Rocket",
		Symbol:       "TR",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 1,
			Outs: []verify.State{
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
	if err := createAssetTx.SignSECP256K1Fx(vm.codec, nil); err != nil {
		t.Fatal(err)
	}

	if _, err = vm.IssueTx(createAssetTx.Bytes()); err != nil {
		t.Fatal(err)
	}

	mintNFTTx := &Tx{UnsignedTx: &OperationTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Ops: []*Operation{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*avax.UTXOID{{
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
	if err := mintNFTTx.SignNFTFx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{keys[0]}}); err != nil {
		t.Fatal(err)
	}

	if _, err = vm.IssueTx(mintNFTTx.Bytes()); err != nil {
		t.Fatal(err)
	}

	transferNFTTx := &Tx{
		UnsignedTx: &OperationTx{
			BaseTx: BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
			}},
			Ops: []*Operation{{
				Asset: avax.Asset{ID: createAssetTx.ID()},
				UTXOIDs: []*avax.UTXOID{{
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
		},
		Creds: []verify.Verifiable{
			&nftfx.Credential{},
		},
	}
	if err := transferNFTTx.SignNFTFx(vm.codec, nil); err != nil {
		t.Fatal(err)
	}

	if _, err = vm.IssueTx(transferNFTTx.Bytes()); err != nil {
		t.Fatal(err)
	}
}

// Test issuing a transaction that creates an Property family
func TestIssueProperty(t *testing.T) {
	vm := &VM{}
	ctx := NewContext(t)
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
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Name:         "Team Rocket",
		Symbol:       "TR",
		Denomination: 0,
		States: []*InitialState{{
			FxID: 2,
			Outs: []verify.State{
				&propertyfx.MintOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			},
		}},
	}}
	if err := createAssetTx.SignSECP256K1Fx(vm.codec, nil); err != nil {
		t.Fatal(err)
	}

	if _, err = vm.IssueTx(createAssetTx.Bytes()); err != nil {
		t.Fatal(err)
	}

	mintPropertyTx := &Tx{UnsignedTx: &OperationTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Ops: []*Operation{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*avax.UTXOID{{
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

	signedBytes, err := vm.codec.Marshal(mintPropertyTx)
	if err != nil {
		t.Fatal(err)
	}
	mintPropertyTx.Initialize(unsignedBytes, signedBytes)

	if _, err = vm.IssueTx(mintPropertyTx.Bytes()); err != nil {
		t.Fatal(err)
	}

	burnPropertyTx := &Tx{UnsignedTx: &OperationTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		}},
		Ops: []*Operation{{
			Asset: avax.Asset{ID: createAssetTx.ID()},
			UTXOIDs: []*avax.UTXOID{{
				TxID:        mintPropertyTx.ID(),
				OutputIndex: 1,
			}},
			Op: &propertyfx.BurnOperation{Input: secp256k1fx.Input{}},
		}},
	}}

	burnPropertyTx.Creds = append(burnPropertyTx.Creds, &propertyfx.Credential{})

	unsignedBytes, err = vm.codec.Marshal(burnPropertyTx.UnsignedTx)
	if err != nil {
		t.Fatal(err)
	}
	signedBytes, err = vm.codec.Marshal(burnPropertyTx)
	if err != nil {
		t.Fatal(err)
	}
	burnPropertyTx.Initialize(unsignedBytes, signedBytes)

	if _, err = vm.IssueTx(burnPropertyTx.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestVMFormat(t *testing.T) {
	_, _, vm, _ := GenesisVM(t)
	defer func() {
		vm.Shutdown()
		vm.ctx.Lock.Unlock()
	}()

	tests := []struct {
		in       ids.ShortID
		expected string
	}{
		{ids.ShortEmpty, "X-testing1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqtu2yas"},
	}
	for _, test := range tests {
		t.Run(test.in.String(), func(t *testing.T) {
			addrStr, err := vm.FormatLocalAddress(test.in)
			if err != nil {
				t.Error(err)
			}
			if test.expected != addrStr {
				t.Errorf("Expected %q, got %q", test.expected, addrStr)
			}
		})
	}
}

func TestTxCached(t *testing.T) {
	genesisBytes, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	newTx := NewTx(t, genesisBytes, vm)
	txBytes := newTx.Bytes()

	_, err := vm.ParseTx(txBytes)
	assert.NoError(t, err)

	db := mockdb.New()
	called := new(bool)
	db.OnGet = func([]byte) ([]byte, error) {
		*called = true
		return nil, errors.New("")
	}
	vm.state.state.DB = db
	vm.state.state.Cache.Flush()

	_, err = vm.ParseTx(txBytes)
	assert.NoError(t, err)
	assert.False(t, *called, "shouldn't have called the DB")
}

func TestTxNotCached(t *testing.T) {
	genesisBytes, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	newTx := NewTx(t, genesisBytes, vm)
	txBytes := newTx.Bytes()

	_, err := vm.ParseTx(txBytes)
	assert.NoError(t, err)

	db := mockdb.New()
	called := new(bool)
	db.OnGet = func([]byte) ([]byte, error) {
		*called = true
		return nil, errors.New("")
	}
	db.OnPut = func([]byte, []byte) error { return nil }
	vm.state.state.DB = db
	vm.state.uniqueTx.Flush()
	vm.state.state.Cache.Flush()

	_, err = vm.ParseTx(txBytes)
	assert.NoError(t, err)
	assert.True(t, *called, "should have called the DB")
}

func TestTxVerifyAfterIssueTx(t *testing.T) {
	genesisBytes, issuer, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	key := keys[0]
	firstTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := firstTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	secondTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := secondTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	parsedSecondTx, err := vm.ParseTx(secondTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsedSecondTx.Verify(); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.IssueTx(firstTx.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := parsedSecondTx.Accept(); err != nil {
		t.Fatal(err)
	}
	ctx.Lock.Unlock()

	msg := <-issuer
	if msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}
	ctx.Lock.Lock()

	txs := vm.PendingTxs()
	if len(txs) != 1 {
		t.Fatalf("Should have returned %d tx(s)", 1)
	}
	parsedFirstTx := txs[0]

	if err := parsedFirstTx.Verify(); err == nil {
		t.Fatalf("Should have errored due to a missing UTXO")
	}
}

func TestTxVerifyAfterGetTx(t *testing.T) {
	genesisBytes, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	key := keys[0]
	firstTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := firstTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	secondTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := secondTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	parsedSecondTx, err := vm.ParseTx(secondTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsedSecondTx.Verify(); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.IssueTx(firstTx.Bytes()); err != nil {
		t.Fatal(err)
	}
	parsedFirstTx, err := vm.GetTx(firstTx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsedSecondTx.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := parsedFirstTx.Verify(); err == nil {
		t.Fatalf("Should have errored due to a missing UTXO")
	}
}

func TestTxVerifyAfterVerifyAncestorTx(t *testing.T) {
	genesisBytes, _, vm, _ := GenesisVM(t)
	ctx := vm.ctx
	defer func() {
		vm.Shutdown()
		ctx.Lock.Unlock()
	}()

	genesisTx := GetFirstTxFromGenesisTest(genesisBytes, t)
	key := keys[0]
	firstTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := firstTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	firstTxDescendant := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        firstTx.ID(),
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 50000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := firstTxDescendant.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	secondTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    networkID,
		BlockchainID: chainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        genesisTx.ID(),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: genesisTx.ID()},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: genesisTx.ID()},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}},
	}}}
	if err := secondTx.SignSECP256K1Fx(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{key}}); err != nil {
		t.Fatal(err)
	}

	parsedSecondTx, err := vm.ParseTx(secondTx.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsedSecondTx.Verify(); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.IssueTx(firstTx.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.IssueTx(firstTxDescendant.Bytes()); err != nil {
		t.Fatal(err)
	}
	parsedFirstTx, err := vm.GetTx(firstTx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsedSecondTx.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := parsedFirstTx.Verify(); err == nil {
		t.Fatalf("Should have errored due to a missing UTXO")
	}
}
