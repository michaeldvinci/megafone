package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v57/github"
	"github.com/sashabaranov/go-openai"
)

// findBestImage searches the README for images and selects the best one
func findBestImage(ctx context.Context, ghClient *github.Client, apiKey, owner, repo, model string) (string, error) {
	// Fetch README content
	readme, _, err := ghClient.Repositories.GetReadme(ctx, owner, repo, nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch README: %w", err)
	}

	readmeContent, err := readme.GetContent()
	if err != nil {
		return "", fmt.Errorf("failed to decode README: %w", err)
	}

	// Extract image URLs from README markdown
	imageURLs := extractImageURLsFromMarkdown(readmeContent, owner, repo)

	if len(imageURLs) == 0 {
		return "", fmt.Errorf("no images found in README")
	}

	logInfo("Found %d images in README", len(imageURLs))

	// If only one image, return it
	if len(imageURLs) == 1 {
		return imageURLs[0], nil
	}

	// Use OpenAI to select the best image
	bestImage, err := selectBestImageWithAI(ctx, apiKey, imageURLs, model)
	if err != nil {
		// Fallback to first image if AI selection fails
		logError("Failed to use AI for image selection: %v", err)
		return imageURLs[0], nil
	}

	return bestImage, nil
}

// extractImageURLsFromMarkdown parses markdown and extracts image URLs
func extractImageURLsFromMarkdown(markdown, owner, repo string) []string {
	var imageURLs []string
	lines := strings.Split(markdown, "\n")

	for _, line := range lines {
		// Match markdown images: ![alt](url)
		if strings.Contains(line, "![") {
			start := strings.Index(line, "](")
			if start == -1 {
				continue
			}
			start += 2
			end := strings.Index(line[start:], ")")
			if end == -1 {
				continue
			}

			imageURL := line[start : start+end]

			// Convert relative URLs to absolute GitHub URLs
			if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
				if isImageFile(imageURL) {
					imageURLs = append(imageURLs, imageURL)
				}
			} else if strings.HasPrefix(imageURL, "/") || !strings.Contains(imageURL, "://") {
				// Relative URL - convert to raw GitHub URL
				imageURL = strings.TrimPrefix(imageURL, "/")
				fullURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s", owner, repo, imageURL)
				if isImageFile(imageURL) {
					imageURLs = append(imageURLs, fullURL)
				}
			}
		}

		// Also match HTML img tags: <img src="url">
		if strings.Contains(line, "<img") {
			start := strings.Index(line, "src=\"")
			if start == -1 {
				start = strings.Index(line, "src='")
				if start == -1 {
					continue
				}
				start += 5
			} else {
				start += 5
			}

			end := strings.IndexAny(line[start:], "\"'")
			if end == -1 {
				continue
			}

			imageURL := line[start : start+end]

			// Convert relative URLs to absolute GitHub URLs
			if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
				if isImageFile(imageURL) {
					imageURLs = append(imageURLs, imageURL)
				}
			} else if strings.HasPrefix(imageURL, "/") || !strings.Contains(imageURL, "://") {
				// Relative URL
				imageURL = strings.TrimPrefix(imageURL, "/")
				fullURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s", owner, repo, imageURL)
				if isImageFile(imageURL) {
					imageURLs = append(imageURLs, fullURL)
				}
			}
		}
	}

	return imageURLs
}

func isImageFile(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".png") ||
		strings.HasSuffix(lower, ".jpg") ||
		strings.HasSuffix(lower, ".jpeg") ||
		strings.HasSuffix(lower, ".gif") ||
		strings.HasSuffix(lower, ".webp")
}

func selectBestImageWithAI(ctx context.Context, apiKey string, imageURLs []string, model string) (string, error) {
	client := openai.NewClient(apiKey)

	// Limit to first 5 images to avoid token limits
	if len(imageURLs) > 5 {
		imageURLs = imageURLs[:5]
	}

	// Build prompt with image list
	var imageList strings.Builder
	imageList.WriteString("Available images:\n")
	for i, url := range imageURLs {
		imageList.WriteString(fmt.Sprintf("%d. %s\n", i+1, url))
	}

	prompt := fmt.Sprintf(`You are selecting a hero image for a technical blog post about a software project.

%s

Choose the BEST image for a blog post hero image. Prefer:
1. Screenshots showing the application UI
2. Diagrams or architecture images
3. Project logos or branding
4. Avoid: generic icons, small badges, favicons

Respond with ONLY the number (1-5) of the best image. No explanation.`, imageList.String())

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You select the best hero image for blog posts. Respond only with a number.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   5,
	})

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from AI")
	}

	// Parse the number response
	choice := strings.TrimSpace(resp.Choices[0].Message.Content)
	var selectedIndex int
	fmt.Sscanf(choice, "%d", &selectedIndex)

	if selectedIndex < 1 || selectedIndex > len(imageURLs) {
		return imageURLs[0], nil
	}

	return imageURLs[selectedIndex-1], nil
}

func downloadAndProcessImage(imageURL, repoName, basePath string) (string, error) {
	// Download the image
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	// Determine file extension from URL
	ext := filepath.Ext(imageURL)
	if ext == "" {
		ext = ".png"
	}

	// Create destination filename
	imageName := fmt.Sprintf("%s%s", strings.ToLower(repoName), ext)
	destPath := filepath.Join(basePath, "assets", "images", "site", imageName)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", err
	}

	// Create the file
	outFile, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer outFile.Close()

	// Copy the data
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return "", err
	}

	logSuccess("Downloaded and saved image: %s", imageName)
	return imageName, nil
}
