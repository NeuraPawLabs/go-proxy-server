package main

import applogger "github.com/apeming/go-proxy-server/internal/logger"

var longRunningCommands = map[string]struct{}{
	"socks":         {},
	"http":          {},
	"both":          {},
	"web":           {},
	"tunnel-server": {},
	"tunnel-client": {},
}

func bootstrapLogLevel(args []string) applogger.LogLevel {
	if len(args) == 0 {
		return applogger.LevelInfo
	}
	if _, ok := longRunningCommands[args[0]]; ok {
		return applogger.LevelInfo
	}
	return applogger.LevelNone
}
