package configlib

import (
	"bytes"
	"fmt"
	"github.com/thoas/go-funk"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/metrumresearchgroup/pkgr/cran"
	"github.com/metrumresearchgroup/pkgr/gpsr"
	"github.com/metrumresearchgroup/pkgr/rcmd"
	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

// packrat uses R.version platform, which is not the same as the Platform
// as printed in R --version, at least on windows
func packratPlatform(p string) string {
	switch p {
	case "x86_64-w64-mingw32/x64":
		return "x86_64-w64-mingw32"
	default:
		return p
	}
}

// NewConfig initialize a PkgrConfig passed in by caller
func NewConfig(cfg *PkgrConfig) {
	_ = viper.Unmarshal(cfg)
	if len(cfg.Library) == 0 {
		rs := rcmd.NewRSettings(cfg.RPath)
		rVersion := rcmd.GetRVersion(&rs)
		cfg.Library = getLibraryPath(cfg.Lockfile.Type, cfg.RPath, rVersion, rs.Platform, cfg.Library)
	}

	// For all cfg values that can be paths, make sure that ~ is expanded to the home directory.
	cfg.Library = expandTilde(cfg.Library)
	cfg.RPath = expandTilde(cfg.RPath)
	cfg.Tarballs = expandTildes(cfg.Tarballs)
	cfg.Repos = expandTildesRepos(cfg.Repos)
	cfg.Logging.All = expandTilde(cfg.Logging.All)
	cfg.Logging.Install = expandTilde(cfg.Logging.Install)
	cfg.Cache = expandTilde(cfg.Cache)

	return
}

/// expand the ~ at the beginning of a path to the home directory.
/// consider any problems a fatal error.
func expandTilde(p string) string {
	expanded, err := homedir.Expand(p)
	if err != nil {
		log.WithFields(log.Fields{
			"path": p,
			"error": err,
		}).Fatal("problem parsing config file -- could not expand path")
	}
	return expanded
}

/// For a list of paths, expand the ~ at the beginning of each path to the home directory.
/// consider any problems a fatal error.
func expandTildes(paths []string) []string {
	var expanded []string
	for _, p := range paths {
		newPath := expandTilde(p)
		expanded = append(expanded, newPath)
	}
	return expanded
}

/// In the PkgrConfig object, Repos are stored as a list of key-value pairs.
/// Keys are repo names and values are paths to those repos
/// For each key-value pair, expand the prefix ~ to be the home directory, if applicable.
/// consider any problems a fatal error.
func expandTildesRepos(repos []map[string]string) []map[string]string {
	var expanded []map[string]string
	//expanded := make(map[string]string)
	for _, keyValuePair := range repos {
		kvpExpanded := make(map[string]string)
		for key, p := range keyValuePair { // should only be one pair, but loop just in case
			kvpExpanded[key] = expandTilde(p)
		}
		expanded = append(expanded, kvpExpanded)
	}

	return expanded
}

func getLibraryPath(lockfileType string, rpath string, rversion cran.RVersion, platform string, library string) string {
	switch lockfileType {
	case "packrat":
		library = filepath.Join("packrat", "lib", packratPlatform(platform), rversion.ToFullString())
	case "renv":
		s := fmt.Sprintf("R-%s", rversion.ToString())
		library = filepath.Join("renv", "library", s, packratPlatform(platform))
	case "pkgr":
	default:
	}
	return library
}

// LoadConfigFromPath loads pkc configuration into the global Viper
func LoadConfigFromPath(configFilename string) error {
	if configFilename == "" {
		configFilename = "pkgr.yml"
	}
	viper.SetEnvPrefix("pkgr")
	viper.AutomaticEnv()
	configFilename, _ = homedir.Expand(filepath.Clean(configFilename))
	viper.SetConfigFile(configFilename)
	b, err := ioutil.ReadFile(configFilename)
	// panic if can't find or parse config as this could be explicit to user expectations
	if err != nil {
		// panic if can't find or parse config as this could be explicit to user expectations
		if _, ok := err.(*os.PathError); ok {
			log.Fatalf("could not find a config file at path: %s", configFilename)
		}
	}
	expb := []byte(os.ExpandEnv(string(b)))
	err = viper.ReadConfig(bytes.NewReader(expb))
	if err != nil {
		if _, ok := err.(viper.ConfigParseError); ok {
			// found config file but couldn't parse it, should error
			log.Fatalf("unable to parse config file with error (%s)", err)
		}
		// maybe could be more loose on this later, but for now will require a config file
		fmt.Println("Error with pkgr config file:")
		fmt.Println(err)
		os.Exit(1)
	}
	loadDefaultSettings()
	return nil
}

// loadDefaultSettings load default settings
func loadDefaultSettings() {
	viper.SetDefault("debug", false)
	viper.SetDefault("preview", false)
	// should be one of Debug,Info,Warn,Error,Fatal,Panic
	viper.SetDefault("loglevel", "info")
	// path to R on system, defaults to R in path
	viper.SetDefault("rpath", "R")
	viper.SetDefault("threads", runtime.NumCPU())
	viper.SetDefault("strict", false)
	viper.SetDefault("rollback", true)
}

// IsCustomizationSet ...
func IsCustomizationSet(key string, elems []interface{}, elem string) bool {
	for _, v := range elems {
		for k, iv := range v.(map[interface{}]interface{}) {
			if k == elem {
				for k2 := range iv.(map[interface{}]interface{}) {
					if k2 == key {
						return true
					}
				}
			}
		}
	}
	return false
}


// AddPackage add a package to the Package section of the yml config file
func AddPackage(name string) error {
	cfgname := viper.ConfigFileUsed()
	err := add(cfgname, name)
	if err != nil {
		return err
	}
	err = LoadConfigFromPath(cfgname)
	if err != nil {
		return err
	}
	return nil
}

// add add a package to the Package section of the yml config file
func add(ymlfile string, packageName string) error {
	appFS := afero.NewOsFs()
	fi, _ := os.Stat(ymlfile)
	yf, err := afero.ReadFile(appFS, ymlfile)
	if err != nil {
		return err
	}
	yf, err = Format(yf)
	if err != nil {
		return err
	}
	
	var pc PkgrConfig
	_ = yaml.Unmarshal(yf, &pc)

	if funk.Contains(pc.Packages, packageName) {
		log.Info(fmt.Sprintf("Package <%s> already found in <%s>", packageName, ymlfile))
		return nil
	}

	var out []byte
	i := 0
	lines := bytes.Split(yf, []byte("\n"))
	for _, line := range lines {
		i++
		out = append(out, line...)
		if i < len(lines) {
			out = append(out, []byte("\n")...)
		}
		if bytes.HasPrefix(line, []byte("Packages:")) {
			out = append(out, []byte("  - "+packageName)...)
			out = append(out, []byte("\n")...)
		}
	}

	err = afero.WriteFile(appFS, ymlfile, out, fi.Mode())
	if err != nil {
		return err
	}
	return nil
}

// RemovePackage remove a package from the Package section of the yml config file
func RemovePackage(name string) error {
	cfgname := viper.ConfigFileUsed()
	err := remove(cfgname, name)
	if err != nil {
		return err
	}
	return nil
}

// remove remove a package from the Package section of the yml config file
func remove(ymlfile string, packageName string) error {
	appFS := afero.NewOsFs()
	yf, _ := afero.ReadFile(appFS, ymlfile)
	fi, err := os.Stat(ymlfile)
	if err != nil {
		return err
	}
	var out []byte
	i := 0
	lines := bytes.Split(yf, []byte("\n"))
	for _, line := range lines {
		i++
		// trim the line to detect the start of the list of packages
		// but do not write the trimmed string as it may cause an
		// unneeded file diff to the yml file
		sline := bytes.TrimLeft(line, " ")
		if bytes.HasPrefix(sline, []byte("- "+packageName)) {
			continue
		}
		out = append(out, line...)
		if i < len(lines) {
			out = append(out, []byte("\n")...)
		}
	}
	err = afero.WriteFile(appFS, ymlfile, out, fi.Mode())
	if err != nil {
		return err
	}
	return nil
}

// SetCustomizations ... set ENV values in Rsettings
func SetCustomizations(rSettings rcmd.RSettings, cfg PkgrConfig) rcmd.RSettings {
	pkgCustomizations := cfg.Customizations.Packages
	for n, v := range pkgCustomizations {
		if v.Env != nil {
			rSettings.PkgEnvVars[n] = v.Env
		}
	}
	return rSettings
}

// SetPlanCustomizations ...
func SetPlanCustomizations(cfg PkgrConfig, dependencyConfigurations gpsr.InstallDeps, pkgNexus *cran.PkgNexus) {

	setCfgCustomizations(cfg, &dependencyConfigurations)

	//if viper.Sub("Customizations") != nil && viper.Sub("Customizations").AllSettings()["packages"] != nil {
	if len(cfg.Customizations.Packages) > 0 {
		pkgSettings := viper.Sub("Customizations").AllSettings()["packages"].([]interface{})
		setViperCustomizations(cfg, pkgSettings, dependencyConfigurations, pkgNexus)
	}
}

func setCfgCustomizations(cfg PkgrConfig, dependencyConfigurations *gpsr.InstallDeps) {
	if cfg.Suggests {
		for _, pkg := range cfg.Packages {
			// set all top level packages to install suggests
			dp := dependencyConfigurations.Default
			dp.Suggests = true
			dependencyConfigurations.Deps[pkg] = dp
		}
	}
}

func setViperCustomizations(cfg PkgrConfig, pkgSettings []interface{}, dependencyConfigurations gpsr.InstallDeps, pkgNexus *cran.PkgNexus) {
	for pkg, v := range cfg.Customizations.Packages {
		if IsCustomizationSet("Suggests", pkgSettings, pkg) {
			pkgDepTypes := dependencyConfigurations.Default
			pkgDepTypes.Suggests = v.Suggests
			dependencyConfigurations.Deps[pkg] = pkgDepTypes
		}
		if IsCustomizationSet("Repo", pkgSettings, pkg) {
			err := pkgNexus.SetPackageRepo(pkg, v.Repo)
			if err != nil {
				log.WithFields(log.Fields{
					"pkg":  pkg,
					"repo": v.Repo,
				}).Fatal("error finding custom repo to set")
			}
		}
		if IsCustomizationSet("Type", pkgSettings, pkg) {
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
