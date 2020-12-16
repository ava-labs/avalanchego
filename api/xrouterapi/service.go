// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// Created by Kent Fourie
// See the file LICENSE for licensing terms.
package xrouterapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	avaxjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/blocknetdx/go-xrouter/xrouter"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// XRouter is the API service for unprivileged info on a node
type XRouterService struct {
	log    logging.Logger
	config chaincfg.Params
	client *xrouter.Client
}

// NewService returns a new XRouter API service
func NewService(log logging.Logger, config chaincfg.Params, client *xrouter.Client) (*common.HTTPHandler, error) {
	newServer := rpc.NewServer()
	codec := avaxjson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	if err := newServer.RegisterService(&XRouterService{
		log:    log,
		config: config,
		client: client,
	}, "xrouterapi"); err != nil {
		return nil, err
	}
	return &common.HTTPHandler{Handler: newServer}, nil
}

// GetNetworkServices
type GetNetworkServicesReply struct {
	Reply interface{} `json:"reply"`
}

// GetNetworkServices
// ListNetworkServices lists all known SPV and XCloud network services (xr and xrs).
func (service *XRouterService) GetNetworkServices(_ *http.Request, _ *struct{}, reply *GetNetworkServicesReply) error {
	service.log.Info("XRouter: GetNetworkServices called")

	networkServicesReply := service.client.ListNetworkServices()
	sort.Strings(networkServicesReply)
	reply.Reply = networkServicesReply
	return nil
}

// GetTransaction
type GetTransactionArgs struct {
	Blockchain string `json:"blockchain"`
	ID         string `json:"tx_id"`
	NodeCount  int    `json:"node_count"`
}

type GetTransactionReply struct {
	UUID  string                   `json:"uuid"`
	Reply []map[string]interface{} `json:"reply"`
	Error map[string]interface{}   `json:"error"`
}

// GetTransaction
func (service *XRouterService) GetTransaction(_ *http.Request, args *GetTransactionArgs, reply *GetTransactionReply) error {
	service.log.Info("XRouter: GetTransaction called")
	if uuid, xrouterReply, err := service.client.GetTransactionRaw(args.Blockchain, args.ID, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var b map[string]interface{}
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &b)
			reply.Error = b
			return nil
		}
		for i := range xrouterReply {
			json.Unmarshal(xrouterReply[i].Reply, &b)
			reply.Reply = append(reply.Reply, b)
		}
		return nil
	}
}

// DecodeTransactionRaw
type DecodeTransactionRawArgs struct {
	Blockchain string `json:"blockchain"`
	Tx         string `json:"tx"`
	NodeCount  int    `json:"node_count"`
}

type DecodeTransactionRawReply struct {
	UUID  string                 `json:"uuid"`
	Reply map[string]interface{} `json:"reply"`
	Error map[string]interface{} `json:"error"`
}

// DecodeTransactionRaw
func (service *XRouterService) DecodeTransactionRaw(_ *http.Request, args *DecodeTransactionRawArgs, reply *DecodeTransactionRawReply) error {
	service.log.Info("XRouter: DecodeTransactionRaw %s called with %v", args.Blockchain, args.Tx)
	if uuid, xrouterReply, err := service.client.DecodeTransactionRaw(args.Blockchain, args.Tx, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var b map[string]interface{}
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &b)
			reply.Error = b
			return nil
		}

		json.Unmarshal(xrouterReply[0].Reply, &b)
		reply.Reply = b
	}
	return nil
}

// SendTransaction
type SendTransactionArgs struct {
	Blockchain string      `json:"blockchain"`
	Tx         interface{} `json:"tx"`
	NodeCount  int         `json:"node_count"`
}

type SendTransactionReply struct {
	UUID  string                 `json:"uuid"`
	Reply string                 `json:"reply"`
	Error map[string]interface{} `json:"error"`
}

// SendTransaction
func (service *XRouterService) SendTransaction(_ *http.Request, args *SendTransactionArgs, reply *SendTransactionReply) error {
	service.log.Info("XRouter: SendTransaction called")
	if uuid, xrouterReply, err := service.client.SendTransactionRaw(args.Blockchain, args.Tx, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var b map[string]interface{}
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &b)
			reply.Error = b
			return nil
		}
		reply.Reply = string(xrouterReply[0].Reply)

	}
	return nil
}

// GetTransactions
type GetTransactionsArgs struct {
	Blockchain string `json:"blockchain"`
	IDS        string `json:"tx_ids"`
	NodeCount  int    `json:"node_count"`
}

type GetTransactionsReply struct {
	UUID  string                   `json:"uuid"`
	Reply []map[string]interface{} `json:"reply"`
	Error map[string]interface{}   `json:"error"`
}

// GetTransactions
func (service *XRouterService) GetTransactions(_ *http.Request, args *GetTransactionsArgs, reply *GetTransactionsReply) error {
	service.log.Info("XRouter: GetTransactions called")
	s := strings.Split(args.IDS, ",")
	params := make([]interface{}, len(s))
	for i, v := range s {
		params[i] = v
	}
	if uuid, xrouterReply, err := service.client.GetTransactionsRaw(args.Blockchain, params, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var e map[string]interface{}
		var b []map[string]interface{}
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &e)
			reply.Error = e
			return nil
		}
		for i := range xrouterReply {
			json.Unmarshal(xrouterReply[i].Reply, &b)
			reply.Reply = append(reply.Reply, b[i])
		}
		return nil
	}
}

// GetConnectedPeers
type GetConnectedPeersReply struct {
	Reply string `json:"reply"`
}

// GetConnectedPeers
func (service *XRouterService) GetConnectedPeers(_ *http.Request, _ *struct{}, reply *GetConnectedPeersReply) error {
	service.log.Info("XRouter: GetConnectedPeers called")
	xrouterReply := service.client.ConnectedCount()
	reply.Reply = strconv.Itoa(int(xrouterReply))
	return nil
}

// GetBlockCount
type GetBlockCountArgs struct {
	Blockchain string `json:"blockchain"`
	NodeCount  int    `json:"node_count"`
}

type GetBlockCountReply struct {
	UUID  string                 `json:"uuid"`
	Reply string                 `json:"reply"`
	Error map[string]interface{} `json:"error"`
}

// GetBlockCount
func (service *XRouterService) GetBlockCount(_ *http.Request, args *GetBlockCountArgs, reply *GetBlockCountReply) error {
	service.log.Info("XRouter: GetBlockCount called with %v", args)
	if uuid, xrouterReply, err := service.client.GetBlockCountRaw(args.Blockchain, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var e map[string]interface{}
		service.log.Info("BLOCK COUNT: %v", string(xrouterReply[0].Reply))
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &e)
			reply.Error = e
			return nil
		}
		reply.Reply = string(xrouterReply[0].Reply)
		return nil
	}
}

// GetBlockHash
type GetBlockHashArgs struct {
	Blockchain string      `json:"blockchain"`
	Block      interface{} `json:"block"`
	NodeCount  int         `json:"node_count"`
}

type GetBlockHashReply struct {
	UUID  string                 `json:"uuid"`
	Reply string                 `json:"reply"`
	Error map[string]interface{} `json:"error"`
}

// GetBlockHash
func (service *XRouterService) GetBlockHash(_ *http.Request, args *GetBlockHashArgs, reply *GetBlockHashReply) error {
	service.log.Info("XRouter: GetBlockHashRaw called")
	if uuid, xrouterReply, err := service.client.GetBlockHashRaw(args.Blockchain, args.Block, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var e map[string]interface{}
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &e)
			reply.Error = e
			return nil
		} else {
			reply.Reply = string(xrouterReply[0].Reply)
		}
		return nil
	}
}

// GetBlocks
type GetBlocksArgs struct {
	Blockchain string `json:"blockchain"`
	IDS        string `json:"block_ids"`
	NodeCount  int    `json:"node_count"`
}

type GetBlocksReply struct {
	UUID  string                   `json:"uuid"`
	Reply []map[string]interface{} `json:"reply"`
	Error map[string]interface{}   `json:"error"`
}

// GetBlocks
func (service *XRouterService) GetBlocks(_ *http.Request, args *GetBlocksArgs, reply *GetBlocksReply) error {
	service.log.Info("XRouter: GetBlocksRaw called")
	s := strings.Split(args.IDS, ",")
	params := make([]interface{}, len(s))
	for i, v := range s {
		params[i] = v
	}
	if uuid, xrouterReply, err := service.client.GetBlocksRaw(args.Blockchain, params, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var e map[string]interface{}
		var b []map[string]interface{}
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &e)
			reply.Error = e
			return nil
		}
		for i := range xrouterReply {
			json.Unmarshal(xrouterReply[i].Reply, &b)
			reply.Reply = append(reply.Reply, b[i])
		}
		return nil
	}
}

// GetBlock
type GetBlockArgs struct {
	Blockchain string `json:"blockchain"`
	Block      string `json:"block"`
	NodeCount  int    `json:"node_count"`
}

type GetBlockReply struct {
	UUID  string                   `json:"uuid"`
	Reply []map[string]interface{} `json:"reply"`
	Error map[string]interface{}   `json:"error"`
}

// GetBlock
func (service *XRouterService) GetBlock(_ *http.Request, args *GetBlockArgs, reply *GetBlockReply) error {
	service.log.Info("XRouter: GetBlock called")
	if uuid, xrouterReply, err := service.client.GetBlockRaw(args.Blockchain, args.Block, args.NodeCount); err != nil {
		service.log.Fatal("error: %v", err)
		return err
	} else {
		reply.UUID = uuid
		var b map[string]interface{}
		if strings.Contains(string(xrouterReply[0].Reply), "error") {
			json.Unmarshal(xrouterReply[0].Reply, &b)
			reply.Error = b
			return nil
		}
		json.Unmarshal(xrouterReply[0].Reply, &b)
		reply.Reply = append(reply.Reply, b)
		return nil
	}
}
