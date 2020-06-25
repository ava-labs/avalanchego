// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sync"

	jwt "github.com/dgrijalva/jwt-go"

	"github.com/gorilla/handlers"

	"github.com/rs/cors"

	"github.com/ava-labs/gecko/api/auth"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/logging"
)

const (
	baseURL      = "/ext"
	authEndpoint = "auth"
)

var (
	errUnknownLockOption = errors.New("invalid lock options")
)

// Server maintains the HTTP router
type Server struct {
	log              logging.Logger
	factory          logging.Factory
	router           *router
	listenAddress    string
	requireAuthToken bool
	auth             *auth.Auth
}

// Wrap a handler. Before passing a request to the handler, check that
func (s *Server) authMiddleware(h http.Handler) http.Handler {
	if !s.requireAuthToken {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Base(r.URL.Path) == authEndpoint { // Don't require auth token to hit auth endpoint
			h.ServeHTTP(w, r)
			return
		}
		tokenStr, err := auth.GetToken(r) // Get the token from the header
		if err == auth.ErrNoToken {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, err.Error())
			return
		} else if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "couldn't parse auth token. Header \"Authorization\" should be \"Bearer TOKEN.GOES.HERE\"")
			return
		}
		token, err := jwt.Parse(tokenStr, func(*jwt.Token) (interface{}, error) {
			return []byte(s.auth.Password), nil
		})
		if !token.Valid {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "auth token is invalid")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Initialize creates the API server at the provided host and port
func (s *Server) Initialize(log logging.Logger, factory logging.Factory, host string, port uint16, requireAuthToken bool, authPassword string) {
	s.log = log
	s.factory = factory
	s.listenAddress = fmt.Sprintf("%s:%d", host, port)
	s.router = newRouter()
	if requireAuthToken {
		s.requireAuthToken = requireAuthToken
		s.auth = &auth.Auth{Password: authPassword}
		authService := auth.NewService(s.log, s.auth)
		s.AddRoute(authService, &sync.RWMutex{}, authEndpoint, "", s.log) // TODO check error
	}
}

// Dispatch starts the API server
func (s *Server) Dispatch() error {
	listener, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		return err
	}
	s.log.Info("HTTP API server listening on %q", s.listenAddress)
	handler := cors.Default().Handler(s.router)
	handler = s.authMiddleware(handler)
	return http.Serve(listener, handler)
}

// DispatchTLS starts the API server with the provided TLS certificate
func (s *Server) DispatchTLS(certFile, keyFile string) error {
	listener, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		return err
	}
	s.log.Info("HTTPS API server listening on %q", s.listenAddress)
	handler := cors.Default().Handler(s.router)
	handler = s.authMiddleware(handler)
	return http.ServeTLS(listener, handler, certFile, keyFile)
}

// RegisterChain registers the API endpoints associated with this chain That
// is, add <route, handler> pairs to server so that http calls can be made to
// the vm
func (s *Server) RegisterChain(ctx *snow.Context, vmIntf interface{}) {
	vm, ok := vmIntf.(common.VM)
	if !ok {
		return
	}

	// all subroutes to a chain begin with "bc/<the chain's ID>"
	chainID := ctx.ChainID.String()
	defaultEndpoint := "bc/" + chainID
	httpLogger, err := s.factory.MakeChain(chainID, "http")
	if err != nil {
		s.log.Error("Failed to create new http logger: %s", err)
		return
	}
	s.log.Verbo("About to add API endpoints for chain with ID %s", ctx.ChainID)

	// Register each endpoint
	for extension, service := range vm.CreateHandlers() {
		// Validate that the route being added is valid
		// e.g. "/foo" and "" are ok but "\n" is not
		_, err := url.ParseRequestURI(extension)
		if extension != "" && err != nil {
			s.log.Warn("could not add route to chain's API handler because route is malformed: %s", extension)
			continue
		}
		s.log.Verbo("adding API endpoint: %s", defaultEndpoint+extension)
		if err := s.AddRoute(service, &ctx.Lock, defaultEndpoint, extension, httpLogger); err != nil {
			s.log.Error("error adding route: %s", err)
		}
	}
}

// AddRoute registers the appropriate endpoint for the vm given an endpoint
func (s *Server) AddRoute(handler *common.HTTPHandler, lock *sync.RWMutex, base, endpoint string, log logging.Logger) error {
	url := fmt.Sprintf("%s/%s", baseURL, base)
	s.log.Info("adding route %s%s", url, endpoint)
	h := handlers.CombinedLoggingHandler(log, handler.Handler)
	switch handler.LockOptions {
	case common.WriteLock:
		return s.router.AddRouter(url, endpoint, middlewareHandler{
			before:  lock.Lock,
			after:   lock.Unlock,
			handler: h,
		})
	case common.ReadLock:
		return s.router.AddRouter(url, endpoint, middlewareHandler{
			before:  lock.RLock,
			after:   lock.RUnlock,
			handler: h,
		})
	case common.NoLock:
		return s.router.AddRouter(url, endpoint, h)
	default:
		return errUnknownLockOption
	}
}

// AddAliases registers aliases to the server
func (s *Server) AddAliases(endpoint string, aliases ...string) error {
	url := fmt.Sprintf("%s/%s", baseURL, endpoint)
	endpoints := make([]string, len(aliases))
	for i, alias := range aliases {
		endpoints[i] = fmt.Sprintf("%s/%s", baseURL, alias)
	}
	return s.router.AddAlias(url, endpoints...)
}

// AddAliasesWithReadLock registers aliases to the server assuming the http read
// lock is currently held.
func (s *Server) AddAliasesWithReadLock(endpoint string, aliases ...string) error {
	// This is safe, as the read lock doesn't actually need to be held once the
	// http handler is called. However, it is unlocked later, so this function
	// must end with the lock held.
	s.router.lock.RUnlock()
	defer s.router.lock.RLock()

	return s.AddAliases(endpoint, aliases...)
}

// Call ...
func (s *Server) Call(
	writer http.ResponseWriter,
	method,
	base,
	endpoint string,
	body io.Reader,
	headers map[string]string,
) error {
	url := fmt.Sprintf("%s/vm/%s", baseURL, base)

	handler, err := s.router.GetHandler(url, endpoint)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "*", body)
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	handler.ServeHTTP(writer, req)

	return nil
}
