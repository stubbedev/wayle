package wallpaper

import (
	"encoding/binary"
	"log"
	"os"

	"golang.org/x/sys/unix"
)

// dirWatcher reports file-level changes in one cycling directory
// (tasks/watcher.rs): a file created or deleted, or any entry renamed
// in or out. Data writes and subdirectory creation do not count, as
// they are not the notify events the Rust watcher filters for.
type dirWatcher struct {
	file   *os.File
	events chan struct{}
}

const watchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO

// watchDirectory starts an inotify watch; nil (logged) when the watch
// cannot be set up, which leaves cycling running without rescans.
func watchDirectory(dir string) *dirWatcher {
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		log.Printf("wallpaper: cannot create directory watcher: %v", err)
		return nil
	}
	if _, err := unix.InotifyAddWatch(fd, dir, watchMask|unix.IN_ONLYDIR); err != nil {
		_ = unix.Close(fd)
		log.Printf("wallpaper: cannot watch directory %s: %v", dir, err)
		return nil
	}
	// A non-blocking fd joins the runtime poller, so Close unblocks Read.
	w := &dirWatcher{file: os.NewFile(uintptr(fd), "inotify:"+dir), events: make(chan struct{}, 1)}
	go w.read()
	return w
}

func (w *dirWatcher) read() {
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	for {
		n, err := w.file.Read(buf)
		if err != nil {
			return
		}
		if fileEvent(buf[:n]) {
			select {
			case w.events <- struct{}{}:
			default:
			}
		}
	}
}

// fileEvent reports whether a read batch carries a relevant change.
func fileEvent(batch []byte) bool {
	for len(batch) >= unix.SizeofInotifyEvent {
		mask := binary.NativeEndian.Uint32(batch[4:8])
		nameLen := binary.NativeEndian.Uint32(batch[12:16])
		isDir := mask&unix.IN_ISDIR != 0
		switch {
		case mask&(unix.IN_MOVED_FROM|unix.IN_MOVED_TO) != 0:
			return true
		case mask&(unix.IN_CREATE|unix.IN_DELETE) != 0 && !isDir:
			return true
		}
		next := unix.SizeofInotifyEvent + int(nameLen)
		if next > len(batch) {
			break
		}
		batch = batch[next:]
	}
	return false
}

// close stops the watch; nil-safe.
func (w *dirWatcher) close() {
	if w != nil {
		_ = w.file.Close()
	}
}
