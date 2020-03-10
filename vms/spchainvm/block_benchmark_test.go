package spchainvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
)

func genBlocks(numBlocks, numTxsPerBlock int, initialParent ids.ID, b *testing.B) []*Block {
	ctx := snow.DefaultContextTest()
	builder := Builder{
		NetworkID: ctx.NetworkID,
		ChainID:   ctx.ChainID,
	}

	blocks := make([]*Block, numBlocks)[:0]
	for j := 0; j < numBlocks; j++ {
		txs := genTxs(numTxsPerBlock, uint64(j*numTxsPerBlock), b)

		blk, err := builder.NewBlock(initialParent, txs)
		if err != nil {
			b.Fatal(err)
		}
		blocks = append(blocks, blk)
		initialParent = blk.ID()
	}
	return blocks
}

func verifyBlocks(blocks []*Block, b *testing.B) {
	ctx := snow.DefaultContextTest()
	factory := crypto.FactorySECP256K1R{}
	for _, blk := range blocks {
		if err := blk.verify(ctx, &factory); err != nil {
			b.Fatal(err)
		}

		for _, tx := range blk.txs {
			// reset the tx so that it won't be cached
			tx.pubkey = nil
			tx.startedVerification = false
			tx.finishedVerification = false
			tx.verificationErr = nil
		}
	}
}

// BenchmarkBlockVerify runs the benchmark of verification of blocks
func BenchmarkBlockVerify(b *testing.B) {
	blocks := genBlocks(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*initialParent=*/ ids.Empty,
		/*testing=*/ b,
	)
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		verifyBlocks(blocks, b)
	}
}
