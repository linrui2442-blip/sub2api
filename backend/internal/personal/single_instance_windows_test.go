//go:build windows

package personal

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcquireDesktopInstanceMutexRejectsDuplicateAndReleases(t *testing.T) {
	name := fmt.Sprintf(`Local\Sub2APIPersonalDesktopTest-%d-%d`, os.Getpid(), time.Now().UnixNano())

	releaseFirst, alreadyRunning, err := acquireDesktopInstanceMutex(name)
	require.NoError(t, err)
	require.False(t, alreadyRunning)
	require.NotNil(t, releaseFirst)

	releaseDuplicate, alreadyRunning, err := acquireDesktopInstanceMutex(name)
	require.NoError(t, err)
	require.True(t, alreadyRunning)
	require.Nil(t, releaseDuplicate)

	releaseFirst()

	releaseAfterClose, alreadyRunning, err := acquireDesktopInstanceMutex(name)
	require.NoError(t, err)
	require.False(t, alreadyRunning)
	require.NotNil(t, releaseAfterClose)
	releaseAfterClose()
}
