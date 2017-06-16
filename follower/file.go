package follower

import (
	"io"
	"os"
	"sync"
)

// File wraps and imitates os.File, with two changes. First, reading blocks and
// waits for more data to be appended to the file, instead of returning io.EOF.
// Second, it is safe to call Close and Read in parallel. Specifically, a
// blocked read will be unblocked by closing the file in another goroutine.
type File struct {
	// Mutex protects os.File, which isn't safe for parallel execution yet.
	// Be aware that this implementation resembles 10186 below, which was
	// rejected because it forces Close to block until any concurrent
	// operations finish. (This can potentially block indefinitely, for
	// example when reading from a pipe.)
	// https://github.com/golang/go/issues/7970
	// https://go-review.googlesource.com/c/10186
	// https://go-review.googlesource.com/c/36800
	mutex sync.Mutex
	file  *os.File

	info os.FileInfo
	name string
	pos  int64
	fsn  *fsnWrap
}

// Open wraps and imitates os.Open, which opens the named file for reading.
// In addition, it records the information from (*os.File).Stat.
func Open(name string) (*File, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	fsn := newFsnWrap()

	return &File{sync.Mutex{}, file, info, name, 0, fsn}, nil
}

// Close closes the File, rendering it unusable for I/O. It returns an error, if
// any. Any blocked Read operations will be unblocked and return errors.
func (f *File) Close() error {
	f.mutex.Lock()
	err := f.file.Close()
	f.mutex.Unlock()

	f.fsn.close()
	return err
}

// Read reads up to len(b) bytes from the File. It returns the number of bytes
// read and any error encountered. At end of file, Read blocks and waits for
// more data to be appended to the file. However, if the file is moved, renamed,
// or truncated (e.g. during log rotation), then Read returns 0, io.EOF.
func (f *File) Read(b []byte) (n int, err error) {
	fresh := true

	for {
		f.mutex.Lock()
		n, err := f.file.Read(b)
		f.mutex.Unlock()

		if !fresh || n != 0 || err != io.EOF {
			f.pos += int64(n)
			return n, err
		}

		fresh = f.isFresh()
		if fresh {
			err := f.fsn.wait()
			if err != nil {
				return 0, err
			}
		}
	}
}

// Seek sets the offset for the next Read or Write on file to offset,
// interpreted according to whence: 0 means relative to the origin of the file,
// 1 means relative to the current offset, and 2 means relative to the end. It
// returns the new offset and an error, if any.
func (f *File) Seek(offset int64, whence int) (ret int64, err error) {
	f.mutex.Lock()
	ret, err = f.file.Seek(offset, whence)
	f.mutex.Unlock()

	if err == nil {
		f.pos = ret
	}
	return
}

// Watch requests change notifications from the filesystem, if available.
func (f *File) Watch() error {
	return f.fsn.add(f.name)
}

func (f *File) isFresh() bool {
	if f.info == nil {
		// Skip freshness testing if initial Stat() failed.
		return true
	}

	info, err := os.Stat(f.name)
	if err != nil {
		// Skip freshness testing if latest Stat() failed.
		return true
	}

	if info.Size() < f.pos && info.Size() > 0 {
		// File truncated. Waited for a nonzero size, in case the first
		// write reverses the truncation.
		return false
	}

	if !os.SameFile(info, f.info) && info.Size() > 0 {
		// File replaced. Waited for a nonzero size, to give writes
		// a chance to flush to the old file.
		return false
	}

	return true
}
