package observability

import (
	"context"
	"log/slog"
	"os"

	"github.com/getsentry/sentry-go"
)

func Init(dsn, environment string) error {
	return sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
}

func SetupLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

func TraceIDAttr(ctx context.Context) slog.Attr {
	span := sentry.SpanFromContext(ctx)
	if span == nil {
		return slog.String("trace_id", "")
	}
	return slog.String("trace_id", span.TraceID.String())
}

// CaptureException reports err to Sentry using the per-request hub carried on
// ctx (set by sentryhttp middleware), so the captured event's trace_id
// matches the current request's trace. Falls back to the global hub if ctx
// carries none (e.g. no request in flight).
func CaptureException(ctx context.Context, err error) {
	if hub := sentry.GetHubFromContext(ctx); hub != nil {
		hub.CaptureException(err)
		return
	}
	sentry.CaptureException(err)
}
