package rpc

import (
	"net/url"
	"strings"
)

func stripPassword(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}

	_, passSet := u.User.Password()
	if passSet {
		return strings.Replace(u.String(), u.User.String()+"@", u.User.Username()+":***@", 1)
	}

	return u.String()
}
