package tracing

import (
	"context"
	"fmt"
	"github.com/danthegoodman1/GoAPITemplate/utils"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"os"
	"strings"
)

var (
	Tracer = otel.Tracer(utils.Env_TracingServiceName)
)

// InitTracer creates a new OLTP trace provider instance and registers it as global trace provider.
func InitTracer(ctx context.Context) (tp *trace.TracerProvider, err error) {
	logger := zerolog.Ctx(ctx)
	var exporter trace.SpanExporter
	if utils.Env_OLTPEndpoint != "" {
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(utils.Env_OLTPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
	} else {
		logger.Warn().Msg("No OLTP endpoint provided, tracing to stdout")
		// exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		exporter, err = stdouttrace.New()
		if err != nil {
			return nil, err
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("error in os.Hostname: %w", err)
	}
	tp = trace.NewTracerProvider(
		// trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(0.01))),
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithBatcher(exporter),
		trace.WithResource(resource.NewSchemaless(
			semconv.ServiceName(utils.Env_TracingServiceName),
			semconv.HostName(hostname),
		)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp, nil
}

func CreateSpan(ctx context.Context, tracer oteltrace.Tracer, s string) (context.Context, oteltrace.Span) {
	queryName, _, _ := strings.Cut(s, "\n")
	opts := []oteltrace.SpanStartOption{
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
	}
	ctx, span := tracer.Start(ctx, queryName, opts...)
	return ctx, span
}
