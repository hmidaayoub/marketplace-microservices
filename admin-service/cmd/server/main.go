// Command server runs the Admin/Contact Service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/admin"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/auth"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/clients"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/config"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/db"
	"github.com/hmidaayoub/marketplace-microservices/admin-service/internal/events"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations run before the pool is used, so the schema a request sees is never
	// half-applied. Like Flyway in the Java services, the migration files are the only
	// thing that defines the schema.
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}

	startupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(startupCtx); err != nil {
		return err
	}

	// One shared client: connection reuse matters here because a single contact
	// lookup fans out to two services per granted customer.
	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}

	// The connection is opened on the first publish, not here: the service must start
	// whether or not the broker is up, and reconnect on its own if it restarts.
	publisher := events.NewPublisher(cfg.RabbitMQURL, "admin-service")
	defer func() { _ = publisher.Close() }()

	// Nothing publishes inline. Events are written to the outbox inside the business
	// transaction and this relay is the only thing that sends them, so a broker outage
	// delays a notification instead of losing it.
	relay := events.NewRelay(pool, publisher, "admin-service")
	relay.Start(ctx)

	handler := admin.NewHandler(admin.NewService(pool, admin.Deps{
		Offers:    clients.NewOffer(cfg.OfferServiceURL, cfg.InternalAPIKey, httpClient),
		Requests:  clients.NewRequest(cfg.RequestServiceURL, cfg.InternalAPIKey, httpClient),
		Sellers:   clients.NewSeller(cfg.SellerServiceURL, cfg.InternalAPIKey, httpClient),
		Customers: clients.NewCustomer(cfg.CustomerServiceURL, cfg.InternalAPIKey, httpClient),
		Auth:      clients.NewAuth(cfg.AuthServiceURL, cfg.InternalAPIKey, httpClient),
	}, relay))

	router := admin.NewRouter(admin.RouterConfig{
		Handler:        handler,
		Verifier:       auth.NewVerifier(cfg.JWTSecret),
		InternalAPIKey: cfg.InternalAPIKey,
		Ready:          func() error { return pool.Ping(context.Background()) },
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("admin-service listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	// The relay stops with ctx; wait for its current batch so shutdown does not race a
	// publish that is already in flight.
	relay.Wait()
	return nil
}
