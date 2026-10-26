package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Journals intentionally contain no credentials. They can contain hook secrets
// and reviewer identities, so both the directory and files must be private.
func (s *serverService) lockMergeOverride(project, slug string) (string, *os.File, error) {
	// LockFileEx is available, but that alone does not make persistence safe.
	// Go's Windows mode bits do not enforce ACL privacy, and Unix directory
	// fsync cannot be used as a durable-publication guarantee on Windows.
	// Fail before creating files or sending requests rather than weaken either
	// requirement. Enabling Windows needs an ACL-backed journal implementation.
	if runtime.GOOS == "windows" {
		return "", nil, fmt.Errorf("temporary merge-check bypass/recovery is unsupported on Windows: private ACL-backed recovery journals and durable directory persistence are not implemented; use a supported Unix client")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(configDir, "bb", "merge-recovery")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", nil, fmt.Errorf("recovery directory must be a private real directory (0700): %s", dir)
	}
	// API versions and casing must not allow concurrent bypasses of one repo.
	identity := strings.ToLower(s.client.HostRoot() + "/" + project + "/" + slug)
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	path := filepath.Join(dir, name+".json")
	lockPath := filepath.Join(dir, name+".lock")
	if info, err := os.Lstat(lockPath); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", nil, fmt.Errorf("unsafe recovery lock %s", lockPath)
		}
	} else if !os.IsNotExist(err) {
		return "", nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", nil, err
	}
	if err := lockOverrideFile(f); err != nil {
		f.Close()
		return "", nil, fmt.Errorf("cannot exclusively lock merge-check recovery (another operation may be active): %w", err)
	}
	// Never unlink lock files: another process might be waiting on that inode.
	return path, f, nil
}

func writeOverrideJournal(path string, journal *overrideJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	// This file is written only once under the repository lock. A crash during
	// writing leaves a blocking journal, but no remote writes have started yet.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return syncOverrideDirectory(filepath.Dir(path))
}

func readOverrideJournal(path string) (*overrideJournal, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("recovery journal must be a private regular file (0600): %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var j overrideJournal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("invalid recovery journal %s; manual inspection required: %w", path, err)
	}
	return &j, nil
}

func removeOverrideJournal(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncOverrideDirectory(filepath.Dir(path))
}

func syncOverrideDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
