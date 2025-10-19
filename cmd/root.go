package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "megafone",
	Short: "AI-powered content generation and distribution for Hugo sites",
	Long: `megafone is a CLI tool that generates technical blog posts from GitHub
repositories and publishes them across multiple platforms. Uses AI to analyze
repos and create content that matches your writing style.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("openai-key", "k", "", "OpenAI API key (or set OPENAI_API_KEY env var)")
}
