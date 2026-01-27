// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// See: https://bitcointalk.org/index.php?topic=285142.msg3300992#msg3300992 as
// the source of these test vectors.
var rfc6979Tests = []test{
	{
		skHex: "0000000000000000000000000000000000000000000000000000000000000001",
		msg:   "Everything should be made as simple as possible, but not simpler.",
		rsHex: "33a69cd2065432a30f3d1ce4eb0d59b8ab58c74f27c41a7fdb5696ad4e6108c96f807982866f785d3f6418d24163ddae117b7db4d5fdf0071de069fa54342262",
	},
	{
		skHex: "fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364140",
		msg:   "Equations are more important to me, because politics is for the present, but an equation is something for eternity.",
		rsHex: "54c4a33c6423d689378f160a7ff8b61330444abb58fb470f96ea16d99d4a2fed07082304410efa6b2943111b6a4e0aaa7b7db55a07e9861d1fb3cb1f421044a5",
	},
	{
		skHex: "fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364140",
		msg:   "Not only is the Universe stranger than we think, it is stranger than we can think.",
		rsHex: "ff466a9f1b7b273e2f4c3ffe032eb2e814121ed18ef84665d0f515360dab3dd06fc95f5132e5ecfdc8e5e6e616cc77151455d46ed48f5589b7db7771a332b283",
	},
	{
		skHex: "0000000000000000000000000000000000000000000000000000000000000001",
		msg:   "How wonderful that we have met with a paradox. Now we have some hope of making progress.",
		rsHex: "c0dafec8251f1d5010289d210232220b03202cba34ec11fec58b3e93a85b91d375afdc06b7d6322a590955bf264e7aaa155847f614d80078a90292fe205064d3",
	},
	{
		skHex: "69ec59eaa1f4f2e36b639716b7c30ca86d9a5375c7b38d8918bd9c0ebc80ba64",
		msg:   "Computer science is no more about computers than astronomy is about telescopes.",
		rsHex: "7186363571d65e084e7f02b0b77c3ec44fb1b257dee26274c38c928986fea45d0de0b38e06807e46bda1f1e293f4f6323e854c86d58abdd00c46c16441085df6",
	},
	{
		skHex: "00000000000000000000000000007246174ab1e92e9149c6e446fe194d072637",
		msg:   "...if you aren't, at any given time, scandalized by code you wrote five or even three years ago, you're not learning anywhere near enough",
		rsHex: "fbfe5076a15860ba8ed00e75e9bd22e05d230f02a936b653eb55b61c99dda4870e68880ebb0050fe4312b1b1eb0899e1b82da89baa5b895f612619edf34cbd37",
	},
	{
		skHex: "000000000000000000000000000000000000000000056916d0f9b31dc9b637f3",
		msg:   "The question of whether computers can think is like the question of whether submarines can swim.",
		rsHex: "cde1302d83f8dd835d89aef803c74a119f561fbaef3eb9129e45f30de86abbf906ce643f5049ee1f27890467b77a6a8e11ec4661cc38cd8badf90115fbd03cef",
	},
}

type test struct {
	skHex string
	msg   string
	rsHex string
}

func TestRFC6979Compliance(t *testing.T) {
	for i, tt := range rfc6979Tests {
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			require := require.New(t)

			skBytes, err := hex.DecodeString(tt.skHex)
			require.NoError(err)

			sk, err := ToPrivateKey(skBytes)
			require.NoError(err)

			msgBytes := []byte(tt.msg)
			sigBytes, err := sk.Sign(msgBytes)
			require.NoError(err)

			expectedRSBytes, err := hex.DecodeString(tt.rsHex)
			require.NoError(err)

			// sigBytes is returned in [R || S || V] format, so we drop last
			// byte to get [R || S]
			rsBytes := sigBytes[:len(sigBytes)-1]
			require.Equal(expectedRSBytes, rsBytes)
		})
	}
}
