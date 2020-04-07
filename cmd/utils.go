package cmd

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"

	"encoding/json"

	"github.com/metrumresearchgroup/pkgr/desc"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"os"
	"path/filepath"
)

// returns the cache or sets to a cache dir
func userCache(pc string) string {
	// if actually set then use that cache dir
	if pc != "" {
		log.WithField("dir", pc).Trace("package cache directory set by user")
		return pc
	}
	cdir, err := os.UserCacheDir()
	if err != nil {
		log.Warn("could not use user cache dir, using temp dir")
		cdir = os.TempDir()
	}
	log.WithField("dir", cdir).Trace("default package cache directory")

	pkgrCacheDir := filepath.Join(cdir, "pkgr")

	return pkgrCacheDir
}

func hashString(s string) string {
	h := md5.New()
	io.WriteString(h, s)
	urlHash := fmt.Sprintf("%x", h.Sum(nil))
	return urlHash[:12]
}

// If user has not specified a thread count themselves, will limit the user to 8 threads max to avoid issues.
func getWorkerCount(threadCount, numCpus int) int {
	var nworkers int
	if threadCount < 1 { // This indicates that the user has not specified a thread count, i.e. we're using the default thread count of "0".

		workerCap := 8 // We have decided to cap this number at 8 unless the user requests otherwise. This is to prevent issues with cyclic Suggests installs.
		if numCpus <= workerCap {
			nworkers = numCpus
		} else {
			nworkers = workerCap
		}
	} else {
		nworkers = threadCount
		if nworkers > numCpus + 2 {
			log.Warn("number of workers exceeds the number of threads on machine by at least 2, this may result in degraded performance")
		}
	}
	return nworkers
}

func stringInSlice(s string, slice []string) bool {
	for _, entry := range slice {
		if s == entry {
			return true
		}
	}
	return false
}

func libraryExists(fileSystem afero.Fs, libraryPath string ) bool {
	result, _ := afero.Exists(fileSystem, libraryPath)
	return result
}

// Adapted from https://stackoverflow.com/questions/28595664/how-to-stop-json-marshal-from-escaping-and
func JsonMarshal(t interface{}) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	err := encoder.Encode(t)
	return buffer.Bytes(), err
}

func extractNamesFromDesc(installedPackages map[string]desc.Desc) []string {
	var installedPackageNames []string
	for key := range installedPackages {
		installedPackageNames = append(installedPackageNames, key)
	}
	return installedPackageNames
}

