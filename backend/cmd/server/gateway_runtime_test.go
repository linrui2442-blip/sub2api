package main

import (
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/personal"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestGatewayControllerStartStopRestart(t *testing.T) {
	port := reserveLocalPort(t)
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	t.Setenv("SUB2_PERSONAL_DATA_DIR", t.TempDir())
	t.Setenv("SUB2_PERSONAL_SQLITE_PATH", filepath.Join(t.TempDir(), "desktop-lifecycle.db"))
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", port)
	t.Setenv("SKIP_SETUP", "1")

	require.True(t, personal.PrepareEnvironment("personal"))
	repository.ClosePersonalEmbeddedRedis()
	t.Cleanup(repository.ClosePersonalEmbeddedRedis)

	controller := &gatewayController{}
	t.Cleanup(func() { _ = controller.Stop() })
	for cycle := 0; cycle < 2; cycle++ {
		require.NoError(t, controller.Start(false))
		require.True(t, controller.Running())
		requireGatewayReachable(t, port)
		require.NoError(t, controller.Stop())
		require.False(t, controller.Running())
		requirePortReleased(t, port)
	}
}

func TestGatewayControllerRecoversAfterPortConflict(t *testing.T) {
	port := reserveLocalPort(t)
	t.Setenv("SUB2_PERSONAL_MODE", "1")
	t.Setenv("SUB2_PERSONAL_DATA_DIR", t.TempDir())
	t.Setenv("SUB2_PERSONAL_SQLITE_PATH", filepath.Join(t.TempDir(), "desktop-recovery.db"))
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", port)
	t.Setenv("SKIP_SETUP", "1")

	require.True(t, personal.PrepareEnvironment("personal"))
	repository.ClosePersonalEmbeddedRedis()
	t.Cleanup(repository.ClosePersonalEmbeddedRedis)

	blocker, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	require.NoError(t, err)

	controller := &gatewayController{}
	t.Cleanup(func() { _ = controller.Stop() })
	require.Error(t, controller.Start(false))
	require.False(t, controller.Running())
	require.NoError(t, blocker.Close())

	require.NoError(t, controller.Start(false))
	require.True(t, controller.Running())
	requireGatewayReachable(t, port)
	require.NoError(t, controller.Stop())
	requirePortReleased(t, port)
}

func reserveLocalPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func requireGatewayReachable(t *testing.T, port string) {
	t.Helper()
	url := "http://" + net.JoinHostPort("127.0.0.1", port) + "/"
	require.Eventually(t, func() bool {
		response, err := http.Get(url)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode >= http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)
}

func requirePortReleased(t *testing.T, port string) {
	t.Helper()
	address := net.JoinHostPort("127.0.0.1", port)
	require.Eventually(t, func() bool {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return false
		}
		return listener.Close() == nil
	}, 5*time.Second, 50*time.Millisecond)
}
