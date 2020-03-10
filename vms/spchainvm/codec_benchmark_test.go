package spchainvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

// BenchmarkUnmarshalBlock runs the benchmark of block parsing
func BenchmarkUnmarshalBlock(b *testing.B) {
	blocks := genBlocks(
		/*numBlocks=*/ 1,
		/*numTxsPerBlock=*/ 1,
		/*initialParent=*/ ids.Empty,
		/*testing=*/ b,
	)

	blockBytes := make([][]byte, len(blocks))
	for i, block := range blocks {
		blockBytes[i] = block.Bytes()
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		c := Codec{}
		for _, block := range blockBytes {
			if _, err := c.UnmarshalBlock(block); err != nil {
				b.Fatal(err)
			}
		}
	}
}
