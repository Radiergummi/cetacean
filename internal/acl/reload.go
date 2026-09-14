package acl

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchPolicyFile watches a policy file and hot-reloads the evaluator's policy,
// returning a stop function. It watches the file's *directory*: fsnotify
// follows the inode, so a rename-over-write leaves the watch on the old file
// and reload stops silently. A directory watch survives any number of swaps.
func WatchPolicyFile(e *Evaluator, path string) (func(), error) {
	warnFilePermissions(path)

	// Absolute, so the comparison against fsnotify's event names — which the
	// watcher reports relative to the directory it was given — is made on one
	// spelling of the path rather than two.
	target, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(filepath.Dir(target)); err != nil {
		watcher.Close()
		return nil, err
	}

	// A ConfigMap or Docker secret does not rewrite the mounted file at all:
	// the name is a symlink and an update swaps a `..data` link beside it, so
	// no event ever names the file. The resolved path is tracked too, and a
	// change to it is itself a reload.
	linkTarget, _ := filepath.EvalSymlinks(target)

	stop := make(chan struct{})
	go func() {
		var debounce *time.Timer

		// Debounce: editors often write multiple times in quick succession.
		schedule := func() {
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(200*time.Millisecond, func() {
				reloadPolicy(e, path)
			})
		}

		// kqueue delivers nothing for the swap above: it diffs a directory
		// listing, which a replaced name does not change, and its per-entry
		// symlink watch is opened on the target the swap does not touch. So
		// the resolved path is re-read on a tick as well.
		poll := time.NewTicker(2 * time.Second)
		defer poll.Stop()

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// Write covers a truncate-in-place write, Create a
				// rename-over-write, which fsnotify maps from IN_MOVED_TO.
				// Rename is deliberately absent: on the watched name it means
				// the file moved *away*, and the Create is the trigger.
				named := filepath.Clean(event.Name) == target &&
					event.Op&(fsnotify.Write|fsnotify.Create) != 0

				resolved, _ := filepath.EvalSymlinks(target)
				relinked := resolved != "" && resolved != linkTarget

				if !named && !relinked {
					continue
				}

				linkTarget = resolved
				schedule()
			case <-poll.C:
				resolved, _ := filepath.EvalSymlinks(target)
				if resolved == "" || resolved == linkTarget {
					continue
				}

				linkTarget = resolved
				schedule()
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
