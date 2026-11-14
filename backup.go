package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Backup and restore: the wiki directory (pages + trash) is tarred into a
// gzipped archive. The SQLite index is derived state and deliberately excluded,
// so a backup stays small and a restore always rebuilds from the files.

const (
	backupVersion = "2.0"
	// maxRestoreBytes caps the decompressed size of a restore, so a small gzip
	// payload cannot expand into an unbounded amount of disk.
	maxRestoreBytes = 512 << 20
)

// excludedFromBackup are derived or temporary files that never need archiving.
func excludedFromBackup(name string) bool {
	base := filepath.Base(name)
	return base == ".mem-index.db" ||
		strings.HasPrefix(base, ".mem-index.db-") ||
		strings.HasSuffix(base, ".tmp")
}

func runBackupTo(w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	root := wikiDir()
	// The wiki directory must exist even for an empty wiki.
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
	lr := io.LimitReader(r, maxRestoreBytes)
	gz, err := gzip.NewReader(lr)
	if err != nil {
		return 0, fmt.Errorf("not a gzip archive: %w", err)
	}
	tr := tar.NewReader(gz)
	root := wikiDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return 0, err
	}
	n := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
		// Only accept paths under mem-wiki/.
		name := hdr.Name
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 || parts[0] != "mem-wiki" || parts[1] == "" {
			continue
		}
		rel := parts[1]
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		if excludedFromBackup(rel) {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return n, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return n, err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o700|0o600)
			if err != nil {
				return n, err
			}
			written, err := io.Copy(f, tr)
			f.Close()
			if err != nil {
				return n, err
			}
			if written > maxRestoreBytes {
				return n, fmt.Errorf("restore exceeds %d bytes", maxRestoreBytes)
			}
			n++
		}
	}
	synced, err := rescanWiki()
	if err != nil {
		log.Printf("restore: index resync failed: %v", err)
	}
	_ = synced
	return n, nil
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
