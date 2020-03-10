// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xputtest

// #include "salticidae/network.h"
// void issueTx(msg_t *, msgnetwork_conn_t *, void *);
import "C"

import (
	"unsafe"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/networking"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/hashing"
)

// CClientHandler is the struct that will be accessed on event calls
var CClientHandler CClient

// CClient manages a client network using the c networking library
type CClient struct {
	issuer *Issuer
	net    salticidae.MsgNetwork
}

// Initialize to the c networking library. This should only be called once
// during setup of the node.
func (h *CClient) Initialize(net salticidae.MsgNetwork, issuer *Issuer) {
	h.issuer = issuer
	h.net = net

	net.RegHandler(networking.IssueTx, salticidae.MsgNetworkMsgCallback(C.issueTx), nil)
}

func (h *CClient) send(msg networking.Msg, conn salticidae.MsgNetworkConn) {
	ds := msg.DataStream()
	defer ds.Free()
	ba := salticidae.NewByteArrayMovedFromDataStream(ds, false)
	defer ba.Free()
	cMsg := salticidae.NewMsgMovedFromByteArray(msg.Op(), ba, false)
	defer cMsg.Free()

	h.net.SendMsg(cMsg, conn)
}

// issueTx handles the recept of an IssueTx message
//export issueTx
func issueTx(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	msg := salticidae.MsgFromC(salticidae.CMsg(_msg))

	build := networking.Builder{}
	pMsg, err := build.Parse(networking.IssueTx, msg.GetPayloadByMove())
	if err != nil {
		return
	}

	chainID, _ := ids.ToID(pMsg.Get(networking.ChainID).([]byte))

	txBytes := pMsg.Get(networking.Tx).([]byte)

	txID := ids.NewID(hashing.ComputeHash256Array(txBytes))

	conn := salticidae.MsgNetworkConnFromC(salticidae.CMsgNetworkConn(_conn)).Copy(false)
	CClientHandler.issuer.IssueTx(chainID, txBytes, func(status choices.Status) {
		build := networking.Builder{}
		msg, _ := build.DecidedTx(txID, status)

		CClientHandler.send(msg, conn)
		conn.Free()
	})
}
