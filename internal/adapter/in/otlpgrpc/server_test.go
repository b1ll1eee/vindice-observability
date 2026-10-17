package otlpgrpc

import (
	"context"
	"errors"
	"testing"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
)

type fakeIngestor struct {
	err   error
	calls int
}

func (f *fakeIngestor) Ingest(context.Context, domain.Signal, []byte) error {
	f.calls++
	return f.err
}

func TestExport(t *testing.T) {
	req := &collogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{}}}
	ctx := context.Background()

	for _, tc := range []struct {
		err  error
		code codes.Code
	}{
		{nil, codes.OK},
		{domain.ErrUnavailable, codes.Unavailable},
		{errors.New("bad"), codes.InvalidArgument},
	} {
		ing := &fakeIngestor{err: tc.err}
		_, err := (&logsService{ingest: ing}).Export(ctx, req)
		if got := status.Code(err); got != tc.code || ing.calls != 1 {
			t.Errorf("ingest err %v: code %v, calls %d", tc.err, got, ing.calls)
		}
	}

	ing := &fakeIngestor{}
	if _, err := (&logsService{ingest: ing}).Export(ctx, &collogs.ExportLogsServiceRequest{}); err != nil || ing.calls != 0 {
		t.Errorf("empty request: err %v, calls %d", err, ing.calls)
	}
}
