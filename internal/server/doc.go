// Package server provides the HTTP server for the assisted-migration-agent.
//
// The server uses the Gin web framework and supports two modes of operation:
// development (HTTP) and production (HTTPS with auto-generated TLS certificates).
//
// # Architecture Overview
//
//	┌───────────────────────────────────────────────────────────────┐
//	│                         HTTP Server                           │
//	├───────────────────────────────────────────────────────────────┤
//	│                                                               │
//	│  Production Mode (TLS)          Development Mode              │
//	│  ┌─────────────────────┐        ┌─────────────────────┐       │
//	│  │ HTTPS :8000         │        │ HTTP :8000          │       │
//	│  │ Self-signed cert    │        │ No TLS              │       │
//	│  │ Static file serving │        │ API only            │       │
//	│  │ SPA fallback        │        │                     │       │
//	│  └─────────────────────┘        └─────────────────────┘       │
//	│                                                               │
//	├───────────────────────────────────────────────────────────────┤
//	│                       Middleware Stack                        │
//	│  ┌─────────────────────────────────────────────────────────┐  │
//	│  │  Logger (request/response logging)                      │  │
//	│  │  Recovery (panic recovery with zap logging)             │  │
//	│  └─────────────────────────────────────────────────────────┘  │
//	├───────────────────────────────────────────────────────────────┤
//	│                       Router (/api/v1)                        │
//	│  ┌─────────────────────────────────────────────────────────┐  │
//	│  │  Handlers (registered via callback)                     │  │
//	│  └─────────────────────────────────────────────────────────┘  │
//	└───────────────────────────────────────────────────────────────┘
//
// # Server Modes
//
// Development Mode (ServerMode = "dev"):
//   - HTTP only (no TLS)
//   - Gin runs in debug mode
//   - API endpoints only
//
// Production Mode (ServerMode = "prod"):
//   - HTTPS with auto-generated self-signed certificate (1 year validity)
//   - Gin runs in release mode
//   - Static file serving from StaticsFolder
//   - SPA fallback: non-API routes serve index.html
//   - API 404s return JSON error response
//
// # Server Lifecycle
//
// Creation:
//
//	server, err := server.NewServer(cfg, func(router *gin.RouterGroup) {
//	    v1.RegisterHandlers(router, handler)
//	})
//
// The registerHandlerFn callback receives a RouterGroup prefixed with /api/v1.
//
// Starting:
//
//	// Blocks until error or shutdown
//	err := server.Start(ctx)
//
// Start automatically chooses HTTP or HTTPS based on TLS configuration.
//
// Stopping:
//
//	server.Stop(ctx)
//
// Performs graceful shutdown, waiting for in-flight requests to complete.
//
// # Middleware
//
// The server applies two middleware to all API routes:
//
// Logger Middleware (middlewares.Logger):
//   - Logs request start: method, path, query, IP, user-agent, timestamp
//   - Logs request end: all above + status code, latency
//   - Errors logged separately if present
//   - Uses zap structured logging with "http" logger name
//
// Recovery Middleware (ginzap.RecoveryWithZap):
//   - Recovers from panics in handlers
//   - Logs panic details with stack trace
//   - Returns 500 Internal Server Error
//
// # Static File Serving (Production Only)
//
// In production mode, the server serves:
//
//	/static/*     → StaticsFolder/
//	/             → StaticsFolder/index.html
//	/favicon.ico  → StaticsFolder/favicon.ico
//	/any/path     → StaticsFolder/index.html (SPA fallback)
//	/api/*        → 404 JSON error (if route not found)
//
// # TLS Configuration
//
// In production mode, TLS is configured with:
//   - Self-signed certificate generated at startup
//   - RSA private key
//   - 1 year certificate validity
//   - Certificate generated via pkg/certificates
//
// # Usage Example
//
//	cfg := &config.Configuration{
//	    Server: config.Server{
//	        ServerMode:    "prod",
//	        HTTPPort:      8443,
//	        StaticsFolder: "/app/static",
//	    },
//	}
//
//	srv, err := server.NewServer(cfg, func(router *gin.RouterGroup) {
//	    v2.RegisterHandlers(router, myHandler)
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Start in goroutine
//	go func() {
//	    if err := srv.Start(ctx); err != http.ErrServerClosed {
//	        log.Printf("server error: %v", err)
//	    }
//	}()
//
//	// Graceful shutdown on signal
//	<-shutdownCh
//	srv.Stop(ctx)
package server
