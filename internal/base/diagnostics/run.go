package diagnostics

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// Run 持有一个运行目录及其存活锁；关闭前调用方必须等待所有写入者结束。
type Run struct {
	ID        string
	Path      string
	root      *os.Root
	lease     *flock.Flock
	config    Config
	base      string
	mu        sync.Mutex
	writers   map[string]*Writer
	closed    bool
	closeOnce sync.Once
	closeErr  error
	warn      func(error)
}
type metadata struct {
	Version  int       `json:"version"`
	ID       string    `json:"run_id"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
}

// NewID 为无文件和有文件的会话生成独立关联标识。
func NewID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + strconv.Itoa(os.Getpid()) + "-" + rand.Text()
}

// Open 创建独立运行目录，先取得存活锁，再发布可清理的元数据。
func Open(base string, config Config, warn func(error)) (*Run, error) {
	// #nosec G703 -- base 来自已校验的用户状态路径或测试临时目录，允许创建该配置位置。
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	if err := Cleanup(base, config); err != nil && warn != nil {
		warn(err)
	}
	parent, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := parent.Close(); err != nil && warn != nil {
			warn(err)
		}
	}()
	id := NewID()
	path := filepath.Join(base, id)
	if err := parent.Mkdir(id, 0o700); err != nil {
		return nil, err
	}
	root, err := parent.OpenRoot(id)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(leaseName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	if err = file.Close(); err != nil {
		return nil, errors.Join(err, root.Close())
	}
	lease := flock.New(filepath.Join(path, leaseName), flock.SetPermissions(0o600), flock.SetFlag(os.O_RDWR))
	locked, err := lease.TryLock()
	if err != nil || !locked {
		return nil, errors.Join(err, leaseunavailable.New(), lease.Close(), root.Close())
	}
	if err = writeMetadata(root, metadata{Version: metadataVersion, ID: id, Started: time.Now().UTC()}); err != nil {
		return nil, errors.Join(err, lease.Close(), root.Close())
	}
	return &Run{
		ID:      id,
		Path:    path,
		root:    root,
		lease:   lease,
		config:  config,
		base:    base,
		writers: map[string]*Writer{},
		warn:    warn,
	}, nil
}

// Writer 返回本会话拥有的文件 writer，只接受登记的诊断文件名。
func (r *Run) Writer(name string) (*Writer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, os.ErrClosed
	}
	if name != EventsFile && name != TraceFile {
		return nil, unknownartifact.New()
	}
	if w, ok := r.writers[name]; ok {
		return w, nil
	}
	w, err := newWriter(r.root, name, int64(r.config.MaxSizeMiB)*mebibyte, r.config.MaxBackups)
	if err == nil {
		r.writers[name] = w
	}
	return w, err
}

// Close 关闭文件、记录完成时间、释放锁，再按策略清理。
func (r *Run) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.closed = true
		var problems []error
		for _, w := range r.writers {
			problems = append(problems, w.Close())
		}
		data, err := r.root.ReadFile(metadataName)
		var meta metadata
		if err == nil {
			err = json.Unmarshal(data, &meta)
		}
		if err == nil {
			meta.Finished = time.Now().UTC()
			err = writeMetadata(r.root, meta)
		}
		problems = append(problems, err, r.lease.Close(), r.root.Close())
		problems = append(problems, Cleanup(r.base, r.config))
		r.closeErr = errors.Join(problems...)
		if r.closeErr != nil && r.warn != nil {
			r.warn(r.closeErr)
		}
	})
	return r.closeErr
}

func writeMetadata(root *os.Root, value metadata) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := root.OpenFile(metadataTemporaryName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return errors.Join(err, root.Remove(metadataTemporaryName))
	}
	return root.Rename(metadataTemporaryName, metadataName)
}

func readEntries(root *os.Root) ([]fs.DirEntry, error) {
	return fs.ReadDir(root.FS(), ".")
}
