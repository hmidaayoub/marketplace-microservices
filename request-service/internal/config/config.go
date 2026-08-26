// Package config loads runtime configuration from the environment, using the same
// variable names as the Java services so one compose file can configure all of them.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          []byte
	InternalAPIKey     string
	CustomerServiceURL string
	RabbitMQURL        string
	HTTPTimeout        time.Duration
	ShutdownTimeout    time.Duration
}

// Load reads configuration and refuses to start when a security-critical value is
// missing. JWT_SECRET and INTERNAL_API_KEY have no safe default: defaulting either one
// would let a misconfigured deployment come up serving unauthenticated traffic.
func Load() (Config, error) {
	cfg := Config{
		Port:               env("SERVER_PORT", "8084"),
		CustomerServiceURL: strings.TrimRight(env("CUSTOMER_SERVICE_URL", "http://localhost:8082"), "/"),
		// Unlike the two secrets below this has a working default: a missing broker
		// costs a notification, not safety, so it must not stop the service starting.
		RabbitMQURL:     env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		HTTPTimeout:     5 * time.Second,
		ShutdownTimeout: 15 * time.Second,
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}
	cfg.JWTSecret = []byte(secret)

	key := os.Getenv("INTERNAL_API_KEY")
	if key == "" {
		return Config{}, fmt.Errorf("INTERNAL_API_KEY is required")
	}
	cfg.InternalAPIKey = key

	if raw := os.Getenv("DATABASE_URL"); raw != "" {
		cfg.DatabaseURL = raw
	} else {
		cfg.DatabaseURL = (&url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(env("DB_USER", "postgres"), env("DB_PASSWORD", "postgres")),
			Host:     hostPort(env("DB_HOST", "localhost"), env("DB_PORT", "5432")),
			Path:     "/" + env("DB_NAME", "request_db"),
			RawQuery: "sslmode=" + env("DB_SSLMODE", "disable"),
		}).String()
	}

	return cfg, nil
}

func hostPort(host, port string) string { return host + ":" + port }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
