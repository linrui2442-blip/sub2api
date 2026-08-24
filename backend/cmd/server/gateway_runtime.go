package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/repository"
)

type gatewayRuntime struct {
	app         *Application
	done        chan struct{}
	serveErr    error
	errMu       sync.RWMutex
	cleanupOnce sync.Once
}

func startGatewayRuntime(openBrowser bool) (*gatewayRuntime, error) {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := logger.Init(logger.OptionsFromConfig(cfg.Log)); err != nil {
		return nil, fmt.Errorf("initialize logger: %w", err)
	}

	app, err := initializePersonalApplication(handler.BuildInfo{Version: Version, BuildType: BuildType})
	if err != nil {
		logger.Close()
		return nil, fmt.Errorf("initialize application: %w", err)
	}
	if app.PromptAudit != nil {
		if err := app.PromptAudit.Start(context.Background()); err != nil {
			log.Printf("Prompt Audit started in degraded state: %v", err)
		}
	}

	listener, err := net.Listen("tcp", app.Server.Addr)
	if err != nil {
		app.Cleanup()
		repository.ClosePersonalEmbeddedRedis()
		logger.Close()
		return nil, fmt.Errorf("listen on %s: %w", app.Server.Addr, err)
	}

	runtime := &gatewayRuntime{app: app, done: make(chan struct{})}
	go func() {
		err := app.Server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtime.errMu.Lock()
			runtime.serveErr = err
			runtime.errMu.Unlock()
		}
		close(runtime.done)
	}()

	log.Printf("Server started on %s", app.Server.Addr)
	if openBrowser {
		personal.OpenLocalBrowser(app.Server.Addr, "/admin/accounts")
	}
	return runtime, nil
}

func (r *gatewayRuntime) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := r.app.Server.Shutdown(ctx)
	cancel()
	<-r.done
	r.cleanup()
	if err != nil {
		return fmt.Errorf("shutdown gateway: %w", err)
	}
	return r.err()
}

func (r *gatewayRuntime) cleanup() {
	r.cleanupOnce.Do(func() {
		r.app.Cleanup()
		repository.ClosePersonalEmbeddedRedis()
		logger.Close()
	})
}

func (r *gatewayRuntime) err() error {
	r.errMu.RLock()
	defer r.errMu.RUnlock()
	return r.serveErr
}

type gatewayController struct {
	mu      sync.Mutex
	runtime *gatewayRuntime
}

func (c *gatewayController) Start(openBrowser bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runtime != nil {
		if openBrowser {
			personal.OpenLocalBrowser(c.runtime.app.Server.Addr, "/admin/accounts")
		}
		return nil
	}
	runtime, err := startGatewayRuntime(openBrowser)
	if err != nil {
		return err
	}
	c.runtime = runtime
	go c.watch(runtime)
	return nil
}

func (c *gatewayController) watch(runtime *gatewayRuntime) {
	<-runtime.done
	runtime.cleanup()
	c.mu.Lock()
	if c.runtime == runtime {
		c.runtime = nil
	}
	c.mu.Unlock()
	if err := runtime.err(); err != nil {
		log.Printf("Gateway stopped unexpectedly: %v", err)
	}
}

func (c *gatewayController) Stop() error {
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	if runtime == nil {
		return nil
	}
	err := runtime.stop()
	c.mu.Lock()
	if c.runtime == runtime {
		c.runtime = nil
	}
	c.mu.Unlock()
	return err
}

func (c *gatewayController) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runtime != nil
}

func (c *gatewayController) OpenManagement() {
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	if runtime != nil {
		personal.OpenLocalBrowser(runtime.app.Server.Addr, "/admin/accounts")
	}
}
