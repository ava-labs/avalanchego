// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
)

const (
	proxyAPIVersion           = 1
	proxyURLVarName           = "GH_PENDING_REVIEW_PROXY_URL"
	proxyEndpointPath         = "/pending-review"
	proxyAllowedRepoVarName   = "GH_PENDING_REVIEW_PROXY_ALLOWED_REPO"
	proxyListenAddrVarName    = "GH_PENDING_REVIEW_PROXY_LISTEN_ADDR"
	defaultProxyListenAddress = "127.0.0.1:18080"
)

type proxyRequestEnvelope struct {
	Version int             `json:"version"`
	Command string          `json:"command"`
	Payload json.RawMessage `json:"payload"`
}

type proxyResponseEnvelope struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

type proxyCreatePayload struct {
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`
	Body     string `json:"body"`
	JSON     bool   `json:"json,omitempty"`
}

type proxyDeletePayload struct {
	Repo         string `json:"repo"`
	PRNumber     int    `json:"pr_number"`
	EnsureAbsent bool   `json:"ensure_absent,omitempty"`
	JSON         bool   `json:"json,omitempty"`
}

type proxyGetPayload struct {
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`
	Pretty   bool   `json:"pretty,omitempty"`
}

type proxyGetStatePayload struct {
	Repo      string `json:"repo"`
	PRNumber  int    `json:"pr_number"`
	UserLogin string `json:"user_login"`
	Pretty    bool   `json:"pretty,omitempty"`
}

type proxyUpdateBodyPayload struct {
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`
	Body     string `json:"body"`
	Force    bool   `json:"force,omitempty"`
	JSON     bool   `json:"json,omitempty"`
}

type proxyDeleteStatePayload struct {
	Repo      string `json:"repo"`
	PRNumber  int    `json:"pr_number"`
	UserLogin string `json:"user_login"`
	JSON      bool   `json:"json,omitempty"`
}

type proxyReplaceCommentsPayload struct {
	Repo            string             `json:"repo"`
	PRNumber        int                `json:"pr_number"`
	Comments        []DraftReviewEntry `json:"comments"`
	Force           bool               `json:"force,omitempty"`
	CreateIfMissing bool               `json:"create_if_missing,omitempty"`
	ReviewBody      string             `json:"review_body,omitempty"`
	JSON            bool               `json:"json,omitempty"`
}

type proxyUpsertCommentPayload struct {
	Repo            string `json:"repo"`
	PRNumber        int    `json:"pr_number"`
	CommentID       string `json:"comment_id,omitempty"`
	Path            string `json:"path,omitempty"`
	Line            int    `json:"line,omitempty"`
	Side            string `json:"side,omitempty"`
	StartLine       int    `json:"start_line,omitempty"`
	StartSide       string `json:"start_side,omitempty"`
	Body            string `json:"body"`
	Force           bool   `json:"force,omitempty"`
	CreateIfMissing bool   `json:"create_if_missing,omitempty"`
	ReviewBody      string `json:"review_body,omitempty"`
	JSON            bool   `json:"json,omitempty"`
}

type proxyCommandExecutor func(ctx context.Context, cmd command, stdout io.Writer, stderr io.Writer) error

type proxyHandler struct {
	allowedRepo string
	execute     proxyCommandExecutor
}

func proxyURL() string {
	return strings.TrimSpace(os.Getenv(proxyURLVarName))
}

func allowedProxyRepo() string {
	if repo := strings.TrimSpace(os.Getenv(proxyAllowedRepoVarName)); repo != "" {
		return repo
	}
	return defaultRepo
}

func defaultProxyListenAddr() string {
	if addr := strings.TrimSpace(os.Getenv(proxyListenAddrVarName)); addr != "" {
		return addr
	}
	return defaultProxyListenAddress
}

func DefaultProxyListenAddr() string {
	return defaultProxyListenAddr()
}

func (a *App) shouldUseProxy() bool {
	return a.proxyURL != ""
}

func (a *App) runProxy(ctx context.Context, cmd command) error {
	req, err := buildProxyRequest(cmd)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.proxyURL+proxyEndpointPath, bytes.NewReader(body))
	if err != nil {
		return stacktrace.Wrap(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	defer resp.Body.Close()

	var envelope proxyResponseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return stacktrace.Wrap(err)
	}
	if _, err := io.WriteString(a.Stdout, envelope.Stdout); err != nil {
		return stacktrace.Wrap(err)
	}
	if _, err := io.WriteString(a.Stderr, envelope.Stderr); err != nil {
		return stacktrace.Wrap(err)
	}
	if envelope.ExitCode != 0 {
		return stacktrace.Errorf("proxy command failed with exit code %d", envelope.ExitCode)
	}
	return nil
}

func NewProxyHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(proxyEndpointPath, proxyHandler{
		allowedRepo: allowedProxyRepo(),
		execute:     runProxyCommandLocally,
	})
	return mux
}

func runProxyCommandLocally(ctx context.Context, cmd command, stdout io.Writer, stderr io.Writer) error {
	app := NewApp(strings.NewReader(""), stdout, stderr)
	app.proxyURL = ""
	return app.runCommand(ctx, cmd)
}

func (h proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req proxyRequestEnvelope
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProxyResponse(w, http.StatusBadRequest, proxyResponseEnvelope{
			Stderr:   fmt.Sprintf("invalid proxy request: %v\n", err),
			ExitCode: 1,
		})
		return
	}

	if req.Version != proxyAPIVersion {
		writeProxyResponse(w, http.StatusConflict, proxyResponseEnvelope{
			Stderr:   fmt.Sprintf("incompatible pending-review proxy version: client=%d server=%d\n", req.Version, proxyAPIVersion),
			ExitCode: 1,
		})
		return
	}

	spec, ok := lookupCommandSpec(req.Command)
	if !ok || spec.decodeProxy == nil {
		writeProxyResponse(w, http.StatusBadRequest, proxyResponseEnvelope{
			Stderr:   fmt.Sprintf("unsupported proxy command %q\n", req.Command),
			ExitCode: 1,
		})
		return
	}

	cmd, err := spec.decodeProxy(req.Payload)
	if err != nil {
		writeProxyResponse(w, http.StatusBadRequest, proxyResponseEnvelope{
			Stderr:   fmt.Sprintf("invalid proxy payload for %s: %v\n", req.Command, err),
			ExitCode: 1,
		})
		return
	}

	if repo, ok := commandRepo(cmd); ok && repo != h.allowedRepo {
		writeProxyResponse(w, http.StatusForbidden, proxyResponseEnvelope{
			Stderr:   fmt.Sprintf("proxy only permits repo %s\n", h.allowedRepo),
			ExitCode: 1,
		})
		return
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := h.execute(r.Context(), cmd, &stdout, &stderr); err != nil {
		err = stacktrace.Wrap(err)
		if stderr.Len() == 0 {
			_, _ = fmt.Fprintln(&stderr, err)
		}

		exitCode := 1
		if IsUsageError(err) {
			exitCode = 2
		}
		writeProxyResponse(w, http.StatusOK, proxyResponseEnvelope{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: exitCode,
		})
		return
	}

	writeProxyResponse(w, http.StatusOK, proxyResponseEnvelope{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	})
}

func writeProxyResponse(w http.ResponseWriter, statusCode int, resp proxyResponseEnvelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

type ProxyServer struct {
	server  *http.Server
	errChan chan error
}

func StartProxyServer(listenAddr string) (*ProxyServer, error) {
	if err := validateProxyListenAddr(listenAddr); err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           NewProxyHandler(),
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       30 * time.Second,
	}

	proxyServer := &ProxyServer{
		server:  server,
		errChan: make(chan error, 1),
	}

	go func() {
		err := server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			proxyServer.errChan <- err
		}
		close(proxyServer.errChan)
	}()

	return proxyServer, nil
}

func ServeProxy(ctx context.Context, listenAddr string, stdout io.Writer) error {
	server, err := StartProxyServer(listenAddr)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	if _, err := fmt.Fprintf(stdout, "pending-review proxy listening on http://%s%s\n", server.Address(), proxyEndpointPath); err != nil {
		return stacktrace.Wrap(err)
	}

	<-ctx.Done()
	return stacktrace.Wrap(server.Stop(context.Background()))
}

func (s *ProxyServer) Address() string {
	return s.server.Addr
}

func (s *ProxyServer) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return errors.Join(
		s.server.Shutdown(shutdownCtx),
		<-s.errChan,
	)
}

func validateProxyListenAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return stacktrace.Errorf("proxy listen address must bind to loopback, got %q", addr)
	}
	return nil
}

func commandRepo(cmd command) (string, bool) {
	switch cmd := cmd.(type) {
	case createCommand:
		return cmd.Repo, true
	case deleteCommand:
		return cmd.Repo, true
	case getCommand:
		return cmd.Repo, true
	case getStateCommand:
		return cmd.Repo, true
	case updateBodyCommand:
		return cmd.Repo, true
	case deleteStateCommand:
		return cmd.Repo, true
	case replaceCommentsCommand:
		return cmd.Repo, true
	case upsertCommentCommand:
		return cmd.Repo, true
	default:
		return "", false
	}
}

func buildProxyRequest(cmd command) (proxyRequestEnvelope, error) {
	payload, err := buildProxyPayload(cmd)
	if err != nil {
		return proxyRequestEnvelope{}, err
	}
	return proxyRequestEnvelope{
		Version: proxyAPIVersion,
		Command: commandName(cmd),
		Payload: payload,
	}, nil
}

func buildProxyPayload(cmd command) (json.RawMessage, error) {
	switch cmd := cmd.(type) {
	case versionCommand:
		return mustMarshalProxyPayload(struct{}{})
	case createCommand:
		body, err := resolveBodyInput(cmd.Body, cmd.BodyFile)
		if err != nil {
			return nil, err
		}
		return mustMarshalProxyPayload(proxyCreatePayload{
			Repo:     cmd.Repo,
			PRNumber: cmd.PRNumber,
			Body:     body,
			JSON:     cmd.JSON,
		})
	case deleteCommand:
		return mustMarshalProxyPayload(proxyDeletePayload{
			Repo:         cmd.Repo,
			PRNumber:     cmd.PRNumber,
			EnsureAbsent: cmd.EnsureAbsent,
			JSON:         cmd.JSON,
		})
	case getCommand:
		return mustMarshalProxyPayload(proxyGetPayload{
			Repo:     cmd.Repo,
			PRNumber: cmd.PRNumber,
			Pretty:   cmd.Pretty,
		})
	case getStateCommand:
		return mustMarshalProxyPayload(proxyGetStatePayload{
			Repo:      cmd.Repo,
			PRNumber:  cmd.PRNumber,
			UserLogin: cmd.UserLogin,
			Pretty:    cmd.Pretty,
		})
	case updateBodyCommand:
		body, err := resolveBodyInput(cmd.Body, cmd.BodyFile)
		if err != nil {
			return nil, err
		}
		return mustMarshalProxyPayload(proxyUpdateBodyPayload{
			Repo:     cmd.Repo,
			PRNumber: cmd.PRNumber,
			Body:     body,
			Force:    cmd.Force,
			JSON:     cmd.JSON,
		})
	case deleteStateCommand:
		return mustMarshalProxyPayload(proxyDeleteStatePayload{
			Repo:      cmd.Repo,
			PRNumber:  cmd.PRNumber,
			UserLogin: cmd.UserLogin,
			JSON:      cmd.JSON,
		})
	case replaceCommentsCommand:
		comments := cmd.Comments
		if cmd.CommentsFile != "" {
			var err error
			comments, err = loadCommentsFile(cmd.CommentsFile)
			if err != nil {
				return nil, formatCommentsPathError(cmd.CommentsFile, err)
			}
		}
		reviewBody, err := resolveOptionalBodyInput(cmd.ReviewBody, cmd.ReviewBodyFile, "")
		if err != nil {
			return nil, err
		}
		return mustMarshalProxyPayload(proxyReplaceCommentsPayload{
			Repo:            cmd.Repo,
			PRNumber:        cmd.PRNumber,
			Comments:        comments,
			Force:           cmd.Force,
			CreateIfMissing: cmd.CreateIfMissing,
			ReviewBody:      reviewBody,
			JSON:            cmd.JSON,
		})
	case upsertCommentCommand:
		body, err := resolveBodyInput(cmd.Body, cmd.BodyFile)
		if err != nil {
			return nil, err
		}
		reviewBody, err := resolveOptionalBodyInput(cmd.ReviewBody, cmd.ReviewBodyFile, "")
		if err != nil {
			return nil, err
		}
		return mustMarshalProxyPayload(proxyUpsertCommentPayload{
			Repo:            cmd.Repo,
			PRNumber:        cmd.PRNumber,
			CommentID:       cmd.CommentID,
			Path:            cmd.Path,
			Line:            cmd.Line,
			Side:            cmd.Side,
			StartLine:       cmd.StartLine,
			StartSide:       cmd.StartSide,
			Body:            body,
			Force:           cmd.Force,
			CreateIfMissing: cmd.CreateIfMissing,
			ReviewBody:      reviewBody,
			JSON:            cmd.JSON,
		})
	default:
		return nil, stacktrace.Errorf("unsupported proxy command type %T", cmd)
	}
}

func decodeProxyVersionCommand(payload json.RawMessage) (command, error) {
	var decoded struct{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return versionCommand{}, nil
}

func decodeProxyCreateCommand(payload json.RawMessage) (command, error) {
	var decoded proxyCreatePayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return createCommand{
		Repo:      decoded.Repo,
		PRNumber:  decoded.PRNumber,
		Body:      decoded.Body,
		ConfigDir: defaultConfigDir(),
		StateDir:  defaultStateDir(),
		JSON:      decoded.JSON,
	}, nil
}

func decodeProxyDeleteCommand(payload json.RawMessage) (command, error) {
	var decoded proxyDeletePayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return deleteCommand{
		Repo:         decoded.Repo,
		PRNumber:     decoded.PRNumber,
		ConfigDir:    defaultConfigDir(),
		StateDir:     defaultStateDir(),
		EnsureAbsent: decoded.EnsureAbsent,
		JSON:         decoded.JSON,
	}, nil
}

func decodeProxyGetCommand(payload json.RawMessage) (command, error) {
	var decoded proxyGetPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return getCommand{
		Repo:      decoded.Repo,
		PRNumber:  decoded.PRNumber,
		ConfigDir: defaultConfigDir(),
		StateDir:  defaultStateDir(),
		Pretty:    decoded.Pretty,
	}, nil
}

func decodeProxyGetStateCommand(payload json.RawMessage) (command, error) {
	var decoded proxyGetStatePayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return getStateCommand{
		Repo:      decoded.Repo,
		PRNumber:  decoded.PRNumber,
		UserLogin: decoded.UserLogin,
		StateDir:  defaultStateDir(),
		Pretty:    decoded.Pretty,
	}, nil
}

func decodeProxyUpdateBodyCommand(payload json.RawMessage) (command, error) {
	var decoded proxyUpdateBodyPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return updateBodyCommand{
		Repo:      decoded.Repo,
		PRNumber:  decoded.PRNumber,
		Body:      decoded.Body,
		ConfigDir: defaultConfigDir(),
		StateDir:  defaultStateDir(),
		Force:     decoded.Force,
		JSON:      decoded.JSON,
	}, nil
}

func decodeProxyDeleteStateCommand(payload json.RawMessage) (command, error) {
	var decoded proxyDeleteStatePayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return deleteStateCommand{
		Repo:      decoded.Repo,
		PRNumber:  decoded.PRNumber,
		UserLogin: decoded.UserLogin,
		StateDir:  defaultStateDir(),
		JSON:      decoded.JSON,
	}, nil
}

func decodeProxyReplaceCommentsCommand(payload json.RawMessage) (command, error) {
	var decoded proxyReplaceCommentsPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return replaceCommentsCommand{
		Repo:            decoded.Repo,
		PRNumber:        decoded.PRNumber,
		Comments:        decoded.Comments,
		ConfigDir:       defaultConfigDir(),
		StateDir:        defaultStateDir(),
		Force:           decoded.Force,
		CreateIfMissing: decoded.CreateIfMissing,
		ReviewBody:      decoded.ReviewBody,
		JSON:            decoded.JSON,
	}, nil
}

func decodeProxyUpsertCommentCommand(payload json.RawMessage) (command, error) {
	var decoded proxyUpsertCommentPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return upsertCommentCommand{
		Repo:            decoded.Repo,
		PRNumber:        decoded.PRNumber,
		CommentID:       decoded.CommentID,
		Path:            decoded.Path,
		Line:            decoded.Line,
		Side:            decoded.Side,
		StartLine:       decoded.StartLine,
		StartSide:       decoded.StartSide,
		Body:            decoded.Body,
		ConfigDir:       defaultConfigDir(),
		StateDir:        defaultStateDir(),
		Force:           decoded.Force,
		CreateIfMissing: decoded.CreateIfMissing,
		ReviewBody:      decoded.ReviewBody,
		JSON:            decoded.JSON,
	}, nil
}

func mustMarshalProxyPayload(payload any) (json.RawMessage, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return json.RawMessage(encoded), nil
}
