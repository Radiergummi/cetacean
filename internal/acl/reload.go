package acl

import (
	"log/slog"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchPolicyFile watches a policy file for changes and hot-reloads the
// evaluator's policy. Returns a stop function to close the watcher.
// Logs a warning if the file is world-readable.
func WatchPolicyFile(e *Evaluator, path string) (func(), error) {
	warnFilePermissions(path)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(path); err != nil {
		watcher.Close()
		return nil, err
	}

	stop := make(chan struct{})
	go func() {
		var debounce *time.Timer
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				// Debounce: editors often write multiple times in quick succession.
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(200*time.Millisecond, func() {
					reloadPolicy(e, path)
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error("policy file watcher error", "error", err)
			case <-stop:
				if debounce != nil {
					debounce.Stop()
				}
				return
			}
		}
	}()

	return func() {
		close(stop)
		watcher.Close()
	}, nil
}

func reloadPolicy(e *Evaluator, path string) {
	p, err := ParsePolicyFile(path)
	if err != nil {
		slog.Error("failed to reload policy file", "path", path, "error", err)
		return
	}
	if err := Validate(p); err != nil {
		slog.Error("reloaded policy is invalid, keeping previous", "path", path, "error", err)
		return
	}
	e.SetPolicy(p)
	slog.Info("policy file reloaded", "path", path, "grants", len(p.Grants))
}

func warnFilePermissions(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	mode := info.Mode().Perm()
	if mode&0004 != 0 {
		slog.Warn("policy file is world-readable", "path", path, "mode", mode.String())
	}
}
