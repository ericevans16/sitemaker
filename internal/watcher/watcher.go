// Package watcher recursively watches content, theme and static directories
// and debounces filesystem events into a single callback.
package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

var defaultDirs = []string{"content", "themes", "layouts", "static"}

// Watch watches the site inputs (plus _config.yaml) and invokes onChange at
// most once per 150ms burst. Call the returned function to stop watching.
func Watch(onChange func(changedPath string)) (func(), error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	for _, root := range defaultDirs {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			_ = addRecursive(fw, root)
		}
	}
	_ = fw.Add(".")

	stop := make(chan struct{})
	var once sync.Once
	stopFn := func() {
		once.Do(func() {
			close(stop)
			_ = fw.Close()
		})
	}

	go func() {
		var timer *time.Timer
		var last string
		for {
			select {
			case <-stop:
				return
			case ev, ok := <-fw.Events:
				if !ok {
					return
				}
				if shouldIgnore(ev.Name) {
					continue
				}
				if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}
				if ev.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
						_ = addRecursive(fw, ev.Name)
					}
				}
				last = ev.Name
				if timer != nil {
					timer.Stop()
				}
				name := last
				timer = time.AfterFunc(150*time.Millisecond, func() { onChange(name) })
			case <-fw.Errors:
			}
		}
	}()

	return stopFn, nil
}

func shouldIgnore(path string) bool {
	slash := filepath.ToSlash(path)
	base := filepath.Base(slash)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") || strings.HasSuffix(base, ".swp") {
		return true
	}
	// Root directory is watched only for _config.yaml.
	if filepath.Dir(slash) == "." && base != "_config.yaml" {
		return true
	}
	return strings.HasPrefix(slash, "public/") || strings.HasPrefix(slash, "node_modules/")
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
}
