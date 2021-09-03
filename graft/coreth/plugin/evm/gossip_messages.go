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
	atmDataType appMsgType = iota + 1
	atmTxType
	ethDataType
	ethTxsType
)

type AppMsg struct {
	MsgType      appMsgType `serialize:"true"`
	Bytes        []byte     `serialize:"true"`
	appGossipObj interface{}
}

func (vm *VM) encodeAtmData(txID ids.ID) ([]byte, error) {
	am := &AppMsg{
		MsgType: atmDataType,
		Bytes:   txID[:],
	}

	return vm.codec.Marshal(codecVersion, am)
}

func (vm *VM) encodeAtmTx(tx *Tx) ([]byte, error) {
	am := &AppMsg{
		MsgType: atmTxType,
	}

	bytes, err := vm.codec.Marshal(codecVersion, tx)
	if err != nil {
		return nil, err
	}

	am.Bytes = bytes
	return vm.codec.Marshal(codecVersion, am)
}

type EthData struct {
	TxHash    common.Hash
	TxAddress common.Address
	TxNonce   uint64
}

func (vm *VM) encodeEthData(ethData []EthData) ([]byte, error) {
	am := &AppMsg{
		MsgType: ethDataType,
	}

	bytes, err := rlp.EncodeToBytes(ethData)
	if err != nil {
		return nil, err
	}

	am.Bytes = bytes
	return vm.codec.Marshal(codecVersion, am)
}

func (vm *VM) encodeEthTxs(ethTxs []*types.Transaction) ([]byte, error) {
	am := &AppMsg{
		MsgType: ethTxsType,
	}

	bytes, err := rlp.EncodeToBytes(ethTxs)
	if err != nil {
		return nil, err
	}

	am.Bytes = bytes
	return vm.codec.Marshal(codecVersion, am)
}

func (vm *VM) decodeToAppMsg(bytes []byte) (*AppMsg, error) {
	appMsg := &AppMsg{}
	if _, err := vm.codec.Unmarshal(bytes, appMsg); err != nil {
		log.Debug(fmt.Sprintf("could not decode AppRequest msg, error %v", err))
		return nil, fmt.Errorf("could not decode AppRequest msg")
	}

	switch appMsg.MsgType {
	case atmDataType:
		if len(appMsg.Bytes) != 32 {
			log.Debug("TxID bytes cannot be decoded into txID")
			return nil, fmt.Errorf("bad atomicTxID AppMsg")
		}
		txID := ids.ID{}
		copy(txID[:], appMsg.Bytes)
		appMsg.appGossipObj = txID
		return appMsg, nil

	case atmTxType:
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

	case ethDataType:
		dataList := make([]EthData, 0)
		if err := rlp.DecodeBytes(appMsg.Bytes, &dataList); err != nil {
			log.Debug(fmt.Sprintf("could not decode AppRequest msg carrying eth hashes, error %v", err))
			return nil, fmt.Errorf("could not decode AppRequest msg with eth hashes")
		}
		appMsg.appGossipObj = dataList
		return appMsg, nil

	case ethTxsType:
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
