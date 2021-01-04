package pubsub

import (
	"encoding/hex"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/json"
)

type FilterParam struct {
	Address []ids.ShortID
}

type Parser interface {
	Filter(*FilterParam) ([]byte, error)
}

type PubSubFilter interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
	Publish(channel string, msg interface{}, parser Parser)
	Register(channel string) error
}

type pubsubfilter struct {
	po   *json.PubSubServer
	lock sync.Mutex
	fp   *FilterParam
}

func NewPubSubServerWithFilter(ctx *snow.Context) PubSubFilter {
	return &pubsubfilter{po: json.NewPubSubServer(ctx)}
}

func (ps *pubsubfilter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ps.lock.Lock()

	fp := &FilterParam{}

	var values = r.URL.Query()
	for valuesk := range values {
		if valuesk != "address" {
			continue
		}

		for _, value := range values[valuesk] {
			addrBytes, err := hex.DecodeString(value)
			if err != nil {
				if len(addrBytes) != 20 {
					continue
				}
				var bits [20]byte
				copy(bits[:], addrBytes[:20])
				sid := ids.ShortID(bits)
				fp.Address = append(fp.Address, sid)
			}
		}
	}
	ps.lock.Unlock()

	ps.po.ServeHTTP(w, r)
}

func (ps *pubsubfilter) Publish(channel string, msg interface{}, parser Parser) {
	if ps.fp != nil {
		json, err := parser.Filter(ps.fp)
		if err != nil && json != nil {
			ps.po.Publish(channel, string(json))
		}
	} else {
		ps.po.Publish(channel, msg)
	}
}

func (ps *pubsubfilter) Register(channel string) error {
	return ps.po.Register(channel)
}
