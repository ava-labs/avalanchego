// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

// var (
// 	ErrConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")

// 	_ Block = &AtomicBlock{}
// )

// // AtomicBlock being accepted results in the atomic transaction contained in the
// // block to be accepted and committed to the chain.
// type AtomicBlock struct {
// 	*stateless.AtomicBlock
// 	*commonBlock
// }

// // NewAtomicBlock returns a new *AtomicBlock where the block's parent, a
// // decision block, has ID [parentID].
// func NewAtomicBlock(
// 	manager Manager,
// 	ctx *snow.Context,
// 	parentID ids.ID,
// 	height uint64,
// 	tx *txs.Tx,
// ) (*AtomicBlock, error) {
// 	statelessBlk, err := stateless.NewAtomicBlock(parentID, height, tx)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return toStatefulAtomicBlock(statelessBlk, manager, ctx, choices.Processing)
// }

// func toStatefulAtomicBlock(
// 	statelessBlk *stateless.AtomicBlock,
// 	manager Manager,
// 	ctx *snow.Context,
// 	status choices.Status,
// ) (*AtomicBlock, error) {
// 	ab := &AtomicBlock{
// 		AtomicBlock: statelessBlk,
// 		commonBlock: &commonBlock{
// 			Manager: manager,
// 			baseBlk: &statelessBlk.CommonBlock,
// 		},
// 	}

// 	ab.Tx.Unsigned.InitCtx(ctx)
// 	return ab, nil
// }

// // conflicts checks to see if the provided input set contains any conflicts with
// // any of this block's non-accepted ancestors or itself.
// func (ab *AtomicBlock) conflicts(s ids.Set) (bool, error) {
// 	return ab.conflictsAtomicBlock(ab, s)
// }

// func (ab *AtomicBlock) Verify() error {
// 	return ab.VerifyAtomicBlock(ab.AtomicBlock)
// }

// func (ab *AtomicBlock) Accept() error {
// 	return ab.AcceptAtomicBlock(ab.AtomicBlock)
// }

// func (ab *AtomicBlock) Reject() error {
// 	return ab.RejectAtomicBlock(ab.AtomicBlock)
// }

// func (ab *AtomicBlock) setBaseState() {
// 	ab.setBaseStateAtomicBlock(ab)
// }
