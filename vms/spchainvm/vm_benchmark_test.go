package spchainvm

import (
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
)

func genGenesisState(numBlocks, numTxsPerBlock int, b *testing.B) ([]byte, []*Block) {
	ctx := snow.DefaultContextTest()
	builder := Builder{
		NetworkID: ctx.NetworkID,
		ChainID:   ctx.ChainID,
	}

	genesisBlock, err := builder.NewBlock(ids.Empty, nil)
	if err != nil {
		b.Fatal(err)
	}

	blocks := genBlocks(
		/*numBlocks=*/ numBlocks,
		/*numTxsPerBlock=*/ numTxsPerBlock,
		/*initialParent=*/ genesisBlock.ID(),
		/*testing=*/ b,
	)

	genesisAccounts := []Account{}
	for _, block := range blocks {
		for _, tx := range block.txs {
			genesisAccounts = append(
				genesisAccounts,
				Account{
					id:      tx.Key(ctx).Address(),
					nonce:   tx.nonce - 1,
					balance: tx.amount,
				},
			)
		}
	}

	codec := Codec{}
	genesisData, err := codec.MarshalGenesis(genesisAccounts)
	if err != nil {
		b.Fatal(err)
	}

	return genesisData, blocks
}

func genGenesisStateBytes(numBlocks, numTxsPerBlock int, b *testing.B) ([]byte, [][]byte) {
	genesisData, blocks := genGenesisState(numBlocks, numTxsPerBlock, b)
	blockBytes := make([][]byte, numBlocks)
	for i, block := range blocks {
		blockBytes[i] = block.Bytes()
	}
	return genesisData, blockBytes
}

// BenchmarkParseBlock runs the benchmark of parsing blocks
func BenchmarkParseBlock(b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	ctx := snow.DefaultContextTest()
	genesisBytes, blocks := genGenesisStateBytes(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
	vm := &VM{}
	defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
	vm.Initialize(
		/*ctx=*/ ctx,
		/*db=*/ memdb.New(),
		/*genesis=*/ genesisBytes,
		/*engineChan=*/ make(chan common.Message, 1),
		/*fxs=*/ nil,
	)
	for n := 0; n < b.N; n++ {
		for _, blockBytes := range blocks {
			vm.state.block.Flush()

			b.StartTimer()
			if _, err := vm.ParseBlock(blockBytes); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
		}
	}
}

// BenchmarkParseAndVerify runs the benchmark of parsing blocks and verifying them
func BenchmarkParseAndVerify(b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	genesisBytes, blocks := genGenesisStateBytes(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)

	for n := 0; n < b.N; n++ {
		vm := &VM{}
		defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
		vm.Initialize(
			/*ctx=*/ snow.DefaultContextTest(),
			/*db=*/ memdb.New(),
			/*genesis=*/ genesisBytes,
			/*engineChan=*/ make(chan common.Message, 1),
			/*fxs=*/ nil,
		)

		b.StartTimer()
		for _, blockBytes := range blocks {
			blk, err := vm.ParseBlock(blockBytes)
			if err != nil {
				b.Fatal(err)
			}
			if err := blk.Verify(); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
	}
}

// BenchmarkAccept runs the benchmark of accepting blocks
func BenchmarkAccept(b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	genesisBytes, blocks := genGenesisStateBytes(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)

	for n := 0; n < b.N; n++ {
		vm := &VM{}
		defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()

		vm.Initialize(
			/*ctx=*/ snow.DefaultContextTest(),
			/*db=*/ memdb.New(),
			/*genesis=*/ genesisBytes,
			/*engineChan=*/ make(chan common.Message, 1),
			/*fxs=*/ nil,
		)

		for _, blockBytes := range blocks {
			blk, err := vm.ParseBlock(blockBytes)
			if err != nil {
				b.Fatal(err)
			}
			if err := blk.Verify(); err != nil {
				b.Fatal(err)
			}

			b.StartTimer()
			blk.Accept()
			b.StopTimer()
		}
	}
}

// ParseAndVerifyAndAccept runs the benchmark of parsing, verifying, and accepting blocks
func ParseAndVerifyAndAccept(numBlocks, numTxsPerBlock int, b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	genesisBytes, blocks := genGenesisStateBytes(
		/*numBlocks=*/ numBlocks,
		/*numTxsPerBlock=*/ numTxsPerBlock,
		/*testing=*/ b,
	)

	for n := 0; n < b.N; n++ {
		vm := &VM{}
		defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
		vm.Initialize(
			/*ctx=*/ snow.DefaultContextTest(),
			/*db=*/ memdb.New(),
			/*genesis=*/ genesisBytes,
			/*engineChan=*/ make(chan common.Message, 1),
			/*fxs=*/ nil,
		)

		b.StartTimer()
		for _, blockBytes := range blocks {
			blk, err := vm.ParseBlock(blockBytes)
			if err != nil {
				b.Fatal(err)
			}
			if err := blk.Verify(); err != nil {
				b.Fatal(err)
			}
			blk.Accept()
		}
		b.StopTimer()
	}
}

// BenchmarkParseAndVerifyAndAccept1 runs the benchmark of parsing, verifying, and accepting 1 block
func BenchmarkParseAndVerifyAndAccept1(b *testing.B) {
	ParseAndVerifyAndAccept(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// BenchmarkParseAndVerifyAndAccept10 runs the benchmark of parsing, verifying, and accepting 10 blocks
func BenchmarkParseAndVerifyAndAccept10(b *testing.B) {
	ParseAndVerifyAndAccept(
		/*numBlocks=*/ 10,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// ParseThenVerifyThenAccept runs the benchmark of parsing then verifying and then accepting blocks
func ParseThenVerifyThenAccept(numBlocks, numTxsPerBlock int, b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	genesisBytes, blocks := genGenesisStateBytes(
		/*numBlocks=*/ numBlocks,
		/*numTxsPerBlock=*/ numTxsPerBlock,
		/*testing=*/ b,
	)

	for n := 0; n < b.N; n++ {
		vm := &VM{}
		defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
		vm.Initialize(
			/*ctx=*/ snow.DefaultContextTest(),
			/*db=*/ memdb.New(),
			/*genesis=*/ genesisBytes,
			/*engineChan=*/ make(chan common.Message, 1),
			/*fxs=*/ nil,
		)

		b.StartTimer()
		parsedBlocks := make([]snowman.Block, len(blocks))
		for i, blockBytes := range blocks {
			blk, err := vm.ParseBlock(blockBytes)
			if err != nil {
				b.Fatal(err)
			}
			parsedBlocks[i] = blk
		}
		for _, blk := range parsedBlocks {
			if err := blk.Verify(); err != nil {
				b.Fatal(err)
			}
		}
		for _, blk := range parsedBlocks {
			blk.Accept()
		}
		b.StopTimer()
	}
}

// BenchmarkParseThenVerifyThenAccept1 runs the benchmark of parsing then verifying and then accepting 1 block
func BenchmarkParseThenVerifyThenAccept1(b *testing.B) {
	ParseThenVerifyThenAccept(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// BenchmarkParseThenVerifyThenAccept10 runs the benchmark of parsing then verifying and then accepting 10 blocks
func BenchmarkParseThenVerifyThenAccept10(b *testing.B) {
	ParseThenVerifyThenAccept(
		/*numBlocks=*/ 10,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// IssueAndVerifyAndAccept runs the benchmark of issuing, verifying, and accepting blocks
func IssueAndVerifyAndAccept(numBlocks, numTxsPerBlock int, b *testing.B) {
	b.StopTimer()
	b.ResetTimer()

	genesisBytes, blocks := genGenesisState(
		/*numBlocks=*/ numBlocks,
		/*numTxsPerBlock=*/ numTxsPerBlock,
		/*testing=*/ b,
	)

	for n := 0; n < b.N; n++ {
		vm := &VM{}
		defer func() { ctx.Lock.Lock(); vm.Shutdown(); vm.ctx.Lock.Unlock() }()
		vm.Initialize(
			/*ctx=*/ snow.DefaultContextTest(),
			/*db=*/ memdb.New(),
			/*genesis=*/ genesisBytes,
			/*engineChan=*/ make(chan common.Message, 1),
			/*fxs=*/ nil,
		)
		vm.SetPreference(vm.LastAccepted())

		b.StartTimer()
		for _, block := range blocks {
			for _, tx := range block.txs {
				if _, err := vm.IssueTx(tx.Bytes(), nil); err != nil {
					b.Fatal(err)
				}
			}

			blk, err := vm.BuildBlock()
			if err != nil {
				b.Fatal(err)
			}
			if err := blk.Verify(); err != nil {
				b.Fatal(err)
			}
			vm.SetPreference(blk.ID())
			blk.Accept()
		}
		b.StopTimer()
	}
}

// BenchmarkIssueAndVerifyAndAccept1 runs the benchmark of issuing, verifying, and accepting 1 block
func BenchmarkIssueAndVerifyAndAccept1(b *testing.B) {
	IssueAndVerifyAndAccept(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}

// BenchmarkIssueAndVerifyAndAccept10 runs the benchmark of issuing, verifying, and accepting 10 blocks
func BenchmarkIssueAndVerifyAndAccept10(b *testing.B) {
	IssueAndVerifyAndAccept(
		/*numBlocks=*/ 10,
		/*numTxsPerBlock=*/ 1,
		/*testing=*/ b,
	)
}
