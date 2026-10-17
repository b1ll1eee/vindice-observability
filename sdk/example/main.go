// Command example is a tiny HTTP service instrumented with the Vindice SDK.
//
//	VINDICE_ENDPOINT=http://localhost:4318 VINDICE_API_KEY=vdc_... go run ./example
//	curl localhost:8090/orders/42
package main

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"time"

	"go.opentelemetry.io/otel"

	vindice "github.com/b1ll1eee/vindice-obs/sdk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	sdk, err := vindice.Start(ctx, vindice.Config{ServiceName: "example-orders", ServiceVersion: "0.1.0"})
	if err != nil {
		panic(err)
	}
	defer sdk.Shutdown(context.Background())
	log := sdk.Logger()

	orders, _ := otel.Meter("example").Int64Counter("orders_served")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, span := otel.Tracer("example").Start(ctx, "load order from db")
		time.Sleep(time.Duration(rand.IntN(20)) * time.Millisecond)
		span.End()

		if rand.IntN(10) == 0 {
			log.ErrorContext(ctx, "order lookup failed", "order_id", r.PathValue("id"))
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		log.InfoContext(ctx, "order served", "order_id", r.PathValue("id"))
		orders.Add(ctx, 1)
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{Addr: ":8090", Handler: vindice.Handler(mux, "example-orders")}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	log.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Error("server stopped", "err", err)
	}
}
