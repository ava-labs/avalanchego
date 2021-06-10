package compression

import (
	"github.com/NYTimes/gziphandler"
	"net/http"
)

func EnableGzipSupport(handler http.Handler) http.Handler {
	return gziphandler.GzipHandler(handler)
}
