package config

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Environment      string        `env:"ENVIRONMENT" env-default:"local" validate:"oneof=local dev test prod"`
	DBConnectTimeout time.Duration `env:"DB_CONNECT_TIMEOUT" env-default:"10s" validate:"min=1s"`
	ShutdownTimeout  time.Duration `env:"SHUTDOWN_TIMEOUT" env-default:"10s" validate:"min=1s"`
	Log              LogConfig
	Server           ServerConfig
	Postgres         PostgresConfig
}

type LogConfig struct {
	Level   int    `env:"LOG_LEVEL" env-default:"0" validate:"oneof=-4 0 4 8"` // -4 = Debug, 0 = Info, 4 = Warn, 8 = Error
	Handler string `env:"LOG_HANDLER" env-default:"text" validate:"oneof=text json"`
}

type ServerConfig struct {
	Port              string        `env:"PORT" env-default:"8080" validate:"numeric"`
	ReadTimeout       time.Duration `env:"READ_TIMEOUT" env-default:"10s" validate:"min=100ms"`
	WriteTimeout      time.Duration `env:"WRITE_TIMEOUT" env-default:"10s" validate:"min=100ms"`
	IdleTimeout       time.Duration `env:"IDLE_TIMEOUT" env-default:"60s" validate:"min=1s"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" env-default:"5s" validate:"min=100ms"`
}

type PostgresConfig struct {
	Host            string        `env:"POSTGRES_HOST" env-required:"true" validate:"hostname|ip"`
	Port            string        `env:"POSTGRES_PORT" env-required:"true" validate:"numeric"`
	User            string        `env:"POSTGRES_USER" env-required:"true"`
	Password        string        `env:"POSTGRES_PASSWORD" env-required:"true"`
	Name            string        `env:"POSTGRES_DB" env-required:"true"`
	SSLMode         string        `env:"POSTGRES_SSLMODE" env-default:"disable" validate:"oneof=disable allow prefer require verify-ca verify-full"`
	MaxOpenConns    int32         `env:"POSTGRES_MAX_OPEN_CONNS" env-default:"10" validate:"min=1"`
	MaxIdleConns    int32         `env:"POSTGRES_MAX_IDLE_CONNS" env-default:"5" validate:"min=1"`
	ConnMaxLifetime time.Duration `env:"POSTGRES_CONN_MAX_LIFETIME" env-default:"1h" validate:"min=1m"`
	DSN             string
}

func MustLoad() *Config {
	var cfg Config

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		log.Fatalf("cannot read config: %v", err)
	}

	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}

	if err := cfg.validateLogic(); err != nil {
		log.Fatalf("config logic error: %v", err)
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

func (cfg *Config) validateLogic() error {
	if cfg.Postgres.MaxIdleConns > cfg.Postgres.MaxOpenConns {
		return fmt.Errorf("postgres: max_idle_conns (%d) cannot be greater than max_open_conns (%d)",
			cfg.Postgres.MaxIdleConns, cfg.Postgres.MaxOpenConns)
	}
	return nil
}
