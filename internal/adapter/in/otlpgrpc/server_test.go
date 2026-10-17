package otlpgrpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
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

type keyAuth struct{}

func (keyAuth) Authorize(_ context.Context, key string) error {
	if key != "vdc_good" {
		return domain.ErrInvalidKey
	}
	return nil
}

func TestAuthInterceptor(t *testing.T) {
	ic := authInterceptor(keyAuth{})
	next := func(context.Context, any) (any, error) { return "ok", nil }
	for md, want := range map[string]codes.Code{
		"":                              codes.Unauthenticated,
		"authorization=Bearer x":        codes.Unauthenticated,
		"authorization=Bearer vdc_good": codes.OK,
		"x-vindice-key=vdc_good":        codes.OK,
	} {
		ctx := context.Background()
		if k, v, ok := strings.Cut(md, "="); ok {
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(k, v))
		}
		_, err := ic(ctx, nil, &grpc.UnaryServerInfo{}, next)
		if got := status.Code(err); got != want {
			t.Errorf("%q: %v, want %v", md, got, want)
		}
	}
}
