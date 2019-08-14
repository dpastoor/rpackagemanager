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
	"github.com/metrumresearchgroup/pkgr/gpsr"
	"path/filepath"
	"time"

	"github.com/spf13/viper"

	"github.com/metrumresearchgroup/pkgr/cran"
	"github.com/metrumresearchgroup/pkgr/logger"
	"github.com/metrumresearchgroup/pkgr/pacman"
	"github.com/metrumresearchgroup/pkgr/rcmd"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// installCmd represents the R CMD install command
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "install a package",
	Long: `
	install a package
 `,
	RunE: rInstall,
}

func init() {
	RootCmd.AddCommand(installCmd)
}

func rInstall(cmd *cobra.Command, args []string) error {

	// Initialize log and start time.
	initInstallLog()
	startTime := time.Now()
	rSettings := rcmd.NewRSettings(cfg.RPath)
	rVersion := rcmd.GetRVersion(&rSettings)
	log.Infoln("R Version " + rVersion.ToFullString())
	// most people should know what platform they are on
	log.Debugln("OS Platform " + rSettings.Platform)

	// Get master object containing the packages available in each repository (pkgNexus),
	//  as well as a master install plan to guide our process.
	_, installPlan := planInstall(rVersion, true)

	//Make a backup of the current library

	//Prepare our environment to update outdated packages if the "--update" flag is set.
	var packageUpdateAttempts []pacman.UpdateAttempt
	if viper.GetBool("update") {
		log.Info("update argument passed. staging packages for update...")
		packageUpdateAttempts = pacman.PreparePackagesForUpdate(fs, cfg.Library, installPlan.OutdatedPackages)
	}

	// Create a list of package download objects using our install plan and our "nexus" object.
	//pkgsToDownload := getPackagesToDownload(installPlan, pkgNexus)

	// Retrieve a cache to store any packages we need to download for the install.
	packageCache := rcmd.NewPackageCache(userCache(cfg.Cache), false)

	//Create a pkgMap object, which helps us with parallel downloads (?)
	pkgMap, err := cran.DownloadPackages(fs, installPlan.PackageDownloads, packageCache.BaseDir, rVersion)
	if err != nil {
		log.Fatalf("error downloading packages: %s", err)
	}

	//Set the arguments to be passed in to the R Package Installer
	pkgInstallArgs := rcmd.NewDefaultInstallArgs()
	pkgInstallArgs.Library, _ = filepath.Abs(cfg.Library)

	// Get the number of workers.
	// leave at least 1 thread open for coordination, given more than 2 threads available.
	// if only 2 available, will let the OS hypervisor coordinate some else would drop the
	// install time too much for the little bit of additional coordination going on.
	nworkers := getWorkerCount()

	// Process any customizations set in the yaml config file for individual packages.
	// TODO: Refactor this into its own method.
	pkgCustomizations := cfg.Customizations.Packages
	for n, v := range pkgCustomizations {
		if v.Env != nil {
			rSettings.PkgEnvVars[n] = v.Env
		}
	}

	//
	// Install the packages
	//
	err = rcmd.InstallPackagePlan(fs, installPlan, pkgMap, packageCache, pkgInstallArgs, rSettings, rcmd.ExecSettings{PkgrVersion: VERSION}, nworkers)
	// After package installation, fix any problems that occurred during reinstallation of
	//  packages that were to be updated.

	if err != nil {
		err = rollbackPackageEnvironment(installPlan, err, packageUpdateAttempts)
	}

	/*
	if cfg.Update {
		pacman.RestoreUnupdatedPackages(fs, packageUpdateAttempts)
	}
	*/
	log.Info("duration:", time.Since(startTime))

	if err != nil {
		log.Fatalf("failed package install with err, %s", err)
	}

	return nil
}

func rollbackPackageEnvironment(installPlan gpsr.InstallPlan, packageUpdateAttempts []pacman.UpdateAttempt) {
	//reset packages
	log.Info("Resetting package environment")

	//figure out which packages we were even dealing with. Maybe this should be a unified list somewhere?
	//as of August 14, 2019, the InstallPlan treats installed packages as if they were going to be installed again,
	//and we simply figure out downstream that the installed packages shouldn't be run.
	//Because of this, toInstall will end up containing info about all packages pkgr is managing, not just the ones
	//to be installed.
	toInstall := installPlan.StartingPackages
	for depsList := range installPlan.DepDb {
		toInstall = append(toInstall, depsList)
	}

	//
	for _, pkg := range toInstall {

		//Filter out the packages that were already installed -- we don't want to touch those, they shouldn't have changed.
		_, found := installPlan.InstalledPackages[pkg]
		if !found {

			//Remove packages that were installed during this run.
			err := fs.RemoveAll(filepath.Join(cfg.Library, pkg))
			if err != nil {
				log.Warn(err)
			}
		}
	}

	//Rollback updated packages -- we have to do this differently than the rest, because updated packages need to be
	//restored from backups.
	if cfg.Update {
		pacman.RollbackUpdatePackages(fs, packageUpdateAttempts)
	}
}

func initInstallLog() {
	//Init install-specific log, if one has been set. This overwrites the default log.
	if cfg.Logging.Install != "" {
		logger.AddLogFile(cfg.Logging.Install, cfg.Logging.Overwrite)
	} else {
		logger.AddLogFile(cfg.Logging.All, cfg.Logging.Overwrite)
	}
}
