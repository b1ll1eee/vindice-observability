// Package otlpgrpc is the OTLP/gRPC receiver.
package otlpgrpc

import (
	"context"
	"errors"
	"strings"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip" // accept gzip-compressed requests
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

// NewServer returns the OTLP/gRPC receiver. auth checks the API key of every
// call; nil accepts calls without a key.
func NewServer(ingest port.Ingestor, auth port.IngestAuthorizer, maxMsgBytes int) *grpc.Server {
	opts := []grpc.ServerOption{grpc.MaxRecvMsgSize(maxMsgBytes)}
	if auth != nil {
		opts = append(opts, grpc.UnaryInterceptor(authInterceptor(auth)))
	}
	s := grpc.NewServer(opts...)
	collogs.RegisterLogsServiceServer(s, &logsService{ingest: ingest})
	coltrace.RegisterTraceServiceServer(s, &traceService{ingest: ingest})
	colmetrics.RegisterMetricsServiceServer(s, &metricsService{ingest: ingest})
	return s
}

type logsService struct {
	collogs.UnimplementedLogsServiceServer
	ingest port.Ingestor
}

func (s *logsService) Export(ctx context.Context, req *collogs.ExportLogsServiceRequest) (*collogs.ExportLogsServiceResponse, error) {
	return &collogs.ExportLogsServiceResponse{}, forward(ctx, s.ingest, domain.SignalLogs, req)
}

type traceService struct {
	coltrace.UnimplementedTraceServiceServer
	ingest port.Ingestor
}

func (s *traceService) Export(ctx context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	return &coltrace.ExportTraceServiceResponse{}, forward(ctx, s.ingest, domain.SignalTraces, req)
}

type metricsService struct {
	colmetrics.UnimplementedMetricsServiceServer
	ingest port.Ingestor
}

func (s *metricsService) Export(ctx context.Context, req *colmetrics.ExportMetricsServiceRequest) (*colmetrics.ExportMetricsServiceResponse, error) {
	return &colmetrics.ExportMetricsServiceResponse{}, forward(ctx, s.ingest, domain.SignalMetrics, req)
}

// authInterceptor reads the key from the "authorization: Bearer <key>" or
// "x-vindice-key" metadata.
func authInterceptor(auth port.IngestAuthorizer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		var key string
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if v := md.Get("authorization"); len(v) > 0 {
				if scheme, k, ok := strings.Cut(v[0], " "); ok && strings.EqualFold(scheme, "Bearer") {
					key = strings.TrimSpace(k)
				}
			}
			if v := md.Get("x-vindice-key"); key == "" && len(v) > 0 {
				key = strings.TrimSpace(v[0])
			}
		}
		if err := auth.Authorize(ctx, key); err != nil {
			if errors.Is(err, domain.ErrUnavailable) {
				return nil, status.Error(codes.Unavailable, "temporarily unavailable")
			}
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		return next(ctx, req)
	}
}

func forward(ctx context.Context, ingest port.Ingestor, sig domain.Signal, req proto.Message) error {
	if proto.Size(req) == 0 {
		return nil
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		return status.Error(codes.Internal, "re-encode request")
	}
	if err := ingest.Ingest(ctx, sig, payload); err != nil {
		if errors.Is(err, domain.ErrUnavailable) {
			return status.Error(codes.Unavailable, "temporarily unavailable")
		}
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return nil
}
