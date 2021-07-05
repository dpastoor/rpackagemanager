package gpsr

import (
	"errors"
	"fmt"
	"github.com/metrumresearchgroup/pkgr/cran"
	"github.com/thoas/go-funk"
	"sort"

	"github.com/deckarep/golang-set"
)

type Graph map[string]*Node

// NewGraph initializes a new graph
func NewGraph() Graph {
	return make(map[string]*Node)
}

// Node represents a single node in the graph with it's dependencies
type Node struct {
	// Name of the node
	Name string

	// Dependencies of the node
	Deps []string
}

// NewNode creates a new node
func NewNode(name string, deps []string) *Node {
	n := &Node{
		Name: name,
		Deps: deps,
	}

	return n
}

// DisplayGraph shows the dependency graph
func DisplayGraph(graph Graph) {
	for _, node := range graph {
		for _, dep := range node.Deps {
			fmt.Printf("%s -> %s\n", node.Name, dep)
		}
	}
}

// ResolveLayers Resolves the dependency graph
// providing the layers of dependencies such that
// each layer only dependends on elements from above
// allowing confident parallel installation at any
// layer
func ResolveLayers(graph Graph, noRecommended bool) ([][]string, error) {
	// A map containing the nodes and their dependencies
	nodeDependencies := make(map[string]mapset.Set)

	// Populate the maps
	for nm, node := range graph {

		dependencySet := mapset.NewSet()
		for _, dep := range node.Deps {
			if !isExcludedPackage(dep, noRecommended) {
				dependencySet.Add(dep)
			}
		}
		nodeDependencies[nm] = dependencySet
	}

	// Iteratively find and remove nodes from the graph which have no dependencies.
	// Removed nodes are added to a "ready" set as they are found, thereby causing other nodes to have no deps.
	// If at some point there are still nodes in the graph and we cannot find
	// nodes without dependencies, that means we have a circular dependency
	var resolved [][]string
	for len(nodeDependencies) != 0 {
		// Get all nodes from the graph which have no dependencies
		readyLayer := mapset.NewSet()
		for name, deps := range nodeDependencies {
			if deps.Cardinality() == 0 {
				readyLayer.Add(name)
			}
		}

		// If there aren't any ready nodes, then we have a cicular dependency
		if readyLayer.Cardinality() == 0 {
			return resolved, errors.New("Circular dependency found")
		}

		// Remove the ready nodes and add them to the resolved graph
		var dl []string
		for name := range readyLayer.Iter() {
			delete(nodeDependencies, name.(string))
			dl = append(dl, name.(string))
		}
		resolved = append(resolved, dl)

		// Also make sure to remove the ready nodes from the
		// remaining node dependencies as well
		for name, deps := range nodeDependencies {
			diff := deps.Difference(readyLayer)
			nodeDependencies[name] = diff
		}
	}

	return resolved, nil
}

// InvertDependencies provides an inversion of the dependencies
// such that each element contains a slice of all packages that depend on it
// This can be used when a package is installed to identify which
// other packages may have all their dependencies satisfied.
func (ip *InstallPlan) InvertDependencies() map[string][]string {
	ddb := ip.DepDb
	idb := make(map[string][]string)
	for pkg, deps := range ddb {
		for _, p := range deps {
			d := idb[p]
			d = append(d, pkg)
			idb[p] = d
		}
	}
	for p := range idb {
		// lets sort everything so deps will be alphabetical
		sort.Strings(idb[p])
	}
	return idb
}

func (ip *InstallPlan) Pack(pkgNexus *cran.PkgNexus) {
	var toDl []cran.PkgDl
	// starting packages
	for _, p := range ip.StartingPackages {
		pkg, cfg, _ := pkgNexus.GetPackage(p)
		toDl = append(toDl, cran.PkgDl{Package: pkg, Config: cfg})
	}
	// all other packages
	for p := range ip.DepDb {
		pkg, cfg, _ := pkgNexus.GetPackage(p)
		toDl = append(toDl, cran.PkgDl{Package: pkg, Config: cfg})
	}
	ip.PackageDownloads = toDl
	//return toDl
}

func (ip *InstallPlan) GetAllPackages() []string {
	toInstall := ip.StartingPackages
	for depsList := range ip.DepDb {
		toInstall = append(toInstall, depsList)
	}
	for pkg := range ip.AdditionalPackageSources {
		toInstall = append(toInstall, pkg)
	}
	return toInstall
}

func (ip *InstallPlan) GetNumPackagesToInstall() int {
	requiredPackages := ip.GetAllPackages()

	// Handle the case where packages not requested via pkgr.yml (or dependencies) are present in directory.
	installedRequired := 0
	for p := range ip.InstalledPackages {

		// AdditionalPackageSources are always installed, even if they're already present.
		_, isAdditionalPkgInstallation := ip.AdditionalPackageSources[p]

		if funk.Contains(requiredPackages, p) && !isAdditionalPkgInstallation {
			installedRequired = installedRequired + 1
		}
	}
	toUpdate := 0
	// Everything in OutdatedPackages should be required, otherwise pkgr wouldn't have checked for an updated version.
	if ip.Update {
		toUpdate = len(ip.OutdatedPackages)
	}

	return len(requiredPackages) - installedRequired + toUpdate

}
