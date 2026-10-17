// Package otlpgrpc is the OTLP/gRPC receiver.
package otlpgrpc

import (
	"context"
	"errors"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip" // accept gzip-compressed requests
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

func NewServer(ingest port.Ingestor, maxMsgBytes int) *grpc.Server {
	s := grpc.NewServer(grpc.MaxRecvMsgSize(maxMsgBytes))
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
