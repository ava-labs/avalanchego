package spchainvm

import (
	"testing"

	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
)

func genTxs(numTxs int, offset uint64, b *testing.B) []*Tx {
	ctx := snow.DefaultContextTest()
	builder := Builder{
		NetworkID: ctx.NetworkID,
		ChainID:   ctx.ChainID,
	}
	factory := crypto.FactorySECP256K1R{}

	destKey, err := factory.NewPrivateKey()
	if err != nil {
		b.Fatal(err)
	}
	dest := destKey.PublicKey().Address()

	txs := make([]*Tx, numTxs)[:0]
	for i := 1; i <= numTxs; i++ {
		keyIntf, err := factory.NewPrivateKey()
		if err != nil {
			b.Fatal(err)
		}
		sk := keyIntf.(*crypto.PrivateKeySECP256K1R)

		tx, err := builder.NewTx(sk, uint64(i)+offset, uint64(i)+offset, dest)
		if err != nil {
			b.Fatal(err)
		}
		txs = append(txs, tx)
	}
	return txs
}

func verifyTxs(txs []*Tx, b *testing.B) {
	ctx := snow.DefaultContextTest()
	factory := crypto.FactorySECP256K1R{}
	for _, tx := range txs {
		if err := tx.verify(ctx, &factory); err != nil {
			b.Fatal(err)
		}

		// reset the tx so that it won't be cached
		tx.pubkey = nil
		tx.startedVerification = false
		tx.finishedVerification = false
		tx.verificationErr = nil
	}
}

// BenchmarkTxVerify runs the benchmark of transaction verification
func BenchmarkTxVerify(b *testing.B) {
	txs := genTxs(
		/*numTxs=*/ 1,
		/*initialOffset=*/ 0,
		/*testing=*/ b,
	)
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		verifyTxs(txs, b)
	}
}
