package request

import "fmt"

// Handler controls issueance of requestID for AppGossip related messages
// RequestIDs are issued sequentially starting from 1 [0 excluded, used as placeholder].
// RequestIDs can be reclaimed in any order.
// Reclaimed requestIDs can be reused as soon as all larger requestIDs are reclaimed.
// TODO: why not reuse them immediately ?

type Handler interface {
	IssueID() uint32
	ReclaimID(reqID uint32) error
}

type neighbors struct {
	prev uint32
	next uint32
}

type handler struct {
	outstandingReqID map[uint32]*neighbors
	lastIssuedReqID  uint32
}

func NewHandler() Handler {
	res := &handler{}
	res.outstandingReqID = make(map[uint32]*neighbors)
	return res
}

func (rq *handler) IssueID() uint32 {
	rq.lastIssuedReqID++
	rq.outstandingReqID[rq.lastIssuedReqID] = &neighbors{
		prev: rq.lastIssuedReqID - 1,
		next: 0,
	}
	if item, ok := rq.outstandingReqID[rq.lastIssuedReqID-1]; ok {
		item.next = rq.lastIssuedReqID
	}

	return rq.lastIssuedReqID
}

func (rq *handler) ReclaimID(reqID uint32) error {
	req, ok := rq.outstandingReqID[reqID]
	if !ok {
		return fmt.Errorf("unknown ReqID")
	}

	if reqID == rq.lastIssuedReqID {
		rq.lastIssuedReqID = rq.outstandingReqID[reqID].prev
	}

	if item, ok := rq.outstandingReqID[req.prev]; ok {
		item.next = req.next
	}

	if item, ok := rq.outstandingReqID[req.next]; ok {
		item.prev = req.prev
	}

	delete(rq.outstandingReqID, reqID)
	return nil
}
