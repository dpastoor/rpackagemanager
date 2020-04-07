// Copyright © 2019 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/afero"

	"github.com/dpastoor/goutils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

//var cleanAllFromCache bool
var srcOnly bool
var binariesOnly bool
var reposToClear string

// cacheCmd represents the cache command
var cleanCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Subcommand to clean cached source and binary files.",
	Long: `This command is a subcommand of the "clean" command.

	Using this command deletes cached source and binary files. Use the
	--src and --binary options to specify which repos to clean each
	file type from.

	`,
	RunE: cache,
}

func init() {
	//cleanCacheCmd.Flags().BoolVar(&cleanAllFromCache, "all", true, "Clean both src and binary files from the cache.")
	cleanCacheCmd.Flags().BoolVar(&srcOnly, "src-only", false, "Clean only src files from the cache")
	cleanCacheCmd.Flags().BoolVar(&binariesOnly, "binaries-only", false, "Clean only binary files from the cache")
	cleanCacheCmd.Flags().StringVar(&reposToClear, "repos", "ALL", "Comma separated list of repositories to be cleaned. Defaults to all.")

	CleanCmd.AddCommand(cleanCacheCmd)
}

func cache(cmd *cobra.Command, args []string) error {
	cleanCacheFolders()
	return nil
}

func cleanCacheFolders() error {
	cachePath := userCache(cfg.Cache)
	var repos []string // make empty
	if reposToClear != "ALL" {
		repos = strings.Split(reposToClear, ",")
		if len(repos) == 0 {
			return errors.New("default 'repos' argument overridden but no repos provided. Please list repos in comma-separated list with no spaces")
		}
	}

	log.WithFields(log.Fields{
		"repos argument": reposToClear,
		"repos parsed":   sliceToString(repos),
		"cache dir":      cachePath,
	}).Trace("cleaning cache")

	if !srcOnly && !binariesOnly {
		log.Info("clearing source and binary files from the cache")
		deleteAllCacheSubfolders(fs, repos, cachePath)
	} else if srcOnly && binariesOnly {
		err := errors.New("invalid argument combination -- cannot combine srcOnly and binaryOnly flags")
		log.Error(err)
		return err
	} else if srcOnly {
		log.Info("clearing source files only from the cache")
		deleteCacheSubfolders(fs, repos, "src", cachePath)
	} else if binariesOnly {
		log.Info("clearing binary files only from the cache")
		deleteCacheSubfolders(fs, repos, "binary", cachePath)
	} else {
		return errors.New("'what? that's impossible! my logic is flawless!'")
	}

	// Clear the tarballs
	log.Info("clearing unpacked tarballs from the cache")
	for _, tgzFile := range cfg.Tarballs {
		if !strings.HasPrefix(tgzFile, "http") {
			openedTgz, err := fs.Open(tgzFile)
			if err != nil {
				log.WithField("file", tgzFile).Warn("could not find untarred directory in local cache for file")
				continue
			}
			hashedDirectoryName, err := getHashedTarballName(openedTgz)
			if err != nil {
				log.WithField("file", tgzFile).Warn("could not get hashed name for tarball when cleaning cache. skipping delete...")
				continue
			}
			err = fs.RemoveAll(filepath.Join(cfg.Cache, hashedDirectoryName))
			if err != nil {
				log.WithFields(log.Fields{
					"file":  tgzFile,
					"error": err,
				}).Error("error removing tarball from cache")
			}
		}
	}
	remoteDownloadsDir, _ := getTarballDownloadFolder(fs, cachePath)
	err := fs.RemoveAll(remoteDownloadsDir)
	if err != nil {
		log.WithFields(log.Fields{
			"dir":  remoteDownloadsDir,
			"error": err,
		}).Error("error removing downloaded tarball dir from cache")
	}

	return nil
}

func deleteAllCacheSubfolders(fs afero.Fs, repos []string, cacheDirectory string) {
	deleteCacheSubfolders(fs, repos, "src", cacheDirectory)
	deleteCacheSubfolders(fs, repos, "binary", cacheDirectory)
}

func deleteCacheSubfolders(fs afero.Fs, repos []string, subfolder string, cacheDirectory string) error {
	var err error

	//Filesystem object for the cache directory
	cacheDirFsObject, err := fs.Open(cacheDirectory)
	defer cacheDirFsObject.Close()
	if err != nil {
		return err
	}

	//File info object, I guess I needed this
	cacheDirObject, _ := cacheDirFsObject.Readdir(0)
	repoFolderNames := goutils.ListDirNames(cacheDirObject)

	if repos == nil || len(repos) == 0 {
		for _, repoFolder := range repoFolderNames {
			subfolderPath := filepath.Join(
				cacheDirectory,
				repoFolder,
				subfolder,
			)
			err = fs.RemoveAll(subfolderPath)
			if err != nil {
				log.Error(err)
			}

			absoluteRepoPath, _ := filepath.Abs(filepath.Join(cacheDirectory, repoFolder))
			dirIsEmpty, _ := afero.IsEmpty(fs, absoluteRepoPath)
			if dirIsEmpty {
				err = fs.Remove(absoluteRepoPath)
				if err != nil {
					log.Error(err)
				}
			}

		}
	} else {
		for _, repoToClear := range repos {
			for _, repoFolder := range repoFolderNames {

				if repoToClear == repoFolder {

					subfolderPath := filepath.Join(
						cacheDirectory,
						repoFolder,
						subfolder,
					)

					log.WithFields(log.Fields{
						"repoToClear":     repoToClear,
						"repoFolder Name": repoFolder,
						"subfolder":       subfolder,
						"subfolder path":  subfolderPath,
					}).Trace("match found")

					err = fs.RemoveAll(subfolderPath)
					if err != nil {
						log.Error(err)
					}

					absoluteRepoPath, _ := filepath.Abs(filepath.Join(cacheDirectory, repoFolder))
					dirIsEmpty, _ := afero.IsEmpty(fs, absoluteRepoPath)
					if dirIsEmpty {
						err = fs.Remove(absoluteRepoPath)
						if err != nil {
							log.Error(err)
						}
					}
				}
			}
		}
	}
	return err
}

func sliceToString(str []string) string {
	returnString := "Size: " + strconv.Itoa(len(str)) + " :"
	for _, s := range str {
		returnString += s + "|"
	}
	return returnString
}
