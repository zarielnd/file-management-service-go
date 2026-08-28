package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer       trace.Tracer
	meter        metric.Meter
	httpDuration metric.Float64Histogram
	httpCounter  metric.Int64Counter
)

type InitOptions struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
}

// Init sets up Cloud Trace, Cloud Monitoring, and structured JSON logging.
// In local/dev environments without GCP credentials, observability degrades
// gracefully instead of crashing.
func Init(ctx context.Context, opts InitOptions) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(opts.ServiceName),
			semconv.ServiceVersion(opts.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(opts.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	// ---- Traces → Cloud Trace (optional) ----
	var tp *sdktrace.TracerProvider
	traceExporter, err := texporter.New()
	if err != nil {
		slog.WarnContext(ctx, "GCP trace exporter unavailable, tracing disabled", "error", err)
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.NeverSample()),
		)
	} else {
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1)),
		)
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	tracer = tp.Tracer(opts.ServiceName)

	// ---- Metrics → Cloud Monitoring (optional) ----
	var mp *sdkmetric.MeterProvider
	metricExporter, err := mexporter.New()
	if err != nil {
		slog.WarnContext(ctx, "GCP metric exporter unavailable, metrics disabled", "error", err)
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
		)
	} else {
		reader := sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(60*time.Second))
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(reader),
			sdkmetric.WithResource(res),
		)
	}
	otel.SetMeterProvider(mp)
	meter = mp.Meter(opts.ServiceName)

	// These will be no-ops if the meter provider has no reader, which is fine.
	httpDuration, _ = meter.Float64Histogram("http.server.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("ms"),
	)
	httpCounter, _ = meter.Int64Counter("http.server.requests.total",
		metric.WithDescription("Total HTTP requests"),
	)

	// ---- Logs → stdout (Cloud Logging) ----
	initLogger()

	shutdown := func(ctx context.Context) error {
		if mp != nil {
			if err := mp.Shutdown(ctx); err != nil {
				slog.Error("metric shutdown failed", "error", err)
			}
		}
		if tp != nil {
			if err := tp.Shutdown(ctx); err != nil {
				slog.Error("tracer shutdown failed", "error", err)
			}
		}
		return nil
	}
	return shutdown, nil
}

func Tracer() trace.Tracer { return tracer }
func Meter() metric.Meter  { return meter }

// ---------------------------------------------------------------------
// Logger
// ---------------------------------------------------------------------

func initLogger() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				a.Key = "severity"
				switch a.Value.Any().(slog.Level) {
				case slog.LevelWarn:
					a.Value = slog.StringValue("WARNING")
				case slog.LevelError:
					a.Value = slog.StringValue("ERROR")
				}
			}
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	})
	slog.SetDefault(slog.New(&traceHandler{Handler: handler}))
}

type traceHandler struct{ slog.Handler }

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
			slog.Bool("trace_sampled", sc.IsSampled()),
		)
		if projectID := os.Getenv("GOOGLE_CLOUD_PROJECT"); projectID != "" {
			r.AddAttrs(
				slog.String("logging.googleapis.com/trace",
					fmt.Sprintf("projects/%s/traces/%s", projectID, sc.TraceID().String())),
				slog.String("logging.googleapis.com/spanId", sc.SpanID().String()),
				slog.Bool("logging.googleapis.com/trace_sampled", sc.IsSampled()),
			)
		}
	}
	return h.Handler.Handle(ctx, r)
}

// ---------------------------------------------------------------------
// HTTP Middleware
// ---------------------------------------------------------------------

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.written {
		rr.statusCode = code
		rr.written = true
		rr.ResponseWriter.WriteHeader(code)
	}
}

func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract trace context from incoming headers so we link to file-server's trace
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		if r.URL.Path != "/" && r.URL.Path == "/health" {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		ctx, span := tracer.Start(ctx, fmt.Sprintf("%s %s", r.Method, r.URL.Path),
			trace.WithAttributes(
				semconv.HTTPRequestMethodOriginal(r.Method),
				semconv.URLFull(r.URL.String()),
				semconv.UserAgentOriginal(r.UserAgent()),
			),
		)
		defer span.End()

		rr := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(rr, r.WithContext(ctx))

		duration := float64(time.Since(start).Milliseconds())

		attrs := []attribute.KeyValue{
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path),
			attribute.Int("http.status_code", rr.statusCode),
		}

		span.SetAttributes(semconv.HTTPResponseStatusCode(rr.statusCode))
		if rr.statusCode >= 400 {
			span.SetStatus(codes.Error, http.StatusText(rr.statusCode))
		}

		httpDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
		httpCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	})
}
