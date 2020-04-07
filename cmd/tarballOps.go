package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"github.com/metrumresearchgroup/pkgr/desc"
	"github.com/metrumresearchgroup/pkgr/gpsr"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	log "github.com/sirupsen/logrus"
)

// Tarball manipulation code taken from https://gist.github.com/indraniel/1a91458984179ab4cf80 -- is there a built-in function that does this?
func unpackTarballs(fs afero.Fs, tarballs []string, cache string) ([]desc.Desc, map[string]gpsr.AdditionalPkg) {
	var err error

	cacheDir := userCache(cache)
	//tarballCache := filepath.Join("tarballs")

	var descriptions []desc.Desc
	untarredMap := make(map[string]gpsr.AdditionalPkg)

	for _, tarballPath := range tarballs {

		var untarredFolder string
		///Download code ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
		//var downloadReaderForHash io.ReadCloser
		if strings.HasPrefix(tarballPath, "http") {
			untarredFolder, err = untarRemote(fs, tarballPath, cacheDir)
			if err != nil {
				continue
			}
		} else {
			//End download code~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
			untarredFolder = untarLocal(fs, tarballPath, cacheDir)
		}
		reader, err := fs.Open(filepath.Join(untarredFolder, "DESCRIPTION"))
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"file":  untarredFolder,
				"error": err,
			}).Fatal("error opening DESCRIPTION file for tarball package")
		}

		desc, err := desc.ParseDesc(reader)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"file":  untarredFolder,
				"error": err,
			}).Fatal("error parsing DESCRIPTION file for tarball package")
		}
		descriptions = append(descriptions, desc)
		untarredMap[desc.Package] = gpsr.AdditionalPkg{InstallPath: untarredFolder, OriginPath: tarballPath, Type: "tarball"}
	}

	return descriptions, untarredMap
}

func getTarballDownloadFolder(fs afero.Fs, cacheDir string) (string, error) {
	tdf := filepath.Join(cacheDir, "tarball_downloads")
	exists, err := afero.DirExists(fs, tdf)
	if err != nil {
		return "", err
	}

	if exists {
		return tdf, nil
	}

	err = fs.MkdirAll(tdf, 0777)
	if err != nil {
		return "", err
	}

	return tdf, nil
}

func untarRemote(fs afero.Fs, tarballPath string, cacheDir string) (string, error) {
	var downloadReader io.ReadCloser

	// Download the tarball to a temporary location
	//tarballDownloadsDir := filepath.Join(cacheDir, "additionalDownloads")
	tarballDownloadsDir, err := getTarballDownloadFolder(fs, cacheDir)
	//err := fs.MkdirAll(tarballDownloadsDir, 0777)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Error("could not make temp directory to hold downloaded tarballs")
		return "", err
	}

	// Parse tarball path into a URL object for ease of use
	urlObj, err := url.Parse(tarballPath)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
			"url": tarballPath,
		}).Error("could not parse tarball URL")
		return "", err
	}

	// Submit HTTP Get to retrieve tarball
	resp, err := http.Get(urlObj.String())
	if err != nil {
		log.WithField("tarball", tarballPath).Error("error downloading package")
		return "", err
	} else if resp.StatusCode != 200 {
		log.WithFields(log.Fields{
			//"package":     d.Package.Package,
			"url":         tarballPath,
			"status":      resp.Status,
			"status_code": resp.StatusCode,
		}).Error("bad server response")
		respBody, _ := ioutil.ReadAll(resp.Body)
		log.WithField("tarball", tarballPath).Println("body: ", string(respBody))
		return "", errors.New("non-200 response from server")
	} else { // Download successful
		downloadReader = resp.Body
	}

	urlSpecificFolder := "url" + hashString(urlObj.String())
	//urlSpecificFolder := path.Base(urlObj.Path)
	downloadedTarballPath := filepath.Join(tarballDownloadsDir, urlSpecificFolder)
	tmpTarball, err := fs.Create(downloadedTarballPath)
	if err != nil {
		log.WithFields(log.Fields{
			"err":            err,
			"downloadedTarballPath": downloadedTarballPath,
			"tarballUrl":     tarballPath,
		}).Error("error copying temporary tarball from remote location")
	}

	// Copy downloaded tarball into temporary tarball location
	io.Copy(tmpTarball, downloadReader)
	downloadReader.Close()

	// Treat the temporary tarball as a local tarball
	untarredFolder := untarLocal(fs, downloadedTarballPath, tarballDownloadsDir)

	return untarredFolder, nil
}

// Returns path to top-level package folder of untarred files
func untarLocal(fs afero.Fs, path string, cacheDir string) string {

	// Part 1
	tgzFile, err := fs.Open(path)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"path": path,
		}).Fatal("error processing specified tarball")
	}
	defer tgzFile.Close()
	tgzFileForHash, err := fs.Open(path) // Shouldn't fail if the first one passed, but I'll check anyway.
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"path": path,
		}).Fatal("error opening second copy of specified tarball for hashing")
	}
	defer tgzFileForHash.Close()

	// Part 1.5
	// Use a hash of the file so that we always regenerate when the tarball is updated.
	// This also prevents collisions when using different tarballs for the same package in a shared cache.
	tarballDirectoryName, err := getHashedTarballName(tgzFileForHash)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"path": path,
		}).Fatalf("error while creating hash for tarball in cache: %s", err)
	}
	tarballDirectoryPath := filepath.Join(cacheDir, tarballDirectoryName)

	//Part 2
	extractedDir := extractDirFromTar(tgzFile, path, tarballDirectoryPath)

	return filepath.Join(tarballDirectoryPath, extractedDir.Name())
}

func extractDirFromTar(tgzReader io.Reader, tarballPath string, destDirectory string) os.FileInfo {
	gzipStream, err := gzip.NewReader(tgzReader)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"tarballPath": tarballPath,
			"err": err,
		}).Fatal("error creating gzip stream for specified tarball")
	}
	defer gzipStream.Close()
	tarStream := tar.NewReader(gzipStream)
	for {
		header, err := tarStream.Next()

		if err == io.EOF {
			break
		} else if err != nil {
			logrus.Error("could not process file in tar stream. Error was: ", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			fs.MkdirAll(filepath.Join(destDirectory, header.Name), 0755)
			break
		case tar.TypeReg:
			dstFile := filepath.Join(destDirectory, header.Name)
			extractFile(dstFile, tarStream)
			break
		default:
			logrus.WithFields(logrus.Fields{
				"type": header.Typeflag,
				"tarballPath": tarballPath,
			}).Error("unknown file type found while processing tarball")
			break
		}
	}
	dirEntries, err := afero.ReadDir(fs, destDirectory)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"directory": destDirectory,
			"tarball":   tarballPath,
			"error":     err,
		}).Error("error encountered while reading untarred directory")
	}
	var extractedDirs []os.FileInfo
	for _, entry := range dirEntries {
		if entry.IsDir() {
			extractedDirs = append(extractedDirs, entry)
		} else {
			logrus.WithFields(logrus.Fields{
				"tarball":           tarballPath,
				"untarredDirectory": destDirectory,
				"item":              entry.Name(),
			}).Trace("extraneous item found in untarred directory. If you have previously installed this tarball via pkgr, it may be a build artifact from the last installation")
		}
	}
	if len(extractedDirs) == 0 {
		logrus.WithFields(logrus.Fields{
			"directory": destDirectory,
			"tarball":   tarballPath,
		}).Fatal("untarred directory is empty -- cannot install tarball package")
	} else if len(extractedDirs) > 1 {
		var entries []string
		for _, entry := range extractedDirs {
			entries = append(entries, entry.Name())
		}
		logrus.WithFields(logrus.Fields{
			"directory": destDirectory,
			"tarball":   tarballPath,
			"items":     entries,
			"using":     extractedDirs[0].Name(),
		}).Warn("found more than one directory at top level in unarchived tarball -- assuming first alphabetical entry is package directory")
	}
	return extractedDirs[0]
}

func getHashedTarballName(tgzFile io.Reader) (string, error) {
	hash := md5.New()
	_, err := io.Copy(hash, tgzFile)
	hashInBytes := hash.Sum(nil)[:8]
	//Convert the bytes to a string, used as a directory name for the package.
	tarballDirectoryName := hex.EncodeToString(hashInBytes)
	// Hashing code adapted from https://mrwaggel.be/post/generate-md5-hash-of-a-file-in-golang/
	return tarballDirectoryName, err
}

func extractFile(dstFile string, tarStream *tar.Reader) {
	outFile, err := os.Create(dstFile)
	if err != nil {
		logrus.Fatalf("ExtractTarGz: Create() failed: %s", err.Error())
	}
	defer outFile.Close()
	if _, err := io.Copy(outFile, tarStream); err != nil {
		logrus.Fatalf("ExtractTarGz: Copy() failed: %s", err.Error())
	}
}
