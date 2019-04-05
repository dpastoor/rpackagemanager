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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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

func plan(cmd *cobra.Command, args []string) error {
	log.Infof("Installation would launch %v workers\n", getWorkerCount())
	rs := rcmd.NewRSettings(cfg.RPath)
	rVersion := rcmd.GetRVersion(&rs)
	log.Infoln("R Version " + rVersion.ToFullString())
	_, ip := planInstall(rVersion)
	if viper.GetBool("show-deps") {
		for pkg, deps := range ip.DepDb {
			fmt.Println("-----------  ", pkg, "   ------------")
			fmt.Println(deps)
		}
	}
	return nil
}

func init() {
	planCmd.PersistentFlags().Bool("show-deps", false, "show the (required) dependencies for each package")
	viper.BindPFlag("show-deps", planCmd.PersistentFlags().Lookup("show-deps"))
	RootCmd.AddCommand(planCmd)
}

func planInstall(rv cran.RVersion) (*cran.PkgNexus, gpsr.InstallPlan) {
	startTime := time.Now()

	var pkgsToInstall []string
	if cfg.PackratDir != "" {
		pkgsToInstall = cfg.Packages
		packratPackages := readPackratPackagesFromLockfile(filepath.Join(cfg.PackratDir, "packrat.lock"))
		for _, packratPackage := range(packratPackages) {
			pkgsToInstall = append(pkgsToInstall, packratPackage.Package)
		}
	} else {
		pkgsToInstall = cfg.Packages
	}

	log.WithField("packages", pkgsToInstall).Info("Packages")

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

	pkgNexus, err := cran.NewPkgNexus(repos, st, cic, rv)

	if err != nil {
		log.Panicln("error getting pkgdb ", err)
	}
	log.Infoln("Default package type: ", st.String())
	for _, db := range pkgNexus.Db {
		log.Infoln(fmt.Sprintf("%v:%v (binary:source) packages available in for %s from %s", len(db.Dbs[st]), len(db.Dbs[cran.Source]), db.Repo.Name, db.Repo.URL))
	}
	ids := gpsr.NewDefaultInstallDeps()
	if cfg.Suggests {
		for _, pkg := range pkgsToInstall {
			// set all top level packages to install suggests
			dp := ids.Default
			dp.Suggests = true
			ids.Deps[pkg] = dp
		}
	}
	if viper.Sub("Customizations") != nil && viper.Sub("Customizations").AllSettings()["packages"] != nil {
		pkgSettings := viper.Sub("Customizations").AllSettings()["packages"].([]interface{})
		//repoSettings := viper.Sub("Customizations").AllSettings()["packages"].([]interface{})
		for pkg, v := range cfg.Customizations.Packages {
			if configlib.IsCustomizationSet("Suggests", pkgSettings, pkg) {
				dp := ids.Default
				dp.Suggests = v.Suggests
				ids.Deps[pkg] = dp
			}
			if configlib.IsCustomizationSet("Repo", pkgSettings, pkg) {
				err := pkgNexus.SetPackageRepo(pkg, v.Repo)
				if err != nil {
					log.WithFields(log.Fields{
						"pkg":  pkg,
						"repo": v.Repo,
					}).Fatal("error finding custom repo to set")
				}
			}
			if configlib.IsCustomizationSet("Type", pkgSettings, pkg) {
				err := pkgNexus.SetPackageType(pkg, v.Type)
				if err != nil {
					log.WithFields(log.Fields{
						"pkg":  pkg,
						"repo": v.Repo,
					}).Fatal("error finding custom repo to set")
				}
			}
		}
	}
	availablePkgs := pkgNexus.GetPackages(pkgsToInstall)
	if len(availablePkgs.Missing) > 0 {
		log.Errorln("missing packages: ", availablePkgs.Missing)
		model := fuzzy.NewModel()

		// For testing only, this is not advisable on production
		model.SetThreshold(1)

		// This expands the distance searched, but costs more resources (memory and time).
		// For spell checking, "2" is typically enough, for query suggestions this can be higher
		model.SetDepth(1)
		pkgs := pkgNexus.GetAllPkgsByName()
		model.Train(pkgs)
		for _, mp := range availablePkgs.Missing {
			log.Warnln("did you mean one of: ", model.Suggestions(mp, false))
		}
		os.Exit(1)
	}
	for _, pkg := range availablePkgs.Packages {
		log.WithFields(log.Fields{
			"pkg":     pkg.Package.Package,
			"repo":    pkg.Config.Repo.Name,
			"type":    pkg.Config.Type,
			"version": pkg.Package.Version,
		}).Info("package repository set")
	}
	ip, err := gpsr.ResolveInstallationReqs(pkgsToInstall, ids, pkgNexus)
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	pkgs := ip.StartingPackages
	for pkg := range ip.DepDb {
		pkgs = append(pkgs, pkg)
	}
	log.Infoln("total packages required:", len(ip.StartingPackages)+len(ip.DepDb))
	log.Infoln("resolution time", time.Since(startTime))
	return pkgNexus, ip
}

type PackratStanza struct {
	Package string
	SourceRepo string
	Version string
	Hash string
	Requires string //We won't use this so I won't bother parsing it out.
}

func readPackratPackagesFromLockfile(lockfilePath string) []PackratStanza {
	//lockfile, err := fs.Open(lockfilePath)
	//scanner := bufio.NewScanner(lockfile)

	var returnList []PackratStanza

	entireFile, _ := ioutil.ReadFile(lockfilePath)
	entireFileAsString := string(entireFile)
	stanzas := strings.Split(entireFileAsString, "\n\n")


	for _, stanza := range(stanzas) {
		var pkgName, sourceRepo, version, hash, requires = "", "", "", "", ""
		lines := strings.Split(stanza, "\n")
		for _, line := range(lines) {
			log.WithField("line", line).Debug("scanning line from lockfile")
			tokens := strings.Split(line, ": ") //the space after the colon is important
			if len(tokens) == 2 {
				key := tokens[0]
				value := tokens[1]
				switch key {
				case "Package":
					pkgName = value
				case "Source":
					sourceRepo = value
				case "Version":
					version = value
				case "Hash":
					hash = value
				case "Requires":
					requires = value
				default:
					log.WithField("key", key).Info("unprocessed key in packrat lockfile")
				}
			}
		}
		if(pkgName != "") {
			returnList = append(returnList,
				PackratStanza{
					Package: pkgName,
					SourceRepo: sourceRepo,
					Version: version,
					Hash: hash,
					Requires: requires,
				})
		}
	}
	return returnList
}