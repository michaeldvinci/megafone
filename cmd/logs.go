package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	tailLines int
	follow    bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View generation logs",
	Long:  `Display the log file showing all post generation activity.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runLogs(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)

	logsCmd.Flags().IntVarP(&tailLines, "tail", "n", 50, "Number of lines to show from the end")
	logsCmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output (like tail -f)")
}

func runLogs() error {
	logPath := getLogFilePath()

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Println("No logs found yet. Generate a post to create logs.")
		return nil
	}

	// Read the entire log file
	content, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	if len(content) == 0 {
		fmt.Println("Log file is empty.")
		return nil
	}

	// For now, just print the entire log
	// TODO: Implement --tail and --follow if needed
	fmt.Print(string(content))

	return nil
}
