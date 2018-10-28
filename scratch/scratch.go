package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/dpastoor/rpackagemanager/cran"
	"github.com/dpastoor/rpackagemanager/desc"
	"github.com/dpastoor/rpackagemanager/gpsr"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
)

func main() {
	GobDB()
	dmap := make(map[string]desc.Desc)
	appFS := afero.NewOsFs()
	log := logrus.New()
	log.Level = logrus.DebugLevel

	file, err := os.Open("crandb.gob")
	if err != nil {
		fmt.Println("problem creating crandb")
		panic(err)
	}
	d := gob.NewDecoder(file)

	// Decoding the serialized data
	err = d.Decode(&dmap)
	if err != nil {
		panic(err)
	}

	// PrettyPrint(dmap["dplyr"])
	// PrettyPrint(dmap["PKPDmisc"])
	//AppFs := afero.NewOsFs()
	// can use this to redirect log output
	// f, err := os.OpenFile("testlogfile", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	// if err != nil {
	// 	log.Fatalf("error opening file: %v", err)
	// }
	// defer f.Close()
	pkgs := []string{
		"PKPDmisc",
		"mrgsolve",
		"rmarkdown",
		"bitops",
		"caTools",
		"GGally",
		"knitr",
		"gridExtra",
		"htmltools",
		"xtable",
		"tidyverse",
		"shiny",
		"shinydashboard",
	}
	workingGraph := gpsr.NewGraph()
	for _, p := range pkgs {
		appendToGraph(workingGraph, dmap[p], dmap)
	}

	resolved, err := gpsr.ResolveGraph(workingGraph)
	if err != nil {
		log.Fatalf("Failed to resolve dependency graph: %s\n", err)
	} else {
		log.Info("The dependency graph resolved successfully")
	}
	var toDl []desc.Desc
	for i, pkglayer := range resolved {
		for _, p := range pkglayer {
			toDl = append(toDl, dmap[p])
		}
		log.WithFields(
			logrus.Fields{
				"layer": i + 1,
				"npkgs": len(pkglayer),
			},
		).Info(pkglayer)
	}
	startTime := time.Now()
	// want to download the packages and return the full path of any downloaded package
	dl, err := cran.DownloadPackages(appFS, toDl, "https://cran.rstudio.com", cran.Source, "dump")
	if err != nil {
		fmt.Println(err)
		panic(err)
	}
	fmt.Println("duration:", time.Since(startTime))

	// ia := rcmd.NewDefaultInstallArgs()
	// ia.Library = "../integration_tests/lib"
	for pn, p := range dl {
		fmt.Println(pn, p)
	}
	// fmt.Println("library: ", viper.GetString("library"))
	//rcmd.InstallThroughBinary(appFS, "", ia, rcmd.RSettings{}, rcmd.ExecSettings{}, log)
}
func appendToGraph(m gpsr.Graph, d desc.Desc, dmap map[string]desc.Desc) {
	var reqs []string
	for r := range d.Imports {
		_, ok := dmap[r]
		if ok {
			reqs = append(reqs, r)
		}
	}
	for r := range d.Depends {
		_, ok := dmap[r]
		if ok {
			reqs = append(reqs, r)
		}
	}
	for r := range d.LinkingTo {
		_, ok := dmap[r]
		if ok {
			reqs = append(reqs, r)
		}
	}
	fmt.Println("pkg: ", d.Package)
	fmt.Println("new reqs: ", reqs)
	m[d.Package] = gpsr.NewNode(d.Package, reqs)
	if len(reqs) > 0 {
		for _, pn := range reqs {
			_, ok := m[pn]
			if pn != "R" && !ok {
				appendToGraph(m, dmap[pn], dmap)
			}
		}
	}
}
func PrettyPrint(v interface{}) (err error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		fmt.Println(string(b))
	}
	return
}

func GobDB() {
	appFS := afero.NewOsFs()
	ok, _ := afero.Exists(appFS, "crandb.gob")
	if !ok {
		startTime := time.Now()
		res, err := http.Get("https://cran.rstudio.com/src/contrib/PACKAGES")
		if err != nil {
			fmt.Println("problem getting packages")
			panic(err)
		}
		file, err := os.Create("crandb.gob")
		if err != nil {
			fmt.Println("problem creating crandb")
			panic(err)
		}
		dmap := make(map[string]desc.Desc)
		defer res.Body.Close()
		body, err := ioutil.ReadAll(res.Body)
		cb := bytes.Split(body, []byte("\n\n"))
		for _, p := range cb {
			reader := bytes.NewReader(p)
			d, err := desc.ParseDesc(reader)
			dmap[d.Package] = d
			if err != nil {
				fmt.Println("problem parsing")
				panic(err)
			}
			//PrettyPrint(d)
		}
		fmt.Println("duration:", time.Since(startTime))
		fmt.Println("length: ", len(dmap))

		e := gob.NewEncoder(file)

		// Encoding the map
		err = e.Encode(dmap)
		if err != nil {
			panic(err)
		}
	}
}
