// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

// #include "salticidae/network.h"
// void onTerm(int sig, void *);
// void decidedTx(msg_t *, msgnetwork_conn_t *, void *);
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/networking"
	"github.com/ava-labs/gecko/utils/logging"
)

// network stores the persistent data needed when running the test.
type network struct {
	ec    salticidae.EventContext
	build networking.Builder

	net  salticidae.MsgNetwork
	conn salticidae.MsgNetworkConn

	log     logging.Logger
	decided chan ids.ID

	networkID uint32
}

var net = network{}

func (n *network) Initialize() error {
	n.ec = salticidae.NewEventContext()
	evInt := salticidae.NewSigEvent(n.ec, salticidae.SigEventCallback(C.onTerm), nil)
	evInt.Add(salticidae.SIGINT)
	evTerm := salticidae.NewSigEvent(n.ec, salticidae.SigEventCallback(C.onTerm), nil)
	evTerm.Add(salticidae.SIGTERM)

	serr := salticidae.NewError()
	netconfig := salticidae.NewMsgNetworkConfig()
	n.net = salticidae.NewMsgNetwork(n.ec, netconfig, &serr)
	if serr.GetCode() != 0 {
		return fmt.Errorf("sync error %s", salticidae.StrError(serr.GetCode()))
	}

	n.net.RegHandler(networking.DecidedTx, salticidae.MsgNetworkMsgCallback(C.decidedTx), nil)
	return nil
}

//export onTerm
func onTerm(C.int, unsafe.Pointer) {
	net.log.Info("Terminate signal received")
	net.ec.Stop()
}

// decidedTx handles the recept of a decidedTx message
//export decidedTx
func decidedTx(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	msg := salticidae.MsgFromC(salticidae.CMsg(_msg))

	pMsg, err := net.build.Parse(networking.DecidedTx, msg.GetPayloadByMove())
	if err != nil {
		net.log.Warn("Failed to parse DecidedTx message")
		return
	}

	txID, err := ids.ToID(pMsg.Get(networking.TxID).([]byte))
	net.log.AssertNoError(err) // Length is checked in message parsing

	net.log.Debug("Decided %s", txID)
	net.decided <- txID
}
