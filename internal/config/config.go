package config

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Environment      string        `env:"ENVIRONMENT" env-default:"local"`
	DBConnectTimeout time.Duration `env:"DB_CONNECT_TIMEOUT" env-default:"10s"`
	ShutdownTimeout  time.Duration `env:"SHUTDOWN_TIMEOUT" env-default:"10s"`
	Log              LogConfig
	Server           ServerConfig
	Postgres         PostgresConfig
}

type LogConfig struct {
	Level   int    `env:"LOG_LEVEL" env-default:"0"`      // -4 = Debug, 0 = Info, 4 = Warn, 8 = Error
	Handler string `env:"LOG_HANDLER" env-default:"text"` // text, json
}

type ServerConfig struct {
	Port              string        `env:"PORT" env-default:"8080"`
	ReadTimeout       time.Duration `env:"READ_TIMEOUT" env-default:"10s"`
	WriteTimeout      time.Duration `env:"WRITE_TIMEOUT" env-default:"10s"`
	IdleTimeout       time.Duration `env:"IDLE_TIMEOUT" env-default:"60s"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" env-default:"5s"`
}

type PostgresConfig struct {
	Host            string        `env:"POSTGRES_HOST" env-required:"true"`
	Port            string        `env:"POSTGRES_PORT" env-required:"true"`
	User            string        `env:"POSTGRES_USER" env-required:"true"`
	Password        string        `env:"POSTGRES_PASSWORD" env-required:"true"`
	Name            string        `env:"POSTGRES_DB" env-required:"true"`
	SSLMode         string        `env:"POSTGRES_SSLMODE" env-default:"disable"`
	MaxOpenConns    int32         `env:"POSTGRES_MAX_OPEN_CONNS" env-default:"10"`
	MaxIdleConns    int32         `env:"POSTGRES_MAX_IDLE_CONNS" env-default:"5"`
	ConnMaxLifetime time.Duration `env:"POSTGRES_CONN_MAX_LIFETIME" env-default:"1h"`
	DSN             string
}

func MustLoad() *Config {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		log.Fatalf("cannot read config: %v", err)
	}

	hostPort := net.JoinHostPort(cfg.Postgres.Host, cfg.Postgres.Port)
	cfg.Postgres.DSN = fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s",
		cfg.Postgres.User,
		cfg.Postgres.Password,
		hostPort,
		cfg.Postgres.Name,
		cfg.Postgres.SSLMode,
	)

	return &cfg
}
