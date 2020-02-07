// Copyright © 2018 Devin Pastoor <devin.pastoor@gmail.com>
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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/spf13/afero"
	"github.com/thoas/go-funk"
	"io"
	"path/filepath"
	"runtime"

	"github.com/metrumresearchgroup/pkgr/rollback"

	"github.com/metrumresearchgroup/pkgr/desc"
	"github.com/metrumresearchgroup/pkgr/pacman"

	"os"

	"strings"
	"time"

	"github.com/metrumresearchgroup/pkgr/rcmd"

	"github.com/metrumresearchgroup/pkgr/configlib"
	"github.com/metrumresearchgroup/pkgr/cran"
	"github.com/metrumresearchgroup/pkgr/gpsr"
	"github.com/sajari/fuzzy"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"archive/tar"
	"compress/gzip"
)

// planCmd shows the install plan
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "plan a full installation",
	Long: `
	see the plan for an install
 `,
	RunE: plan,
}

func init() {
	planCmd.PersistentFlags().Bool("show-deps", false, "show the (required) dependencies for each package")
	viper.BindPFlag("show-deps", planCmd.PersistentFlags().Lookup("show-deps"))
	RootCmd.AddCommand(planCmd)
}

func plan(cmd *cobra.Command, args []string) error {
	log.Infof("Installation would launch %v workers\n", getWorkerCount(cfg.Threads, runtime.NumCPU()))
	rs := rcmd.NewRSettings(cfg.RPath)
	rVersion := rcmd.GetRVersion(&rs)
	log.Infoln("R Version " + rVersion.ToFullString())
	log.Debugln("OS Platform " + rs.Platform)
	_, ip, _ := planInstall(rVersion, true)
	if viper.GetBool("show-deps") {
		for pkg, deps := range ip.DepDb {
			fmt.Println("-----------  ", pkg, "   ------------")
			fmt.Println(deps)
		}
	}
	return nil
}

func planInstall(rv cran.RVersion, exitOnMissing bool) (*cran.PkgNexus, gpsr.InstallPlan, rollback.RollbackPlan) {
	startTime := time.Now()

	//Check library existence
	libraryExists, err := afero.DirExists(fs, cfg.Library)

	if err != nil {
		log.WithFields(log.Fields{
			"library": cfg.Library,
			"error" : err,
		}).Error("unexpected error when checking existence of library")
	}

	if !libraryExists && cfg.Strict {
		log.WithFields(log.Fields{
			"library": cfg.Library,
		}).Error("library directory must exist before running pkgr in strict mode")
	}

	var installedPackageNames []string
	var installedPackages map[string]desc.Desc
	var whereInstalledFrom pacman.InstalledFromPkgs

	if libraryExists {
		installedPackages = pacman.GetPriorInstalledPackages(fs, cfg.Library)
		installedPackageNames = extractNamesFromDesc(installedPackages)
		log.WithField("count", len(installedPackages)).Info("found installed packages")
		whereInstalledFrom = pacman.GetInstallers(installedPackages)
		notPkgr := whereInstalledFrom.NotFromPkgr()
		if len(notPkgr) > 0 {
			// TODO: should this say "prior installed packages" not ...
			log.WithFields(log.Fields{
				"packages": notPkgr,
			}).Warn("Packages not installed by pkgr")
		}
	} else {
		log.WithFields(log.Fields{
			"path": cfg.Library,
		}).Info("Package Library will be created")
		//fs.Create(cfg.Library)
		//fs.Chmod(cfg.Library, 0755)
	}

	var repos []cran.RepoURL
	for _, r := range cfg.Repos {
		for nm, url := range r {
			repos = append(repos, cran.RepoURL{Name: nm, URL: url})
		}
	}
	st := cran.DefaultType()
	cic := cran.NewInstallConfig()
	for rn, val := range cfg.Customizations.Repos {
		if strings.EqualFold(val.Type, "binary") {
			cic.Repos[rn] = cran.RepoConfig{DefaultSourceType: cran.Binary}
		}
		if strings.EqualFold(val.Type, "source") {
			cic.Repos[rn] = cran.RepoConfig{DefaultSourceType: cran.Source}
		}
	}
	pkgNexus, err := cran.NewPkgDb(repos, st, cic, rv)
	if err != nil {
		log.Panicln("error getting pkgdb ", err)
	}
	log.Infoln("Default package installation type: ", st.String())
	for _, db := range pkgNexus.Db {
		log.Infoln(fmt.Sprintf("%v:%v (binary:source) packages available in for %s from %s", len(db.DescriptionsBySourceType[st]), len(db.DescriptionsBySourceType[cran.Source]), db.Repo.Name, db.Repo.URL))
	}

	dependencyConfigurations := gpsr.NewDefaultInstallDeps()
	configlib.SetPlanCustomizations(cfg, dependencyConfigurations, pkgNexus)

	var tarballDescriptions []desc.Desc
	var tarballPathMap map[string]string
	// Check for tarball installations and add deps to cfg.Packages
	if len(cfg.Tarballs) > 0 {
		tarballDescriptions, tarballPathMap = unpackTarballs(fs, cfg)
		for _, tarballDesc := range tarballDescriptions {
			tarballDeps := tarballDesc.GetCombinedDependencies(false)
			for _, d := range tarballDeps {
				if !funk.Contains(cfg.Packages, d.Name) {
					cfg.Packages = append(cfg.Packages, d.Name)
				}
			}
		}

	}
	cfg.Packages = removeBasePackages(cfg.Packages)

	availableUserPackages := pkgNexus.GetPackages(cfg.Packages)
	if len(availableUserPackages.Missing) > 0 {
		log.Errorln("missing packages: ", availableUserPackages.Missing)
		model := fuzzy.NewModel()

		// For testing only, this is not advisable on production
		model.SetThreshold(1)

		// This expands the distance searched, but costs more resources (memory and time).
		// For spell checking, "2" is typically enough, for query suggestions this can be higher
		model.SetDepth(1)
		pkgs := pkgNexus.GetAllPkgsByName()
		model.Train(pkgs)
		for _, mp := range availableUserPackages.Missing {
			log.Warnln("did you mean one of: ", model.Suggestions(mp, false))
		}
		if exitOnMissing {
			os.Exit(1)
		} else {
			return pkgNexus, gpsr.InstallPlan{}, rollback.RollbackPlan{}
		}
	}
	logUserPackageRepos(availableUserPackages.Packages)

	installPlan, err := gpsr.ResolveInstallationReqs(
		cfg.Packages,
		installedPackages,
		//tarballDescriptions,
		dependencyConfigurations,
		pkgNexus,
		cfg.Update,
		libraryExists)

	installPlan.Tarballs = tarballPathMap

	rollbackPlan := rollback.CreateRollbackPlan(cfg.Library, installPlan, installedPackages)

	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	logDependencyRepos(installPlan.PackageDownloads)

	pkgs := installPlan.GetAllPackages()

	pkgsToUpdateCount := 0
	for _, p := range installPlan.OutdatedPackages {
		updateLogFields := log.Fields{
			"pkg":               p.Package,
			"installed_version": p.OldVersion,
			"update_version":    p.NewVersion,
		}
		if cfg.Update {
			log.WithFields(updateLogFields).Info("package will be updated")
			pkgsToUpdateCount = len(installPlan.OutdatedPackages)
		} else {
			log.WithFields(updateLogFields).Warn("outdated package found")
		}
	}

	totalPackagesRequired := len(pkgs)
	toInstall := installPlan.GetNumPackagesToInstall()
	log.WithFields(log.Fields{
		"total_packages_required": totalPackagesRequired,
		"installed":               len(installedPackages),
		"outdated":                len(installPlan.OutdatedPackages),
		"not_from_pkgr":           len(whereInstalledFrom.NotFromPkgr()),
	}).Info("package installation status")

	installSources := make(map[string]int)
	for _, pkgdl := range installPlan.PackageDownloads {
		_, rn := pkgdl.PkgAndRepoNames()
		installSources[rn]++
	}
	fields := make(log.Fields)
	for k, v := range installSources {
		fields[k] = v
	}
	log.WithFields(fields).Info("package installation sources")

	log.WithFields(log.Fields{
		"to_install": toInstall,
		"to_update":  pkgsToUpdateCount,
	}).Info("package installation plan")

	if toInstall > 0 && toInstall != totalPackagesRequired {
		// log which packages to install, but only if doing an incremental install
		for _, pn := range pkgs {
			if !funk.ContainsString(installedPackageNames, pn) {
				pkgDesc, cfg, _ := pkgNexus.GetPackage(pn)
				log.WithFields(log.Fields{
					"package": pkgDesc.Package,
					"version": pkgDesc.Version,
					"repo":    cfg.Repo.Name,
					"type":    cfg.Type,
				}).Info("to install")
			}
		}
	}

	log.Infoln("resolution time", time.Since(startTime))
	return pkgNexus, installPlan, rollbackPlan
}

// Removes any "base" packages from the given list.
func removeBasePackages(pkgList []string) []string {
	var nonbasePkgList []string
	for _, p := range pkgList {
		pType, found := gpsr.DefaultPackages[p]
		if !found || pType != "base" {
			nonbasePkgList = append(nonbasePkgList, p)
		} else {
			log.WithFields(log.Fields{
				"pkg": p,
			}).Warn("removing base package from user-defined package list")
		}
	}
	return nonbasePkgList
}

// Tarball manipulation code taken from https://gist.github.com/indraniel/1a91458984179ab4cf80 -- is there a built-in function that does this?
func unpackTarballs(fs afero.Fs, cfg configlib.PkgrConfig) ([]desc.Desc, map[string]string)  {
	cacheDir := userCache(cfg.Cache)

	untarredMap := make(map[string]string)

	var untarredPaths []string
	for _, path := range cfg.Tarballs {
		untarredFolder := untar(fs, path, cacheDir)
		untarredPaths = append(untarredPaths, untarredFolder)
	}

	var descriptions []desc.Desc
	for _, path := range untarredPaths {
		reader, err := fs.Open(filepath.Join(path, "DESCRIPTION"))
		if err != nil {
			log.WithFields(log.Fields{
				"file": path,
				"error": err,
			}).Fatal("error opening DESCRIPTION file for tarball package")
		}

		desc, err := desc.ParseDesc(reader)
		if err != nil {
			log.WithFields(log.Fields{
				"file": path,
				"error": err,
			}).Fatal("error parsing DESCRIPTION file for tarball package")
		}
		descriptions = append(descriptions, desc)
		untarredMap[desc.Package] = path
	}

	// Put dependencies of tarball packages as user-level packages.
	//var deps []desc.Dep
	//for _, d := range descriptions {
	//	for _, dep := range d.GetCombinedDependencies() {
	//		deps = append(deps, dep)
	//	}
	//}
	return descriptions, untarredMap
}

// Returns path to top-level package folder of untarred files
func untar(fs afero.Fs, path string, cacheDir string) string {

	// Part 1
	tgzFile, err := fs.Open(path)
	if err != nil {
		log.WithFields(log.Fields{
			"path": path,
		}).Fatal("error processing specified tarball")
	}
	defer tgzFile.Close()
	tgzFileForHash, err := fs.Open(path) // Shouldn't fail if the first one passed, but I'll check anyway.
	if err != nil {
		log.WithFields(log.Fields{
			"path": path,
		}).Fatal("error opening second copy of specified tarball for hashing")
	}
	defer tgzFileForHash.Close()

	// Part 1.5
	//Use a hash of the file so that we always regenerate when the tarball is updated.
	tarballDirectoryName, err := GetHashedTarballName(tgzFileForHash)
	if err != nil {
		log.WithFields(log.Fields{
			"path": path,
		}).Fatalf("error while creating hash for tarball in cache: %s", err)
	}
	tarballDirectoryPath := filepath.Join(cacheDir, tarballDirectoryName)

	//Part 2
	gzipStream, err := gzip.NewReader(tgzFile)
	if err != nil {
		log.WithFields(log.Fields{
			"path": path,
		}).Fatal("error creating gzip stream for specified tarball")
	}
	defer gzipStream.Close()
	tarStream := tar.NewReader(gzipStream)
	for {
		header, err := tarStream.Next()

		if err == io.EOF {
			break
		} else if err != nil {
			log.Error("could not process file in tar stream. Error was: ", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			fs.MkdirAll(filepath.Join(tarballDirectoryPath, header.Name), 0755)
			break
		case tar.TypeReg:
			dstFile := filepath.Join(tarballDirectoryPath, header.Name)
			extractFile(dstFile, tarStream)
			break
		default:
			log.WithFields(log.Fields{
				"type": header.Typeflag,
				"path": path,
			}).Error("unknown file type found while processing tarball")
			break
		}
	}

	dirEntries, err := afero.ReadDir(fs, tarballDirectoryPath)
	if err != nil {
		log.WithFields(log.Fields{
			"directory": tarballDirectoryPath,
			"tarball": path,
			"error": err,
		}).Error("error encountered while reading untarred directory")
	}

	if len(dirEntries) == 0 {
		log.WithFields(log.Fields{
			"directory": tarballDirectoryPath,
			"tarball": path,
		}).Fatal("untarred directory is empty -- cannot install tarball package")
	} else if len(dirEntries) > 1 {
		log.WithFields(log.Fields{
			"directory": tarballDirectoryPath,
			"tarball": path,
		}).Warn("found more than one item at top level in unarchived tarball -- assuming first alphabetical entry is package directory")
	}

	//log.WithFields(log.Fields{
	//	"directory": tarballDirectoryPath,
	//	"tarball": path,
	//	"returning": filepath.Join(tarballDirectoryPath, dirEntries[0].Name()),
	//}).Info("returning tarball package path")
	return filepath.Join(tarballDirectoryPath, dirEntries[0].Name())
}

func extractFile(dstFile string, tarStream *tar.Reader) {
	outFile, err := os.Create(dstFile)
	if err != nil {
		log.Fatalf("ExtractTarGz: Create() failed: %s", err.Error())
	}
	defer outFile.Close()
	if _, err := io.Copy(outFile, tarStream); err != nil {
		log.Fatalf("ExtractTarGz: Copy() failed: %s", err.Error())
	}
}

func GetHashedTarballName(tgzFile afero.File) (string, error) {
	hash := md5.New()
	_, err := io.Copy(hash, tgzFile)
	hashInBytes := hash.Sum(nil)[:8]
	//Convert the bytes to a string, used as a directory name for the package.
	tarballDirectoryName := hex.EncodeToString(hashInBytes)
	// Hashing code adapted from https://mrwaggel.be/post/generate-md5-hash-of-a-file-in-golang/
	return tarballDirectoryName, err
}

func logUserPackageRepos(packageDownloads []cran.PkgDl) {
	for _, pkg := range packageDownloads {
		log.WithFields(log.Fields{
			"pkg":          pkg.Package.Package,
			"repo":         pkg.Config.Repo.Name,
			"type":         pkg.Config.Type,
			"version":      pkg.Package.Version,
			"relationship": "user package",
		}).Debug("package repository set")
	}
}

func logDependencyRepos(dependencyDownloads []cran.PkgDl) {
	for _, pkgToDownload := range dependencyDownloads {
		pkg := pkgToDownload.Package.Package

		if !stringInSlice(pkg, cfg.Packages) {
			log.WithFields(log.Fields{
				"pkg":          pkgToDownload.Package.Package,
				"repo":         pkgToDownload.Config.Repo.Name,
				"type":         pkgToDownload.Config.Type,
				"version":      pkgToDownload.Package.Version,
				"relationship": "dependency",
			}).Debug("package repository set")
		}
	}
}

func extractNamesFromDesc(installedPackages map[string]desc.Desc) []string {
	var installedPackageNames []string
	for key := range installedPackages {
		installedPackageNames = append(installedPackageNames, key)
	}
	return installedPackageNames
}
