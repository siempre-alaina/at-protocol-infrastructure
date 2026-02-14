package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/siempre-alaina/at-protocol/relay/internal/config"
	"github.com/siempre-alaina/at-protocol/relay/internal/firehose"
	"github.com/siempre-alaina/at-protocol/relay/internal/ingest"
	"github.com/siempre-alaina/at-protocol/relay/internal/metrics"
	"github.com/siempre-alaina/at-protocol/relay/internal/storage"
	"github.com/siempre-alaina/at-protocol/relay/internal/verify"
	"github.com/siempre-alaina/at-protocol/relay/pkg/health"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	debug := flag.Bool("debug", false, "Enable debug logging")
	jsonLogs := flag.Bool("json", false, "Output logs as JSON")
	showVersion := flag.Bool("version", false, "Show version and exit")
	enableVerify := flag.Bool("verify", false, "Enable signature verification (requires network for DID resolution)")
	enableDB := flag.Bool("db", false, "Enable PostgreSQL storage for events and cursors")
	flag.Parse()

	if *showVersion {
		fmt.Printf("relay version %s (commit: %s, built: %s)\n", Version, GitCommit, BuildTime)
		os.Exit(0)
	}

	// Set up logging
	setupLogging(*debug, *jsonLogs)

	log.Info().
		Str("version", Version).
		Str("commit", GitCommit).
		Msg("Starting AT Protocol Relay")

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	log.Info().
		Str("address", cfg.Server.Address()).
		Int("worker_count", cfg.Ingest.WorkerCount).
		Strs("initial_hosts", cfg.Ingest.InitialHosts).
		Msg("Configuration loaded")

	// Initialize metrics
	m := metrics.New()

	// Initialize database (if enabled)
	var db *storage.DB
	var cursorStore *storage.CursorStore
	var eventStore *storage.EventStore
	if *enableDB {
		connMaxLifetime, err := time.ParseDuration(cfg.Database.ConnMaxLifetime)
		if err != nil {
			log.Fatal().Err(err).Str("value", cfg.Database.ConnMaxLifetime).Msg("Invalid conn_max_lifetime duration")
		}
		db, err = storage.NewDB(
			cfg.Database.PostgresURL,
			cfg.Database.MaxOpenConns,
			cfg.Database.MaxIdleConns,
			connMaxLifetime,
		)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to connect to database")
		}

		// Run migrations
		if err := db.Migrate(context.Background()); err != nil {
			log.Fatal().Err(err).Msg("Failed to run database migrations")
		}

		cursorStore = storage.NewCursorStore(db)
		eventStore = storage.NewEventStore(db)

		log.Info().
			Str("url", cfg.Database.PostgresURL).
			Msg("Database connected and migrated")
	}

	// Initialize firehose server
	firehoseServer := firehose.NewServer(eventStore, m)
	firehoseServer.Start()

	// Initialize verification pipeline (if enabled)
	var verifyPipeline *verify.Pipeline
	if *enableVerify {
		verifyPipeline = verify.NewPipeline(cfg.Identity, m)
		log.Info().
			Str("plc_url", cfg.Identity.PLCURL).
			Int("cache_size", cfg.Identity.CacheSize).
			Msg("Verification pipeline initialized")
	}

	// Initialize health handler
	healthHandler := health.New(Version)

	// Event counter for logging
	var eventCount atomic.Int64

	// Event handler - processes events from all PDSs
	eventHandler := func(ctx context.Context, event ingest.Event) error {
		count := eventCount.Add(1)

		// Log every 100th event at info level, others at debug
		if count%100 == 0 {
			log.Info().
				Int64("total_events", count).
				Str("type", event.Type).
				Str("host", event.Host).
				Msg("Processing events")
		} else {
			log.Debug().
				Str("type", event.Type).
				Str("did", event.DID).
				Int64("seq", event.Seq).
				Str("host", event.Host).
				Msg("Event received")
		}

		// Phase 3: Verification (if enabled)
		if verifyPipeline != nil && event.Type == "commit" {
			result := verifyPipeline.Verify(ctx, event)
			if !result.Valid {
				log.Warn().
					Str("did", event.DID).
					Str("host", event.Host).
					Err(result.Error).
					Dur("duration", result.Duration).
					Msg("Event verification failed")
				// For now, log but don't reject - allows testing without blocking
				// In production, you may want to return an error here
			} else if count%100 == 0 {
				log.Debug().
					Str("did", event.DID).
					Str("identity", result.Identity).
					Dur("duration", result.Duration).
					Msg("Event verified")
			}
		}

		// Phase 4: Store to PostgreSQL (if enabled)
		var seq int64
		if eventStore != nil {
			var err error
			seq, err = eventStore.StoreEvent(ctx, event.Type, event.DID, event.CommitCID, event.PrevCID, event.Host, event.RawData, event.RecordPath, event.RecordContent)
			if err != nil {
				log.Error().Err(err).Str("did", event.DID).Msg("Failed to store event")
			} else {
				m.CurrentSequence.Set(float64(seq))
				if count%100 == 0 {
					log.Debug().Int64("seq", seq).Str("did", event.DID).Str("content", event.RecordContent).Msg("Event stored")
				}
			}
		}

		// Phase 5: Broadcast to firehose clients
		if seq > 0 {
			firehoseServer.BroadcastWithSeq(event, seq)
		} else {
			firehoseServer.Broadcast(event)
		}

		return nil
	}

	// Initialize ingest manager with cursor store (if database enabled)
	ingestManager := ingest.NewManager(cfg.Ingest, eventHandler, cursorStore, m)

	// Start ingest manager
	if err := ingestManager.Start(); err != nil {
		log.Fatal().Err(err).Msg("Failed to start ingest manager")
	}

	// Register health check for ingest
	healthHandler.RegisterCheck("ingest", func() health.CheckResult {
		hosts := ingestManager.GetHosts()
		connectedCount := 0
		for _, host := range hosts {
			connected, _, _, _ := ingestManager.GetHostStatus(host)
			if connected {
				connectedCount++
			}
		}

		if connectedCount == 0 && len(hosts) > 0 {
			return health.CheckResult{
				Status:  health.StatusUnhealthy,
				Message: fmt.Sprintf("0/%d PDSs connected", len(hosts)),
			}
		} else if connectedCount < len(hosts) {
			return health.CheckResult{
				Status:  health.StatusDegraded,
				Message: fmt.Sprintf("%d/%d PDSs connected", connectedCount, len(hosts)),
			}
		}
		return health.CheckResult{
			Status:  health.StatusHealthy,
			Message: fmt.Sprintf("%d PDSs connected", connectedCount),
		}
	})

	// Register health check for database (if enabled)
	if db != nil {
		healthHandler.RegisterCheck("database", func() health.CheckResult {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				return health.CheckResult{
					Status:  health.StatusUnhealthy,
					Message: fmt.Sprintf("Database ping failed: %v", err),
				}
			}
			seq, _ := eventStore.GetCurrentSeq(context.Background())
			return health.CheckResult{
				Status:  health.StatusHealthy,
				Message: fmt.Sprintf("Connected, seq: %d", seq),
			}
		})
	}

	// Register health check for firehose
	healthHandler.RegisterCheck("firehose", func() health.CheckResult {
		clientCount := firehoseServer.ClientCount()
		return health.CheckResult{
			Status:  health.StatusHealthy,
			Message: fmt.Sprintf("%d clients connected", clientCount),
		}
	})

	// Register health check for verification (if enabled)
	if verifyPipeline != nil {
		healthHandler.RegisterCheck("verification", func() health.CheckResult {
			verified, failed := verifyPipeline.Stats()
			total := verified + failed
			if total == 0 {
				return health.CheckResult{
					Status:  health.StatusHealthy,
					Message: "No events verified yet",
				}
			}

			failRate := float64(failed) / float64(total) * 100
			if failRate > 50 {
				return health.CheckResult{
					Status:  health.StatusUnhealthy,
					Message: fmt.Sprintf("%.1f%% verification failure rate (%d/%d)", failRate, failed, total),
				}
			} else if failRate > 10 {
				return health.CheckResult{
					Status:  health.StatusDegraded,
					Message: fmt.Sprintf("%.1f%% verification failure rate (%d/%d)", failRate, failed, total),
				}
			}
			return health.CheckResult{
				Status:  health.StatusHealthy,
				Message: fmt.Sprintf("%d verified, %d failed", verified, failed),
			}
		})
	}

	// Create main HTTP server mux
	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/xrpc/_health", healthHandler.HealthHandler())
	mux.HandleFunc("/health", healthHandler.HealthHandler())
	mux.HandleFunc("/ready", healthHandler.ReadyHandler())
	mux.HandleFunc("/live", healthHandler.LiveHandler())

	// Stats endpoint
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := make(map[string]interface{})
		stats["total_events"] = eventCount.Load()

		hosts := ingestManager.GetHosts()
		hostStats := make(map[string]interface{})
		for _, host := range hosts {
			connected, cursor, eventsCount, lastEvent := ingestManager.GetHostStatus(host)
			hostStats[host] = map[string]interface{}{
				"connected":    connected,
				"cursor":       cursor,
				"events_count": eventsCount,
				"last_event":   lastEvent,
			}
		}
		stats["hosts"] = hostStats

		bufSize, bufCap := ingestManager.BufferStats()
		stats["buffer"] = map[string]interface{}{
			"size":     bufSize,
			"capacity": bufCap,
		}

		// Add verification stats if enabled
		if verifyPipeline != nil {
			verified, failed := verifyPipeline.Stats()
			stats["verification"] = map[string]interface{}{
				"enabled":  true,
				"verified": verified,
				"failed":   failed,
			}
		} else {
			stats["verification"] = map[string]interface{}{
				"enabled": false,
			}
		}

		// Add database stats if enabled
		if eventStore != nil {
			seq, _ := eventStore.GetCurrentSeq(r.Context())
			stats["database"] = map[string]interface{}{
				"enabled":         true,
				"current_seq":     seq,
				"events_stored":   seq, // seq starts at 1
			}
		} else {
			stats["database"] = map[string]interface{}{
				"enabled": false,
			}
		}

		// Add firehose stats
		stats["firehose"] = map[string]interface{}{
			"clients":     firehoseServer.ClientCount(),
			"client_info": firehoseServer.GetClientInfo(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// Firehose WebSocket endpoint
	mux.HandleFunc("/xrpc/com.atproto.sync.subscribeRepos", firehoseServer.Handler())

	mux.HandleFunc("/xrpc/com.atproto.sync.requestCrawl", func(w http.ResponseWriter, r *http.Request) {
		hostname := r.URL.Query().Get("hostname")
		if hostname == "" {
			http.Error(w, "hostname parameter required", http.StatusBadRequest)
			return
		}

		if err := ingestManager.AddHost(hostname); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Main server
	server := &http.Server{
		Addr:         cfg.Server.Address(),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Metrics server (separate port)
	var metricsServer *http.Server
	if cfg.Metrics.Enabled {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", metrics.Handler())

		metricsServer = &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Metrics.Port),
			Handler: metricsMux,
		}

		go func() {
			log.Info().Int("port", cfg.Metrics.Port).Msg("Starting metrics server")
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("Metrics server error")
			}
		}()
	}

	// Start main server
	go func() {
		log.Info().Str("address", cfg.Server.Address()).Msg("Starting main server")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server error")
		}
	}()

	// Mark as ready after startup
	healthHandler.SetReady(true)
	log.Info().Msg("Relay is ready")

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down...")
	healthHandler.SetReady(false)

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop ingest manager first (stop receiving new events)
	if err := ingestManager.Stop(); err != nil {
		log.Error().Err(err).Msg("Ingest manager shutdown error")
	}

	// Stop firehose server
	firehoseServer.Stop()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}

	if metricsServer != nil {
		if err := metricsServer.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Metrics server shutdown error")
		}
	}

	// Close database connection
	if db != nil {
		if err := db.Close(); err != nil {
			log.Error().Err(err).Msg("Database close error")
		}
	}

	log.Info().Int64("total_events_processed", eventCount.Load()).Msg("Relay stopped")
}

func setupLogging(debug bool, jsonFormat bool) {
	// Set log level
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Set output format
	if jsonFormat {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()
	} else {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}
}
