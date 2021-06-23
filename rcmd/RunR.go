package rcmd

import (
	"os"
	"os/exec"
	"runtime"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
)

const defaultFailedCode = 1
const defaultSuccessCode = 0

// StartR launches an interactive R console given the same
// configuration as a specific package.
func StartR(
	fs afero.Fs,
	pkg string,
	rs RSettings,
	rdir string, // this should be put into RSettings
) error {

	envVars := configureEnv(os.Environ(), rs, pkg)
	cmdArgs := []string{
		"--vanilla",
	}

	log.WithFields(
		log.Fields{
			"cmdArgs":   cmdArgs,
			"RSettings": rs,
			"env":       envVars,
		}).Trace("command args")

	// --vanilla is a command for R and should be specified before the CMD, eg
	// R --vanilla CMD check
	// if cs.Vanilla {
	// 	cmdArgs = append([]string{"--vanilla"}, cmdArgs...)
	// }
	cmd := exec.Command(
		rs.R(runtime.GOOS),
		cmdArgs...,
	)

	if rdir == "" {
		rdir, _ = os.Getwd()
		log.WithFields(
			log.Fields{"rdir": rdir},
		).Debug("launch dir")
	}
	cmd.Dir = rdir
	cmd.Env = envVars
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// RunR launches an interactive R console
func RunR(
	fs afero.Fs,
	pkg string,
	rs RSettings,
	script string,
	rdir string, // this should be put into RSettings
) ([]byte, error) {

	envVars := configureEnv(os.Environ(), rs, pkg)
	cmdArgs := []string{
		"--vanilla",
		"-e",
		script,
	}

	log.WithFields(
		log.Fields{
			"cmdArgs":   cmdArgs,
			"RSettings": rs,
			"env":       envVars,
		}).Trace("command args")

	// --vanilla is a command for R and should be specified before the CMD, eg
	// R --vanilla CMD check
	// if cs.Vanilla {
	// 	cmdArgs = append([]string{"--vanilla"}, cmdArgs...)
	// }
	cmd := exec.Command(
		rs.R(runtime.GOOS)+"script",
		cmdArgs...,
	)

	if rdir == "" {
		rdir, _ = os.Getwd()
		log.WithFields(
			log.Fields{"rdir": rdir},
		).Debug("launch dir")
	}
	cmd.Dir = rdir
	cmd.Env = envVars
	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr

	return cmd.Output()
}

// RunRBatch runs a non-interactive R command
func RunRBatch(
	// TODO: use or remove fs
	fs afero.Fs,
	rs RSettings,
	cmdArgs []string,
) ([]byte, error) {
	envVars := configureEnv(os.Environ(), rs, "")
	rpath := rs.R(runtime.GOOS)
	log.WithFields(
		log.Fields{
			"cmdArgs":   cmdArgs,
			"RSettings": rs,
			"rpath":     rpath,
		}).Trace("command args")

	cmd := exec.Command(
		rpath,
		cmdArgs...,
	)
	cmd.Env = envVars

	return cmd.CombinedOutput()
}
