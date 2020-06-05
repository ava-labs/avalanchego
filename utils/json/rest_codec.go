package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/rpc/v2"
)

type URIMethodMap map[string]string
type RestCodec struct {
	Mapping URIMethodMap
}

type RestCodecRequest struct {
	*http.Request
	Mapping URIMethodMap
}

var (
	RestBaseURL = "/rest"
)

//  Checks if the uri is in the mapping of RestCodecRequest
func (r *RestCodecRequest) Method() (string, error) {
	//
	req := r.Request
	uri := strings.ToLower(req.RequestURI)
	for k, v := range r.Mapping {
		if strings.HasSuffix(uri, k) {
			return v, nil
		}
	}
	return "", errors.New("Path not found")
}

// generates mapping of uri to base.method
func MappingGenerator(m map[string]string, base string) URIMethodMap {
	r := URIMethodMap{}
	for k, v := range m {
		r[k] = fmt.Sprintf("%s.%s", base, v)
	}
	return r
}
func (r *RestCodecRequest) ReadRequest(args interface{}) error {
	buf := new(bytes.Buffer)
	buf.ReadFrom(r.Request.Body)
	if len(buf.Bytes()) == 0 {
		return nil
	}
	if err := json.Unmarshal(buf.Bytes(), args); err != nil {
		return err
	}
	fmt.Println(args)
	return nil
}
func newEncode(w http.ResponseWriter) io.Writer {
	return w
}
func (c *RestCodecRequest) WriteResponse(w http.ResponseWriter, reply interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(newEncode(w))
	err := encoder.Encode(reply)
	fmt.Println(reply)
	if err != nil {
		rpc.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}
func (codec RestCodec) NewRequest(req *http.Request) rpc.CodecRequest {

	if strings.HasPrefix(req.RequestURI, RestBaseURL) {
		re := regexp.MustCompile(fmt.Sprintf("^%s", RestBaseURL))
		req.RequestURI = re.ReplaceAllString(req.RequestURI, "")
		r := RestCodecRequest{Request: req, Mapping: codec.Mapping}
		return &r
	}
	return NewCodec().NewRequest(req)
}
func (c *RestCodecRequest) WriteError(w http.ResponseWriter, status int, err error) {
	rpc.WriteError(w, http.StatusInternalServerError, err.Error())
}
