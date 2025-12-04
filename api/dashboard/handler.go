// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dashboard

import (
	"net/http"
)

// NewHandler returns an HTTP handler that serves the node dashboard.
func NewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only serve dashboard on GET requests to root
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(dashboardHTML))
	})
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AvalancheGo Dashboard</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=Sora:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            /* Dark theme (default) */
            --bg-primary: #09090b;
            --bg-secondary: #0f0f12;
            --bg-card: #141418;
            --bg-card-hover: #1a1a1f;
            --bg-elevated: #1c1c22;
            --border-color: #27272a;
            --border-hover: #3f3f46;
            --text-primary: #fafafa;
            --text-secondary: #a1a1aa;
            --text-muted: #71717a;
            
            /* Avalanche brand colors */
            --avax-red: #e84142;
            --avax-red-light: #ff5a5b;
            --avax-red-dark: #c73536;
            --avax-red-glow: rgba(232, 65, 66, 0.2);
            
            /* Semantic colors */
            --color-success: #22c55e;
            --color-success-glow: rgba(34, 197, 94, 0.15);
            --color-warning: #eab308;
            --color-warning-glow: rgba(234, 179, 8, 0.15);
            --color-error: #ef4444;
            --color-error-glow: rgba(239, 68, 68, 0.15);
            
            --shadow-sm: 0 1px 2px rgba(0,0,0,0.4);
            --shadow-md: 0 4px 12px rgba(0,0,0,0.5);
            --gradient-brand: linear-gradient(135deg, #e84142 0%, #ff6b6b 100%);
        }

        [data-theme="light"] {
            --bg-primary: #fafafa;
            --bg-secondary: #ffffff;
            --bg-card: #ffffff;
            --bg-card-hover: #f4f4f5;
            --bg-elevated: #f4f4f5;
            --border-color: #e4e4e7;
            --border-hover: #d4d4d8;
            --text-primary: #09090b;
            --text-secondary: #52525b;
            --text-muted: #a1a1aa;
            --avax-red-glow: rgba(232, 65, 66, 0.1);
            --color-success-glow: rgba(34, 197, 94, 0.1);
            --color-warning-glow: rgba(234, 179, 8, 0.1);
            --color-error-glow: rgba(239, 68, 68, 0.1);
            --shadow-sm: 0 1px 2px rgba(0,0,0,0.05);
            --shadow-md: 0 4px 12px rgba(0,0,0,0.08);
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }

        body {
            font-family: 'Sora', -apple-system, sans-serif;
            background: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
            line-height: 1.6;
            transition: background 0.2s, color 0.2s;
        }

        .container {
            max-width: 1800px;
            margin: 0 auto;
            padding: 1.25rem 1.5rem;
        }

        /* Header */
        header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 1.5rem;
            padding: 0.875rem 1.25rem;
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 12px;
        }

        .logo-section {
            display: flex;
            align-items: center;
            gap: 0.875rem;
        }

        .logo {
            width: 44px;
            height: 44px;
            background: var(--gradient-brand);
            border-radius: 10px;
            display: flex;
            align-items: center;
            justify-content: center;
            box-shadow: 0 0 20px var(--avax-red-glow);
        }

        .logo svg {
            width: 24px;
            height: 24px;
            color: white;
        }

        .header-info h1 {
            font-size: 1.25rem;
            font-weight: 700;
            letter-spacing: -0.02em;
        }

        .header-info .subtitle {
            font-size: 0.7rem;
            color: var(--text-muted);
            font-family: 'IBM Plex Mono', monospace;
        }

        .header-controls {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .status-badge {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            padding: 0.5rem 0.875rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            font-size: 0.8rem;
            font-weight: 500;
        }

        .status-dot {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            animation: pulse 2s ease-in-out infinite;
        }

        .status-dot.healthy { background: var(--color-success); box-shadow: 0 0 8px var(--color-success-glow); }
        .status-dot.unhealthy { background: var(--color-error); box-shadow: 0 0 8px var(--color-error-glow); }
        .status-dot.loading { background: var(--color-warning); }

        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        .theme-toggle {
            display: flex;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            overflow: hidden;
        }

        .theme-toggle button {
            width: 34px;
            height: 34px;
            border: none;
            background: transparent;
            cursor: pointer;
            display: flex;
            align-items: center;
            justify-content: center;
            color: var(--text-muted);
            transition: all 0.15s;
        }

        .theme-toggle button.active {
            background: var(--bg-card);
            color: var(--text-primary);
        }

        .theme-toggle button svg { width: 16px; height: 16px; }

        .refresh-all-btn {
            display: flex;
            align-items: center;
            gap: 0.375rem;
            padding: 0.5rem 1rem;
            background: var(--avax-red);
            border: none;
            border-radius: 8px;
            color: white;
            font-family: 'Sora', sans-serif;
            font-size: 0.8rem;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.15s;
        }

        .refresh-all-btn:hover { background: var(--avax-red-light); }
        .refresh-all-btn svg { width: 14px; height: 14px; }
        .refresh-all-btn.spinning svg { animation: spin 1s linear infinite; }

        @keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

        /* Tabs */
        .tabs {
            display: flex;
            gap: 0.25rem;
            margin-bottom: 1.25rem;
            padding: 0.25rem;
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 10px;
            overflow-x: auto;
        }

        .tab {
            padding: 0.625rem 1rem;
            background: transparent;
            border: none;
            border-radius: 6px;
            color: var(--text-muted);
            font-family: 'Sora', sans-serif;
            font-size: 0.8rem;
            font-weight: 500;
            cursor: pointer;
            white-space: nowrap;
            transition: all 0.15s;
        }

        .tab:hover { color: var(--text-secondary); background: var(--bg-elevated); }
        .tab.active { background: var(--avax-red); color: white; }

        .tab-content { display: none; }
        .tab-content.active { display: block; }

        /* Grid */
        .grid {
            display: grid;
            grid-template-columns: repeat(12, 1fr);
            gap: 1rem;
        }

        .span-2 { grid-column: span 2; }
        .span-3 { grid-column: span 3; }
        .span-4 { grid-column: span 4; }
        .span-6 { grid-column: span 6; }
        .span-8 { grid-column: span 8; }
        .span-12 { grid-column: span 12; }

        @media (max-width: 1400px) {
            .span-2, .span-3 { grid-column: span 4; }
            .span-4 { grid-column: span 6; }
        }
        @media (max-width: 900px) {
            .span-2, .span-3, .span-4, .span-6 { grid-column: span 12; }
            header { flex-direction: column; gap: 0.75rem; }
            .header-controls { width: 100%; justify-content: center; flex-wrap: wrap; }
        }

        /* Cards */
        .card {
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 10px;
            overflow: hidden;
        }

        .card-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0.75rem 1rem;
            border-bottom: 1px solid var(--border-color);
            background: var(--bg-secondary);
        }

        .card-title {
            font-size: 0.75rem;
            font-weight: 600;
            color: var(--text-secondary);
            text-transform: uppercase;
            letter-spacing: 0.03em;
        }

        .section-refresh-btn {
            width: 26px;
            height: 26px;
            border: 1px solid var(--border-color);
            background: var(--bg-card);
            border-radius: 6px;
            cursor: pointer;
            display: flex;
            align-items: center;
            justify-content: center;
            color: var(--text-muted);
            transition: all 0.15s;
        }

        .section-refresh-btn:hover { border-color: var(--border-hover); color: var(--text-primary); }
        .section-refresh-btn svg { width: 12px; height: 12px; }
        .section-refresh-btn.spinning svg { animation: spin 1s linear infinite; }

        .card-body { padding: 1rem; }
        .card-body.compact { padding: 0.75rem; }

        /* Stats Grid */
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
            gap: 0.75rem;
            margin-bottom: 1.25rem;
        }

        .stat-item {
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            border-radius: 10px;
            padding: 1rem;
            text-align: center;
        }

        .stat-value {
            font-size: 1.5rem;
            font-weight: 700;
            color: var(--avax-red);
            letter-spacing: -0.02em;
        }

        .stat-label {
            font-size: 0.7rem;
            color: var(--text-muted);
            margin-top: 0.125rem;
        }

        /* Info Rows */
        .info-list { display: flex; flex-direction: column; }

        .info-row {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 0.5rem 0;
            border-bottom: 1px solid var(--border-color);
            gap: 1rem;
        }

        .info-row:last-child { border-bottom: none; }

        .info-label {
            font-size: 0.75rem;
            color: var(--text-muted);
            flex-shrink: 0;
        }

        .info-value {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.75rem;
            color: var(--text-primary);
            text-align: right;
            word-break: break-all;
        }

        .info-value.success { color: var(--color-success); }
        .info-value.warning { color: var(--color-warning); }
        .info-value.error { color: var(--color-error); }

        /* Mono Box */
        .mono-box {
            margin-top: 0.75rem;
            padding: 0.625rem 0.75rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.65rem;
            color: var(--text-secondary);
            word-break: break-all;
        }

        .mono-box .label {
            display: block;
            font-size: 0.6rem;
            color: var(--text-muted);
            margin-bottom: 0.25rem;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            font-family: 'Sora', sans-serif;
            font-weight: 600;
        }

        /* Health Items */
        .health-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
            gap: 0.5rem;
        }

        .health-item {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0.625rem 0.75rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 6px;
        }

        .health-name { font-size: 0.75rem; font-weight: 500; }

        .health-badge {
            font-size: 0.65rem;
            font-weight: 600;
            padding: 0.125rem 0.5rem;
            border-radius: 4px;
        }

        .health-badge.pass { background: var(--color-success-glow); color: var(--color-success); }
        .health-badge.fail { background: var(--color-error-glow); color: var(--color-error); }

        /* Chain Cards */
        .chain-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
            gap: 0.75rem;
        }

        .chain-item {
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 1rem;
        }

        .chain-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 0.5rem;
        }

        .chain-name { font-weight: 600; font-size: 0.9rem; }
        .chain-badge {
            font-size: 0.6rem;
            font-weight: 600;
            padding: 0.125rem 0.375rem;
            border-radius: 4px;
            background: var(--bg-card);
            border: 1px solid var(--border-color);
            color: var(--text-muted);
        }

        .chain-status {
            font-size: 0.65rem;
            font-weight: 600;
            padding: 0.125rem 0.375rem;
            border-radius: 4px;
        }

        .chain-status.synced { background: var(--color-success-glow); color: var(--color-success); }
        .chain-status.syncing { background: var(--color-warning-glow); color: var(--color-warning); }

        .chain-desc { font-size: 0.7rem; color: var(--text-muted); margin-top: 0.375rem; }

        /* Peer List */
        .peer-list {
            max-height: 300px;
            overflow-y: auto;
            display: flex;
            flex-direction: column;
            gap: 0.375rem;
        }

        .peer-list::-webkit-scrollbar { width: 4px; }
        .peer-list::-webkit-scrollbar-track { background: var(--bg-elevated); }
        .peer-list::-webkit-scrollbar-thumb { background: var(--border-color); border-radius: 2px; }

        .peer-item {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0.5rem 0.625rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 6px;
        }

        .peer-id {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.65rem;
            color: var(--text-secondary);
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            max-width: 200px;
        }

        .peer-meta {
            font-size: 0.6rem;
            color: var(--text-muted);
        }

        .peer-version {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.6rem;
            color: var(--text-muted);
            background: var(--bg-card);
            padding: 0.125rem 0.375rem;
            border-radius: 3px;
        }

        /* Subnet Cards */
        .subnet-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
            gap: 0.75rem;
        }

        .subnet-item {
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 1rem;
        }

        .subnet-header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            margin-bottom: 0.625rem;
        }

        .subnet-name { font-weight: 600; font-size: 0.85rem; }

        .subnet-id {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.6rem;
            color: var(--text-muted);
            background: var(--bg-card);
            padding: 0.375rem 0.5rem;
            border-radius: 4px;
            margin-bottom: 0.625rem;
            word-break: break-all;
        }

        .subnet-chains {
            display: flex;
            flex-direction: column;
            gap: 0.375rem;
        }

        .subnet-chain {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0.375rem 0.5rem;
            background: var(--bg-card);
            border-radius: 4px;
            font-size: 0.7rem;
        }

        /* VM Grid */
        .vm-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
            gap: 0.5rem;
        }

        .vm-item {
            display: flex;
            flex-direction: column;
            padding: 0.75rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 6px;
        }

        .vm-name { font-weight: 600; font-size: 0.8rem; }
        .vm-id {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.6rem;
            color: var(--text-muted);
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }

        /* API Endpoint List */
        .endpoint-list {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
            gap: 0.5rem;
        }

        .endpoint-item {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            padding: 0.5rem 0.625rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            font-size: 0.7rem;
        }

        .endpoint-method {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.6rem;
            font-weight: 600;
            padding: 0.125rem 0.375rem;
            border-radius: 3px;
            background: var(--avax-red-glow);
            color: var(--avax-red);
        }

        .endpoint-path {
            font-family: 'IBM Plex Mono', monospace;
            color: var(--text-secondary);
        }

        /* Uptime */
        .uptime-display {
            text-align: center;
            padding: 1rem 0;
        }

        .uptime-value {
            font-size: 2.5rem;
            font-weight: 700;
            color: var(--avax-red);
            letter-spacing: -0.03em;
        }

        .uptime-label { font-size: 0.75rem; color: var(--text-muted); }

        .uptime-bar {
            margin-top: 1rem;
            height: 6px;
            background: var(--bg-elevated);
            border-radius: 3px;
            overflow: hidden;
        }

        .uptime-fill {
            height: 100%;
            background: var(--avax-red);
            border-radius: 3px;
            transition: width 0.3s;
        }

        /* Loading & Error */
        .loading { 
            background: linear-gradient(90deg, var(--bg-elevated) 25%, var(--bg-card-hover) 50%, var(--bg-elevated) 75%);
            background-size: 200% 100%;
            animation: shimmer 1.5s infinite;
            border-radius: 4px;
        }

        @keyframes shimmer {
            0% { background-position: 200% 0; }
            100% { background-position: -200% 0; }
        }

        .empty-state {
            text-align: center;
            padding: 2rem;
            color: var(--text-muted);
            font-size: 0.8rem;
        }

        .error-state {
            text-align: center;
            padding: 1.5rem;
            color: var(--color-error);
            font-size: 0.8rem;
        }

        /* Scrollable Table */
        .table-container {
            overflow-x: auto;
        }

        .data-table {
            width: 100%;
            border-collapse: collapse;
            font-size: 0.75rem;
        }

        .data-table th {
            text-align: left;
            padding: 0.5rem 0.625rem;
            background: var(--bg-secondary);
            border-bottom: 1px solid var(--border-color);
            font-weight: 600;
            color: var(--text-muted);
            font-size: 0.65rem;
            text-transform: uppercase;
            letter-spacing: 0.03em;
        }

        .data-table td {
            padding: 0.5rem 0.625rem;
            border-bottom: 1px solid var(--border-color);
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.7rem;
        }

        .data-table tr:last-child td { border-bottom: none; }

        /* Footer */
        footer {
            margin-top: 1.5rem;
            padding: 1rem;
            text-align: center;
            color: var(--text-muted);
            font-size: 0.7rem;
            border-top: 1px solid var(--border-color);
        }

        footer a { color: var(--avax-red); text-decoration: none; }
        footer a:hover { text-decoration: underline; }

        /* API Explorer */
        .api-method-list {
            display: flex;
            flex-wrap: wrap;
            gap: 0.375rem;
        }

        .api-btn {
            padding: 0.375rem 0.625rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 4px;
            color: var(--text-secondary);
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.65rem;
            cursor: pointer;
            transition: all 0.15s;
        }

        .api-btn:hover {
            border-color: var(--avax-red);
            color: var(--avax-red);
            background: var(--avax-red-glow);
        }

        .api-code-block {
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            padding: 0.75rem;
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.7rem;
            color: var(--text-secondary);
            overflow: auto;
            white-space: pre-wrap;
            word-break: break-all;
            margin: 0;
            min-height: 60px;
        }

        .api-input {
            width: 100%;
            padding: 0.5rem 0.625rem;
            background: var(--bg-elevated);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            color: var(--text-primary);
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.75rem;
        }

        .api-input:focus {
            outline: none;
            border-color: var(--avax-red);
        }

        .api-send-btn {
            width: 100%;
            padding: 0.5rem 1rem;
            background: var(--avax-red);
            border: none;
            border-radius: 6px;
            color: white;
            font-family: 'Sora', sans-serif;
            font-size: 0.75rem;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.15s;
        }

        .api-send-btn:hover {
            background: var(--avax-red-light);
        }

        #apiStatus {
            font-size: 0.6rem;
            padding: 0.125rem 0.375rem;
            border-radius: 3px;
            margin-left: 0.5rem;
        }

        #apiStatus.success {
            background: var(--color-success-glow);
            color: var(--color-success);
        }

        #apiStatus.error {
            background: var(--color-error-glow);
            color: var(--color-error);
        }
    </style>
</head>
<body data-theme="dark">
    <div class="container">
        <header>
            <div class="logo-section">
                <div class="logo">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                        <polygon points="12,2 22,20 2,20"/>
                    </svg>
                </div>
                <div class="header-info">
                    <h1>AvalancheGo</h1>
                    <div class="subtitle" id="lastUpdated">Connecting...</div>
                </div>
            </div>
            <div class="header-controls">
                <div class="status-badge">
                    <div class="status-dot loading" id="statusDot"></div>
                    <span id="statusText">Connecting</span>
                </div>
                <div class="theme-toggle">
                    <button id="lightBtn" onclick="setTheme('light')">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <circle cx="12" cy="12" r="5"/><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>
                        </svg>
                    </button>
                    <button id="darkBtn" class="active" onclick="setTheme('dark')">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
                        </svg>
                    </button>
                </div>
                <button class="refresh-all-btn" id="refreshAllBtn" onclick="refreshAll()">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/>
                    </svg>
                    Refresh
                </button>
            </div>
        </header>

        <!-- Stats -->
        <div class="stats-grid">
            <div class="stat-item">
                <div class="stat-value" id="statPeers">—</div>
                <div class="stat-label">Peers</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="statChains">—</div>
                <div class="stat-label">Chains Synced</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="statSubnets">—</div>
                <div class="stat-label">Subnets</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="statVMs">—</div>
                <div class="stat-label">VMs</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="statHealth">—</div>
                <div class="stat-label">Health Checks</div>
            </div>
            <div class="stat-item">
                <div class="stat-value" id="statUptime">—</div>
                <div class="stat-label">Uptime</div>
            </div>
        </div>

        <!-- Tabs -->
        <div class="tabs">
            <button class="tab active" onclick="showTab('overview')">Overview</button>
            <button class="tab" onclick="showTab('network')">Network</button>
            <button class="tab" onclick="showTab('chains')">Chains & Subnets</button>
            <button class="tab" onclick="showTab('health')">Health</button>
            <button class="tab" onclick="showTab('apis')">APIs</button>
            <button class="tab" onclick="showTab('advanced')">Advanced</button>
        </div>

        <!-- Overview Tab -->
        <div id="tab-overview" class="tab-content active">
            <div class="grid">
                <div class="card span-4">
                    <div class="card-header">
                        <span class="card-title">Node Info</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchNodeInfo)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="nodeInfoCard">
                        <div class="loading" style="height: 150px;"></div>
                    </div>
                </div>

                <div class="card span-4">
                    <div class="card-header">
                        <span class="card-title">Network</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchNetworkInfo)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="networkInfoCard">
                        <div class="loading" style="height: 150px;"></div>
                    </div>
                </div>

                <div class="card span-4">
                    <div class="card-header">
                        <span class="card-title">Staking & Uptime</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchUptime)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="uptimeCard">
                        <div class="loading" style="height: 150px;"></div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Primary Chains</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchChains)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="chainsCard">
                        <div class="loading" style="height: 100px;"></div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Connected Peers</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchPeers)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body compact" id="peersCard">
                        <div class="loading" style="height: 200px;"></div>
                    </div>
                </div>

                <div class="card span-12">
                    <div class="card-header">
                        <span class="card-title">Health Checks</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchHealth)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body compact" id="healthCard">
                        <div class="loading" style="height: 80px;"></div>
                    </div>
                </div>
            </div>
        </div>

        <!-- Network Tab -->
        <div id="tab-network" class="tab-content">
            <div class="grid">
                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Peer Details</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchPeersDetailed)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body compact">
                        <div class="table-container" id="peersTableCard">
                            <div class="loading" style="height: 300px;"></div>
                        </div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Network Statistics</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchNetworkStats)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="networkStatsCard">
                        <div class="loading" style="height: 200px;"></div>
                    </div>
                </div>

                <div class="card span-12">
                    <div class="card-header">
                        <span class="card-title">ACP Voting Status</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchACPs)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="acpsCard">
                        <div class="loading" style="height: 100px;"></div>
                    </div>
                </div>
            </div>
        </div>

        <!-- Chains & Subnets Tab -->
        <div id="tab-chains" class="tab-content">
            <div class="grid">
                <div class="card span-12">
                    <div class="card-header">
                        <span class="card-title">Subnets & Blockchains</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchSubnets)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="subnetsCard">
                        <div class="loading" style="height: 200px;"></div>
                    </div>
                </div>

                <div class="card span-12">
                    <div class="card-header">
                        <span class="card-title">Virtual Machines</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchVMs)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="vmsCard">
                        <div class="loading" style="height: 100px;"></div>
                    </div>
                </div>
            </div>
        </div>

        <!-- Health Tab -->
        <div id="tab-health" class="tab-content">
            <div class="grid">
                <div class="card span-12">
                    <div class="card-header">
                        <span class="card-title">Health Check Details</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchHealthDetailed)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="healthDetailCard">
                        <div class="loading" style="height: 300px;"></div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Liveness</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchLiveness)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="livenessCard">
                        <div class="loading" style="height: 100px;"></div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Readiness</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchReadiness)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="readinessCard">
                        <div class="loading" style="height: 100px;"></div>
                    </div>
                </div>
            </div>
        </div>

        <!-- APIs Tab -->
        <div id="tab-apis" class="tab-content">
            <div class="grid">
                <div class="card span-4">
                    <div class="card-header">
                        <span class="card-title">API Explorer</span>
                    </div>
                    <div class="card-body compact" style="max-height: 600px; overflow-y: auto;">
                        <div style="margin-bottom: 0.75rem;">
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem;">Info API — /ext/info</div>
                            <div class="api-method-list">
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.getNodeVersion')">info.getNodeVersion</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.getNodeID')">info.getNodeID</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.getNodeIP')">info.getNodeIP</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.getNetworkID')">info.getNetworkID</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.getNetworkName')">info.getNetworkName</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.peers')">info.peers</button>
                                <button class="api-btn" onclick="callApiWithParams('/ext/info', 'info.isBootstrapped', {chain: 'P'})">info.isBootstrapped (P)</button>
                                <button class="api-btn" onclick="callApiWithParams('/ext/info', 'info.isBootstrapped', {chain: 'X'})">info.isBootstrapped (X)</button>
                                <button class="api-btn" onclick="callApiWithParams('/ext/info', 'info.isBootstrapped', {chain: 'C'})">info.isBootstrapped (C)</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.upgrades')">info.upgrades</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.uptime')">info.uptime</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.acps')">info.acps</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.getVMs')">info.getVMs</button>
                                <button class="api-btn" onclick="callApi('/ext/info', 'info.getTxFee')">info.getTxFee</button>
                            </div>
                        </div>
                        <div style="margin-bottom: 0.75rem;">
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem;">Health API — /ext/health</div>
                            <div class="api-method-list">
                                <button class="api-btn" onclick="callGetApi('/ext/health')">GET /ext/health</button>
                                <button class="api-btn" onclick="callGetApi('/ext/health/liveness')">GET /ext/health/liveness</button>
                                <button class="api-btn" onclick="callGetApi('/ext/health/readiness')">GET /ext/health/readiness</button>
                                <button class="api-btn" onclick="callApi('/ext/health', 'health.health')">health.health</button>
                                <button class="api-btn" onclick="callApi('/ext/health', 'health.readiness')">health.readiness</button>
                                <button class="api-btn" onclick="callApi('/ext/health', 'health.liveness')">health.liveness</button>
                            </div>
                        </div>
                        <div style="margin-bottom: 0.75rem;">
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem;">Admin API — /ext/admin</div>
                            <div class="api-method-list">
                                <button class="api-btn" onclick="callApi('/ext/admin', 'admin.getChainAliases', {chain: 'P'})">admin.getChainAliases (P)</button>
                                <button class="api-btn" onclick="callApi('/ext/admin', 'admin.getLoggerLevel')">admin.getLoggerLevel</button>
                                <button class="api-btn" onclick="callApi('/ext/admin', 'admin.getConfig')">admin.getConfig</button>
                            </div>
                        </div>
                        <div style="margin-bottom: 0.75rem;">
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem;">Platform API (P-Chain) — /ext/P</div>
                            <div class="api-method-list">
                                <button class="api-btn" onclick="callApi('/ext/P', 'platform.getHeight')">platform.getHeight</button>
                                <button class="api-btn" onclick="callApi('/ext/P', 'platform.getBlockchains')">platform.getBlockchains</button>
                                <button class="api-btn" onclick="callApi('/ext/P', 'platform.getSubnets')">platform.getSubnets</button>
                                <button class="api-btn" onclick="callApiWithParams('/ext/P', 'platform.getCurrentValidators', {subnetID: '11111111111111111111111111111111LpoYY'})">platform.getCurrentValidators</button>
                                <button class="api-btn" onclick="callApiWithParams('/ext/P', 'platform.getPendingValidators', {subnetID: '11111111111111111111111111111111LpoYY'})">platform.getPendingValidators</button>
                                <button class="api-btn" onclick="callApi('/ext/P', 'platform.getMinStake')">platform.getMinStake</button>
                                <button class="api-btn" onclick="callApi('/ext/P', 'platform.getStakingAssetID')">platform.getStakingAssetID</button>
                                <button class="api-btn" onclick="callApi('/ext/P', 'platform.getTimestamp')">platform.getTimestamp</button>
                                <button class="api-btn" onclick="callApi('/ext/P', 'platform.getTotalStake')">platform.getTotalStake</button>
                            </div>
                        </div>
                        <div style="margin-bottom: 0.75rem;">
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem;">X-Chain (AVM) — /ext/X</div>
                            <div class="api-method-list">
                                <button class="api-btn" onclick="callApi('/ext/X', 'avm.getHeight')">avm.getHeight</button>
                                <button class="api-btn" onclick="callApiWithParams('/ext/X', 'avm.getAssetDescription', {assetID: 'AVAX'})">avm.getAssetDescription (AVAX)</button>
                            </div>
                        </div>
                        <div style="margin-bottom: 0.75rem;">
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem;">C-Chain (EVM) — /ext/bc/C/rpc</div>
                            <div class="api-method-list">
                                <button class="api-btn" onclick="callEthApi('eth_blockNumber', [])">eth_blockNumber</button>
                                <button class="api-btn" onclick="callEthApi('eth_chainId', [])">eth_chainId</button>
                                <button class="api-btn" onclick="callEthApi('eth_gasPrice', [])">eth_gasPrice</button>
                                <button class="api-btn" onclick="callEthApi('eth_getBlockByNumber', ['latest', false])">eth_getBlockByNumber (latest)</button>
                                <button class="api-btn" onclick="callEthApi('net_version', [])">net_version</button>
                                <button class="api-btn" onclick="callEthApi('web3_clientVersion', [])">web3_clientVersion</button>
                            </div>
                        </div>
                        <div>
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem;">Other</div>
                            <div class="api-method-list">
                                <button class="api-btn" onclick="callGetApi('/ext/metrics')">GET /ext/metrics</button>
                            </div>
                        </div>
                    </div>
                </div>

                <div class="card span-8">
                    <div class="card-header">
                        <span class="card-title">Request / Response</span>
                        <button class="section-refresh-btn" onclick="clearApiResponse()" title="Clear">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg>
                        </button>
                    </div>
                    <div class="card-body compact">
                        <div style="margin-bottom: 0.75rem;">
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.375rem;">Request</div>
                            <pre id="apiRequest" class="api-code-block">Click a method on the left to execute a request</pre>
                        </div>
                        <div>
                            <div style="font-size: 0.65rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.375rem;">Response <span id="apiStatus"></span></div>
                            <pre id="apiResponse" class="api-code-block" style="max-height: 400px;">Response will appear here</pre>
                        </div>
                    </div>
                </div>

                <div class="card span-12">
                    <div class="card-header">
                        <span class="card-title">Custom Request</span>
                    </div>
                    <div class="card-body compact">
                        <div class="grid" style="gap: 0.75rem;">
                            <div class="span-3">
                                <label style="font-size: 0.65rem; color: var(--text-muted); display: block; margin-bottom: 0.25rem;">Endpoint</label>
                                <input type="text" id="customEndpoint" value="/ext/info" class="api-input" placeholder="/ext/info">
                            </div>
                            <div class="span-3">
                                <label style="font-size: 0.65rem; color: var(--text-muted); display: block; margin-bottom: 0.25rem;">Method</label>
                                <input type="text" id="customMethod" value="info.getNodeVersion" class="api-input" placeholder="info.getNodeVersion">
                            </div>
                            <div class="span-4">
                                <label style="font-size: 0.65rem; color: var(--text-muted); display: block; margin-bottom: 0.25rem;">Params (JSON)</label>
                                <input type="text" id="customParams" value="{}" class="api-input" placeholder="{}">
                            </div>
                            <div class="span-2" style="display: flex; align-items: flex-end;">
                                <button class="api-send-btn" onclick="sendCustomRequest()">Send Request</button>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <!-- Advanced Tab -->
        <div id="tab-advanced" class="tab-content">
            <div class="grid">
                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Upgrade Schedule</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchUpgrades)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="upgradesCard">
                        <div class="loading" style="height: 200px;"></div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">Transaction Fees</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchTxFees)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="txFeesCard">
                        <div class="loading" style="height: 200px;"></div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">P-Chain Height</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchPChainHeight)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="pchainHeightCard">
                        <div class="loading" style="height: 100px;"></div>
                    </div>
                </div>

                <div class="card span-6">
                    <div class="card-header">
                        <span class="card-title">C-Chain Info</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchCChainInfo)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body" id="cchainInfoCard">
                        <div class="loading" style="height: 100px;"></div>
                    </div>
                </div>

                <div class="card span-12">
                    <div class="card-header">
                        <span class="card-title">Current Validators (Sample)</span>
                        <button class="section-refresh-btn" onclick="refreshSection(this, fetchValidators)">
                            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 4v6h6M23 20v-6h-6"/><path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/></svg>
                        </button>
                    </div>
                    <div class="card-body compact" id="validatorsCard">
                        <div class="loading" style="height: 200px;"></div>
                    </div>
                </div>
            </div>
        </div>

        <footer>
            AvalancheGo Dashboard &bull; 
            <a href="https://build.avax.network" target="_blank">Docs</a> &bull;
            <a href="https://github.com/ava-labs/avalanchego" target="_blank">GitHub</a>
        </footer>
    </div>

    <script>
        const baseURL = window.location.origin;
        let currentTheme = localStorage.getItem('avago-theme') || 'dark';

        function setTheme(theme) {
            currentTheme = theme;
            document.body.setAttribute('data-theme', theme);
            localStorage.setItem('avago-theme', theme);
            document.getElementById('lightBtn').classList.toggle('active', theme === 'light');
            document.getElementById('darkBtn').classList.toggle('active', theme === 'dark');
        }
        setTheme(currentTheme);

        const tabLoaded = {};
        function showTab(tabId) {
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            event.target.classList.add('active');
            document.getElementById('tab-' + tabId).classList.add('active');
            
            // Load tab data on first view
            if (!tabLoaded[tabId]) {
                tabLoaded[tabId] = true;
                if (tabId === 'network') {
                    fetchPeersDetailed();
                    fetchNetworkStats();
                    fetchACPs();
                } else if (tabId === 'chains') {
                    fetchSubnets();
                    fetchVMs();
                } else if (tabId === 'health') {
                    fetchHealthDetailed();
                    fetchLiveness();
                    fetchReadiness();
                } else if (tabId === 'advanced') {
                    fetchUpgrades();
                    fetchTxFees();
                    fetchPChainHeight();
                    fetchCChainInfo();
                    fetchValidators();
                }
            }
        }

        async function fetchJSON(endpoint, method = 'POST', body = null) {
            try {
                const opts = { method, headers: { 'Content-Type': 'application/json' } };
                if (body) opts.body = JSON.stringify(body);
                const res = await fetch(baseURL + endpoint, opts);
                return await res.json();
            } catch (e) { console.error(e); return null; }
        }

        async function rpcCall(endpoint, method, params = {}) {
            return fetchJSON(endpoint, 'POST', { jsonrpc: '2.0', id: 1, method, params });
        }

        async function refreshSection(btn, fn) {
            btn.classList.add('spinning');
            await fn();
            btn.classList.remove('spinning');
        }

        function nanoToAvax(n) { return (parseInt(n) / 1e9).toFixed(4); }

        // API Explorer Functions
        async function callApi(endpoint, method, params = {}) {
            const reqEl = document.getElementById('apiRequest');
            const resEl = document.getElementById('apiResponse');
            const statusEl = document.getElementById('apiStatus');
            
            const requestBody = { jsonrpc: '2.0', id: 1, method: method, params: params };
            reqEl.textContent = 'POST ' + endpoint + '\n\n' + JSON.stringify(requestBody, null, 2);
            resEl.textContent = 'Loading...';
            statusEl.textContent = '';
            statusEl.className = '';
            
            try {
                const start = performance.now();
                const res = await fetch(baseURL + endpoint, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(requestBody)
                });
                const elapsed = (performance.now() - start).toFixed(0);
                const text = await res.text();
                let output;
                try {
                    output = JSON.stringify(JSON.parse(text), null, 2);
                } catch {
                    output = text;
                }
                resEl.textContent = output;
                statusEl.textContent = res.status + (res.ok ? ' OK' : ' Error') + ' — ' + elapsed + 'ms';
                statusEl.className = res.ok ? 'success' : 'error';
            } catch (e) {
                resEl.textContent = 'Error: ' + e.message;
                statusEl.textContent = 'Failed';
                statusEl.className = 'error';
            }
        }

        function callApiWithParams(endpoint, method, params) {
            callApi(endpoint, method, params);
        }

        async function callGetApi(endpoint) {
            const reqEl = document.getElementById('apiRequest');
            const resEl = document.getElementById('apiResponse');
            const statusEl = document.getElementById('apiStatus');
            
            reqEl.textContent = 'GET ' + endpoint;
            resEl.textContent = 'Loading...';
            statusEl.textContent = '';
            statusEl.className = '';
            
            try {
                const start = performance.now();
                const res = await fetch(baseURL + endpoint);
                const elapsed = (performance.now() - start).toFixed(0);
                const contentType = res.headers.get('content-type') || '';
                let data;
                if (contentType.includes('application/json')) {
                    data = JSON.stringify(await res.json(), null, 2);
                } else {
                    const text = await res.text();
                    data = text.length > 5000 ? text.substring(0, 5000) + '\n\n... (truncated)' : text;
                }
                resEl.textContent = data;
                statusEl.textContent = res.status + ' OK — ' + elapsed + 'ms';
                statusEl.className = res.ok ? 'success' : 'error';
            } catch (e) {
                resEl.textContent = 'Error: ' + e.message;
                statusEl.textContent = 'Failed';
                statusEl.className = 'error';
            }
        }

        async function callEthApi(method, params) {
            const endpoint = '/ext/bc/C/rpc';
            const reqEl = document.getElementById('apiRequest');
            const resEl = document.getElementById('apiResponse');
            const statusEl = document.getElementById('apiStatus');
            
            const requestBody = { jsonrpc: '2.0', id: 1, method: method, params: params };
            reqEl.textContent = 'POST ' + endpoint + '\n\n' + JSON.stringify(requestBody, null, 2);
            resEl.textContent = 'Loading...';
            statusEl.textContent = '';
            statusEl.className = '';
            
            try {
                const start = performance.now();
                const res = await fetch(baseURL + endpoint, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(requestBody)
                });
                const elapsed = (performance.now() - start).toFixed(0);
                const text = await res.text();
                let output;
                try {
                    output = JSON.stringify(JSON.parse(text), null, 2);
                } catch {
                    output = text;
                }
                resEl.textContent = output;
                statusEl.textContent = res.status + (res.ok ? ' OK' : ' Error') + ' — ' + elapsed + 'ms';
                statusEl.className = res.ok ? 'success' : 'error';
            } catch (e) {
                resEl.textContent = 'Error: ' + e.message;
                statusEl.textContent = 'Failed';
                statusEl.className = 'error';
            }
        }

        async function sendCustomRequest() {
            const endpoint = document.getElementById('customEndpoint').value;
            const method = document.getElementById('customMethod').value;
            let params = {};
            try {
                params = JSON.parse(document.getElementById('customParams').value || '{}');
            } catch (e) {
                alert('Invalid JSON in params');
                return;
            }
            await callApi(endpoint, method, params);
        }

        function clearApiResponse() {
            document.getElementById('apiRequest').textContent = 'Click a method on the left to execute a request';
            document.getElementById('apiResponse').textContent = 'Response will appear here';
            document.getElementById('apiStatus').textContent = '';
            document.getElementById('apiStatus').className = '';
        }

        // Node Info
        async function fetchNodeInfo() {
            const [ver, id, ip] = await Promise.all([
                rpcCall('/ext/info', 'info.getNodeVersion'),
                rpcCall('/ext/info', 'info.getNodeID'),
                rpcCall('/ext/info', 'info.getNodeIP')
            ]);
            const el = document.getElementById('nodeInfoCard');
            if (ver?.result) {
                const v = ver.result;
                el.innerHTML = ` + "`" + `
                    <div class="info-list">
                        <div class="info-row"><span class="info-label">Version</span><span class="info-value">${v.version}</span></div>
                        <div class="info-row"><span class="info-label">Database</span><span class="info-value">${v.databaseVersion}</span></div>
                        <div class="info-row"><span class="info-label">RPC Protocol</span><span class="info-value">${v.rpcProtocolVersion}</span></div>
                        <div class="info-row"><span class="info-label">Git Commit</span><span class="info-value">${v.gitCommit}</span></div>
                        <div class="info-row"><span class="info-label">Public IP</span><span class="info-value">${ip?.result?.ip || 'N/A'}</span></div>
                    </div>
                    <div class="mono-box"><span class="label">Node ID</span>${id?.result?.nodeID || 'Unknown'}</div>
                ` + "`" + `;
            } else {
                el.innerHTML = '<div class="error-state">Failed to load node info</div>';
            }
        }

        // Network Info
        async function fetchNetworkInfo() {
            const [net, name] = await Promise.all([
                rpcCall('/ext/info', 'info.getNetworkID'),
                rpcCall('/ext/info', 'info.getNetworkName')
            ]);
            const el = document.getElementById('networkInfoCard');
            el.innerHTML = ` + "`" + `
                <div class="info-list">
                    <div class="info-row"><span class="info-label">Network</span><span class="info-value">${name?.result?.networkName || 'Unknown'}</span></div>
                    <div class="info-row"><span class="info-label">Network ID</span><span class="info-value">${net?.result?.networkID || 'Unknown'}</span></div>
                    <div class="info-row"><span class="info-label">API Endpoint</span><span class="info-value">${baseURL}</span></div>
                </div>
            ` + "`" + `;
        }

        // Uptime
        async function fetchUptime() {
            const data = await rpcCall('/ext/info', 'info.uptime');
            const el = document.getElementById('uptimeCard');
            if (data?.result) {
                const w = (data.result.weightedAveragePercentage * 100).toFixed(2);
                const r = (data.result.rewardingStakePercentage * 100).toFixed(2);
                document.getElementById('statUptime').textContent = w + '%';
                el.innerHTML = ` + "`" + `
                    <div class="uptime-display">
                        <div class="uptime-value">${w}%</div>
                        <div class="uptime-label">Weighted Average Uptime</div>
                    </div>
                    <div class="info-list" style="margin-top:0.75rem;">
                        <div class="info-row"><span class="info-label">Rewarding Stake</span><span class="info-value">${r}%</span></div>
                    </div>
                    <div class="uptime-bar"><div class="uptime-fill" style="width:${w}%"></div></div>
                ` + "`" + `;
            } else {
                document.getElementById('statUptime').textContent = 'N/A';
                el.innerHTML = '<div class="empty-state">Uptime not available (not a validator?)</div>';
            }
        }

        // Peers
        async function fetchPeers() {
            const data = await rpcCall('/ext/info', 'info.peers');
            const el = document.getElementById('peersCard');
            if (data?.result?.peers) {
                const peers = data.result.peers;
                document.getElementById('statPeers').textContent = peers.length;
                if (peers.length === 0) {
                    el.innerHTML = '<div class="empty-state">No peers connected</div>';
                } else {
                    el.innerHTML = '<div class="peer-list">' + peers.slice(0, 20).map(p => ` + "`" + `
                        <div class="peer-item">
                            <div><div class="peer-id">${p.nodeID}</div><div class="peer-meta">${p.ip || 'Unknown'}</div></div>
                            <span class="peer-version">${p.version || '?'}</span>
                        </div>
                    ` + "`" + `).join('') + (peers.length > 20 ? '<div class="empty-state">+' + (peers.length - 20) + ' more</div>' : '') + '</div>';
                }
            } else {
                document.getElementById('statPeers').textContent = '0';
                el.innerHTML = '<div class="error-state">Failed to load peers</div>';
            }
        }

        async function fetchPeersDetailed() {
            const data = await rpcCall('/ext/info', 'info.peers');
            const el = document.getElementById('peersTableCard');
            if (data?.result?.peers?.length) {
                const peers = data.result.peers;
                el.innerHTML = ` + "`" + `<table class="data-table">
                    <thead><tr><th>Node ID</th><th>IP</th><th>Version</th><th>Last Sent</th><th>Last Received</th></tr></thead>
                    <tbody>${peers.slice(0, 30).map(p => ` + "`" + `
                        <tr>
                            <td>${p.nodeID?.substring(0, 24)}...</td>
                            <td>${p.ip || 'N/A'}</td>
                            <td>${p.version || 'N/A'}</td>
                            <td>${p.lastSent ? new Date(p.lastSent).toLocaleTimeString() : 'N/A'}</td>
                            <td>${p.lastReceived ? new Date(p.lastReceived).toLocaleTimeString() : 'N/A'}</td>
                        </tr>
                    ` + "`" + `).join('')}</tbody>
                </table>` + "`" + `;
            } else {
                el.innerHTML = '<div class="empty-state">No peer data</div>';
            }
        }

        async function fetchNetworkStats() {
            const data = await rpcCall('/ext/info', 'info.peers');
            const el = document.getElementById('networkStatsCard');
            if (data?.result?.peers) {
                const peers = data.result.peers;
                const versions = {};
                peers.forEach(p => { const v = p.version || 'unknown'; versions[v] = (versions[v] || 0) + 1; });
                el.innerHTML = ` + "`" + `
                    <div class="info-list">
                        <div class="info-row"><span class="info-label">Total Peers</span><span class="info-value">${peers.length}</span></div>
                        <div class="info-row"><span class="info-label">Unique Versions</span><span class="info-value">${Object.keys(versions).length}</span></div>
                    </div>
                    <div style="margin-top:0.75rem;"><strong style="font-size:0.7rem;color:var(--text-muted);">VERSION DISTRIBUTION</strong></div>
                    <div class="info-list" style="margin-top:0.5rem;">
                        ${Object.entries(versions).sort((a,b)=>b[1]-a[1]).slice(0,5).map(([v,c]) => ` + "`" + `
                            <div class="info-row"><span class="info-label">${v}</span><span class="info-value">${c} peers</span></div>
                        ` + "`" + `).join('')}
                    </div>
                ` + "`" + `;
            } else {
                el.innerHTML = '<div class="error-state">Failed to load stats</div>';
            }
        }

        // Health
        async function fetchHealth() {
            let data;
            try { data = await (await fetch(baseURL + '/ext/health')).json(); } catch (e) { data = null; }
            const statusDot = document.getElementById('statusDot');
            const statusText = document.getElementById('statusText');
            const el = document.getElementById('healthCard');
            if (data) {
                statusDot.className = 'status-dot ' + (data.healthy ? 'healthy' : 'unhealthy');
                statusText.textContent = data.healthy ? 'Healthy' : 'Unhealthy';
                if (data.checks) {
                    const checks = Object.entries(data.checks);
                    const passing = checks.filter(([_,c]) => !c.error).length;
                    document.getElementById('statHealth').textContent = passing + '/' + checks.length;
                    el.innerHTML = '<div class="health-grid">' + checks.map(([n,c]) => ` + "`" + `
                        <div class="health-item">
                            <span class="health-name">${n}</span>
                            <span class="health-badge ${c.error ? 'fail' : 'pass'}">${c.error ? 'FAIL' : 'PASS'}</span>
                        </div>
                    ` + "`" + `).join('') + '</div>';
                }
            } else {
                statusDot.className = 'status-dot unhealthy';
                statusText.textContent = 'Unreachable';
                document.getElementById('statHealth').textContent = '—';
                el.innerHTML = '<div class="error-state">Failed to fetch health</div>';
            }
        }

        async function fetchHealthDetailed() {
            let data;
            try { data = await (await fetch(baseURL + '/ext/health')).json(); } catch (e) { data = null; }
            const el = document.getElementById('healthDetailCard');
            if (data?.checks) {
                el.innerHTML = ` + "`" + `<table class="data-table">
                    <thead><tr><th>Check</th><th>Status</th><th>Message</th><th>Timestamp</th></tr></thead>
                    <tbody>${Object.entries(data.checks).map(([n,c]) => ` + "`" + `
                        <tr>
                            <td><strong>${n}</strong></td>
                            <td><span class="health-badge ${c.error ? 'fail' : 'pass'}">${c.error ? 'FAIL' : 'PASS'}</span></td>
                            <td style="max-width:400px;overflow:hidden;text-overflow:ellipsis;">${c.error?.message || c.message?.toString()?.substring(0,100) || '—'}</td>
                            <td>${c.timestamp ? new Date(c.timestamp).toLocaleString() : '—'}</td>
                        </tr>
                    ` + "`" + `).join('')}</tbody>
                </table>` + "`" + `;
            } else {
                el.innerHTML = '<div class="error-state">Failed to load health details</div>';
            }
        }

        async function fetchLiveness() {
            let data;
            try { data = await (await fetch(baseURL + '/ext/health/liveness')).json(); } catch (e) { data = null; }
            const el = document.getElementById('livenessCard');
            el.innerHTML = data ? ` + "`" + `
                <div class="info-list">
                    <div class="info-row"><span class="info-label">Liveness</span><span class="info-value ${data.healthy ? 'success' : 'error'}">${data.healthy ? 'ALIVE' : 'UNHEALTHY'}</span></div>
                </div>
            ` + "`" + ` : '<div class="error-state">Failed</div>';
        }

        async function fetchReadiness() {
            let data;
            try { data = await (await fetch(baseURL + '/ext/health/readiness')).json(); } catch (e) { data = null; }
            const el = document.getElementById('readinessCard');
            el.innerHTML = data ? ` + "`" + `
                <div class="info-list">
                    <div class="info-row"><span class="info-label">Readiness</span><span class="info-value ${data.healthy ? 'success' : 'warning'}">${data.healthy ? 'READY' : 'NOT READY'}</span></div>
                </div>
            ` + "`" + ` : '<div class="error-state">Failed</div>';
        }

        // Chains
        async function fetchChains() {
            const chains = [
                { id: 'P', name: 'Platform Chain', desc: 'Validators, subnets, staking' },
                { id: 'X', name: 'Exchange Chain', desc: 'Asset transfers' },
                { id: 'C', name: 'Contract Chain', desc: 'EVM smart contracts' }
            ];
            const results = await Promise.all(chains.map(async c => {
                const r = await rpcCall('/ext/info', 'info.isBootstrapped', { chain: c.id });
                return { ...c, synced: r?.result?.isBootstrapped };
            }));
            document.getElementById('statChains').textContent = results.filter(r => r.synced).length + '/3';
            document.getElementById('chainsCard').innerHTML = '<div class="chain-grid">' + results.map(c => ` + "`" + `
                <div class="chain-item">
                    <div class="chain-header">
                        <span class="chain-name">${c.name} <span class="chain-badge">${c.id}</span></span>
                        <span class="chain-status ${c.synced ? 'synced' : 'syncing'}">${c.synced ? 'Synced' : 'Syncing'}</span>
                    </div>
                    <div class="chain-desc">${c.desc}</div>
                </div>
            ` + "`" + `).join('') + '</div>';
        }

        // Subnets
        async function fetchSubnets() {
            const data = await rpcCall('/ext/P', 'platform.getBlockchains');
            const el = document.getElementById('subnetsCard');
            if (data?.result?.blockchains) {
                const bcs = data.result.blockchains;
                const subnets = new Map();
                bcs.forEach(bc => {
                    const sid = bc.subnetID || 'Primary';
                    if (!subnets.has(sid)) subnets.set(sid, []);
                    subnets.get(sid).push(bc);
                });
                document.getElementById('statSubnets').textContent = subnets.size;
                let html = '';
                for (const [sid, chains] of subnets) {
                    const isPrimary = sid === '11111111111111111111111111111111LpoYY';
                    html += ` + "`" + `
                        <div class="subnet-item">
                            <div class="subnet-header">
                                <span class="subnet-name">${isPrimary ? 'Primary Network' : 'Subnet'}</span>
                                <span style="font-size:0.7rem;color:var(--text-muted);">${chains.length} chain${chains.length>1?'s':''}</span>
                            </div>
                            ${!isPrimary ? ` + "`" + `<div class="subnet-id">${sid}</div>` + "`" + ` : ''}
                            <div class="subnet-chains">
                                ${chains.map(c => ` + "`" + `<div class="subnet-chain"><span>${c.name || 'Unnamed'}</span><span style="color:var(--text-muted)">${c.vmID?.substring(0,12)}...</span></div>` + "`" + `).join('')}
                            </div>
                        </div>
                    ` + "`" + `;
                }
                el.innerHTML = '<div class="subnet-grid">' + html + '</div>';
            } else {
                document.getElementById('statSubnets').textContent = '—';
                el.innerHTML = '<div class="empty-state">Subnet data not available yet</div>';
            }
        }

        // VMs
        async function fetchVMs() {
            const data = await rpcCall('/ext/info', 'info.getVMs');
            const el = document.getElementById('vmsCard');
            if (data?.result?.vms) {
                const vms = Object.entries(data.result.vms);
                document.getElementById('statVMs').textContent = vms.length;
                el.innerHTML = '<div class="vm-grid">' + vms.map(([id, aliases]) => ` + "`" + `
                    <div class="vm-item">
                        <span class="vm-name">${aliases[0] || 'Unknown'}</span>
                        <span class="vm-id">${id}</span>
                    </div>
                ` + "`" + `).join('') + '</div>';
            } else {
                document.getElementById('statVMs').textContent = '—';
                el.innerHTML = '<div class="error-state">Failed to load VMs</div>';
            }
        }

        // ACPs
        async function fetchACPs() {
            const data = await rpcCall('/ext/info', 'info.acps');
            const el = document.getElementById('acpsCard');
            if (data?.result?.acps) {
                const acps = Object.entries(data.result.acps);
                if (acps.length === 0) {
                    el.innerHTML = '<div class="empty-state">No active ACPs</div>';
                } else {
                    el.innerHTML = ` + "`" + `<table class="data-table">
                        <thead><tr><th>ACP</th><th>Support Weight</th><th>Object Weight</th><th>Abstain Weight</th></tr></thead>
                        <tbody>${acps.map(([num, acp]) => ` + "`" + `
                            <tr>
                                <td>ACP-${num}</td>
                                <td>${acp.supportWeight || 0}</td>
                                <td>${acp.objectWeight || 0}</td>
                                <td>${acp.abstainWeight || 0}</td>
                            </tr>
                        ` + "`" + `).join('')}</tbody>
                    </table>` + "`" + `;
                }
            } else {
                el.innerHTML = '<div class="empty-state">No ACP data available</div>';
            }
        }

        // Upgrades
        async function fetchUpgrades() {
            const data = await rpcCall('/ext/info', 'info.upgrades');
            const el = document.getElementById('upgradesCard');
            if (data?.result) {
                const u = data.result;
                el.innerHTML = '<div class="info-list">' + Object.entries(u).filter(([k]) => k.endsWith('Time')).map(([k,v]) => ` + "`" + `
                    <div class="info-row"><span class="info-label">${k}</span><span class="info-value">${v ? new Date(v).toLocaleString() : 'Not scheduled'}</span></div>
                ` + "`" + `).join('') + '</div>';
            } else {
                el.innerHTML = '<div class="error-state">Failed to load upgrades</div>';
            }
        }

        // TX Fees
        async function fetchTxFees() {
            const data = await rpcCall('/ext/info', 'info.getTxFee');
            const el = document.getElementById('txFeesCard');
            if (data?.result) {
                const f = data.result;
                el.innerHTML = '<div class="info-list">' + Object.entries(f).map(([k,v]) => ` + "`" + `
                    <div class="info-row"><span class="info-label">${k}</span><span class="info-value">${nanoToAvax(v)} AVAX</span></div>
                ` + "`" + `).join('') + '</div>';
            } else {
                el.innerHTML = '<div class="error-state">Failed to load tx fees</div>';
            }
        }

        // P-Chain Height
        async function fetchPChainHeight() {
            const data = await rpcCall('/ext/P', 'platform.getHeight');
            const el = document.getElementById('pchainHeightCard');
            if (data?.result?.height) {
                el.innerHTML = ` + "`" + `
                    <div class="info-list">
                        <div class="info-row"><span class="info-label">P-Chain Height</span><span class="info-value">${data.result.height}</span></div>
                    </div>
                ` + "`" + `;
            } else {
                el.innerHTML = '<div class="empty-state">P-Chain height not available</div>';
            }
        }

        // C-Chain Info
        async function fetchCChainInfo() {
            const [blockNum, chainId] = await Promise.all([
                fetchJSON('/ext/bc/C/rpc', 'POST', { jsonrpc: '2.0', id: 1, method: 'eth_blockNumber', params: [] }),
                fetchJSON('/ext/bc/C/rpc', 'POST', { jsonrpc: '2.0', id: 1, method: 'eth_chainId', params: [] })
            ]);
            const el = document.getElementById('cchainInfoCard');
            el.innerHTML = ` + "`" + `
                <div class="info-list">
                    <div class="info-row"><span class="info-label">C-Chain Block Height</span><span class="info-value">${blockNum?.result ? parseInt(blockNum.result, 16) : 'N/A'}</span></div>
                    <div class="info-row"><span class="info-label">Chain ID</span><span class="info-value">${chainId?.result ? parseInt(chainId.result, 16) : 'N/A'}</span></div>
                </div>
            ` + "`" + `;
        }

        // Validators
        async function fetchValidators() {
            const data = await rpcCall('/ext/P', 'platform.getCurrentValidators', { subnetID: '11111111111111111111111111111111LpoYY' });
            const el = document.getElementById('validatorsCard');
            if (data?.result?.validators?.length) {
                const vals = data.result.validators.slice(0, 10);
                el.innerHTML = ` + "`" + `<table class="data-table">
                    <thead><tr><th>Node ID</th><th>Stake</th><th>Start Time</th><th>End Time</th><th>Uptime</th></tr></thead>
                    <tbody>${vals.map(v => ` + "`" + `
                        <tr>
                            <td>${v.nodeID?.substring(0, 20)}...</td>
                            <td>${nanoToAvax(v.stakeAmount || v.weight || 0)} AVAX</td>
                            <td>${new Date(parseInt(v.startTime) * 1000).toLocaleDateString()}</td>
                            <td>${new Date(parseInt(v.endTime) * 1000).toLocaleDateString()}</td>
                            <td>${v.uptime ? (parseFloat(v.uptime) * 100).toFixed(2) + '%' : 'N/A'}</td>
                        </tr>
                    ` + "`" + `).join('')}</tbody>
                </table><div class="empty-state">Showing first 10 of ${data.result.validators.length} validators</div>` + "`" + `;
            } else {
                el.innerHTML = '<div class="empty-state">Validator data not available</div>';
            }
        }

        function updateTimestamp() {
            document.getElementById('lastUpdated').textContent = 'Last updated: ' + new Date().toLocaleTimeString();
        }

        async function refreshAll() {
            const btn = document.getElementById('refreshAllBtn');
            btn.classList.add('spinning');
            await Promise.all([
                fetchNodeInfo(), fetchNetworkInfo(), fetchUptime(), fetchPeers(),
                fetchHealth(), fetchChains(), fetchSubnets(), fetchVMs()
            ]);
            updateTimestamp();
            btn.classList.remove('spinning');
        }

        refreshAll();
    </script>
</body>
</html>`
