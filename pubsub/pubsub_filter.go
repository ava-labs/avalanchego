package pubsub

import (
	"encoding/hex"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	avalancheGoJson "github.com/ava-labs/avalanchego/utils/json"
)

type FilterParam struct {
	Address []ids.ShortID
}

type FilterResponse struct {
	Channel string      `json:"channel"`
	TxID    ids.ID      `json:"txID"`
	Address ids.ShortID `json:"address"`
}

type Parser interface {
	Filter(*FilterParam) (*FilterResponse, error)
}

type PubSubFilter interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
	Publish(channel string, msg interface{}, parser Parser)
	Register(channel string) error
}

type pubsubfilter struct {
	po   *avalancheGoJson.PubSubServer
	lock sync.Mutex
	fp   *FilterParam
}

func NewPubSubServerWithFilter(ctx *snow.Context) PubSubFilter {
	return &pubsubfilter{po: avalancheGoJson.NewPubSubServer(ctx)}
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
			if err == nil {
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
	if len(fp.Address) != 0 {
		ps.fp = fp
	}
	ps.lock.Unlock()

	ps.po.ServeHTTP(w, r)
}

func (ps *pubsubfilter) Publish(channel string, msg interface{}, parser Parser) {
	if ps.fp != nil {
		fr, err := parser.Filter(ps.fp)
		if err == nil && fr != nil {
			fr.Channel = channel
			ps.po.PublishRaw(channel, fr)
		}
	} else {
		ps.po.Publish(channel, msg)
	}
}

func (ps *pubsubfilter) Register(channel string) error {
	return ps.po.Register(channel)
}
