package cmd

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

var logger *log.Logger

func initLogger() error {
	logPath := getLogFilePath()

	// Ensure log directory exists
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file (append mode)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Write to both file and stdout
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	logger = log.New(multiWriter, "", 0)

	return nil
}

func getLogFilePath() string {
	return filepath.Join("logs", "generation.log")
}

func logInfo(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logger.Printf("[%s] INFO: %s", timestamp, msg)
}

func logSuccess(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logger.Printf("[%s] SUCCESS: %s", timestamp, msg)
}

func logError(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logger.Printf("[%s] ERROR: %s", timestamp, msg)
}

func logGeneration(repo, postPath, imagePath string, tags []string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logger.Printf("[%s] GENERATION: repo=%s, post=%s, image=%s, tags=%v",
		timestamp, repo, postPath, imagePath, tags)
}
