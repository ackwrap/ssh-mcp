package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func mergeJSONConfig(path string, rootPath []string, entry map[string]any) (string, error) {
	if len(rootPath) == 0 {
		return "", errors.New("配置字段路径不能为空")
	}
	snapshot, err := captureFile(path)
	if err != nil {
		return "", err
	}
	root := map[string]any{}
	if snapshot.exists && len(bytes.TrimSpace(snapshot.data)) > 0 {
		clean := removeTrailingCommas(stripJSONComments(snapshot.data))
		if !bytes.Equal(clean, snapshot.data) {
			return "", fmt.Errorf("配置文件包含注释或尾逗号，为避免破坏原格式未修改 %s", path)
		}
		root, err = decodeJSONObject(snapshot.data)
		if err != nil {
			return "", fmt.Errorf("配置文件不是有效的 JSON/JSONC，未修改 %s: %w", path, err)
		}
	}
	entries := root
	for index, key := range rootPath {
		next, ok := entries[key].(map[string]any)
		if !ok {
			if entries[key] != nil {
				return "", fmt.Errorf("配置字段 %s 不是对象，未修改 %s", strings.Join(rootPath[:index+1], "."), path)
			}
			next = map[string]any{}
			entries[key] = next
		}
		entries = next
	}
	entries[serverName] = entry
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	backup, err := snapshot.writeBackup()
	if err != nil {
		return "", err
	}
	mode := fs.FileMode(0o600)
	if snapshot.exists {
		mode = snapshot.mode
	}
	unchanged, err := snapshot.unchanged()
	if err != nil {
		return backup, fmt.Errorf("重新检查配置失败: %w", err)
	}
	if !unchanged {
		return backup, errors.New("配置已被其他进程更新，请刷新检测后重试")
	}
	if err := atomicWrite(path, data, mode); err != nil {
		if restoreErr := snapshot.restore(); restoreErr != nil {
			return backup, fmt.Errorf("写入配置失败: %v；自动回滚也失败: %v；请从备份手动恢复", err, restoreErr)
		}
		return backup, fmt.Errorf("写入配置失败: %w", err)
	}
	return backup, nil
}

type jsonRemoval struct {
	snapshot fileSnapshot
	data     []byte
}

func removeJSONConfigs(paths []string, rootPaths ...[]string) (string, bool, error) {
	if len(rootPaths) == 0 {
		return "", false, errors.New("配置字段路径不能为空")
	}
	for _, rootPath := range rootPaths {
		if len(rootPath) == 0 {
			return "", false, errors.New("配置字段路径不能为空")
		}
	}
	removals := make([]jsonRemoval, 0, len(paths))
	for _, path := range paths {
		snapshot, err := captureFile(path)
		if err != nil {
			return "", false, err
		}
		if !snapshot.exists || len(bytes.TrimSpace(snapshot.data)) == 0 {
			continue
		}
		root, err := decodeJSONObject(snapshot.data)
		if err != nil {
			return "", false, fmt.Errorf("配置文件不是有效的 JSON/JSONC，未修改 %s: %w", path, err)
		}
		changed := false
		for _, rootPath := range rootPaths {
			entries, ok := objectAtPath(root, rootPath)
			if !ok {
				continue
			}
			if _, exists := entries[serverName]; exists {
				delete(entries, serverName)
				changed = true
			}
		}
		if !changed {
			continue
		}
		clean := removeTrailingCommas(stripJSONComments(snapshot.data))
		if !bytes.Equal(clean, snapshot.data) {
			return "", false, fmt.Errorf("配置文件包含注释或尾逗号，为避免破坏原格式未修改 %s", path)
		}
		data, err := json.MarshalIndent(root, "", "  ")
		if err != nil {
			return "", false, err
		}
		removals = append(removals, jsonRemoval{snapshot: snapshot, data: append(data, '\n')})
	}
	if len(removals) == 0 {
		return "", false, nil
	}
	if len(removals) > 1 {
		return "", false, errors.New("多个配置文件同时包含 ssh-mcp，为避免跨文件覆盖未自动删除，请先保留一个有效配置")
	}
	snapshots := make([]fileSnapshot, 0, len(removals))
	for _, removal := range removals {
		snapshots = append(snapshots, removal.snapshot)
	}
	backup, err := writeBackups(snapshots)
	if err != nil {
		return backup, false, err
	}
	for _, snapshot := range snapshots {
		unchanged, err := snapshot.unchanged()
		if err != nil {
			return backup, false, fmt.Errorf("重新检查配置失败: %w", err)
		}
		if !unchanged {
			return backup, false, errors.New("配置已被其他进程更新，请刷新检测后重试")
		}
	}
	for _, removal := range removals {
		if err := atomicWrite(removal.snapshot.path, removal.data, removal.snapshot.mode); err != nil {
			return backup, false, fmt.Errorf("移除配置失败，原文件未替换: %w", err)
		}
	}
	return backup, true, nil
}

type fileSnapshot struct {
	path   string
	exists bool
	data   []byte
	mode   fs.FileMode
}

func captureFile(path string) (fileSnapshot, error) {
	snapshot := fileSnapshot{path: path, mode: 0o600}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, fmt.Errorf("读取配置文件失败: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return snapshot, err
	}
	snapshot.exists = true
	snapshot.data = data
	snapshot.mode = info.Mode().Perm()
	return snapshot, nil
}

func captureFiles(paths []string) ([]fileSnapshot, error) {
	snapshots := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot, err := captureFile(path)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func writeBackups(snapshots []fileSnapshot) (string, error) {
	backup := ""
	for _, snapshot := range snapshots {
		path, err := snapshot.writeBackup()
		if err != nil {
			return backup, err
		}
		if backup == "" && path != "" {
			backup = path
		}
	}
	return backup, nil
}

func restoreSnapshots(snapshots []fileSnapshot) error {
	var failures []error
	for _, snapshot := range snapshots {
		if err := snapshot.restore(); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", snapshot.path, err))
		}
	}
	return errors.Join(failures...)
}

func (s fileSnapshot) writeBackup() (string, error) {
	if !s.exists {
		return "", nil
	}
	backup := s.path + ".ssh-mcp.bak"
	if err := atomicWrite(backup, s.data, s.mode); err != nil {
		return "", fmt.Errorf("创建配置备份失败: %w", err)
	}
	return backup, nil
}

func (s fileSnapshot) restore() error {
	if s.exists {
		return atomicWrite(s.path, s.data, s.mode)
	}
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s fileSnapshot) unchanged() (bool, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return !s.exists, nil
	}
	if err != nil {
		return false, err
	}
	return s.exists && bytes.Equal(data, s.data), nil
}

func atomicWrite(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ssh-mcp-integration-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeJSONObject(data)
}

func decodeJSONObject(data []byte) (map[string]any, error) {
	clean := removeTrailingCommas(stripJSONComments(data))
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("配置文件顶层必须是 JSON 对象")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("配置文件包含多个 JSON 值")
	}
	return value, nil
}

func stripJSONComments(data []byte) []byte {
	result := append([]byte(nil), data...)
	inString, escaped, lineComment, blockComment := false, false, false, false
	for index := 0; index < len(result); index++ {
		current := result[index]
		if lineComment {
			if current == '\n' || current == '\r' {
				lineComment = false
			} else {
				result[index] = ' '
			}
			continue
		}
		if blockComment {
			if current == '*' && index+1 < len(result) && result[index+1] == '/' {
				result[index], result[index+1] = ' ', ' '
				index++
				blockComment = false
			} else if current != '\n' && current != '\r' {
				result[index] = ' '
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			continue
		}
		if current == '/' && index+1 < len(result) {
			if result[index+1] == '/' {
				result[index], result[index+1] = ' ', ' '
				index++
				lineComment = true
			} else if result[index+1] == '*' {
				result[index], result[index+1] = ' ', ' '
				index++
				blockComment = true
			}
		}
	}
	return result
}

func removeTrailingCommas(data []byte) []byte {
	result := make([]byte, 0, len(data))
	inString, escaped := false, false
	for index := 0; index < len(data); index++ {
		current := data[index]
		if inString {
			result = append(result, current)
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			result = append(result, current)
			continue
		}
		if current == ',' {
			next := index + 1
			for next < len(data) && (data[next] == ' ' || data[next] == '\t' || data[next] == '\n' || data[next] == '\r') {
				next++
			}
			if next < len(data) && (data[next] == '}' || data[next] == ']') {
				continue
			}
		}
		result = append(result, current)
	}
	return result
}

var codexSection = regexp.MustCompile(`(?m)^\s*\[\s*mcp_servers\.(?:ssh-mcp|"ssh-mcp")\s*\]\s*$`)
