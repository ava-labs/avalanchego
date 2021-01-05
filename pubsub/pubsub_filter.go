package pubsub

import (
	"encoding/hex"
	"net/http"
	"sync"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	avalancheGoJson "github.com/ava-labs/avalanchego/utils/json"
)

type FilterParam struct {
	Address []ids.ShortID
}

type FilterResponse struct {
	Channel         string      `json:"channel"`
	TxID            ids.ID      `json:"txID"`
	Address         string      `json:"address"`
	FilteredAddress ids.ShortID `json:"filteredAddress"`
}

type Parser interface {
	Filter(*FilterParam) *FilterResponse
}

type Filter interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
	Publish(channel string, msg interface{}, parser Parser)
	Register(channel string) error
}

type pubsubfilter struct {
	po   *avalancheGoJson.PubSubServer
	lock sync.Mutex
	fp   *FilterParam
	hrp  string
}

func NewPubSubServerWithFilter(ctx *snow.Context) Filter {
	hrp := constants.GetHRP(ctx.NetworkID)
	return &pubsubfilter{hrp: hrp, po: avalancheGoJson.NewPubSubServer(ctx)}
}

func (ps *pubsubfilter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ps.lock.Lock()
	fp := ps.buildFilter(r)
	if len(fp.Address) != 0 {
		ps.fp = fp
	}
	ps.lock.Unlock()

	ps.po.ServeHTTP(w, r)
}

func (ps *pubsubfilter) buildFilter(r *http.Request) *FilterParam {
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
	return fp
}

func (ps *pubsubfilter) Publish(channel string, msg interface{}, parser Parser) {
	if ps.fp != nil {
		fr := parser.Filter(ps.fp)
		if fr == nil {
			return
		}
		var err error
		fr.Channel = channel
		fr.Address, err = formatting.FormatBech32(ps.hrp, fr.FilteredAddress.Bytes())
		if err != nil {
			return
		}
		ps.po.PublishRaw(channel, fr)
	} else {
		ps.po.Publish(channel, msg)
	}
}

func (ps *pubsubfilter) Register(channel string) error {
	return ps.po.Register(channel)
}
