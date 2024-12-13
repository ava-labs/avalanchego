package bls

var (
	// The ciphersuite is more commonly known as G2ProofOfPossession.
	// There are two digests to ensure that message space for normal
	// signatures and the proof of possession are distinct.
	CiphersuiteSignature         = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")
	CiphersuiteProofOfPossession = []byte("BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")
)
