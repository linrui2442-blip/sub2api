package main

//go:generate go run github.com/google/wire/cmd/wire

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/Wei-Shaw/sub2api/internal/web"

	"github.com/gin-gonic/gin"
)

//go:embed VERSION
var embeddedVersion string

// Build-time variables (can be set by ldflags)
var (
	Version   = ""
	Commit    = "unknown"
	Date      = "unknown"
	BuildType = "source" // "source" for manual builds, "release"/"personal" for CI builds
)

func init() {
	if strings.TrimSpace(Version) != "" {
		return
	}
	Version = strings.TrimSpace(embeddedVersion)
	if Version == "" {
		Version = "0.0.0-dev"
	}
}

func main() {
	logger.InitBootstrap()
	defer logger.Sync()

	// Dedicated Personal Edition builds activate themselves automatically.
	// Source builds can opt in with SUB2_PERSONAL_MODE=1.
	personalRuntime := personal.PrepareEnvironment(BuildType)

	setupMode := flag.Bool("setup", false, "Run setup wizard in CLI mode")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		log.Printf("Sub2API %s (commit: %s, built: %s, build-type: %s)\n", Version, Commit, Date, BuildType)
		return
	}

	if personalRuntime {
		log.Println("Personal Edition runtime enabled: private routes + upstream SIMPLE semantics")
	}

	if *setupMode {
		if personalRuntime {
			log.Println("Personal Edition setup uses the local owner-only browser wizard")
			runSetupServer()
			return
		}
		if err := setup.RunCLI(); err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
		return
	}

	if setup.NeedsSetup() {
		// Personal Edition never enters upstream PostgreSQL/Redis auto-setup.
		if personalRuntime {
			log.Println("Personal Edition first run detected; starting owner setup wizard...")
			runSetupServer()
			return
		}

		if setup.AutoSetupEnabled() {
			log.Println("Auto setup mode enabled...")
			if err := setup.AutoSetupFromEnv(); err != nil {
				log.Fatalf("Auto setup failed: %v", err)
			}
		} else {
			log.Println("First run detected, starting setup wizard...")
			runSetupServer()
			return
		}
	}

	runMainServer(personalRuntime)
}

func runSetupServer() {
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(config.CORSConfig{}))
	r.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: true, Policy: config.DefaultCSPPolicy}, nil))

	var personalInstalled chan struct{}
	if personal.Enabled() {
		personalInstalled = make(chan struct{}, 1)
		setup.RegisterPersonalRoutes(r, func() {
			select {
			case personalInstalled <- struct{}{}:
			default:
			}
		})
	} else {
		setup.RegisterRoutes(r)
	}

	if web.HasEmbeddedFrontend() {
		r.Use(web.ServeEmbeddedFrontend())
	}

	addr := config.GetServerAddress()
	log.Printf("Setup wizard available at http://%s", addr)
	if personal.Enabled() {
		log.Println("Create the Personal Edition owner account to finish setup")
	} else {
		log.Println("Complete the setup wizard to configure Sub2API")
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
		Protocols:         protocols,
	}

	if !personal.Enabled() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start setup server: %v", err)
		}
		return
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()
	personal.OpenLocalBrowser(addr, "/setup")

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start setup server: %v", err)
		}
		return
	case <-personalInstalled:
		log.Println("Personal Edition setup completed; switching to main gateway...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Failed to gracefully stop setup server: %v", err)
	}
	cancel()

	// Wait briefly for ListenAndServe to release the port before the full app
	// binds the same address. A graceful shutdown normally returns immediately.
	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Setup server stopped with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		log.Println("Setup server shutdown wait timed out; continuing to main gateway")
	}

	// The setup browser tab polls the main API and redirects itself to /login,
	// so do not open a second browser tab during this transition.
	runMainServer(false)
}

func runMainServer(openBrowser bool) {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := logger.Init(logger.OptionsFromConfig(cfg.Log)); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	if personal.Enabled() {
		log.Println("Personal Edition: public registration/payment/model-plaza routes are disabled")
	} else if cfg.RunMode == config.RunModeSimple {
		log.Println("⚠️  WARNING: Running in SIMPLE mode - billing and quota checks are DISABLED")
	}

	buildInfo := handler.BuildInfo{
		Version:   Version,
		BuildType: BuildType,
	}

	app, err := initializeApplication(buildInfo)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	// Keep the embedded Redis listener alive until all application services have
	// stopped and their Redis clients are closed. Defers execute LIFO, so app
	// cleanup runs first and the embedded server closes last.
	defer repository.ClosePersonalEmbeddedRedis()
	defer app.Cleanup()
	if app.PromptAudit != nil {
		if err := app.PromptAudit.Start(context.Background()); err != nil {
			log.Printf("Prompt Audit started in degraded state: %v", err)
		}
	}

	go func() {
		if err := app.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Printf("Server started on %s", app.Server.Addr)
	if openBrowser && personal.Enabled() {
		personal.OpenLocalBrowser(app.Server.Addr, "/login")
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
