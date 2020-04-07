// Copyright © 2019 John Carlo Salter <juuncaerlum@gmail.com>
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
	"github.com/metrumresearchgroup/pkgr/logger"

	"github.com/spf13/cobra"
)

var cleanAll bool

// CleanCmd represents the clean command
var CleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "clean up cached information",
	Long:  "clean up cached source files and binaries, as well as the saved package database.",
	RunE:  clean,
}

func init() {
	CleanCmd.Flags().BoolVar(&cleanAll, "all", false, "clean all cached items")

	RootCmd.AddCommand(CleanCmd)
}

func clean(cmd *cobra.Command, args []string) error {

	logger.AddLogFile(cfg.Logging.All, cfg.Logging.Overwrite)

	var err error

	if !cleanAll {
		fmt.Println("No clean options passed -- not cleaning.")
	}
	if cleanAll {
		fmt.Println("Cleaning all.")
		// Have to clean pkgrDbs first, as the required call to "plan" will cause Tarballs to be appear in cache and need cleaning.
		err = cleanPackageDatabases("ALL")
		if err != nil {
			return err
		}
		err = cleanCacheFolders()
		if err != nil {
			return err
		}
	}
	return nil
}
