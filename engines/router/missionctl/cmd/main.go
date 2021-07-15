package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "gopkg.in/confluentinc/confluent-kafka-go.v1/kafka/librdkafka"

	"github.com/gojek/mlp/pkg/instrumentation/sentry"
	_ "github.com/gojek/turing/engines/experiment/runner/nop"
	"github.com/gojek/turing/engines/router/missionctl"
	"github.com/gojek/turing/engines/router/missionctl/config"
	"github.com/gojek/turing/engines/router/missionctl/handlers"
	"github.com/gojek/turing/engines/router/missionctl/instrumentation/metrics"
	"github.com/gojek/turing/engines/router/missionctl/instrumentation/tracing"
	"github.com/gojek/turing/engines/router/missionctl/log"
	"github.com/gojek/turing/engines/router/missionctl/log/resultlog"
)

// initInstrumentation initializes the metrics collector and tracing client
func initInstrumentation(cfg *config.Config) func() {
	var tracingCloser io.Closer
	var err error

	// Init metrics collector
	err = metrics.InitMetricsCollector(cfg.AppConfig.CustomMetrics)
	if err != nil {
		log.Glob().Fatalf("Failed initializing Metrics Collector: %v", err)
	}

	// Init tracing client
	tracingCloser, err = tracing.InitGlobalTracer(cfg.AppConfig.Name, cfg.AppConfig.Jaeger)
	if err != nil {
		log.Glob().Fatalf("Failed initializing Tracer: %v", err)
	}
	// Return closer function
	return func() {
		if err := tracingCloser.Close(); err != nil {
			panic(err)
		}
	}
}

// initSentryClient initializes the Sentry client for error logging
func initSentryClient(cfg *config.Config) func() {
	if cfg.AppConfig.Sentry.Enabled {
		cfg.AppConfig.Sentry.Labels = map[string]string{
			"environment": cfg.AppConfig.Environment,
			"app":         fmt.Sprintf("turing-router-%s", cfg.AppConfig.Name),
		}
		if err := sentry.InitSentry(cfg.AppConfig.Sentry); err != nil {
			log.Glob().Fatalf("Failed initializing sentry client: %s", err)
		}
		return func() {
			defer sentry.Close()
		}
	}
	// Sentry not enabled, return dummy close function
	return func() {}
}

func main() {
	// Read env vars
	cfg, err := config.InitConfigEnv()
	if err != nil {
		log.Glob().Panicf("Failed initializing config: %v", err)
	}

	// Init logger
	log.InitGlobalLogger(cfg.AppConfig)
	defer func() {
		_ = log.Glob().Sync()
	}()

	// Init Turing result logger
	err = resultlog.InitTuringResultLogger(cfg.AppConfig)
	if err != nil {
		log.Glob().Fatalf("Failed initializing Turing Result Logger: %v", err)
	}

	// Init instrumentation, defer closing tracer
	defer initInstrumentation(cfg)()
	// Init Sentry, defer closing client
	defer initSentryClient(cfg)()

	// Init mission control
	missionCtl, err := missionctl.NewMissionControl(
		nil,
		cfg.EnrichmentConfig,
		cfg.RouterConfig,
		cfg.EnsemblerConfig,
		cfg.AppConfig,
	)
	if err != nil {
		log.Glob().Panicf("Failed initializing Mission Control: %v", err)
	}

	// Register handlers
	http.Handle("/v1/internal/", http.StripPrefix(
		"/v1/internal",
		handlers.NewInternalAPIHandler([]string{
			cfg.EnsemblerConfig.Endpoint,
			cfg.EnrichmentConfig.Endpoint,
		}),
	))
	http.Handle("/v1/predict", sentry.Recoverer(handlers.NewHTTPHandler(missionCtl)))
	// Register metrics handler
	if cfg.AppConfig.CustomMetrics {
		http.Handle("/metrics", promhttp.Handler())
	}

	// Define custom HTTP server
	httpServer := &http.Server{
		Addr:    cfg.ListenAddress(),
		Handler: http.DefaultServeMux,
	}

	// idleConnsClosed channel won't be close until
	// Received an interrupt signal or error running server
	idleConnsClosed := make(chan struct{})
	go func() {
		defer close(idleConnsClosed)

		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		signal.Notify(sigint, syscall.SIGTERM)

		<-sigint

		err := httpServer.Shutdown(context.Background())
		if err != nil {
			log.Glob().Errorf("Failed to shutdown server: %s", err)
		}
	}()

	// Serve
	log.Glob().Infof("listening at port %d", cfg.Port)
	err = httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Glob().Errorf("Failed to start Turing Mission Control API: %s", err)
		close(idleConnsClosed)
	}

	<-idleConnsClosed
}
