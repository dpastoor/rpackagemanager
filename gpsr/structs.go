package gpsr

import (
	"github.com/metrumresearchgroup/pkgr/cran"
	"github.com/metrumresearchgroup/pkgr/desc"
)

//InstallPlan provides metadata around an installation plan
type InstallPlan struct {
	StartingPackages []string
	DepDb            map[string][]string // This is a map of the dependencies [D1, D2, ... Dn] for a given package (A). The map is keyed by package name, i.e. DepDb[A] = [D1, D2, ..., Dn]
	PackageDownloads []cran.PkgDl
	OutdatedPackages []cran.OutdatedPackage
	InstalledPackages map[string]desc.Desc
	CreateLibrary bool
	Update bool
}

// PkgDeps contains which dependencies should be installed
// for a particular package
type PkgDeps struct {
	Depends   bool
	Imports   bool
	Suggests  bool
	LinkingTo bool
	NoRecommended bool
}

// InstallDeps contains the information about dependencies to be installed
type InstallDeps struct {
	Deps    map[string]PkgDeps
	Default PkgDeps
}

