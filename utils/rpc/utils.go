package rpc

import (
	"net/url"
)

func stripPassword(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}

	return u.Redacted()
}
