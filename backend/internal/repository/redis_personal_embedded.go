package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// personalEmbeddedRedis is a compatibility bridge for Personal Edition.
// It keeps the existing Redis-backed scheduler/cache contracts intact while
// removing the need for a separately installed Redis process. miniredis is
// normally a test server and does not advance TTLs automatically, so this
// wrapper advances its logical TTL clock using wall-clock time.
type personalEmbeddedRedis struct {
	server *miniredis.Miniredis
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

var (
	personalEmbeddedRedisMu      sync.Mutex
	personalEmbeddedRedisRuntime *personalEmbeddedRedis
)

func newPersonalEmbeddedRedis() (*personalEmbeddedRedis, error) {
	mr := miniredis.NewMiniRedis()
	if err := mr.StartAddr("127.0.0.1:0"); err != nil {
		return nil, err
	}

	r := &personalEmbeddedRedis{
		server: mr,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	mr.SetTime(time.Now())
	go r.runClock()
	return r, nil
}

func (r *personalEmbeddedRedis) Client() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         r.server.Addr(),
		DB:           0,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})
}

func (r *personalEmbeddedRedis) runClock() {
	defer close(r.done)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	last := time.Now()

	advance := func(now time.Time) {
		elapsed := now.Sub(last)
		if elapsed > 0 {
			r.server.FastForward(elapsed)
		}
		r.server.SetTime(now)
		last = now
	}

	for {
		select {
		case now := <-ticker.C:
			advance(now)
		case <-r.stop:
			advance(time.Now())
			return
		}
	}
}

func (r *personalEmbeddedRedis) Close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		close(r.stop)
		<-r.done
		r.server.Close()
	})
}

func getPersonalEmbeddedRedis() (*personalEmbeddedRedis, error) {
	personalEmbeddedRedisMu.Lock()
	defer personalEmbeddedRedisMu.Unlock()
	if personalEmbeddedRedisRuntime != nil {
		return personalEmbeddedRedisRuntime, nil
	}
	runtime, err := newPersonalEmbeddedRedis()
	if err != nil {
		return nil, err
	}
	personalEmbeddedRedisRuntime = runtime
	return runtime, nil
}

func personalEmbeddedRedisClient() *redis.Client {
	runtime, err := getPersonalEmbeddedRedis()
	if err != nil {
		// InitRedis historically cannot return an error. Failing to bind a random
		// loopback port means the Personal runtime cannot safely start, so fail
		// immediately instead of silently falling back to an external Redis.
		panic(fmt.Sprintf("start Personal Edition embedded Redis: %v", err))
	}
	return runtime.Client()
}

// ClosePersonalEmbeddedRedis is idempotent and is exposed for tests and the
// eventual Personal Edition shutdown path. Normal process exit also releases
// the loopback listener.
func ClosePersonalEmbeddedRedis() {
	personalEmbeddedRedisMu.Lock()
	runtime := personalEmbeddedRedisRuntime
	personalEmbeddedRedisRuntime = nil
	personalEmbeddedRedisMu.Unlock()
	if runtime != nil {
		runtime.Close()
	}
}

func pingPersonalEmbeddedRedis(ctx context.Context, client *redis.Client) error {
	return client.Ping(ctx).Err()
}
