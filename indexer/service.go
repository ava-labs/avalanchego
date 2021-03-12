package indexer

import (
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
)

type service struct {
	Index
}

type FormattedContainer struct {
	ID        string              `json:"id"`
	Bytes     string              `json:"bytes"`
	Timestamp string              `json:"timestamp"`
	Encoding  formatting.Encoding `json:"encoding"`
}

func newFormattedContainer(c Container, enc formatting.Encoding) (FormattedContainer, error) {
	fc := FormattedContainer{
		Encoding: enc,
	}
	idStr, err := formatting.Encode(enc, c.ID[:])
	if err != nil {
		return fc, err
	}
	fc.ID = idStr
	bytesStr, err := formatting.Encode(enc, c.Bytes)
	if err != nil {
		return fc, err
	}
	fc.Timestamp = time.Unix(c.Timestamp, 0).String()
	fc.Bytes = bytesStr
	return fc, nil
}

type GetLastAcceptedArgs struct {
	ChainID  string              `json:"chainID"`
	Encoding formatting.Encoding `json:"encoding"`
}

func (s *service) GetLastAccepted(_ *http.Request, args *GetLastAcceptedArgs, reply *FormattedContainer) error {
	container, err := s.Index.GetLastAccepted()
	if err != nil {
		return err
	}

	*reply, err = newFormattedContainer(container, args.Encoding)
	return err
}

type GetContainer struct {
	ChainID  string              `json:"chainID"`
	Index    json.Uint64         `json:"index"`
	Encoding formatting.Encoding `json:"encoding"`
}

func (s *service) GetContainerByIndex(_ *http.Request, args *GetContainer, reply *FormattedContainer) error {
	// chainID, err := s.Index.chainLookup(args.ChainID)
	// if err != nil {
	// 	return fmt.Errorf("couldn't find chain %s: %w", args.ChainID, err)
	// }

	container, err := s.Index.GetContainerByIndex(uint64(args.Index))
	if err != nil {
		return err
	}

	*reply, err = newFormattedContainer(container, args.Encoding)
	return err
}

type GetContainerRange struct {
	ChainID    string              `json:"chainID"`
	StartIndex json.Uint64         `json:"startIndex"`
	NumToFetch json.Uint64         `json:"numToFetch"`
	Encoding   formatting.Encoding `json:"encoding"`
}

// GetContainerRange returns the transactions at index [startIndex], [startIndex+1], ... , [startIndex+n-1]
// If [n] == 0, returns an empty response (i.e. null).
// If [startIndex] > the last accepted index, returns an error (unless the above apply.)
// If [n] > [maxFetchedByRange], returns an error.
// If we run out of transactions, returns the ones fetched before running out.
func (s *service) GetContainerRange(r *http.Request, args *GetContainerRange, reply *[]FormattedContainer) error {
	containers, err := s.Index.GetContainerRange(uint64(args.StartIndex), uint64(args.NumToFetch))
	if err != nil {
		return err
	}

	formattedContainers := make([]FormattedContainer, len(containers))
	for i, container := range containers {
		formattedContainers[i], err = newFormattedContainer(container, args.Encoding)
		if err != nil {
			return err
		}
	}

	*reply = formattedContainers
	return nil
}

type GetIndexArgs struct {
	ChainID     string              `json:"chainID"`
	ContainerID ids.ID              `json:"containerID"`
	Encoding    formatting.Encoding `json:"encoding"`
}

type GetIndexResponse struct {
	Index json.Uint64 `json:"index"`
}

func (s *service) GetIndex(r *http.Request, args *GetIndexArgs, reply *GetIndexResponse) error {
	index, err := s.Index.GetIndex(args.ContainerID)
	reply.Index = json.Uint64(index)
	return err
}

func (s *service) IsAccepted(r *http.Request, args *GetIndexArgs, reply *bool) error {
	_, err := s.Index.GetIndex(args.ContainerID)
	*reply = err == nil
	return nil
}

func (s *service) GetContainerByID(r *http.Request, args *GetIndexArgs, reply *FormattedContainer) error {
	container, err := s.Index.GetContainerByID(args.ContainerID)
	if err != nil {
		return err
	}

	*reply, err = newFormattedContainer(container, args.Encoding)
	return err
}
