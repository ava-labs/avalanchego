package spchainvm

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
	"github.com/ava-labs/gecko/snow/networking/handler"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/networking/sender"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/logging"

	smcon "github.com/ava-labs/gecko/snow/consensus/snowman"
	smeng "github.com/ava-labs/gecko/snow/engine/snowman"
)

// ConsensusLeader runs the leader consensus benchmark for blocks
func ConsensusLeader(numBlocks, numTxsPerBlock int, b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	ctx := snow.DefaultContextTest()
	genesisData, blocks := genGenesisState(
		/*numBlocks=*/ numBlocks,
		/*numTxsPerBlock=*/ numTxsPerBlock,
		/*testing=*/ b,
	)

	maxBatchSize = numTxsPerBlock
	for n := 0; n < b.N; n++ {
		db := memdb.New()
		vmDB := prefixdb.New([]byte("vm"), db)
		bootstrappingDB := prefixdb.New([]byte("bootstrapping"), db)

		blocked, err := queue.New(bootstrappingDB)
		if err != nil {
			b.Fatal(err)
		}

		// The channel through which a VM may send messages to the consensus engine
		// VM uses this channel to notify engine that a block is ready to be made
		msgChan := make(chan common.Message, 1000)

		vdrs := validators.NewSet()
		vdrs.Add(validators.NewValidator(ctx.NodeID, 1))
		beacons := validators.NewSet()

		timeoutManager := timeout.Manager{}
		timeoutManager.Initialize(2 * time.Second)
		go timeoutManager.Dispatch()

		router := &router.ChainRouter{}
		router.Initialize(logging.NoLog{}, &timeoutManager, time.Hour)

		// Initialize the VM
		vm := &VM{}
		defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
		ctx.Lock.Lock()
		if err := vm.Initialize(ctx, vmDB, genesisData, msgChan, nil); err != nil {
			b.Fatal(err)
		}

		externalSender := &sender.ExternalSenderTest{B: b}

		// Passes messages from the consensus engine to the network
		sender := sender.Sender{}

		sender.Initialize(ctx, externalSender, router, &timeoutManager)

		// The engine handles consensus
		engine := smeng.Transitive{}
		engine.Initialize(smeng.Config{
			BootstrapConfig: smeng.BootstrapConfig{
				Config: common.Config{
					Context:    ctx,
					Validators: vdrs,
					Beacons:    beacons,
					Alpha:      uint64(beacons.Len()/2 + 1),
					Sender:     &sender,
				},
				Blocked: blocked,
				VM:      vm,
			},
			Params: snowball.Parameters{
				Metrics:           prometheus.NewRegistry(),
				K:                 1,
				Alpha:             1,
				BetaVirtuous:      20,
				BetaRogue:         20,
				ConcurrentRepolls: 1,
			},
			Consensus: &smcon.Topological{},
		})

		// Asynchronously passes messages from the network to the consensus engine
		handler := &handler.Handler{}
		handler.Initialize(&engine, msgChan, 1000)

		// Allow incoming messages to be routed to the new chain
		router.AddChain(handler)
		go ctx.Log.RecoverAndPanic(handler.Dispatch)

		engine.Startup()
		ctx.Lock.Unlock()

		wg := sync.WaitGroup{}
		wg.Add(numBlocks * numTxsPerBlock)

		b.StartTimer()
		for _, block := range blocks {
			for _, tx := range block.txs {
				ctx.Lock.Lock()
				if _, err := vm.IssueTx(tx.Bytes(), func(choices.Status) {
					wg.Done()
				}); err != nil {
					ctx.Lock.Unlock()
					b.Fatal(err)
				}
				ctx.Lock.Unlock()
			}
		}
		wg.Wait()
		b.StopTimer()
	}
}

// BenchmarkConsensusLeader1 runs the leader consensus benchmark for 1 block
func BenchmarkConsensusLeader1(b *testing.B) {
	ConsensusLeader(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// BenchmarkConsensusLeader10 runs the leader consensus benchmark for 10 blocks
func BenchmarkConsensusLeader10(b *testing.B) {
	ConsensusLeader(
		/*numBlocks=*/ 10,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// ConsensusFollower runs the follower consensus benchmark for blocks
func ConsensusFollower(numBlocks, numTxsPerBlock int, b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	ctx := snow.DefaultContextTest()
	genesisData, blocks := genGenesisState(
		/*numBlocks=*/ numBlocks,
		/*numTxsPerBlock=*/ numTxsPerBlock,
		/*testing=*/ b,
	)

	maxBatchSize = 1
	for n := 0; n < b.N; n++ {
		db := memdb.New()
		vmDB := prefixdb.New([]byte("vm"), db)
		bootstrappingDB := prefixdb.New([]byte("bootstrapping"), db)

		blocked, err := queue.New(bootstrappingDB)
		if err != nil {
			b.Fatal(err)
		}

		// The channel through which a VM may send messages to the consensus engine
		// VM uses this channel to notify engine that a block is ready to be made
		msgChan := make(chan common.Message, 1000)

		vdrs := validators.NewSet()
		vdrs.Add(validators.NewValidator(ctx.NodeID, 1))
		beacons := validators.NewSet()

		timeoutManager := timeout.Manager{}
		timeoutManager.Initialize(2 * time.Second)
		go timeoutManager.Dispatch()

		router := &router.ChainRouter{}
		router.Initialize(logging.NoLog{}, &timeoutManager, time.Hour)

		wg := sync.WaitGroup{}
		wg.Add(numBlocks)

		// Initialize the VM
		vm := &VM{
			onAccept: func(ids.ID) { wg.Done() },
		}
		defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
		ctx.Lock.Lock()
		if err := vm.Initialize(ctx, vmDB, genesisData, msgChan, nil); err != nil {
			b.Fatal(err)
		}

		externalSender := &sender.ExternalSenderTest{B: b}

		// Passes messages from the consensus engine to the network
		sender := sender.Sender{}

		sender.Initialize(ctx, externalSender, router, &timeoutManager)

		// The engine handles consensus
		engine := smeng.Transitive{}
		engine.Initialize(smeng.Config{
			BootstrapConfig: smeng.BootstrapConfig{
				Config: common.Config{
					Context:    ctx,
					Validators: vdrs,
					Beacons:    beacons,
					Alpha:      uint64(beacons.Len()/2 + 1),
					Sender:     &sender,
				},
				Blocked: blocked,
				VM:      vm,
			},
			Params: snowball.Parameters{
				Metrics:           prometheus.NewRegistry(),
				K:                 1,
				Alpha:             1,
				BetaVirtuous:      20,
				BetaRogue:         20,
				ConcurrentRepolls: 1,
			},
			Consensus: &smcon.Topological{},
		})

		// Asynchronously passes messages from the network to the consensus engine
		handler := &handler.Handler{}
		handler.Initialize(&engine, msgChan, 1000)

		// Allow incoming messages to be routed to the new chain
		router.AddChain(handler)
		go ctx.Log.RecoverAndPanic(handler.Dispatch)

		engine.Startup()
		ctx.Lock.Unlock()

		b.StartTimer()
		for _, block := range blocks {
			router.Put(ctx.NodeID, ctx.ChainID, 0, block.ID(), block.Bytes())
		}
		wg.Wait()
		b.StopTimer()
	}
}

// BenchmarkConsensusFollower1 runs the follower consensus benchmark for 1 block
func BenchmarkConsensusFollower1(b *testing.B) {
	ConsensusFollower(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// BenchmarkConsensusFollower10 runs the follower consensus benchmark for 10 blocks
func BenchmarkConsensusFollower10(b *testing.B) {
	ConsensusFollower(
		/*numBlocks=*/ 10,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// BenchmarkConsensusFollower100 runs the follower consensus benchmark for 100 blocks
func BenchmarkConsensusFollower100(b *testing.B) {
	ConsensusFollower(
		/*numBlocks=*/ 100,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}
