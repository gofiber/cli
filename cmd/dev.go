package cmd

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var c config

func init() {
	DevCmd.PersistentFlags().StringVarP(&c.root, "root", "r", ".",
		"root path for watch, all files must be under root")
	DevCmd.PersistentFlags().StringVarP(&c.target, "target", "t", ".",
		"target path for go build")
	DevCmd.PersistentFlags().StringSliceVarP(&c.extensions, "extensions", "e",
		[]string{"go", "tmpl", "tpl", "html"}, "file extensions to watch")
	DevCmd.PersistentFlags().StringSliceVarP(&c.excludeDirs, "exclude_dirs", "D",
		[]string{"assets", "tmp", "vendor", "node_modules"}, "ignore these directories")
	DevCmd.PersistentFlags().StringSliceVarP(&c.excludeFiles, "exclude_files", "F", nil, "ignore these files")
	DevCmd.PersistentFlags().DurationVarP(&c.delay, "delay", "d", time.Second,
		"delay to trigger rerun")
}

// DevCmd reruns the fiber project if watched files changed
var DevCmd = &cobra.Command{
	Use:   "dev",
	Short: "Rerun the fiber project if watched files changed",
	RunE:  devRunE,
}

func devRunE(_ *cobra.Command, _ []string) error {
	return newEscort(c).run()
}

type config struct {
	root         string
	target       string
	binPath      string
	extensions   []string
	excludeDirs  []string
	excludeFiles []string
	delay        time.Duration
}

type escort struct {
	config

	ctx       context.Context
	terminate context.CancelFunc

	w             *fsnotify.Watcher
	watcherEvents chan fsnotify.Event
	watcherErrors chan error
	sig           chan os.Signal

	binPath    string
	bin        *exec.Cmd
	stdoutPipe io.ReadCloser
	stderrPipe io.ReadCloser
	hitCh      chan struct{}
	hitFunc    func()
	compiling  atomic.Value
}

func newEscort(c config) *escort {
	return &escort{
		config: c,
		hitCh:  make(chan struct{}, 1),
		sig:    make(chan os.Signal, 1),
	}
}

func (e *escort) run() (err error) {
	if err = e.init(); err != nil {
		return
	}

	log.Println("Welcome to fiber dev 👋")

	defer func() {
		_ = e.w.Close()
		_ = os.Remove(e.binPath)
	}()

	go e.runBin()
	go e.watchingBin()
	go e.watchingFiles()

	signal.Notify(e.sig, syscall.SIGTERM, syscall.SIGINT, os.Interrupt)
	<-e.sig

	e.terminate()

	log.Println("See you next time 👋")

	return nil
}

func (e *escort) init() (err error) {
	if e.w, err = fsnotify.NewWatcher(); err != nil {
		return
	}

	e.watcherEvents = e.w.Events
	e.watcherErrors = e.w.Errors

	e.ctx, e.terminate = context.WithCancel(context.Background())

	// normalize root
	if e.root, err = filepath.Abs(e.root); err != nil {
		return
	}

	// create bin target
	var f *os.File
	if f, err = ioutil.TempFile("", ""); err != nil {
		return
	}
	defer func() {
		if e := f.Close(); e != nil {
			err = e
		}
	}()

	e.binPath = f.Name()

	e.hitFunc = e.runBin

	return
}

func (e *escort) watchingFiles() {
	// walk root and add all dirs
	e.walkForWatcher(e.root)

	var (
		info os.FileInfo
		err  error
	)

	for {
		select {
		case <-e.ctx.Done():
			return
		case event := <-e.watcherEvents:
			p, op := event.Name, event.Op

			// ignore chmod
			if isChmoded(op) {
				continue
			}

			if isRemoved(op) {
				e.tryRemoveWatch(p)
				continue
			}

			if info, err = os.Stat(p); err != nil {
				log.Printf("Failed to get info of %s: %s\n", p, err)
				continue
			}

			base := filepath.Base(p)

			if info.IsDir() && isCreated(op) {
				e.walkForWatcher(p)
				e.hitCh <- struct{}{}
				continue
			}

			if e.ignoredFiles(base) {
				continue
			}

			if e.hitExtension(filepath.Ext(base)) {
				e.hitCh <- struct{}{}
			}
		case err := <-e.watcherErrors:
			log.Printf("Watcher error: %v\n", err)
		}
	}
}

func (e *escort) watchingBin() {
	var timer *time.Timer
	for range e.hitCh {
		// reset timer
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = time.AfterFunc(e.delay, e.hitFunc)
	}
}

func (e *escort) runBin() {
	if ok := e.compiling.Load(); ok != nil && ok.(bool) {
		return
	}

	e.compiling.Store(true)
	defer e.compiling.Store(false)

	if e.bin != nil {
		e.cleanOldBin()
		log.Println("Recompiling...")
	} else {
		log.Println("Compiling...")
	}

	start := time.Now()

	// build target
	compile := execCommand("go", "build", "-o", e.binPath, e.target)
	if out, err := compile.CombinedOutput(); err != nil {
		log.Printf("Failed to compile %s: %s\n", e.target, out)
		return
	}

	log.Printf("Compile done in %s!\n", formatLatency(time.Since(start)))

	e.bin = execCommand(e.binPath)

	e.bin.Env = os.Environ()

	e.watchingPipes()

	if err := e.bin.Start(); err != nil {
		log.Printf("Failed to start bin: %s\n", err)
		e.bin = nil
		return
	}

	log.Println("New pid is", e.bin.Process.Pid)
}

func (e *escort) cleanOldBin() {
	defer func() {
		if e.stdoutPipe != nil {
			_ = e.stdoutPipe.Close()
		}
		if e.stderrPipe != nil {
			_ = e.stderrPipe.Close()
		}
	}()

	pid := e.bin.Process.Pid
	log.Println("Killing old pid", pid)

	var err error
	if runtime.GOOS == "windows" {
		err = execCommand("TASKKILL", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
	} else {
		err = e.bin.Process.Kill()
		_, _ = e.bin.Process.Wait()
	}

	if err != nil {
		log.Printf("Failed to kill old pid %d: %s\n", pid, err)
	}

	e.bin = nil
}

func (e *escort) watchingPipes() {
	var err error
	if e.stdoutPipe, err = e.bin.StdoutPipe(); err != nil {
		log.Printf("Failed to get stdout pipe: %s", err)
	} else {
		go func() { _, _ = io.Copy(os.Stdout, e.stdoutPipe) }()
	}

	if e.stderrPipe, err = e.bin.StderrPipe(); err != nil {
		log.Printf("Failed to get stderr pipe: %s", err)
	} else {
		go func() { _, _ = io.Copy(os.Stderr, e.stderrPipe) }()
	}
}

func (e *escort) walkForWatcher(root string) {
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info != nil && !info.IsDir() {
			return nil
		}

		base := filepath.Base(path)

		if e.ignoredDirs(base) {
			return filepath.SkipDir
		}

		log.Println("Add", path, "to watch")
		return e.w.Add(path)
	}); err != nil {
		log.Printf("Failed to walk root %s: %s\n", e.root, err)
	}
}

func (e *escort) tryRemoveWatch(p string) {
	if err := e.w.Remove(p); err != nil && !strings.Contains(err.Error(), "non-existent") {
		log.Printf("Failed to remove %s from watch: %s\n", p, err)
	}
}

func (e *escort) hitExtension(ext string) bool {
	if ext == "" {
		return false
	}
	// remove '.'
	ext = ext[1:]
	for _, e := range e.extensions {
		if ext == e {
			return true
		}
	}

	return false
}

func (e *escort) ignoredDirs(dir string) bool {
	// exclude hidden directories like .git, .idea, etc.
	if len(dir) > 1 && dir[0] == '.' {
		return true
	}

	for _, d := range e.excludeDirs {
		if dir == d {
			return true
		}
	}

	return false
}

func (e *escort) ignoredFiles(filename string) bool {
	for _, f := range e.excludeFiles {
		if filename == f {
			return true
		}
	}

	return false
}

func isRemoved(op fsnotify.Op) bool {
	return op&fsnotify.Remove != 0
}

func isCreated(op fsnotify.Op) bool {
	return op&fsnotify.Create != 0
}

func isChmoded(op fsnotify.Op) bool {
	return op&fsnotify.Chmod != 0
}
