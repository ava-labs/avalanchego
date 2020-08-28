// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timestampvm

import (
	"errors"
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/json"
)

var (
	errBadData     = errors.New("data must be base 58 repr. of 32 bytes")
	errNoSuchBlock = errors.New("couldn't get block from database. Does it exist?")
)

// Service is the API service for this VM
type Service struct{ vm *VM }

// ProposeBlockArgs are the arguments to function ProposeValue
type ProposeBlockArgs struct {
	// Data in the block. Must be base 58 encoding of 32 bytes.
	Data string `json:"data"`
}

// ProposeBlockReply is the reply from function ProposeBlock
type ProposeBlockReply struct{ Success bool }

// ProposeBlock is an API method to propose a new block whose data is [args].Data.
// [args].Data must be a string repr. of a 32 byte array
func (s *Service) ProposeBlock(_ *http.Request, args *ProposeBlockArgs, reply *ProposeBlockReply) error {
	byteFormatter := formatting.CB58{}
	if err := byteFormatter.FromString(args.Data); err != nil {
		return errBadData
	}
	dataSlice := byteFormatter.Bytes
	if len(dataSlice) != dataLen {
		return errBadData
	}
	var data [dataLen]byte             // The data as an array of bytes
	copy(data[:], dataSlice[:dataLen]) // Copy the bytes in dataSlice to data
	s.vm.proposeBlock(data)
	reply.Success = true
	return nil
}

// APIBlock is the API representation of a block
type APIBlock struct {
	Timestamp json.Uint64 `json:"timestamp"` // Timestamp of most recent block
	Data      string      `json:"data"`      // Data in the most recent block. Base 58 repr. of 5 bytes.
	ID        string      `json:"id"`        // String repr. of ID of the most recent block
	ParentID  string      `json:"parentID"`  // String repr. of ID of the most recent block's parent
}

// GetBlockArgs are the arguments to GetBlock
type GetBlockArgs struct {
	// ID of the block we're getting.
	// If left blank, gets the latest block
	ID string
}

// GetBlockReply is the reply from GetBlock
type GetBlockReply struct {
	APIBlock
}

// GetBlock gets the block whose ID is [args.ID]
// If [args.ID] is empty, get the latest block
func (s *Service) GetBlock(_ *http.Request, args *GetBlockArgs, reply *GetBlockReply) error {
	var ID ids.ID
	var err error
	if args.ID == "" {
		ID = s.vm.LastAccepted()
	} else {
		ID, err = ids.FromString(args.ID)
		if err != nil {
			return errors.New("problem parsing ID")
		}
	}

	blockInterface, err := s.vm.GetBlock(ID)
	if err != nil {
		return errNoSuchBlock
	}

	block, ok := blockInterface.(*Block)
	if !ok {
		return errBadData
	}

	reply.APIBlock.ID = block.ID().String()
	reply.APIBlock.Timestamp = json.Uint64(block.Timestamp)
	reply.APIBlock.ParentID = block.ParentID().String()
	byteFormatter := formatting.CB58{Bytes: block.Data[:]}
	reply.Data = byteFormatter.String()

	return nil
}
