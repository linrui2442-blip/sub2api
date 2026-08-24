package main

import (
	"net"
	"path/filepath"
	"strconv"
	"testing"

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
	for cycle := 0; cycle < 2; cycle++ {
		require.NoError(t, controller.Start(false))
		require.True(t, controller.Running())
		require.NoError(t, controller.Stop())
		require.False(t, controller.Running())
	}
}

func reserveLocalPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}
