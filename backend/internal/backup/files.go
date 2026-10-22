package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
)

var backupName = regexp.MustCompile(`^aiq-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z$`)

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".status-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	// Windows does not support fsync on directory handles through os.File.
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func checksum(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// The operator provisions this directory on a separate encrypted mount. Never
// create it here: a missing mount must fail, not fill the application disk.
func checkStore(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return errors.New("storage_unavailable")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return errors.New("storage_permissions")
	}
	b, err := os.ReadFile(filepath.Join(dir, ".aiq-backup-store"))
	if err != nil || string(b) != "aiq-backup-v1\n" {
		return errors.New("storage_marker_missing")
	}
	return nil
}

func copies(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && backupName.MatchString(e.Name()) {
			var m Manifest
			if err := readJSON(filepath.Join(dir, e.Name(), "manifest.json"), &m); err != nil || m.ID != e.Name() || m.SHA256 == "" {
				return nil, errors.New("invalid_saved_manifest")
			}
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func validate(dir string) (Manifest, error) {
	var m Manifest
	if err := readJSON(filepath.Join(dir, "manifest.json"), &m); err != nil {
		return m, errors.New("manifest_unreadable")
	}
	if !backupName.MatchString(m.ID) || m.ID != filepath.Base(dir) || len(m.Tables) == 0 {
		return m, errors.New("invalid_manifest")
	}
	h, n, err := checksum(filepath.Join(dir, "database.dump"))
	if err != nil || h != m.SHA256 || n != m.Bytes {
		return m, errors.New("checksum_mismatch")
	}
	return m, nil
}
