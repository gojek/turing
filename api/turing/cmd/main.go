package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/cors"

	"github.com/gojek/mlp/pkg/authz/enforcer"
	"github.com/gojek/mlp/pkg/instrumentation/newrelic"
	"github.com/gojek/mlp/pkg/instrumentation/sentry"
	"github.com/gojek/turing/api/turing/api"
	batchrunner "github.com/gojek/turing/api/turing/batch/runner"
	"github.com/gojek/turing/api/turing/config"
	"github.com/gojek/turing/api/turing/log"
	"github.com/gojek/turing/api/turing/vault"
	"github.com/gojek/turing/api/turing/web"
	"github.com/heptiolabs/healthcheck"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

type configFlags []string

func (c *configFlags) String() string {
	return strings.Join(*c, ",")
}

func (c *configFlags) Set(value string) error {
	*c = append(*c, value)
	return nil
}

func main() {
	var configFlags configFlags
	flag.Var(&configFlags, "config", "Path to a configuration file. This flag can be specified multiple "+
		"times to load multiple configurations.")
	flag.Parse()

	if len(configFlags) < 1 {
		log.Panicf("Must specify at least one config path using -config")
	}

	cfg, err := config.Load(configFlags...)
	if err != nil {
		log.Panicf("%s", err)
	}
	err = cfg.Validate()
	if err != nil {
		log.Panicf("Failed validating config: %s", err)
	}

	// Init db
	db, err := gorm.Open(
		"postgres",
		fmt.Sprintf("host=%s port=%d user=%s dbname=%s password=%s sslmode=disable",
			cfg.DbConfig.Host,
			cfg.DbConfig.Port,
			cfg.DbConfig.User,
			cfg.DbConfig.Database,
			cfg.DbConfig.Password))
	if err != nil {
		panic(err)
	}
	db.LogMode(false)
	defer db.Close()

	// Initialise NewRelic
	if err := newrelic.InitNewRelic(cfg.NewRelicConfig); err != nil {
		log.Errorf("Failed to initialize newrelic: %s", err)
	}
	defer newrelic.Shutdown(5 * time.Second)

	// Init auth enforcer, vault client
	var authEnforcer *enforcer.Enforcer
	if cfg.AuthConfig.Enabled {
		// Use product mlp as the policies are shared across the mlp products.
		ae, err := enforcer.NewEnforcerBuilder().Product("mlp").
			URL(cfg.AuthConfig.URL).Build()
		if err != nil {
			log.Panicf("Failed initializing authorization enforcer %v", err)
		}
		authEnforcer = &ae
	}
	vaultClient, err := vault.NewClientFromConfig(cfg)
	if err != nil {
		log.Panicf("Failed initializing vault client: %v", err)
	}

	// Init app context
	appCtx, err := api.NewAppContext(db, cfg, authEnforcer, vaultClient)
	if err != nil {
		log.Panicf("Failed initializing application context: %v", err)
	}

	// Init Sentry client
	if cfg.Sentry.Enabled {
		cfg.Sentry.Labels = map[string]string{
			"environment": cfg.DeployConfig.EnvironmentType,
			"app":         "turing-api",
		}
		if err := sentry.InitSentry(cfg.Sentry); err != nil {
			log.Errorf("Failed initializing sentry client: %s", err)
		}
		defer sentry.Close()
	}

	// Run batch runners
	go batchrunner.RunBatchRunners(
		cfg.BatchRunnerConfig.TimeInterval,
		appCtx.BatchRunners,
	)

	// Register handlers
	health := healthcheck.NewHandler()
	health.AddReadinessCheck("database", healthcheck.DatabasePingCheck(db.DB(), 1*time.Second))

	mux := http.NewServeMux()
	mux.Handle("/v1/internal/", http.StripPrefix("/v1/internal", health))
	mux.Handle("/v1/", http.StripPrefix("/v1", api.NewRouter(appCtx)))

	if len(cfg.SwaggerFile) > 0 {
		// Serve Swagger Spec
		mux.Handle("/openapi.yaml", web.FileHandler(cfg.SwaggerFile, false))
	}
	// Serve UI
	if cfg.TuringUIConfig.AppDirectory != "" {
		log.Infof(
			"Serving Turing UI from %s at %s",
			cfg.TuringUIConfig.AppDirectory,
			cfg.TuringUIConfig.Homepage)
		web.ServeReactApp(mux, cfg.TuringUIConfig.Homepage, cfg.TuringUIConfig.AppDirectory)
	}

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: cfg.AllowedOrigins,
	})

	// Define custom HTTP server
	httpServer := &http.Server{
		Addr:    cfg.ListenAddress(),
		Handler: corsHandler.Handler(mux),
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
			log.Errorf("Failed to shutdown server: %s", err)
		}
	}()

	log.Infof("Listening on port %d", cfg.Port)
	err = httpServer.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Errorf("Failed to start turing-api: %s", err)
		close(idleConnsClosed)
	}

	<-idleConnsClosed
}
