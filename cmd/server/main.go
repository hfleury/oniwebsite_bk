package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"

	"oniwebsite_bk/internal/handlers"
	"oniwebsite_bk/internal/middleware"
	"oniwebsite_bk/internal/observability"
	"oniwebsite_bk/internal/services"
)

func main() {
	observability.SetupLogger()

	// Flags
	devMode := flag.Bool("dev", false, "Run in development mode (proxy to Vite)")
	port := flag.String("port", "8080", "Port to run the server on")
	flag.Parse()

	dsn := os.Getenv("SENTRY_DSN")
	env := os.Getenv("SENTRY_ENVIRONMENT")
	if env == "" {
		if *devMode {
			env = "development"
		} else {
			env = "production"
		}
	}
	if err := observability.Init(dsn, env); err != nil {
		slog.Error("failed to initialize sentry", slog.Any("error", err))
	}
	defer sentry.Flush(2 * time.Second)

	sh := sentryhttp.New(sentryhttp.Options{Repanic: true})

	// Paths
	cwd, _ := os.Getwd()
	localesDir := filepath.Join(cwd, "locales")
	distDir := filepath.Join(cwd, "../oniwebsite/dist") // Assuming sibling dir structure

	// 1. Initialize Services
	translator := services.NewFileTranslationService(localesDir)
	if err := translator.LoadTranslations(); err != nil {
		slog.Error("failed to load translations", slog.Any("error", err))
		sentry.CaptureException(err)
		sentry.Flush(2 * time.Second)
		os.Exit(1)
	}
	slog.Info("translations loaded successfully")

	// 2. Initialize Handlers
	htmlHandler := handlers.NewHTMLHandler(translator, *devMode, distDir)
	translationHandler := handlers.NewTranslationHandler(translator)

	// 3. Setup Router
	// We wrap the HTML handler with the language detector
	langAwareHTML := middleware.LanguageDetectorMiddleware(htmlHandler)

	// Assets / Static Files
	// If Dev, proxy everything else to Vite
	// If Prod, serve from dist/
	if *devMode {
		slog.Info("running in dev mode - proxying assets to http://localhost:5173")
		proxy := handlers.DevProxyHandler("http://localhost:5173")
		// We can't easily distinguish 404s vs assets in simple mux without pattern matching
		// essentially everything that is NOT captured above should go to proxy.
		// Since "/" matches everything in Go 1.21- mux if not careful, but Go 1.22 has better matching.
		// For standard http.ServeMux, "/" is a catch-all.
		// So we need to be careful.
		// "pattern / matches all paths not matched by other patterns"

		// Strategy: Custom root handler that decides.
		rootMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// API Routes (Manual check because we aren't using the mux directly here)
			// Better: use the mux for everything and have the proxy be the "fallback" pattern?
			// But for now, let's just intercept /api/
			if strings_HasPrefix(path, "/api/") {
				translationHandler.ServeHTTP(w, r)
				return
			}

			// Exact matches or specific prefixes for I18n
			if path == "/" || path == "/index.html" || strings_HasPrefix(path, "/pt/") || strings_HasPrefix(path, "/sv/") {
				langAwareHTML.ServeHTTP(w, r)
				return
			}
			// Otherwise proxy
			proxy.ServeHTTP(w, r)
		})

		slog.Info("server listening", slog.String("port", *port))
		if err := http.ListenAndServe(":"+*port, sh.Handle(rootMux)); err != nil {
			slog.Error("server exited", slog.Any("error", err))
			sentry.CaptureException(err)
			sentry.Flush(2 * time.Second)
			os.Exit(1)
		}

	} else {
		slog.Info("running in production mode - serving static files", slog.String("distDir", distDir))
		// Serve static files
		fs := http.FileServer(http.Dir(distDir))

		// For production, we also need to route assets.
		// Usually assets are in /assets/ or root.
		// Safe bet: ServeFile for anything that looks like a file, else serve HTML (SPA fallback)
		// But we have SSR for specific routes.

		rootMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// I18n Routes
			if path == "/" || path == "/index.html" || strings_HasPrefix(path, "/pt/") || strings_HasPrefix(path, "/sv/") {
				langAwareHTML.ServeHTTP(w, r)
				return
			}

			// API Routes
			if strings_HasPrefix(path, "/api/") {
				translationHandler.ServeHTTP(w, r)
				return
			}

			// Try to serve file
			// clean path to prevent directory traversal
			fPath := filepath.Join(distDir, filepath.Clean(path))
			info, err := os.Stat(fPath)
			if err == nil && !info.IsDir() {
				fs.ServeHTTP(w, r)
				return
			}

			// Fallback?
			// If not found, maybe 404? Or SPA fallback?
			// Let's 404 for now to be strict.
			// Or maybe the user has other routes like /login?
			// For now, assume a landing page site.
			http.NotFound(w, r)
		})

		slog.Info("server listening", slog.String("port", *port))
		if err := http.ListenAndServe(":"+*port, sh.Handle(rootMux)); err != nil {
			slog.Error("server exited", slog.Any("error", err))
			sentry.CaptureException(err)
			sentry.Flush(2 * time.Second)
			os.Exit(1)
		}
	}
}

// Helper strict check for simple logic
func strings_HasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[0:len(prefix)] == prefix
}
