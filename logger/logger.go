package logger

import (
	"context"
	"io"
	"log"
	"os"
)

type Level int

const (
	LevelDebug Level = 0
	LevelInfo        = 1
	LevelWarn        = 2
	LevelErr         = 3
)

const (
	debug = "DEBG"
	info  = "INFO"
	warn  = "WARN"
	err   = "ERRR"
)

var (
	activeLevel Level = LevelInfo
	appLogger         = log.New(os.Stderr, "", log.Ldate|log.Ltime) // |log.Lmicroseconds
)

func SetLogLevel(level Level) {
	activeLevel = level
}

func SetOutput(w io.Writer) {
	appLogger.SetOutput(w)
}

func Debug(text string) {
	if activeLevel <= LevelDebug {
		logText(context.TODO(), debug, text)
	}
}

func Info(text string) {
	if activeLevel <= LevelInfo {
		logText(context.TODO(), info, text)
	}
}

func Warn(text string) {
	if activeLevel <= LevelWarn {
		logText(context.TODO(), warn, text)
	}
}

func Err(text string) {
	if activeLevel <= LevelErr {
		logText(context.TODO(), err, text)
	}
}

func logText(_ context.Context, level string, text string) {
	appLogger.Printf("[scheduler] [%s] %q", level, text)
}
