package main

import (
	"fmt"
	"os"

	"github.com/kaspanet/kaspad/infrastructure/logger"
)

var (
	backendLog = logger.NewBackend()
	log        = backendLog.Logger("ROTS")
)

func initLogs(backendLog *logger.Backend, logFile, errLogFile string) {
	err := backendLog.AddLogFile(logFile, logger.LevelTrace)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error adding log file %s as log rotator for level %s: %+v\n", logFile, logger.LevelTrace, err)
		os.Exit(1)
	}
	err = backendLog.AddLogFile(errLogFile, logger.LevelWarn)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error adding log file %s as log rotator for level %s: %+v\n", errLogFile, logger.LevelWarn, err)
		os.Exit(1)
	}

	err = backendLog.AddLogWriter(os.Stdout, logger.LevelDebug)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error adding stdout to the loggerfor level %s: %+v\n", logger.LevelInfo, err)
		os.Exit(1)
	}

	err = backendLog.Run()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error starting the logger: %s ", err)
		os.Exit(1)
	}
}
