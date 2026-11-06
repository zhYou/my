package scaffold

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

type commandMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func validateMove(move *commandMove, name string) error {
	if move == nil {
		return nil
	}
	if validateCommandName(name) != nil || name == TemplateName || move.From != "cmd/"+TemplateName ||
		move.To != "cmd/"+name {
		return fmt.Errorf("命令目录移动记录无效")
	}
	return nil
}

func isCommandFile(name string) bool {
	return name == mainFile || name == "main_test.go" || name == "doc.go"
}

func (i *Initializer) commandMembers(root, relative string, temporary ...string) ([]os.FileInfo, error) {
	dir, err := i.safePath(root, relative)
	if err != nil {
		return nil, err
	}
	entries, err := afero.ReadDir(i.FS, dir)
	if err != nil {
		return nil, err
	}
	allowedTemporary := map[string]bool{}
	for _, name := range temporary {
		allowedTemporary[name] = true
	}
	main := false
	for _, entry := range entries {
		if !isCommandFile(entry.Name()) && !allowedTemporary[entry.Name()] {
			return nil, fmt.Errorf("命令目录包含非受管内容：%s", entry.Name())
		}
		file, err := i.safePath(root, path.Join(relative, entry.Name()))
		if err != nil {
			return nil, err
		}
		info, err := i.FS.Stat(file)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("命令目录包含非常规文件：%s", file)
		}
		main = main || entry.Name() == mainFile
	}
	if !main {
		return nil, fmt.Errorf("命令目录缺少 main.go")
	}
	return entries, nil
}

func (i *Initializer) preflightCommandMove(root string, move *commandMove) error {
	if move == nil {
		return nil
	}
	if err := validateMove(move, path.Base(move.To)); err != nil {
		return err
	}
	target, err := i.safePath(root, move.To)
	if err != nil {
		return err
	}
	if _, err := i.FS.Stat(target); err == nil {
		return fmt.Errorf("目标命令目录已经存在：%s", move.To)
	} else if !os.IsNotExist(err) {
		return err
	}
	_, err = i.commandMembers(root, move.From)
	return err
}

func (i *Initializer) commandLocation(root string, tx transaction) (string, error) {
	if tx.Move == nil {
		return "", nil
	}
	if err := validateMove(tx.Move, tx.Name); err != nil {
		return "", err
	}
	locations := []string{}
	for _, relative := range []string{tx.Move.From, tx.Move.To} {
		dir, err := i.safePath(root, relative)
		if err != nil {
			return "", err
		}
		info, err := i.FS.Stat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("命令移动目标不是目录：%s", relative)
		}
		locations = append(locations, relative)
	}
	if len(locations) != 1 {
		return "", fmt.Errorf("命令目录存在状态不明确，保留恢复现场")
	}
	location := locations[0]
	if (tx.Phase == phasePrepared && location != tx.Move.From) ||
		(tx.Phase == phaseCommitted && location != tx.Move.To) {
		return "", fmt.Errorf("命令目录与恢复阶段不一致")
	}
	expected := map[string]bool{}
	temporary := []string{}
	for _, file := range tx.Files {
		if after, ok := strings.CutPrefix(file.Path, tx.Move.From+"/"); ok {
			expected[after] = true
			temporary = append(temporary, path.Base(transactionTemporary(file.Path)))
		}
	}
	entries, err := i.commandMembers(root, location, temporary...)
	if err != nil {
		return "", err
	}
	observed := map[string]bool{}
	for _, entry := range entries {
		if expected[entry.Name()] {
			observed[entry.Name()] = true
		} else if isCommandFile(entry.Name()) {
			return "", fmt.Errorf("命令目录出现未记录的文件：%s", entry.Name())
		}
	}
	if len(observed) != len(expected) {
		return "", fmt.Errorf("命令目录成员与恢复记录不一致")
	}
	return location, nil
}

func (i *Initializer) transactionPath(root string, tx transaction, relative string) (string, error) {
	location, err := i.commandLocation(root, tx)
	if err != nil {
		return "", err
	}
	if tx.Move != nil && strings.HasPrefix(relative, tx.Move.From+"/") {
		relative = location + strings.TrimPrefix(relative, tx.Move.From)
	}
	return i.safePath(root, relative)
}

func (i *Initializer) includeCommandFiles(root string, changes []change, move *commandMove) ([]change, error) {
	if move == nil {
		return changes, nil
	}
	entries, err := i.commandMembers(root, move.From)
	if err != nil {
		return nil, err
	}
	result := append([]change(nil), changes...)
	seen := map[string]bool{}
	for _, item := range result {
		seen[item.path] = true
	}
	for _, entry := range entries {
		file := filepath.Join(root, filepath.FromSlash(move.From), entry.Name())
		if seen[file] {
			continue
		}
		data, err := afero.ReadFile(i.FS, file)
		if err != nil {
			return nil, err
		}
		result = append(result, change{file, data, entry.Mode().Perm()})
	}
	return result, nil
}
