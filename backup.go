package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Backup and restore: the wiki directory (pages + trash) is tarred into a
// gzipped archive. The SQLite index is derived state and deliberately excluded,
// so a backup stays small and a restore always rebuilds from the files.

const (
	backupVersion = "2.0"
	// maxRestoreBytes caps both compressed input and aggregate extracted bytes.
	maxRestoreBytes = 512 << 20
	maxRestoreFiles = 100000
)

// excludedFromBackup are derived or temporary files that never need archiving.
func excludedFromBackup(name string) bool {
	base := filepath.Base(name)
	return base == ".mem-index.db" ||
		strings.HasPrefix(base, ".mem-index.db-") ||
		strings.HasSuffix(base, ".tmp")
}

func runBackupTo(w io.Writer) error {
	unlock, err := lockWikiWrites()
	if err != nil {
		return err
	}
	defer unlock()
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	root := wikiDir()
	// The wiki directory must exist even for an empty wiki.
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if excludedFromBackup(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join("mem-wiki", rel))
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// runRestoreFrom extracts a backup archive into the wiki directory and rebuilds
// the index from the restored files.
func runRestoreFrom(r io.Reader) (int, error) {
	lr := &io.LimitedReader{R: r, N: maxRestoreBytes + 1}
	gz, err := gzip.NewReader(lr)
	if err != nil {
		return 0, fmt.Errorf("not a gzip archive: %w", err)
	}
	tr := tar.NewReader(gz)
	root := wikiDir()
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return 0, err
	}
	stage, err := os.MkdirTemp(parent, ".mem-restore-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(stage)
	n := 0
	entries := 0
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		entries++
		if entries > maxRestoreFiles*2 {
			return n, fmt.Errorf("restore exceeds %d archive entries", maxRestoreFiles*2)
		}
		// Only accept paths under mem-wiki/.
		name := hdr.Name
		clean := path.Clean(name)
		parts := strings.SplitN(clean, "/", 2)
		if len(parts) != 2 || parts[0] != "mem-wiki" || parts[1] == "" || parts[1] == "." {
			continue
		}
		rel := parts[1]
		if strings.Contains(rel, "\\") || !filepath.IsLocal(filepath.FromSlash(rel)) {
			return n, fmt.Errorf("unsafe restore path %q", name)
		}
		if excludedFromBackup(rel) {
			continue
		}
		dst := filepath.Join(stage, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return n, err
			}
		case tar.TypeReg:
			if hdr.Size < 0 || hdr.Size > maxRestoreBytes-total {
				return n, fmt.Errorf("restore exceeds %d total extracted bytes", maxRestoreBytes)
			}
			if n >= maxRestoreFiles {
				return n, fmt.Errorf("restore exceeds %d files", maxRestoreFiles)
			}
			total += hdr.Size
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return n, err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o700|0o600)
			if err != nil {
				return n, err
			}
			written, err := io.CopyN(f, tr, hdr.Size)
			closeErr := f.Close()
			if err != nil {
				return n, err
			}
			if closeErr != nil {
				return n, closeErr
			}
			if written != hdr.Size {
				return n, io.ErrUnexpectedEOF
			}
			n++
		default:
			return n, fmt.Errorf("unsupported restore entry type %d for %q", hdr.Typeflag, name)
		}
	}
	remaining, err := io.Copy(io.Discard, io.LimitReader(gz, maxRestoreBytes-total+1))
	if err != nil {
		return n, err
	}
	if remaining > maxRestoreBytes-total {
		return n, fmt.Errorf("restore exceeds %d total extracted bytes", maxRestoreBytes)
	}
	if lr.N == 0 {
		return n, fmt.Errorf("restore exceeds %d compressed bytes", maxRestoreBytes)
	}
	if err := gz.Close(); err != nil {
		return n, err
	}
	unlock, err := lockWikiWrites()
	if err != nil {
		return n, err
	}
	defer unlock()
	old, err := replaceWikiContent(root, stage)
	if err != nil {
		return n, err
	}
	if err := indexReset(); err != nil {
		if rollbackErr := restoreWikiContent(root, old); rollbackErr != nil {
			return n, fmt.Errorf("reset index for restore: %w (rollback failed; previous data remains at %s: %v)", err, old, rollbackErr)
		}
		_ = indexReset()
		_, _ = rescanWikiUnlocked()
		return n, err
	}
	if _, err := rescanWikiUnlocked(); err != nil {
		if rollbackErr := restoreWikiContent(root, old); rollbackErr != nil {
			return n, fmt.Errorf("index restored wiki: %w (rollback failed; previous data remains at %s: %v)", err, old, rollbackErr)
		}
		_ = indexReset()
		_, _ = rescanWikiUnlocked()
		return n, err
	}
	if err := os.RemoveAll(old); err != nil {
		return n, fmt.Errorf("restore succeeded but old-data cleanup failed: %w", err)
	}
	return n, nil
}

// replaceWikiContent replaces user data while leaving the live SQLite index in
// place. Existing content is retained in a sibling directory until the staged
// content has been moved successfully, allowing rollback on filesystem errors.
func replaceWikiContent(root, stage string) (string, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	old, err := os.MkdirTemp(filepath.Dir(root), ".mem-restore-old-")
	if err != nil {
		return "", err
	}
	keepIndex := func(name string) bool {
		base := filepath.Base(name)
		return base == ".mem-index.db" || strings.HasPrefix(base, ".mem-index.db-")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		os.RemoveAll(old)
		return "", err
	}
	for _, entry := range entries {
		if keepIndex(entry.Name()) {
			continue
		}
		if err := os.Rename(filepath.Join(root, entry.Name()), filepath.Join(old, entry.Name())); err != nil {
			if rollbackErr := restoreWikiContent(root, old); rollbackErr != nil {
				return "", fmt.Errorf("move old wiki content: %w (rollback failed; previous data remains at %s: %v)", err, old, rollbackErr)
			}
			return "", err
		}
	}
	entries, err = os.ReadDir(stage)
	if err != nil {
		if rollbackErr := restoreWikiContent(root, old); rollbackErr != nil {
			return "", fmt.Errorf("read staged restore: %w (rollback failed; previous data remains at %s: %v)", err, old, rollbackErr)
		}
		return "", err
	}
	for _, entry := range entries {
		if err := os.Rename(filepath.Join(stage, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
			if rollbackErr := restoreWikiContent(root, old); rollbackErr != nil {
				return "", fmt.Errorf("install restored wiki content: %w (rollback failed; previous data remains at %s: %v)", err, old, rollbackErr)
			}
			return "", err
		}
	}
	return old, nil
}

func restoreWikiContent(root, old string) error {
	keepIndex := func(name string) bool {
		base := filepath.Base(name)
		return base == ".mem-index.db" || strings.HasPrefix(base, ".mem-index.db-")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !keepIndex(entry.Name()) {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	previous, err := os.ReadDir(old)
	if err != nil {
		return err
	}
	for _, entry := range previous {
		if err := os.Rename(filepath.Join(old, entry.Name()), filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return os.RemoveAll(old)
}

// --- CLI ---

func runCLIBackup(path string, useGzip bool) error {
	if strings.TrimSpace(path) == "" || path == "-" {
		return runBackupTo(os.Stdout)
	}
	if !useGzip && !strings.HasSuffix(path, ".gz") {
		log.Printf("note: backups are always gzipped tar archives")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := runBackupTo(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	log.Printf("Backup complete: %s", path)
	return nil
}

func runCLIRestore(path string) error {
	var src io.Reader
	if path == "" || path == "-" {
		src = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}
	n, err := runRestoreFrom(src)
	if err != nil {
		return err
	}
	log.Printf("Restore complete: %d files", n)
	return nil
}

// --- HTTP ---

func handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	name := "mem-wiki-backup-" + time.Now().Format("20061114-150405") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if err := runBackupTo(w); err != nil {
		log.Printf("backup failed: %v", err)
	}
}

func handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	n, err := runRestoreFrom(r.Body)
	if err != nil {
		jsonResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, 200, map[string]interface{}{"status": "restored", "files": n, "version": backupVersion})
}
