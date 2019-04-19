package cran

import (
	"bytes"
	"crypto/md5"
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/metrumresearchgroup/pkgr/desc"
	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
)

// NewRepoDb returns a new Repo database
func NewRepoDb(url RepoURL, dst SourceType, rc RepoConfig, rv RVersion) (*RepoDb, error) {
	ddb := &RepoDb{
		Dbs:  make(map[SourceType]map[string]desc.Desc),
		Time: time.Now(),
		Repo: url,
	}
	if rc.DefaultSourceType == Default {
		ddb.DefaultSourceType = dst
	} else {
		ddb.DefaultSourceType = rc.DefaultSourceType
	}

	if SupportsCranBinary() {
		ddb.Dbs[Binary] = make(map[string]desc.Desc)
	}

	ddb.Dbs[Source] = make(map[string]desc.Desc)

	return ddb, ddb.FetchPackages(rv)
}

// GetPackageDbFilePath get the filepath for the cached pkgdb
func (r *RepoDb) GetPackageDbFilePath() string {
	cdir, err := os.UserCacheDir()
	if err != nil {
		fmt.Println("could not use user cache dir, using temp dir")
		cdir = os.TempDir()
	}
	pkgdbHash := r.Hash()
	return filepath.Join(cdir, "pkgr", "r_packagedb_caches", pkgdbHash)
}

// Decode decodes the package database
func (r *RepoDb) Decode(file string) error {
	f, err := os.Open(file)
	if err != nil {
		fmt.Println("problem opening crandb", file)
		return err
	}
	d := gob.NewDecoder(f)
	return d.Decode(&r.Dbs)
}

// Encode encodes the PackageDatabase
func (r *RepoDb) Encode(file string) error {
	err := os.MkdirAll(filepath.Dir(file), 0777)
	if err != nil {
		return err
	}
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	e := gob.NewEncoder(f)

	// Encoding the map
	err = e.Encode(r.Dbs)
	if err != nil {
		return err
	}
	return nil
}

// Hash provides a hash based on the RepoDb sources
func (r *RepoDb) Hash() string {
	h := md5.New()
	// want to get the unique elements in the Dbs so the cache
	// will be representative of the config. Eg if set to only source
	// vs Source/Binary
	stsum := Source
	for st := range r.Dbs {
		stsum += st + 1
	}
	io.WriteString(h, r.Repo.Name+r.Repo.URL+string(stsum))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// GetBaseURL provides the base URL for a package in a cran-like repo given the source type and version of R
func GetBaseURL(r RepoURL, st SourceType, rv RVersion) string {
	if st == Source {
		return fmt.Sprintf("%s/src/contrib/PACKAGES", strings.TrimSuffix(r.URL, "/"))
		// TODO: fix so isn't hard coded to 3.5 binaries
	}
	return fmt.Sprintf("%s/bin/%s/contrib/%s/PACKAGES", strings.TrimSuffix(r.URL, "/"), cranBinaryURL(), rv.ToString())
}

// FetchPackages gets the packages for  RepoDb
// R_AVAILABLE_PACKAGES_CACHE_CONTROL_MAX_AGE controls the timing to requery the cache in R
func (r *RepoDb) FetchPackages(rv RVersion) error {
	var err error
	pkgdbFile := r.GetRepoDbCacheFilePath()
	if fi, err := os.Stat(pkgdbFile); !os.IsNotExist(err) {
		if fi.ModTime().Add(1*time.Hour).Unix() > time.Now().Unix() {
			// only read if was cached in the last hour
			return r.Decode(pkgdbFile)
		}
		err := os.Remove(pkgdbFile)
		if err != nil {
			fmt.Println("error removing cache ", pkgdbFile, err)
		}
	}
	type dldb struct {
		St   SourceType
		Stdb map[string]desc.Desc
		Err  error
	}
	dlchan := make(chan dldb, len(r.Dbs))
	defer close(dlchan)
	for st := range r.Dbs {
		go func(st SourceType) {
			ddb := make(map[string]desc.Desc)
			pkgURL := GetBaseURL(r.Repo, st, rv)
			var body []byte
			if strings.HasPrefix(pkgURL, "http") {
				res, err := http.Get(pkgURL)
				if res.StatusCode != 200 {
					dlchan <- dldb{St: st,
						Stdb: ddb,
						Err:  fmt.Errorf("failed fetching PACKAGES file from %s, with status %s", pkgURL, res.Status)}
					return
				}
				if err != nil {
					err = fmt.Errorf("problem getting packages from url %s: %s", pkgURL, err)
					dlchan <- dldb{St: st, Stdb: ddb, Err: err}
					return
				}
				defer res.Body.Close()
				body, err = ioutil.ReadAll(res.Body)
				if err != nil {
					err = fmt.Errorf("error reading body: %s", err)
					dlchan <- dldb{St: st, Stdb: ddb, Err: err}
					return
				}
			} else {
				pkgdir, _ := homedir.Expand(pkgURL)
				pkgdir, _ = filepath.Abs(pkgdir)
				if fi, err := os.Open(pkgdir); !os.IsNotExist(err) {
					body, err = ioutil.ReadAll(fi)
				} else {
					err = fmt.Errorf("no package file found at: %s", pkgdir)
					dlchan <- dldb{St: st, Stdb: ddb, Err: err}
					return
				}
			}
			cb := bytes.Split(body, []byte("\n\n"))
			for _, p := range cb {
				if len(p) == 0 {
					// end of file might have double spaces
					// and thus will be one split, so want
					// to skip that
					continue
				}
				reader := bytes.NewReader(p)
				d, err := desc.ParseDesc(reader)
				ddb[d.Package] = d
				if err != nil {
					fmt.Println("problem parsing package with info ", string(p))
					fmt.Println(err)
					dlchan <- dldb{St: st, Stdb: ddb, Err: err}
					return
				}
			}
			dlchan <- dldb{St: st, Stdb: ddb, Err: err}
		}(st)

	}
	nerr := 0
	var lasterr error
	for i := 0; i < len(r.Dbs); i++ {
		result := <-dlchan
		if result.Err != nil {
			log.Warnf("error downloading repo %s, type: %s, with information: %s\n", r.Repo.Name, result.St, result.Err)
			nerr++
			lasterr = result.Err
			// if one repo fails should return the error and not continue
			// as don't want a partial repodb as it might cause improperly pulled packages
		} else {
			r.Dbs[result.St] = result.Stdb
		}
	}
	// if only one source fails, this could be because it isn't present - eg if have binary/source but only source available
	if len(r.Dbs) > 1 && nerr == len(r.Dbs) {
		return lasterr
	}
	return r.Encode(pkgdbFile)
}

//GetRepoDbCacheFilePath Get the filename of the file in the cache that will store this RepoDB
func (r *RepoDb) GetRepoDbCacheFilePath() string {
	cdir, err := os.UserCacheDir()
	if err != nil {
		fmt.Println("could not use user cache dir, using temp dir")
		cdir = os.TempDir()
	}
	return (filepath.Join(cdir, "pkgr", "r_packagedb_caches", r.Hash()))
}
