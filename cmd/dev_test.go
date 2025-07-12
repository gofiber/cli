package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Dev_Escort_New(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, newEscort(config{}))
}

func Test_Dev_Escort_Init(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	e := getEscort()
	require.NoError(t, e.init())

	at.Contains(e.root, "cli")
	at.NotEmpty(e.binPath)
	if runtime.GOOS != windowsOS {
		require.NoError(t, os.Remove(e.binPath))
	}
}

func Test_Dev_Escort_Run(t *testing.T) {
	setupCmd()
	defer teardownCmd()

	e := getEscort()

	var err error
	e.root, err = os.MkdirTemp("", "test_run")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(e.root))
	}()

	e.sig = make(chan os.Signal, 1)

	go func() {
		time.Sleep(time.Millisecond * 500)
		e.sig <- syscall.SIGINT
	}()

	require.NoError(t, e.run())
}

func Test_Dev_Escort_RunBin(t *testing.T) {
	setupCmd(errFlag)
	defer teardownCmd()

	e := getEscort()

	e.bin = exec.Command("go", "version")
	_, err := e.bin.CombinedOutput()
	require.NoError(t, err)

	rc := io.NopCloser(strings.NewReader(""))
	e.stdoutPipe = rc
	e.stderrPipe = rc

	e.runBin()
}

func Test_Dev_Escort_WatchingPipes(t *testing.T) {
	t.Parallel()

	e := getEscort()
	e.bin = exec.Command("go", "version")
	_, err := e.bin.CombinedOutput()
	require.NoError(t, err)
	e.watchingPipes()
}

func Test_Dev_Escort_WatchingBin(t *testing.T) {
	t.Parallel()

	var count int32
	e := getEscort()
	e.delay = time.Millisecond * 50
	e.hitCh = make(chan struct{})
	e.hitFunc = func() { atomic.AddInt32(&count, 1) }

	go e.watchingBin()

	e.hitCh <- struct{}{}
	e.hitCh <- struct{}{}
	time.Sleep(time.Millisecond * 75)
	e.hitCh <- struct{}{}
	time.Sleep(time.Millisecond * 75)

	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
}

func Test_Dev_Escort_WatchingFiles(t *testing.T) {
	t.Parallel()

	var (
		at  = assert.New(t)
		err error
	)

	e := getEscort()
	e.hitCh = make(chan struct{}, 2)

	e.w, err = fsnotify.NewWatcher()
	require.NoError(t, err)
	defer func() { require.NoError(t, e.w.Close()) }()

	e.extensions = []string{"go"}
	e.watcherEvents = make(chan fsnotify.Event)
	e.watcherErrors = make(chan error)

	e.root, err = os.MkdirTemp("", "test_watch")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(e.root))
	}()

	_, err = os.MkdirTemp(e.root, ".git")
	require.NoError(t, err)

	newDir, err := os.MkdirTemp(e.root, "")
	require.NoError(t, err)

	ignoredFile, err := os.MkdirTemp(e.root, "")
	require.NoError(t, err)
	e.excludeFiles = []string{filepath.Base(ignoredFile)}

	f, err := os.CreateTemp(e.root, "*.go")
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	name := f.Name()

	go e.watchingFiles()

	e.watcherErrors <- errors.New("fake error")
	e.watcherEvents <- fsnotify.Event{Name: name, Op: fsnotify.Chmod}
	e.watcherEvents <- fsnotify.Event{Name: name, Op: fsnotify.Remove}
	e.watcherEvents <- fsnotify.Event{Name: name + "non", Op: fsnotify.Create}
	e.watcherEvents <- fsnotify.Event{Name: newDir, Op: fsnotify.Create}
	select {
	case <-e.hitCh:
	case <-time.NewTimer(time.Second).C:
		at.Fail("should hit")
	}

	e.watcherEvents <- fsnotify.Event{Name: ignoredFile, Op: fsnotify.Create}
	e.watcherEvents <- fsnotify.Event{Name: name, Op: fsnotify.Create}

	e.terminate()

	select {
	case <-e.hitCh:
	case <-time.NewTimer(time.Second).C:
		at.Fail("should hit")
	}
}

func Test_Dev_Escort_WalkForWatcher(t *testing.T) {
	t.Parallel()

	e := getEscort()

	e.walkForWatcher(" ")
}

func Test_Dev_Escort_HitExtensions(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	e := getEscort()
	e.extensions = []string{"go"}

	at.False(e.hitExtension(""))
	at.True(e.hitExtension(".go"))
	at.False(e.hitExtension(".js"))
}

func Test_Dev_Escort_IgnoredDirs(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	e := getEscort()
	e.excludeDirs = []string{"a"}

	at.True(e.ignoredDirs(".git"))
	at.True(e.ignoredDirs("a"))
	at.False(e.ignoredDirs("b"))
}

func Test_Dev_Escort_IgnoredFiles(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	e := getEscort()
	e.excludeFiles = []string{"a"}

	at.True(e.ignoredFiles("a"))
	at.False(e.ignoredFiles("b"))
}

func Test_Dev_Escort_DoPreRun(t *testing.T) {
	t.Parallel()

	e := getEscort()
	e.preRunCommands = [][]string{{"go", "version"}, {"non-exist-command"}}

	e.doPreRun()
}

func Test_Dev_IsRemoved(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fsnotify.Op
		bool
	}{
		{fsnotify.Create, false},
		{fsnotify.Write, false},
		{fsnotify.Remove, true},
		{fsnotify.Rename, false},
		{fsnotify.Chmod, false},
	}

	for _, tc := range cases {
		t.Run(tc.Op.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.bool, isRemoved(tc.Op))
		})
	}
}

func Test_Dev_IsCreated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fsnotify.Op
		bool
	}{
		{fsnotify.Create, true},
		{fsnotify.Write, false},
		{fsnotify.Remove, false},
		{fsnotify.Rename, false},
		{fsnotify.Chmod, false},
	}

	for _, tc := range cases {
		t.Run(tc.Op.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.bool, isCreated(tc.Op))
		})
	}
}

func Test_Dev_IsChmoded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fsnotify.Op
		bool
	}{
		{fsnotify.Create, false},
		{fsnotify.Write, false},
		{fsnotify.Remove, false},
		{fsnotify.Rename, false},
		{fsnotify.Chmod, true},
	}

	for _, tc := range cases {
		t.Run(tc.Op.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.bool, isChmoded(tc.Op))
		})
	}
}

func Test_Dev_ParsePreRunCommands(t *testing.T) {
	t.Parallel()

	list := parsePreRunCommands([]string{"go", "", "swag init"})
	assert.Len(t, list, 2)
	assert.Equal(t, []string{"go"}, list[0])
	assert.Equal(t, []string{"swag", "init"}, list[1])
}

func getEscort() *escort {
	c, t := context.WithCancel(context.Background())
	return &escort{
		config: config{
			root:   ".",
			target: ".",
		},
		ctx:       c,
		terminate: t,
		hitCh:     make(chan struct{}, 1),
		sig:       make(chan os.Signal, 1),
	}
}
