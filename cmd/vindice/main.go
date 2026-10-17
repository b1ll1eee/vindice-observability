// Command vindice runs the Vindice observability backend. It is the
// composition root: adapters are built here and injected into the core.
//
//	vindice -mode=all      everything in one process (default)
//	vindice -mode=ingest   OTLP receivers -> Kafka
//	vindice -mode=writer   Kafka -> ClickHouse
//	vindice -mode=query    REST API and UI over ClickHouse
//	vindice -mode=alert    alert evaluation and Telegram notifications (run one)
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/httpapi"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/kafkaconsumer"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/otlpgrpc"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/otlphttp"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/in/promwrite"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/kafka"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/out/authcrypto"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/out/clickhouse"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/out/kafkapublisher"
	"github.com/b1ll1eee/vindice-obs/internal/adapter/out/telegram"
	"github.com/b1ll1eee/vindice-obs/internal/config"
	"github.com/b1ll1eee/vindice-obs/internal/core/policy"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
	"github.com/b1ll1eee/vindice-obs/internal/core/service"
	"github.com/b1ll1eee/vindice-obs/web"
)

func main() {
	mode := flag.String("mode", "all", "all | ingest | writer | query | alert")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *mode, cfg, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("vindice stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode string, cfg config.Config, log *slog.Logger) error {
	var ingest, writer, query, alert bool
	switch mode {
	case "all":
		ingest, writer, query, alert = true, true, true, true
	case "ingest":
		ingest = true
	case "writer":
		writer = true
	case "query":
		query = true
	case "alert":
		alert = true
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	log.Info("starting vindice", "mode", mode)

	if ingest || writer {
		if err := retry(ctx, log, "kafka", func(ctx context.Context) error {
			return kafka.EnsureTopics(ctx, cfg.KafkaBrokers, cfg.KafkaPartitions, cfg.KafkaRetention)
		}); err != nil {
			return err
		}
	}

	// Writer, query and alert share one ClickHouse connection pool. Ingest
	// needs it only to load API keys.
	ingestAuth := ingest && cfg.IngestAuth == "required"
	var repo *clickhouse.Repository
	if writer || query || alert || ingestAuth {
		var err error
		if repo, err = openClickHouse(ctx, cfg, log); err != nil {
			return err
		}
		defer repo.Close()
	}

	var keys *service.APIKeyService
	if repo != nil {
		keys = service.NewAPIKeyService(repo, log)
	}

	g, ctx := errgroup.WithContext(ctx)
	if ingest {
		var auth port.IngestAuthorizer
		if ingestAuth {
			auth = keys
			g.Go(func() error { return keys.Run(ctx, 30*time.Second) })
		} else {
			log.Warn("VINDICE_INGEST_AUTH=off: OTLP ports accept data without an API key")
		}
		if err := startIngest(ctx, g, cfg, auth, log); err != nil {
			return err
		}
	}
	if writer {
		if err := startWriter(ctx, g, cfg, repo, log); err != nil {
			return err
		}
	}
	if query || alert {
		queries := service.NewQueryService(repo)
		alerts := service.NewAlertService(service.AlertDeps{
			Rules: repo, State: repo, Signals: queries, Notifier: notifier(cfg, log),
			PublicURL: cfg.PublicURL, Log: log,
		})
		if query {
			auth, err := newAuth(ctx, cfg, repo, log)
			if err != nil {
				return err
			}
			startQuery(ctx, g, cfg, httpapi.Deps{
				Querier: queries, Metrics: queries, Dashboards: service.NewDashboardService(repo),
				Alerts: alerts, Auth: auth, Users: auth, Keys: keys,
				SecureCookies: strings.HasPrefix(cfg.PublicURL, "https://"), SessionTTL: cfg.SessionTTL,
				UI: web.Handler(), Log: log,
			}, log)
		}
		if alert {
			g.Go(func() error {
				log.Info("alert evaluator running", "interval", cfg.AlertInterval)
				return alerts.Run(ctx, cfg.AlertInterval)
			})
		}
	}
	return g.Wait()
}

func startIngest(ctx context.Context, g *errgroup.Group, cfg config.Config, auth port.IngestAuthorizer, log *slog.Logger) error {
	pub, err := kafkapublisher.New(cfg.KafkaBrokers)
	if err != nil {
		return err
	}
	ingestSvc := service.NewIngestService(pub)

	httpSrv := &http.Server{
		Addr:              cfg.OTLPHTTPAddr,
		Handler:           ingestMux(ingestSvc, auth, cfg.MaxRequestBytes, log),
		ReadHeaderTimeout: 10 * time.Second,
	}
	grpcSrv := otlpgrpc.NewServer(ingestSvc, auth, int(cfg.MaxRequestBytes))
	lis, err := net.Listen("tcp", cfg.OTLPGRPCAddr)
	if err != nil {
		pub.Close()
		return err
	}

	g.Go(func() error {
		log.Info("otlp/http listening", "addr", cfg.OTLPHTTPAddr)
		if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		log.Info("otlp/grpc listening", "addr", cfg.OTLPGRPCAddr)
		return grpcSrv.Serve(lis)
	})
	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		grpcSrv.GracefulStop()
		pub.Close() // after the servers, so in-flight requests can finish publishing
		return nil
	})
	return nil
}

// ingestMux serves OTLP/HTTP and Prometheus remote write on one port.
// /healthz needs no key.
func ingestMux(ingest port.Ingestor, auth port.IngestAuthorizer, maxBody int64, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("POST /api/v1/write", promwrite.New(ingest, auth, maxBody, log))
	mux.Handle("/", otlphttp.New(ingest, auth, maxBody, log))
	return mux
}

// openClickHouse connects (waiting for ClickHouse if needed) and migrates the
// schema, which is idempotent, so any mode may start first.
func openClickHouse(ctx context.Context, cfg config.Config, log *slog.Logger) (*clickhouse.Repository, error) {
	var repo *clickhouse.Repository
	err := retry(ctx, log, "clickhouse", func(ctx context.Context) (err error) {
		repo, err = clickhouse.Open(ctx, clickhouse.Config{
			Addrs:             cfg.ClickHouseAddrs,
			Database:          cfg.ClickHouseDB,
			User:              cfg.ClickHouseUser,
			Password:          cfg.ClickHousePassword,
			ColdStoragePolicy: cfg.ColdStoragePolicy,
			ColdAfterDays:     cfg.ColdAfterDays,
			Retention: clickhouse.Retention{
				DebugLogs:  cfg.Retention.DebugLogs,
				InfoLogs:   cfg.Retention.InfoLogs,
				ErrorLogs:  cfg.Retention.ErrorLogs,
				Traces:     cfg.Retention.Traces,
				MetricsRaw: cfg.Retention.MetricsRaw,
				Metrics1m:  cfg.Retention.Metrics1m,
				Metrics1h:  cfg.Retention.Metrics1h,
			},
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := repo.Migrate(ctx); err != nil {
		_ = repo.Close()
		return nil, err
	}
	return repo, nil
}

func startWriter(ctx context.Context, g *errgroup.Group, cfg config.Config, repo *clickhouse.Repository, log *slog.Logger) error {
	filter, err := policy.LoadFilter(cfg.RulesFile)
	if err != nil {
		return err
	}

	writerSvc := service.NewWriterService(service.WriterDeps{
		Repo:   repo,
		Filter: filter,
		Sampler: policy.NewSampler(policy.SamplerConfig{
			Rate:       cfg.SampleRate,
			Slow:       cfg.SampleSlow,
			Wait:       cfg.SampleWait,
			MaxPending: cfg.SampleMaxPending,
		}),
		Resources: policy.NewSeen(time.Hour, 1_000_000),
		Series:    policy.NewSeen(time.Hour, 1_000_000),
	})

	consumer, err := kafkaconsumer.New(kafkaconsumer.Config{
		Brokers:       cfg.KafkaBrokers,
		Group:         cfg.KafkaGroup,
		BatchRows:     cfg.BatchRows,
		FlushInterval: cfg.FlushInterval,
	}, writerSvc, log)
	if err != nil {
		return err
	}

	g.Go(func() error {
		log.Info("writer consuming", "group", cfg.KafkaGroup)
		return consumer.Run(ctx)
	})
	return nil
}

func startQuery(ctx context.Context, g *errgroup.Group, cfg config.Config, deps httpapi.Deps, log *slog.Logger) {
	srv := &http.Server{
		Addr:              cfg.QueryAddr,
		Handler:           httpapi.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
	}
	g.Go(func() error {
		log.Info("query api and ui listening", "addr", cfg.QueryAddr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})
}

// newAuth builds the authenticator and creates the first admin if there
// are no users yet.
func newAuth(ctx context.Context, cfg config.Config, repo *clickhouse.Repository, log *slog.Logger) (*service.AuthService, error) {
	secret := cfg.SessionSecret
	if secret == "" {
		var err error
		secret, err = repo.GetOrCreateSecret(ctx, "session-key", func() string {
			b := make([]byte, 32)
			_, _ = rand.Read(b)
			return hex.EncodeToString(b)
		})
		if err != nil {
			return nil, fmt.Errorf("session key: %w", err)
		}
	}
	codec, err := authcrypto.NewHMAC([]byte(secret))
	if err != nil {
		return nil, fmt.Errorf("VINDICE_SESSION_SECRET: %w", err)
	}
	auth, err := service.NewAuthService(repo, authcrypto.Bcrypt{}, codec, cfg.SessionTTL)
	if err != nil {
		return nil, err
	}
	generated, err := auth.Bootstrap(ctx, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return nil, fmt.Errorf("create first admin: %w", err)
	}
	if generated != "" {
		// Shown once, on the very first start; change it after signing in.
		log.Warn("created first admin user, change this password after signing in",
			"username", cfg.AdminUsername, "password", generated)
	}
	return auth, nil
}

// notifier picks Telegram when a bot token is set, else logs alerts.
func notifier(cfg config.Config, log *slog.Logger) port.Notifier {
	if cfg.TelegramToken == "" {
		log.Warn("VINDICE_TELEGRAM_BOT_TOKEN not set: alerts are logged, not sent")
		return telegram.LogNotifier{Log: log}
	}
	return telegram.New(cfg.TelegramAPI, cfg.TelegramToken, cfg.TelegramChatID)
}

// retry calls fn until it succeeds or ctx ends, so services can start before
// their dependencies are ready.
func retry(ctx context.Context, log *slog.Logger, dep string, fn func(context.Context) error) error {
	backoff := time.Second
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := fn(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		log.Warn("waiting for dependency", "dep", dep, "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}
