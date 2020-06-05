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

type RPCRestMap map[string]map[string]string
type RestCodec struct {
	Mapping RPCRestMap
}

type RestCodecRequest struct {
	*http.Request
	Mapping RPCRestMap
}

func (r *RestCodecRequest) Method() (string, error) {
	req := r.Request
	uri := strings.ToLower(req.RequestURI)
	if r.Mapping[uri] == nil {
		return "", errors.New("Path not found")
	}
	method := strings.ToLower(req.Method)
	if r.Mapping[uri][method] == "" {
		return "", errors.New("Method not allowed")
	}
	return r.Mapping[uri][method], nil
	// uriSections := strings.SplitN(strings.ToLower(req.RequestURI), "/", 5)
	// method := fmt.Sprintf("%s.%s%s", uriSections[2], strings.Title(uriSections[4]), strings.Title(uriSections[3]))
	// return method, nil
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
	restBase := "/api"
	if strings.HasPrefix(req.RequestURI, restBase) {
		re := regexp.MustCompile(`^/api`)
		req.RequestURI = re.ReplaceAllString(req.RequestURI, "")
		r := RestCodecRequest{Request: req, Mapping: codec.Mapping}
		return &r
	}
	return NewCodec().NewRequest(req)
}
func (c *RestCodecRequest) WriteError(w http.ResponseWriter, status int, err error) {
	rpc.WriteError(w, http.StatusInternalServerError, err.Error())
}
