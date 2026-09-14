package acl

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchPolicyFile watches a policy file for changes and hot-reloads the
// evaluator's policy. Returns a stop function to close the watcher.
// Logs a warning if the file is world-readable.
//
// It watches the file's *directory*, not the file. fsnotify follows the inode,
// so a watch on the file itself only ever sees a truncate-in-place write: an
// atomic rename-over-write leaves the watch pointed at the old, unlinked inode
// and hot reload then stops permanently, with no error and no log line. Since
// rename-over-write is how several editors save and the safe way for a
// deployment to replace a config file, the file watch failed precisely the
// callers most likely to use the feature, while docs/authorization.md promises
// hot reload unconditionally. A directory watch survives any number of swaps,
// and covers a policy file that does not exist yet at startup.
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

	// A Kubernetes ConfigMap (and a Docker secret) does not rewrite the
	// mounted file at all: the name is a symlink into a timestamped directory
	// and an update swaps a `..data` symlink beside it, so no event ever names
	// the file and a basename filter alone would miss every update. The
	// resolved path is therefore tracked as well, and a change to it is itself
	// a reload — the same pair of conditions viper and client-go settled on.
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

		// kqueue (macOS, the BSDs) delivers nothing at all for the swap above:
		// it reports a directory by diffing its listing, which a replaced name
		// does not change, and the per-entry watch it keeps for a symlink is
		// opened on the symlink's target, which the swap does not touch. So the
		// resolved path is re-read on a tick as well. inotify reports the swap
		// and reloads long before the first one arrives.
		poll := time.NewTicker(2 * time.Second)
		defer poll.Stop()

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// Write covers a truncate-in-place write; Create covers a
				// rename-over-write, which inotify reports as IN_MOVED_TO and
				// fsnotify maps to Create. Rename is deliberately absent: on
				// the watched name it means the file moved *away*, so acting
				// on it would only log a failure to read what is no longer
				// there. The Create for whatever replaced it is the trigger.
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
