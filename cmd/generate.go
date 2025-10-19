package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-github/v57/github"
	"github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
)

var (
	topicURL   string
	imagePath  string
	tags       string
	promptFile string
	dryRun     bool
	model      string
	siteSource string
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new blog post from a GitHub repository",
	Long: `Fetches repository metadata from GitHub, analyzes the README,
and uses OpenAI to generate a technical blog post matching your style.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runGenerate(cmd); err != nil {
			log.Fatalf("Error: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.Flags().StringVarP(&topicURL, "topic", "t", "", "GitHub repository URL to write about (required)")
	generateCmd.Flags().StringVarP(&imagePath, "image", "i", "", "Path to hero image")
	generateCmd.Flags().StringVarP(&tags, "tags", "T", "", "Comma-separated tags (AI will suggest if not provided)")
	generateCmd.Flags().StringVarP(&promptFile, "prompt", "p", "prompt.txt", "Path to prompt template file")
	generateCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Print generated content without writing files")
	generateCmd.Flags().StringVarP(&model, "model", "m", "gpt-4o-mini", "OpenAI model to use (gpt-4o, gpt-4o-mini, gpt-4-turbo)")
	generateCmd.Flags().StringVarP(&siteSource, "site-source", "s", "", "Path to local Hugo site repository (if not provided, will show git clone command)")

	generateCmd.MarkFlagRequired("topic")
}

func runGenerate(cmd *cobra.Command) error {
	// Initialize logger
	if err := initLogger(); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	ctx := context.Background()

	logInfo("Starting post generation for %s", topicURL)

	// Determine base path for Hugo site
	basePath, err := resolveSitePath()
	if err != nil {
		return err
	}
	logInfo("Using Hugo site at: %s", basePath)

	// Get OpenAI API key
	apiKey, _ := cmd.Flags().GetString("openai-key")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		logError("OpenAI API key not provided")
		return fmt.Errorf("OpenAI API key required (use --openai-key or OPENAI_API_KEY env var)")
	}

	// Determine if this is a GitHub repo or a regular website
	isGitHub := isGitHubURL(topicURL)

	var repoData *github.Repository
	var readmeContent string
	var contentTitle string
	var imageName string

	if isGitHub {
		// Parse GitHub repo URL
		owner, repo, err := parseGitHubURL(topicURL)
		if err != nil {
			logError("Invalid GitHub URL: %s", topicURL)
			return fmt.Errorf("invalid GitHub URL: %w", err)
		}

		logInfo("📦 Fetching repository: %s/%s", owner, repo)

		// Fetch repo metadata
		ghClient := github.NewClient(nil)
		repoData, _, err = ghClient.Repositories.Get(ctx, owner, repo)
		if err != nil {
			logError("Failed to fetch repository: %v", err)
			return fmt.Errorf("failed to fetch repository: %w", err)
		}

		// Fetch README
		logInfo("📄 Reading README...")
		readme, _, err := ghClient.Repositories.GetReadme(ctx, owner, repo, nil)
		if err == nil && readme != nil {
			content, err := readme.GetContent()
			if err == nil {
				readmeContent = content
			}
		}

		// Detect/process image FIRST so we can include it in the generated content
		if imagePath != "" {
			logInfo("🖼️  Processing provided image: %s", imagePath)
			imageName, err = processImage(imagePath, repo, basePath)
			if err != nil {
				logError("Failed to process image: %v", err)
				return fmt.Errorf("failed to process image: %w", err)
			}
		} else {
			// Try to auto-detect image from repository
			logInfo("🔍 Searching for hero image in repository...")
			autoImage, err := findBestImage(ctx, ghClient, apiKey, owner, repo, model)
			if err != nil {
				logInfo("No suitable image found in repository: %v", err)
			} else if autoImage != "" {
				logInfo("✨ Found image: %s", autoImage)
				imageName, err = downloadAndProcessImage(autoImage, repo, basePath)
				if err != nil {
					logError("Failed to download image: %v", err)
				}
			}
		}
	} else {
		// Handle regular website
		logInfo("🌐 Fetching website content...")
		websiteContent, title, err := fetchWebsiteContent(topicURL)
		if err != nil {
			logError("Failed to fetch website: %v", err)
			return fmt.Errorf("failed to fetch website: %w", err)
		}
		readmeContent = websiteContent
		contentTitle = title
		logInfo("📄 Fetched content from: %s", title)

		// Process image if provided
		if imagePath != "" {
			logInfo("🖼️  Processing provided image: %s", imagePath)
			// Use a sanitized version of the title for the image name
			imgBaseName := sanitizeFilename(title)
			imageName, err = processImageWithName(imagePath, imgBaseName, basePath)
			if err != nil {
				logError("Failed to process image: %v", err)
				return fmt.Errorf("failed to process image: %w", err)
			}
		}
	}

	// Load prompt template
	logInfo("📝 Loading prompt template from %s", promptFile)
	promptTemplate, err := os.ReadFile(promptFile)
	if err != nil {
		logError("Failed to read prompt file: %v", err)
		return fmt.Errorf("failed to read prompt file: %w", err)
	}

	// Generate content with OpenAI (now with image info)
	logInfo("🤖 Generating blog post with OpenAI (%s)...", model)
	var content, filename string
	if isGitHub {
		content, filename, err = generateWithOpenAI(ctx, apiKey, string(promptTemplate), repoData, readmeContent, tags, imageName, model)
	} else {
		content, filename, err = generateFromWebsite(ctx, apiKey, string(promptTemplate), topicURL, contentTitle, readmeContent, tags, imageName, model)
	}
	if err != nil {
		logError("OpenAI generation failed: %v", err)
		return fmt.Errorf("failed to generate content: %w", err)
	}

	logInfo("Generated filename: %s", filename)

	if dryRun {
		logInfo("Dry run mode - not writing files")
		fmt.Println("\n" + strings.Repeat("=", 80))
		fmt.Println("DRY RUN - Generated Content:")
		fmt.Println(strings.Repeat("=", 80))
		fmt.Println(content)
		fmt.Println(strings.Repeat("=", 80))
		return nil
	}

	// Write post to content directory
	postPath := filepath.Join(basePath, "content", "posts", "en", fmt.Sprintf("%s.md", filename))
	if err := os.WriteFile(postPath, []byte(content), 0644); err != nil {
		logError("Failed to write post file: %v", err)
		return fmt.Errorf("failed to write post: %w", err)
	}

	logSuccess("✅ Post created: %s", postPath)
	if imageName != "" {
		logSuccess("✅ Image copied: assets/images/site/%s", imageName)
	}

	// Parse tags for logging
	var tagList []string
	if tags != "" {
		tagList = strings.Split(tags, ",")
	}

	// Log the successful generation
	logGeneration(topicURL, postPath, imagePath, tagList)

	return nil
}

func generateWithOpenAI(ctx context.Context, apiKey, promptTemplate string, repo *github.Repository, readme, userTags, heroImage, model string) (content, filename string, err error) {
	client := openai.NewClient(apiKey)

	// Build context for the AI
	repoContext := fmt.Sprintf(`
Repository: %s
Description: %s
Language: %s
Stars: %d
URL: %s

README Content:
%s
`, repo.GetFullName(), repo.GetDescription(), repo.GetLanguage(), repo.GetStargazersCount(), repo.GetHTMLURL(), readme)

	// Get current date for the post
	currentDate := time.Now().Format("2006-01-02")

	heroImageInfo := ""
	if heroImage != "" {
		heroImageInfo = fmt.Sprintf("\nHero image available: %s (use path: /images/site/%s)", heroImage, heroImage)
	}

	userPrompt := fmt.Sprintf(`%s

Please generate a blog post for this GitHub repository:

%s
%s

User-provided tags: %s (suggest appropriate tags if none provided)

IMPORTANT: Your response must be ONLY valid markdown. Do not include any explanatory text before or after the markdown.
IMPORTANT: Use date: %s in the front matter.
%s

Generate a complete Hugo markdown post following the style guide above.
`, promptTemplate, repoContext, heroImageInfo, userTags, currentDate,
		func() string {
			if heroImage != "" {
				return fmt.Sprintf("IMPORTANT: Include 'hero: /images/site/%s' in the front matter.", heroImage)
			}
			return ""
		}())

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a technical blog writer who creates detailed, honest posts about software projects. Follow the style guide precisely. Output ONLY the markdown content, no explanations.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature: 0.7,
	})

	if err != nil {
		return "", "", fmt.Errorf("OpenAI API error: %w\n\nTroubleshooting:\n- Check your API key is valid\n- Verify your OpenAI account has credits: https://platform.openai.com/usage\n- Try a different model with --model gpt-4o-mini\n- Check rate limits: https://platform.openai.com/account/limits", err)
	}

	if len(resp.Choices) == 0 {
		return "", "", fmt.Errorf("no response from OpenAI")
	}

	content = resp.Choices[0].Message.Content

	// Generate filename from content
	filename, err = generateFilename(ctx, client, content, model)
	if err != nil {
		// Fallback to repo name if filename generation fails
		logError("Failed to generate filename, using repo name: %v", err)
		filename = strings.ToLower(repo.GetName())
	}

	return content, filename, nil
}

func generateFilename(ctx context.Context, client *openai.Client, content, model string) (string, error) {
	prompt := fmt.Sprintf(`Given this blog post content, generate a short, SEO-friendly filename (without .md extension).

Rules:
- Use lowercase
- Use hyphens instead of spaces
- 3-6 words maximum
- Descriptive of the post topic
- No special characters except hyphens
- Example: "syllabus-audiobook-tracker" or "echo-show-home-assistant"

Blog post:
%s

Respond with ONLY the filename, nothing else.`, content)

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You generate SEO-friendly filenames. Output only the filename with no explanation.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   20,
	})

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no filename generated")
	}

	filename := strings.TrimSpace(resp.Choices[0].Message.Content)
	filename = strings.ToLower(filename)
	filename = strings.ReplaceAll(filename, " ", "-")

	// Remove any quotes or markdown artifacts
	filename = strings.Trim(filename, "`\"'")

	return filename, nil
}

func parseGitHubURL(url string) (owner, repo string, err error) {
	// Support formats:
	// - https://github.com/owner/repo
	// - github.com/owner/repo
	// - owner/repo
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "github.com/")
	url = strings.TrimSuffix(url, ".git")

	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid GitHub URL format")
	}

	return parts[0], parts[1], nil
}

func processImage(srcPath, repoName, basePath string) (string, error) {
	// Determine destination path
	ext := filepath.Ext(srcPath)
	imageName := fmt.Sprintf("%s%s", strings.ToLower(repoName), ext)
	destPath := filepath.Join(basePath, "assets", "images", "site", imageName)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", err
	}

	// Copy image file
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return "", err
	}

	return imageName, nil
}

func resolveSitePath() (string, error) {
	// If user provided a path, validate it
	if siteSource != "" {
		absPath, err := filepath.Abs(siteSource)
		if err != nil {
			return "", fmt.Errorf("invalid site-source: %w", err)
		}

		// Check if path exists
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return "", fmt.Errorf("site-source does not exist: %s", absPath)
		}

		// Check if it looks like a Hugo site (has content/ directory)
		contentDir := filepath.Join(absPath, "content")
		if _, err := os.Stat(contentDir); os.IsNotExist(err) {
			return "", fmt.Errorf("path does not appear to be a Hugo site (no content/ directory): %s", absPath)
		}

		return absPath, nil
	}

	// No path provided - show git clone stub
	fmt.Println("\n⚠️  No Hugo site source path provided.")
	fmt.Println("\nTo clone your Hugo site repository, run:")
	fmt.Println("  git clone <your-hugo-site-repo-url> /path/to/hugo-site")
	fmt.Println("\nThen use --site-source flag:")
	fmt.Println("  hugo-companion generate --topic <url> --site-source /path/to/hugo-site")
	fmt.Println()

	return "", fmt.Errorf("Hugo site source path required (use --site-source)")
}

func isGitHubURL(urlStr string) bool {
	return strings.Contains(urlStr, "github.com")
}

func fetchWebsiteContent(urlStr string) (content, title string, err error) {
	// Parse and validate URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}

	// Ensure we have a scheme
	if parsedURL.Scheme == "" {
		urlStr = "https://" + urlStr
	}

	// Fetch the webpage
	resp, err := http.Get(urlStr)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP error: %s", resp.Status)
	}

	// Read the body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response: %w", err)
	}

	htmlContent := string(body)

	// Extract title from HTML
	title = extractTitle(htmlContent)
	if title == "" {
		title = parsedURL.Host
	}

	// Basic HTML to text conversion (strip tags)
	content = stripHTMLTags(htmlContent)

	return content, title, nil
}

func extractTitle(html string) string {
	// Try to extract <title> tag
	titleRegex := regexp.MustCompile(`<title[^>]*>([^<]+)</title>`)
	matches := titleRegex.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Try og:title meta tag
	ogTitleRegex := regexp.MustCompile(`<meta[^>]*property="og:title"[^>]*content="([^"]+)"`)
	matches = ogTitleRegex.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	return ""
}

func stripHTMLTags(html string) string {
	// Remove script and style elements
	scriptRegex := regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	html = scriptRegex.ReplaceAllString(html, "")
	styleRegex := regexp.MustCompile(`(?i)<style[^>]*>.*?</style>`)
	html = styleRegex.ReplaceAllString(html, "")

	// Remove HTML tags
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	text := tagRegex.ReplaceAllString(html, " ")

	// Clean up whitespace
	spaceRegex := regexp.MustCompile(`\s+`)
	text = spaceRegex.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

func sanitizeFilename(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)
	// Replace spaces and special chars with hyphens
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	s = reg.ReplaceAllString(s, "-")
	// Remove leading/trailing hyphens
	s = strings.Trim(s, "-")
	// Limit length
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

func processImageWithName(srcPath, baseName, basePath string) (string, error) {
	ext := filepath.Ext(srcPath)
	imageName := fmt.Sprintf("%s%s", baseName, ext)
	destPath := filepath.Join(basePath, "assets", "images", "site", imageName)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", err
	}

	// Copy image file
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return "", err
	}

	return imageName, nil
}

func generateFromWebsite(ctx context.Context, apiKey, promptTemplate, urlStr, title, content, userTags, heroImage, model string) (postContent, filename string, err error) {
	client := openai.NewClient(apiKey)

	// Build context for the AI
	websiteContext := fmt.Sprintf(`
Website URL: %s
Title: %s

Content:
%s
`, urlStr, title, content)

	// Get current date for the post
	currentDate := time.Now().Format("2006-01-02")

	heroImageInfo := ""
	if heroImage != "" {
		heroImageInfo = fmt.Sprintf("\nHero image available: %s (use path: /images/site/%s)", heroImage, heroImage)
	}

	userPrompt := fmt.Sprintf(`%s

Please generate a blog post about this website/article:

%s
%s

User-provided tags: %s (suggest appropriate tags if none provided)

IMPORTANT: Your response must be ONLY valid markdown. Do not include any explanatory text before or after the markdown.
IMPORTANT: Use date: %s in the front matter.
%s

Generate a complete Hugo markdown post following the style guide above.
`, promptTemplate, websiteContext, heroImageInfo, userTags, currentDate,
		func() string {
			if heroImage != "" {
				return fmt.Sprintf("IMPORTANT: Include 'hero: /images/site/%s' in the front matter.", heroImage)
			}
			return ""
		}())

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a technical blog writer who creates detailed, honest posts about web content and articles. Follow the style guide precisely. Output ONLY the markdown content, no explanations.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature: 0.7,
	})

	if err != nil {
		return "", "", fmt.Errorf("OpenAI API error: %w\n\nTroubleshooting:\n- Check your API key is valid\n- Verify your OpenAI account has credits: https://platform.openai.com/usage\n- Try a different model with --model gpt-4o-mini\n- Check rate limits: https://platform.openai.com/account/limits", err)
	}

	if len(resp.Choices) == 0 {
		return "", "", fmt.Errorf("no response from OpenAI")
	}

	postContent = resp.Choices[0].Message.Content

	// Generate filename from content
	filename, err = generateFilename(ctx, client, postContent, model)
	if err != nil {
		// Fallback to sanitized title if filename generation fails
		logError("Failed to generate filename, using article title: %v", err)
		filename = sanitizeFilename(title)
	}

	return postContent, filename, nil
}
