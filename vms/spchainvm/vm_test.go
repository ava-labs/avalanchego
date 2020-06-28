// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/snow/engine/snowman/bootstrap"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/units"

	smcon "github.com/ava-labs/gecko/snow/consensus/snowman"
	smeng "github.com/ava-labs/gecko/snow/engine/snowman"
)

var keys []*crypto.PrivateKeySECP256K1R

var ctx = snow.DefaultContextTest()

func init() {
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

func GenesisAccounts() []Account {
	accounts := []Account(nil)
	for _, key := range keys {
		accounts = append(accounts,
			Account{
				id:      key.PublicKey().Address(),
				balance: 20 * units.KiloAva,
			})
	}
	return accounts
}

func TestPayments(t *testing.T) {
	genesisAccounts := GenesisAccounts()

	codec := Codec{}
	genesisData, _ := codec.MarshalGenesis(genesisAccounts)
	db := memdb.New()
	bootstrappingDB := memdb.New()

	msgChan := make(chan common.Message, 1)
	blocker, _ := queue.New(bootstrappingDB)

	vm := &VM{}
	defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
	vm.Initialize(ctx, db, genesisData, msgChan, nil)

	sender := &common.SenderTest{}
	sender.T = t
	sender.Default(true)

	vdrs := validators.NewSet()
	vdr := validators.GenerateRandomValidator(1)
	vdrs.Add(vdr)

	ctx.Lock.Lock()
	consensus := smeng.Transitive{}
	consensus.Initialize(smeng.Config{
		Config: bootstrap.Config{
			Config: common.Config{
				Context:    ctx,
				Validators: vdrs,
				Beacons:    validators.NewSet(),
				Sender:     sender,
			},
			Blocked: blocker,
			VM:      vm,
		},
		Params: snowball.Parameters{
			Metrics:      prometheus.NewRegistry(),
			K:            1,
			Alpha:        1,
			BetaVirtuous: 1,
			BetaRogue:    2,
		},
		Consensus: &smcon.Topological{},
	})
	consensus.Startup()

	account := vm.GetAccount(vm.baseDB, keys[0].PublicKey().Address())

	tx, _, err := account.CreateTx(200, keys[1].PublicKey().Address(), ctx, keys[0])
	if err != nil {
		t.Fatal(err)
	}

	vm.issueTx(tx)
	ctx.Lock.Unlock()

	if msg := <-msgChan; msg != common.PendingTxs {
		t.Fatalf("Wrong message")
	}

	queriedVtxID := new(ids.ID)
	queried := new(int)
	queryRequestID := new(uint32)
	sender.PushQueryF = func(_ ids.ShortSet, requestID uint32, vtxID ids.ID, _ []byte) {
		*queriedVtxID = vtxID
		*queried++
		*queryRequestID = requestID
	}

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	consensus.Notify(common.PendingTxs)

	sender.PushQueryF = nil
	if *queried != 1 {
		t.Fatalf("Should have launched one query for the vertex")
	}

	queriedVtxIDSet := ids.Set{}
	queriedVtxIDSet.Add(*queriedVtxID)
	consensus.Chits(vdr.ID(), *queryRequestID, queriedVtxIDSet)

	if account := vm.GetAccount(vm.baseDB, keys[0].PublicKey().Address()); account.Balance() != 20*units.KiloAva-200 {
		t.Fatalf("Wrong Balance")
	} else if account := vm.GetAccount(vm.baseDB, keys[1].PublicKey().Address()); account.Balance() != 20*units.KiloAva+200 {
		t.Fatalf("Wrong Balance")
	}
}
