package main

//go:generate go run github.com/google/wire/cmd/wire

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
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

	setupMode := flag.Bool("setup", false, "Run setup wizard in CLI mode")
	showVersion := flag.Bool("version", false, "Show version information")
	consoleMode := flag.Bool("console", false, "Run Personal Edition in the foreground console")
	flag.Parse()

	if *showVersion {
		log.Printf("Sub2API %s (commit: %s, built: %s, build-type: %s)\n", Version, Commit, Date, BuildType)
		return
	}

	desktopMode := BuildType == "personal" && personal.DesktopSupported() && !*consoleMode
	if desktopMode {
		releaseInstance, alreadyRunning, err := personal.AcquireDesktopInstance()
		if err != nil {
			log.Printf("Failed to acquire Windows desktop instance lock: %v", err)
			return
		}
		if alreadyRunning {
			log.Println("Sub2API Personal desktop is already running")
			return
		}
		defer releaseInstance()
	}

	// Personal Edition is the only runtime shipped by this branch. Desktop
	// mode acquires its single-instance guard before touching runtime data.
	personal.PrepareEnvironment("personal")

	log.Println("Personal Edition runtime enabled: private routes + upstream SIMPLE semantics")

	setupCompleted := false
	if *setupMode || setup.NeedsSetup() {
		log.Println("Personal Edition setup uses the local owner-only browser wizard")
		if err := runSetupServer(); err != nil {
			log.Printf("Setup server failed: %v", err)
			return
		}
		setupCompleted = true
	}

	if desktopMode {
		runDesktopServer(!setupCompleted)
		return
	}
	runMainServer(true)
}

func runSetupServer() error {
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(config.CORSConfig{}))
	r.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: true, Policy: config.DefaultCSPPolicy}, nil))

	personalInstalled := make(chan struct{}, 1)
	setup.RegisterPersonalRoutes(r, func() {
		select {
		case personalInstalled <- struct{}{}:
		default:
		}
	})

	if web.HasEmbeddedFrontend() {
		r.Use(web.ServeEmbeddedFrontend())
	}

	addr := config.GetServerAddress()
	log.Printf("Setup wizard available at http://%s", addr)
	log.Println("Create the Personal Edition owner account to finish setup")

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

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()
	personal.OpenLocalBrowser(addr, "/setup")

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("start setup server: %w", err)
		}
		return nil
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

	return nil
}

func runMainServer(openBrowser bool) {
	controller := &gatewayController{}
	if err := controller.Start(openBrowser); err != nil {
		log.Printf("Failed to start Gateway: %v", err)
		return
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	if err := controller.Stop(); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func runDesktopServer(openBrowser bool) {
	controller := &gatewayController{}
	if err := controller.Start(openBrowser); err != nil {
		log.Printf("Failed to start Gateway: %v", err)
	}
	callbacks := personal.DesktopCallbacks{
		Running: controller.Running,
		StartGateway: func() error {
			return controller.Start(false)
		},
		StopGateway: controller.Stop,
		RestartGateway: func() error {
			if err := controller.Stop(); err != nil {
				return err
			}
			return controller.Start(false)
		},
		OpenManagement: controller.OpenManagement,
		OpenLogs:       personal.OpenPersonalLogs,
	}
	if err := personal.RunDesktop(callbacks); err != nil {
		log.Printf("Windows tray stopped with error: %v", err)
	}
	_ = controller.Stop()
}
