package follower

import (
	"time"

	"github.com/fsnotify/fsnotify"
)

// FsnWrap wraps the fsnotify package, for use by File.
type fsnWrap struct {
	wakeCh chan error
	watch  *fsnotify.Watcher
	done   chan struct{}
}

// NewFsnWrap returns an fsnWrap configured for polling.
func newFsnWrap() *fsnWrap {
	wakeCh := make(chan error, 1)
	return &fsnWrap{wakeCh, nil, nil}
}

// Add requests change notifications from the filesystem, if available. It is
// safe to call more than once, in order to monitor multiple files. Upon
// failure, an error is returned.
func (f *fsnWrap) add(name string) error {
	if f.watch != nil {
		return f.watch.Add(name)
	}

	watch, err := fsnotify.NewWatcher()
	if err == nil {
		err = watch.Add(name)
		if err != nil {
			// TODO: Not sure if it's safe to call Close here,
			// without a goroutine to consume errors.
			watch.Close()
		}
	}

	if err == nil {
		f.watch = watch
		f.done = make(chan struct{})
		go f.eat()
	}

	return err
}

// Close stops all filesystem notifications. All concurrent or future calls to
// wait return immediately. Close panics if called more than once.
func (f *fsnWrap) close() error {
	var err error
	if f.watch != nil {
		err = f.watch.Close()
		close(f.done)
	} else {
		close(f.wakeCh)
	}
	return err
}

// Wait deschedules the current goroutine until either the polling interval
// elapses, or a filesystem notification is received, whichever comes first.
// The polling interval is one minute when filesystem notifications are
// available, and one second otherwise.
func (f *fsnWrap) wait() error {
	poll := time.Minute
	if f.watch == nil {
		poll = time.Second
	}

	select {
	case err := <-f.wakeCh:
		// Wake after any filesystem notification or error.
		return err
	case <-time.After(poll):
		// Wake after polling interval elapses.
		return nil
	}
}

func (f *fsnWrap) eat() {
	ok := true
	for ok {
		var err error
		select {
		case err, ok = <-f.watch.Errors:
		case _, ok = <-f.watch.Events:
		case _, ok = <-f.done:
		}
		f.wake(err)
	}
	close(f.wakeCh)
}

func (f *fsnWrap) wake(err error) {
	if err != nil {
		select {
		case oldErr := <-f.wakeCh:
			// Make room for error.
			if oldErr != nil {
				// Keep first error.
				err = oldErr
			}
		default:
		}
	}
	select {
	case f.wakeCh <- err:
	default:
		// Debounce any extra filesystem notifications.
	}
}
