package compression

import (
	"net/http"

	"github.com/NYTimes/gziphandler"
)

func EnableGzipSupport(handler http.Handler) http.Handler {
	return gziphandler.GzipHandler(handler)
}
