package logger

import (
	"log"
	"os"
	"strings"
)

type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

var currentLevel = InfoLevel

func init() {
	lvlStr := strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	switch lvlStr {
	case "DEBUG":
		currentLevel = DebugLevel
	case "INFO", "":
		currentLevel = InfoLevel
	case "WARN":
		currentLevel = WarnLevel
	case "ERROR":
		currentLevel = ErrorLevel
	default:
		currentLevel = InfoLevel
	}
}

func Debug(format string, args ...interface{}) {
	if currentLevel <= DebugLevel {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func Info(format string, args ...interface{}) {
	if currentLevel <= InfoLevel {
		log.Printf("[INFO] "+format, args...)
	}
}

func Warn(format string, args ...interface{}) {
	if currentLevel <= WarnLevel {
		log.Printf("[WARN] "+format, args...)
	}
}

func Error(format string, args ...interface{}) {
	if currentLevel <= ErrorLevel {
		log.Printf("[ERROR] "+format, args...)
	}
}
