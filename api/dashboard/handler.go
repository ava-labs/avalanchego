// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dashboard

import (
	"bytes"
	"html/template"
	"net/http"
	"sync"

	_ "embed"
)

//go:embed static/index.html
var indexHTML string

//go:embed static/styles.css
var stylesCSS string

//go:embed static/app.js
var appJS string

//go:embed static/logo.svg
var logoSVG string

// dashboardData holds the template data for the dashboard.
type dashboardData struct {
	Styles template.CSS
	Script template.JS
	Logo   template.HTML
}

var (
	// compiledHTML is the pre-rendered HTML with CSS and JS injected.
	compiledHTML []byte
	compileOnce  sync.Once
	compileErr   error
)

// compileTemplate combines the HTML template with CSS and JS.
func compileTemplate() ([]byte, error) {
	tmpl, err := template.New("dashboard").Parse(indexHTML)
	if err != nil {
		return nil, err
	}

	// These conversions are safe because the content is embedded at compile time
	// from our own source files, not from user input.
	//nolint:gosec // G203: Content is from embedded static files, not user input
	data := dashboardData{
		Styles: template.CSS(stylesCSS),
		Script: template.JS(appJS),
		Logo:   template.HTML(logoSVG),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// getCompiledHTML returns the pre-compiled HTML, compiling it once on first call.
func getCompiledHTML() ([]byte, error) {
	compileOnce.Do(func() {
		compiledHTML, compileErr = compileTemplate()
	})
	return compiledHTML, compileErr
}

// NewHandler returns an HTTP handler that serves the node dashboard.
func NewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only serve dashboard on GET requests to root
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		html, err := getCompiledHTML()
		if err != nil {
			http.Error(w, "Failed to render dashboard", http.StatusInternalServerError)
			return
		}

		// Set security headers
		// CSP: Allow inline styles/scripts (required for current implementation),
		// restrict everything else to same-origin
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		// Prevent clickjacking by disallowing iframe embedding
		w.Header().Set("X-Frame-Options", "DENY")
		// Prevent MIME-type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Referrer policy for privacy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(html)
	})
}
