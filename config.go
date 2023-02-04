package main

import (
	"os"
	"path/filepath"

	"github.com/kaspanet/kaspad/domain/dagconfig"
	"github.com/kaspanet/kaspad/infrastructure/logger"
	"github.com/kaspanet/kaspad/util"

	"github.com/jessevdk/go-flags"
)

const (
	defaultLogFilename    = "kaspa-tx-sender.log"
	defaultErrLogFilename = "kaspa-tx-sender-err.log"
)

var (
	defaultHomeDir = util.AppDir("kaspa-tx-sender", false)
	// Default configuration options
	defaultLogFile    = filepath.Join(defaultHomeDir, defaultLogFilename)
	defaultErrLogFile = filepath.Join(defaultHomeDir, defaultErrLogFilename)
)

type configFlags struct {
	Profile         string `long:"profile" description:"Enable HTTP profiling on given port -- NOTE port must be between 1024 and 65536"`
	RPCServer       string `long:"rpcserver" short:"s" description:"RPC server to connect to"`
	ActiveNetParams *dagconfig.Params
}

var cfg *configFlags

func activeConfig() *configFlags {
	return cfg
}

func parseConfig() error {
	cfg = &configFlags{}
	parser := flags.NewParser(cfg, flags.PrintErrors|flags.HelpFlag)

	_, err := parser.Parse()

	if err != nil {
		if err, ok := err.(*flags.Error); ok && err.Type == flags.ErrHelp {
			os.Exit(0)
		}
		return err
	}

	cfg.ActiveNetParams = &dagconfig.DevnetParams

	log.SetLevel(logger.LevelInfo)
	initLogs(backendLog, defaultLogFile, defaultErrLogFile)

	return nil
}
