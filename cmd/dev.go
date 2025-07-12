package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var c config

func init() {
	devCmd.PersistentFlags().StringVarP(&c.root, "root", "r", ".",
		"root path for watch, all files must be under root")
	devCmd.PersistentFlags().StringVarP(&c.target, "target", "t", ".",
		"target path for go build")
	devCmd.PersistentFlags().StringSliceVarP(&c.extensions, "extensions", "e",
		[]string{"go", "tmpl", "tpl", "html"}, "file extensions to watch")
	devCmd.PersistentFlags().StringSliceVarP(&c.excludeDirs, "exclude_dirs", "D",
		[]string{"assets", "tmp", "vendor", "node_modules"}, "ignore these directories")
	devCmd.PersistentFlags().StringSliceVarP(&c.excludeFiles, "exclude_files", "F", nil, "ignore these files")
	devCmd.PersistentFlags().DurationVarP(&c.delay, "delay", "d", time.Second,
		"delay to trigger rerun")
	devCmd.PersistentFlags().StringSliceVarP(&c.preRun, "pre-run", "p", nil,
		"pre run commands, see example for more detail")
	devCmd.PersistentFlags().StringSliceVarP(&c.args, "args", "a", nil,
		"arguments for exec")
}

// devCmd reruns the fiber project if watched files changed
var devCmd = &cobra.Command{
	Use:     "dev",
	Short:   "Rerun the fiber project if watched files changed",
	RunE:    devRunE,
	Example: devExample,
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
	preRun       []string
	args         []string
	delay        time.Duration
}

type escort struct {
	ctx        context.Context
	stdoutPipe io.ReadCloser
	stderrPipe io.ReadCloser
	compiling  atomic.Value

	terminate context.CancelFunc

	w             *fsnotify.Watcher
	watcherEvents chan fsnotify.Event
	watcherErrors chan error
	sig           chan os.Signal

	bin     *exec.Cmd
	hitCh   chan struct{}
	hitFunc func()

	binPath string

	preRunCommands [][]string

	config

	wg sync.WaitGroup
}

func newEscort(c config) *escort {
	return &escort{
		config: c,
		hitCh:  make(chan struct{}, 1),
		sig:    make(chan os.Signal, 1),
	}
}

func (e *escort) run() error {
	if err := e.init(); err != nil {
		return err
	}

	log.Println("Welcome to fiber dev 👋")

	defer func() {
		if err := e.w.Close(); err != nil {
			log.Printf("Failed to close watcher: %v", err)
		}
		if err := os.Remove(e.binPath); err != nil {
			log.Printf("Failed to remove bin: %v", err)
		}
	}()

	e.wg.Add(3)
	go func() { defer e.wg.Done(); e.runBin() }()
	go func() { defer e.wg.Done(); e.watchingBin() }()
	go func() { defer e.wg.Done(); e.watchingFiles() }()

	signal.Notify(e.sig, syscall.SIGTERM, syscall.SIGINT, os.Interrupt)
	<-e.sig

	e.terminate()
	close(e.hitCh)
	e.wg.Wait()

	log.Println("See you next time 👋")

	return nil
}

func (e *escort) init() error {
	var err error
	if e.w, err = fsnotify.NewWatcher(); err != nil {
		return err
	}

	e.watcherEvents = e.w.Events
	e.watcherErrors = e.w.Errors

	e.ctx, e.terminate = context.WithCancel(context.Background())

	// normalize root
	if e.root, err = filepath.Abs(e.root); err != nil {
		return err
	}

	// create bin target
	f, err := os.CreateTemp("", "")
	if err != nil {
		return err
	}
	if cerr := f.Close(); cerr != nil {
		return cerr
	}

	e.binPath = f.Name()
	if runtime.GOOS == "windows" {
		e.binPath += ".exe"
	}

	e.hitFunc = func() {
		e.wg.Add(1)
		e.runBin()
		e.wg.Done()
	}

	e.preRunCommands = parsePreRunCommands(c.preRun)

	return nil
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
	for {
		select {
		case <-e.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-e.hitCh:
			if timer != nil && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer = time.AfterFunc(e.delay, e.hitFunc)
		}
	}
}

func (e *escort) runBin() {
	if ok := e.compiling.Load(); ok != nil && ok.(bool) {
		return
	}

	e.doPreRun()

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

	e.bin = execCommand(e.binPath, e.args...)

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

func (e *escort) doPreRun() {
	for _, command := range e.preRunCommands {
		cmd := execCommand(command[0], command[1:]...)
		out, err := cmd.CombinedOutput()
		var buf bytes.Buffer
		_, _ = buf.WriteString(fmt.Sprintf("Pre running %s... ", command))
		if err != nil {
			_, _ = buf.WriteString(err.Error())
			_, _ = buf.WriteString(":")
		}
		_, _ = buf.Write(out)
		log.Print(buf.String())
	}
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

func parsePreRunCommands(commands []string) (list [][]string) {
	for _, command := range commands {
		if r := strings.Fields(strings.Trim(command, " ")); len(r) > 0 {
			list = append(list, r)
		}
	}
	return
}

const (
	devExample = `  fiber dev --pre-run="command1 flag,command2 flag"
  Pre run specific commands before running the project`
)
