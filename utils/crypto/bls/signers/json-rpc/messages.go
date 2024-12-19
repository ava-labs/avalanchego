package jsonrpc

type PublicKeyArgs struct{}

type SignArgs struct {
	Msg []byte
}

type SignProofOfPossessionArgs struct {
	Msg []byte
}

type PublicKeyReply struct {
	PublicKey []byte
}

type SignReply struct {
	Signature []byte
}

type SignProofOfPossessionReply struct {
	Signature []byte
}
