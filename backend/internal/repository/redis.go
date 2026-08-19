package repository

import (
	"crypto/tls"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/personal"

	"github.com/redis/go-redis/v9"
)

// InitRedis initializes the Redis client used by upstream cache/scheduler code.
// Personal Edition uses an in-process Redis-compatible server so users do not
// need to install or operate an external Redis service. Standard/Simple modes
// keep the original external Redis configuration unchanged.
func InitRedis(cfg *config.Config) *redis.Client {
	if personal.Enabled() {
		return personalEmbeddedRedisClient()
	}

	client := redis.NewClient(buildRedisOptions(cfg))
	if cfg.Server.EnableServerTiming {
		client.AddHook(serverTimingRedisHook{})
	}
	return client
}

// buildRedisOptions builds Redis connection options for upstream deployments.
func buildRedisOptions(cfg *config.Config) *redis.Options {
	opts := &redis.Options{
		Addr:         cfg.Redis.Address(),
		Username:     cfg.Redis.Username,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  time.Duration(cfg.Redis.DialTimeoutSeconds) * time.Second,
		ReadTimeout:  time.Duration(cfg.Redis.ReadTimeoutSeconds) * time.Second,
		WriteTimeout: time.Duration(cfg.Redis.WriteTimeoutSeconds) * time.Second,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
	}

	if cfg.Redis.EnableTLS {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: cfg.Redis.Host,
		}
	}

	return opts
}
