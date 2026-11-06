package diagnostics

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/gofrs/flock"
)

var artifactPattern = regexp.MustCompile(`^(events|trace)(\.[0-9]{6,})?\.jsonl$`)

type candidate struct {
	name     string
	size     int64
	last     time.Time
	identity os.FileInfo
	files    map[string]os.FileInfo
}

// Cleanup 非阻塞取得维护锁，只清理有有效标记且未被写入者锁定的运行目录。
func Cleanup(base string, config Config) error {
	return cleanup(base, config, (*os.Root).Remove)
}

// cleanup 的删除操作按调用传入，便于验证文件系统失败或进程中断后的恢复。
func cleanup(base string, config Config, remove func(*os.Root, string) error) (resultErr error) {
	if config.RetentionDays == 0 && config.MaxTotalSizeMiB == 0 {
		return nil
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
	}()
	if info, err := root.Lstat(maintenanceName); err == nil && !info.Mode().IsRegular() {
		return invalidmaintenancelock.New()
	}
	maintenance := flock.New(filepath.Join(base, maintenanceName), flock.SetPermissions(0o600))
	locked, err := maintenance.TryLock()
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, maintenance.Close())
	}()
	if !locked {
		return nil
	}
	entries, err := readEntries(root)
	if err != nil {
		return err
	}
	var total int64
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		c, err := inspect(root, entry.Name())
		if err != nil {
			continue
		}
		total += c.size
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].last.Before(candidates[j].last)
	})
	cutoff := time.Now().Add(-time.Duration(config.RetentionDays) * 24 * time.Hour)
	for _, c := range candidates {
		expired := config.RetentionDays > 0 && c.last.Before(cutoff)
		oversized := config.MaxTotalSizeMiB > 0 && total > int64(config.MaxTotalSizeMiB)*mebibyte
		if !expired && !oversized {
			continue
		}
		removed, err := removeCandidate(root, base, c, remove)
		if err != nil {
			return err
		}
		if removed {
			total -= c.size
		}
	}
	return nil
}

func inspect(root *os.Root, name string) (c candidate, resultErr error) {
	c = candidate{
		name:  name,
		files: map[string]os.FileInfo{},
	}
	info, err := root.Lstat(name)
	if err != nil {
		return c, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return c, invaliddirectory.New()
	}
	c.identity = info
	child, err := root.OpenRoot(name)
	if err != nil {
		return c, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, child.Close())
	}()
	entries, err := readEntries(child)
	if err != nil {
		return c, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() ||
			(e.Name() != metadataName && e.Name() != metadataTemporaryName && e.Name() != leaseName && !artifactPattern.MatchString(e.Name())) {
			return c, unknowncontent.New()
		}
		info, err := e.Info()
		if err != nil {
			return c, err
		}
		c.files[e.Name()] = info
		c.size += info.Size()
		if info.ModTime().After(c.last) {
			c.last = info.ModTime()
		}
	}
	mi, err := child.Lstat(metadataName)
	if err != nil || !mi.Mode().IsRegular() {
		return c, invalidmetadata.New()
	}
	data, err := child.ReadFile(metadataName)
	if err != nil {
		return c, err
	}
	var meta metadata
	if err = json.Unmarshal(data, &meta); err != nil {
		return c, err
	}
	if meta.Version != metadataVersion || meta.ID != name || meta.Started.IsZero() {
		return c, unrecognizedmetadata.New()
	}
	if !meta.Finished.IsZero() {
		c.last = meta.Finished
	}
	return c, nil
}

func removeCandidate(
	root *os.Root,
	base string,
	c candidate,
	remove func(*os.Root, string) error,
) (_ bool, resultErr error) {
	leaseInfo, err := root.Lstat(filepath.Join(c.name, leaseName))
	if err != nil || !leaseInfo.Mode().IsRegular() {
		return false, nil
	}
	lease := flock.New(filepath.Join(base, c.name, leaseName), flock.SetFlag(os.O_RDWR))
	locked, err := lease.TryLock()
	if err != nil {
		return false, nil
	}
	defer func() {
		resultErr = errors.Join(resultErr, lease.Close())
	}()
	if !locked {
		return false, nil
	}
	held, err := lease.Stat()
	if err != nil || !os.SameFile(leaseInfo, held) {
		return false, nil
	}
	fresh, err := inspect(root, c.name)
	if err != nil || !os.SameFile(c.identity, fresh.identity) || !fresh.last.Equal(c.last) {
		return false, nil
	}
	// 逐项移除已登记文件，未知内容会使目录保留；不对整个路径做递归删除。
	child, err := root.OpenRoot(c.name)
	if err != nil {
		return false, err
	}
	defer func() {
		if child != nil {
			resultErr = errors.Join(resultErr, child.Close())
		}
	}()
	entries, err := readEntries(child)
	if err != nil {
		return false, err
	}
	// 删除前重新核对完整清单；后来新增的文件（即使名称已登记）不属于已校验快照。
	if !matchesSnapshot(child, entries, fresh.files) {
		return false, nil
	}
	for _, e := range entries {
		if e.Name() == leaseName || e.Name() == metadataName {
			continue
		}
		info, statErr := child.Lstat(e.Name())
		if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(fresh.files[e.Name()], info) {
			return false, nil
		}
		if err = remove(child, e.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
		delete(fresh.files, e.Name())
	}
	entries, err = readEntries(child)
	if err != nil {
		return false, err
	}
	if !matchesSnapshot(child, entries, fresh.files) {
		return false, nil
	}
	// 数据清理全部成功后再移除标记，进程中断或单个删除失败时仍可被下次清理识别。
	if err = remove(child, metadataName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	err = child.Close()
	child = nil
	if err != nil {
		return false, err
	}
	// Windows 不允许删除仍打开的锁文件，释放后再次核对目录身份；发布后的目录不会复用。
	if err = lease.Close(); err != nil {
		return false, err
	}
	if err = root.Remove(filepath.Join(c.name, leaseName)); err != nil {
		return false, err
	}
	return true, root.Remove(c.name)
}

// matchesSnapshot 拒绝任何新增、替换或非普通文件，防止扫描之间的用户内容进入删除清单。
func matchesSnapshot(root *os.Root, entries []fs.DirEntry, verified map[string]os.FileInfo) bool {
	if len(entries) != len(verified) {
		return false
	}
	for _, entry := range entries {
		before, known := verified[entry.Name()]
		if !known {
			return false
		}
		current, err := root.Lstat(entry.Name())
		if err != nil || !current.Mode().IsRegular() || !os.SameFile(before, current) {
			return false
		}
	}
	return true
}
