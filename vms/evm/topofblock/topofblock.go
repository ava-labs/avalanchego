package topofblock

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/holiman/uint256"
)

var (
	Auctioneer = crypto.CreateAddress2(
		common.Address{},
		crypto.Keccak256Hash([]byte("topofblock")),
		[]byte("auctioneer"),
	)

	ErrDuplicate = errors.New("duplicate top-of-block bid")
	ErrEmpty     = errors.New("empty top-of-block bid")
	ErrNotAtTop  = errors.New("top-of-block bid not first tx in block")
)

const SettlementEventSignature = "TopOfBlockAuction(address,address,uint256)"

var settlementEventTopic0 = crypto.Keccak256Hash([]byte(SettlementEventSignature))

func Bid(bid uint256.Int) types.AccessTuple {
	return types.AccessTuple{
		Address:     Auctioneer,
		StorageKeys: []common.Hash{bid.Bytes32()},
	}
}

func BidOf(tx *types.Transaction) (bid uint256.Int, ok bool, _ error) {
	for _, a := range tx.AccessList() {
		if a.Address != Auctioneer {
			continue
		}
		if ok || len(a.StorageKeys) > 1 {
			return uint256.Int{}, false, ErrDuplicate
		}
		if len(a.StorageKeys) == 0 {
			return uint256.Int{}, false, ErrEmpty
		}

		ok = true
		bid.SetBytes32(a.StorageKeys[0][:])
	}
	return bid, ok, nil
}

func Settle(bCtx vm.BlockContext, sdb *state.StateDB, tx *types.Transaction, signer types.Signer, beneficiary common.Address) error {
	bid, ok, err := BidOf(tx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	sender, err := types.Sender(signer, tx)
	if err != nil {
		return err
	}
	if !bCtx.CanTransfer(sdb, sender, &bid) {
		return vm.ErrInsufficientBalance
	}

	bCtx.Transfer(sdb, sender, beneficiary, &bid)
	sdb.AddLog(SettledAuctionLog(sender, beneficiary, bid))
	return nil
}

func SettledAuctionLog(sender, recipient common.Address, bid uint256.Int) *types.Log {
	return &types.Log{
		Address: Auctioneer,
		Topics: []common.Hash{
			settlementEventTopic0,
			common.BytesToHash(sender.Bytes()),
			common.BytesToHash(recipient.Bytes()),
			bid.Bytes32(),
		},
	}
}
