package logger

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

type LogrusFileHook struct {
	file      *os.File
	flag      int
	chmod     os.FileMode
	formatter *log.JSONFormatter
}

// Log Reinstantiable log to be used globally in the application.
// var Log *log.Logger

func init() {
	// Log = log.New()
	log.SetOutput(os.Stdout)
}

// NewLogrusFileHook
func NewLogrusFileHook(file string, flag int, chmod os.FileMode) (*LogrusFileHook, error) {

	jsonFormatter := &log.JSONFormatter{}
	logFile, err := os.OpenFile(file, flag, chmod)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "unable to write file on filehook %v", err)
		return nil, err
	}

	return &LogrusFileHook{logFile, flag, chmod, jsonFormatter}, err
}

// Fire event
func (hook *LogrusFileHook) Fire(entry *log.Entry) error {

	jsonformat, err := hook.formatter.Format(entry)
	line := string(jsonformat)
	_, err = hook.file.WriteString(line)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "unable to write file on filehook(entry.String)%v", err)
		return err
	}

	return nil
}

func (hook *LogrusFileHook) Levels() []log.Level {
	return []log.Level{
		log.PanicLevel,
		log.FatalLevel,
		log.ErrorLevel,
		log.WarnLevel,
		log.InfoLevel,
		log.DebugLevel,
		log.TraceLevel,
	}

}

func SetLogJson(enabled bool) {
	if enabled {
		log.SetFormatter(&log.JSONFormatter{
			TimestampFormat:   "",
			DisableTimestamp:  true,
			DisableHTMLEscape: false,
			FieldMap:          nil,
			CallerPrettyfier:  nil,
			PrettyPrint:       false,
		})
	}
}

func SetLogLevel(level string) {
	// We want the log to be reset whenever it is initialized.
	logLevel := strings.ToLower(level)

	switch logLevel {
	case "trace":
		log.SetLevel(log.TraceLevel)
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	case "fatal":
		log.SetLevel(log.FatalLevel)
	case "panic":
		log.SetLevel(log.PanicLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}
}

func AddLogFile(outputFile string, overwrite bool) {

	if outputFile != "" {

		var osFlag int

		if overwrite {
			osFlag = os.O_CREATE | os.O_TRUNC | os.O_APPEND | os.O_RDWR
		} else {
			osFlag = os.O_CREATE | os.O_APPEND | os.O_RDWR
		}

		fileHook, err := NewLogrusFileHook(outputFile, osFlag, 0666)
		if err == nil {
			log.AddHook(fileHook)
		}
	}
}
