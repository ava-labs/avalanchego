// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"net/http"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/builder"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/chain"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/state"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// Server defines the xsvm API server.
type Server interface {
	Network(r *http.Request, args *struct{}, reply *NetworkReply) error
	Genesis(r *http.Request, args *struct{}, reply *GenesisReply) error
	Nonce(r *http.Request, args *NonceArgs, reply *NonceReply) error
	Balance(r *http.Request, args *BalanceArgs, reply *BalanceReply) error
	Loan(r *http.Request, args *LoanArgs, reply *LoanReply) error
	IssueTx(r *http.Request, args *IssueTxArgs, reply *IssueTxReply) error
	LastAccepted(r *http.Request, args *struct{}, reply *LastAcceptedReply) error
	Block(r *http.Request, args *BlockArgs, reply *BlockReply) error
	Message(r *http.Request, args *MessageArgs, reply *MessageReply) error
}

func NewServer(
	ctx *snow.Context,
	genesis *genesis.Genesis,
	state database.KeyValueReader,
	chain chain.Chain,
	builder builder.Builder,
) Server {
	return &server{
		ctx:     ctx,
		genesis: genesis,
		state:   state,
		chain:   chain,
		builder: builder,
	}
}

type server struct {
	ctx     *snow.Context
	genesis *genesis.Genesis
	state   database.KeyValueReader
	chain   chain.Chain
	builder builder.Builder
}

type NetworkReply struct {
	NetworkID uint32 `json:"networkID"`
	SubnetID  ids.ID `json:"subnetID"`
	ChainID   ids.ID `json:"chainID"`
}

func (s *server) Network(_ *http.Request, _ *struct{}, reply *NetworkReply) error {
	reply.NetworkID = s.ctx.NetworkID
	reply.SubnetID = s.ctx.SubnetID
	reply.ChainID = s.ctx.ChainID
	return nil
}

type GenesisReply struct {
	Genesis *genesis.Genesis `json:"genesis"`
}

func (s *server) Genesis(_ *http.Request, _ *struct{}, reply *GenesisReply) error {
	reply.Genesis = s.genesis
	return nil
}

type NonceArgs struct {
	Address ids.ShortID `json:"address"`
}

type NonceReply struct {
	Nonce uint64 `json:"nonce"`
}

func (s *server) Nonce(_ *http.Request, args *NonceArgs, reply *NonceReply) error {
	nonce, err := state.GetNonce(s.state, args.Address)
	reply.Nonce = nonce
	return err
}

type BalanceArgs struct {
	Address ids.ShortID `json:"address"`
	AssetID ids.ID      `json:"assetID"`
}

type BalanceReply struct {
	Balance uint64 `json:"balance"`
}

func (s *server) Balance(_ *http.Request, args *BalanceArgs, reply *BalanceReply) error {
	balance, err := state.GetBalance(s.state, args.Address, args.AssetID)
	reply.Balance = balance
	return err
}

type LoanArgs struct {
	ChainID ids.ID `json:"chainID"`
}

type LoanReply struct {
	Amount uint64 `json:"amount"`
}

func (s *server) Loan(_ *http.Request, args *LoanArgs, reply *LoanReply) error {
	amount, err := state.GetLoan(s.state, args.ChainID)
	reply.Amount = amount
	return err
}

type IssueTxArgs struct {
	Tx []byte `json:"tx"`
}

type IssueTxReply struct {
	TxID ids.ID `json:"txID"`
}

func (s *server) IssueTx(r *http.Request, args *IssueTxArgs, reply *IssueTxReply) error {
	newTx, err := tx.Parse(args.Tx)
	if err != nil {
		return err
	}

	ctx := r.Context()
	s.ctx.Lock.Lock()
	err = s.builder.AddTx(ctx, newTx)
	s.ctx.Lock.Unlock()
	if err != nil {
		return err
	}

	txID, err := newTx.ID()
	reply.TxID = txID
	return err
}

type LastAcceptedReply struct {
	BlockID    ids.ID `json:"blockID"`
	BlockBytes []byte `json:"blockBytes"`
}

func (s *server) LastAccepted(_ *http.Request, _ *struct{}, reply *LastAcceptedReply) error {
	s.ctx.Lock.RLock()
	reply.BlockID = s.chain.LastAccepted()
	s.ctx.Lock.RUnlock()
	blkBytes, err := state.GetBlock(s.state, reply.BlockID)
	reply.BlockBytes = blkBytes
	return err
}

type BlockArgs struct {
	BlockID ids.ID `json:"blockID"`
}

type BlockReply struct {
	BlockBytes []byte `json:"blockBytes"`
}

func (s *server) Block(_ *http.Request, args *BlockArgs, reply *BlockReply) error {
	blkBytes, err := state.GetBlock(s.state, args.BlockID)
	reply.BlockBytes = blkBytes
	return err
}

type MessageArgs struct {
	TxID ids.ID `json:"txID"`
}

type MessageReply struct {
	Message   *warp.UnsignedMessage `json:"message"`
	Signature []byte                `json:"signature"`
}

func (s *server) Message(_ *http.Request, args *MessageArgs, reply *MessageReply) error {
	message, err := state.GetMessage(s.state, args.TxID)
	if err != nil {
		return err
	}

	reply.Message = message
	reply.Signature, err = s.ctx.WarpSigner.Sign(message)
	return err
}
