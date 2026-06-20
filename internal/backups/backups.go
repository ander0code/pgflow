// Package backups scans the on-disk backup directory into folders and dumps,
// enriching each dump with size, modification time and integrity status.
package backups

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ander0code/pgflow/internal/pg"
)

// Dump is a single .dump file with its metadata.
type Dump struct {
	Name      string
	Path      string
	SizeBytes int64
	ModTime   time.Time
	Valid     bool
	Objects   int
}

// Folder groups the dumps under one project directory.
type Folder struct {
	Name  string
	Path  string
	Dumps []Dump
}

// Scan reads backupDir into folders (skipping "logs"), each with its dumps,
// newest first. A missing directory yields no folders and no error.
func Scan(backupDir string) ([]Folder, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var folders []Folder
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "logs" {
			continue
		}
		fpath := filepath.Join(backupDir, e.Name())
		folder := Folder{Name: e.Name(), Path: fpath}

		dents, _ := os.ReadDir(fpath)
		for _, d := range dents {
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".dump") {
				continue
			}
			fi, err := d.Info()
			if err != nil {
				continue
			}
			dpath := filepath.Join(fpath, d.Name())
			vr := pg.Verify(dpath)
			folder.Dumps = append(folder.Dumps, Dump{
				Name:      d.Name(),
				Path:      dpath,
				SizeBytes: fi.Size(),
				ModTime:   fi.ModTime(),
				Valid:     vr.Valid,
				Objects:   vr.Objects,
			})
		}

		sort.Slice(folder.Dumps, func(i, j int) bool {
			return folder.Dumps[i].ModTime.After(folder.Dumps[j].ModTime)
		})
		folders = append(folders, folder)
	}

	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
	return folders, nil
}

// Total returns the number of dumps across all folders.
func Total(folders []Folder) int {
	n := 0
	for _, f := range folders {
		n += len(f.Dumps)
	}
	return n
}
