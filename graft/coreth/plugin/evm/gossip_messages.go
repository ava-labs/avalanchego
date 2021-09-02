package evm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

type appMsgType byte

const (
	atomicTxIDType appMsgType = iota + 1
	atomicTxType
	ethHashesType
	ethTxListType
)

type AppMsg struct {
	MsgType      appMsgType `serialize:"true"`
	Bytes        []byte     `serialize:"true"`
	appGossipObj interface{}
}

func (vm *VM) decodeToAppMsg(bytes []byte) (*AppMsg, error) {
	appMsg := &AppMsg{}
	if _, err := vm.codec.Unmarshal(bytes, appMsg); err != nil {
		log.Debug(fmt.Sprintf("could not decode AppRequest msg, error %v", err))
		return nil, fmt.Errorf("could not decode AppRequest msg")
	}

	switch appMsg.MsgType {
	case atomicTxIDType:
		if len(appMsg.Bytes) != 32 {
			log.Debug("TxID bytes cannot be decoded into txID")
			return nil, fmt.Errorf("bad atomicTxID AppMsg")
		}
		txID := ids.ID{}
		copy(txID[:], appMsg.Bytes)
		appMsg.appGossipObj = txID
		return appMsg, nil

	case atomicTxType:
		tx := &Tx{}
		if _, err := vm.codec.Unmarshal(appMsg.Bytes, tx); err != nil {
			log.Debug(fmt.Sprintf("could not decode atomic tx, error %v", err))
			return nil, err
		}
		unsignedBytes, err := vm.codec.Marshal(codecVersion, &tx.UnsignedAtomicTx)
		if err != nil {
			log.Debug(fmt.Sprintf("could not decode unsigned atomic tx, error %v", err))
			return nil, err
		}
		tx.Initialize(unsignedBytes, appMsg.Bytes)
		appMsg.appGossipObj = tx
		return appMsg, nil

	case ethHashesType:
		hashList := make([]common.Hash, 0)
		if err := rlp.DecodeBytes(appMsg.Bytes, &hashList); err != nil {
			log.Debug(fmt.Sprintf("could not decode AppRequest msg carrying eth hashes, error %v", err))
			return nil, fmt.Errorf("could not decode AppRequest msg with eth hashes")
		}
		appMsg.appGossipObj = hashList
		return appMsg, nil

	case ethTxListType:
		ethTxs := make([]*types.Transaction, 0)
		if err := rlp.DecodeBytes(appMsg.Bytes, &ethTxs); err != nil {
			log.Debug(fmt.Sprintf("could not decode AppRequest msg carrying eth txs, error %v", err))
			return nil, fmt.Errorf("could not decode AppRequest msg with eth txs")
		}
		appMsg.appGossipObj = ethTxs
		return appMsg, nil

	default:
		log.Debug(fmt.Sprintf("Unknown AppRequest msg txIDType %v", appMsg.MsgType))
		return nil, fmt.Errorf("unknown AppRequest msg txIDType")
	}
}

func (vm *VM) encodeTxID(txID ids.ID) ([]byte, error) {
	am := &AppMsg{
		MsgType: atomicTxIDType,
		Bytes:   txID[:],
	}

	return vm.codec.Marshal(codecVersion, am)
}

func (vm *VM) encodeAtomicTx(tx *Tx) ([]byte, error) {
	am := &AppMsg{
		MsgType: atomicTxType,
	}

	bytes, err := vm.codec.Marshal(codecVersion, tx)
	if err != nil {
		return nil, err
	}

	am.Bytes = bytes
	return vm.codec.Marshal(codecVersion, am)
}

func (vm *VM) encodeEthHashes(ethTxHashes []common.Hash) ([]byte, error) {
	am := &AppMsg{
		MsgType: ethHashesType,
	}

	bytes, err := rlp.EncodeToBytes(ethTxHashes)
	if err != nil {
		return nil, err
	}

	am.Bytes = bytes
	return vm.codec.Marshal(codecVersion, am)
}

func (vm *VM) encodeEthTxs(ethTxs []*types.Transaction) ([]byte, error) {
	am := &AppMsg{
		MsgType: ethTxListType,
	}

	bytes, err := rlp.EncodeToBytes(ethTxs)
	if err != nil {
		return nil, err
	}

	am.Bytes = bytes
	return vm.codec.Marshal(codecVersion, am)
}
