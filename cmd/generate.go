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
	Short: "Generate a new blog post from a URL or research topic",
	Long: `Fetches content from a GitHub repository, website URL, or researches a topic
and uses OpenAI to generate a blog post matching your style.

Examples:
  # From GitHub repo
  megafone generate -t https://github.com/user/repo -s ~/hugo

  # From news article
  megafone generate -t https://www.cnn.com/article -s ~/hugo

  # Research a topic
  megafone generate -t "kubernetes security best practices" -s ~/hugo
  megafone generate -t "how LLMs work" -s ~/hugo`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runGenerate(cmd); err != nil {
			log.Fatalf("Error: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.Flags().StringVarP(&topicURL, "topic", "t", "", "GitHub URL, website URL, or research topic string (required)")
	generateCmd.Flags().StringVarP(&imagePath, "image", "i", "", "Path to hero image")
	generateCmd.Flags().StringVarP(&tags, "tags", "T", "", "Comma-separated tags (AI will suggest if not provided)")
	generateCmd.Flags().StringVarP(&promptFile, "prompt", "p", "", "Path to prompt template file (auto-selected if not provided)")
	generateCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Print generated content without writing files")
	generateCmd.Flags().StringVarP(&model, "model", "m", "gpt-4o", "OpenAI model to use (gpt-4o, gpt-4o-mini, gpt-4-turbo, or gpt-5)")
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

	// Determine content type: GitHub URL, website URL, or research topic
	contentType := detectContentType(topicURL)

	// Auto-select prompt template if not specified
	if promptFile == "" {
		promptFile = selectPromptTemplate(contentType, topicURL)
		logInfo("📋 Auto-selected prompt template: %s", promptFile)
	}

	var repoData *github.Repository
	var readmeContent string
	var contentTitle string
	var imageName string

	if contentType == "github" {
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
	} else if contentType == "website" {
		// Handle regular website
		logInfo("🌐 Fetching website content...")
		websiteContent, title, htmlContent, err := fetchWebsiteContent(topicURL)
		if err != nil {
			logError("Failed to fetch website: %v", err)
			return fmt.Errorf("failed to fetch website: %w", err)
		}
		readmeContent = websiteContent
		contentTitle = title
		logInfo("📄 Fetched content from: %s", title)

		// Process image if provided, otherwise try to extract from page
		if imagePath != "" {
			logInfo("🖼️  Processing provided image: %s", imagePath)
			// Use a sanitized version of the title for the image name
			imgBaseName := sanitizeFilename(title)
			imageName, err = processImageWithName(imagePath, imgBaseName, basePath)
			if err != nil {
				logError("Failed to process image: %v", err)
				return fmt.Errorf("failed to process image: %w", err)
			}
		} else {
			// Try to extract hero image from the webpage
			logInfo("🔍 Searching for hero image in webpage...")
			imageURL := extractBestImage(htmlContent, topicURL)
			if imageURL != "" {
				logInfo("✨ Found image: %s", imageURL)
				imgBaseName := sanitizeFilename(title)
				imageName, err = downloadAndProcessWebImage(imageURL, imgBaseName, basePath)
				if err != nil {
					logError("Failed to download image: %v", err)
				}
			} else {
				logInfo("No suitable image found in webpage")
			}
		}
	} else {
		// Handle research topic
		logInfo("🔬 Researching topic: %s", topicURL)
		researchContent, title, err := researchTopic(ctx, apiKey, topicURL, model)
		if err != nil {
			logError("Failed to research topic: %v", err)
			return fmt.Errorf("failed to research topic: %w", err)
		}
		readmeContent = researchContent
		contentTitle = title
		logInfo("📚 Research completed: %s", title)

		// Process image if provided (will generate one later if not)
		if imagePath != "" {
			logInfo("🖼️  Processing provided image: %s", imagePath)
			imgBaseName := sanitizeFilename(title)
			imageName, err = processImageWithName(imagePath, imgBaseName, basePath)
			if err != nil {
				logError("Failed to process image: %v", err)
				return fmt.Errorf("failed to process image: %w", err)
			}
		}
		// Note: For research topics, we'll generate an image after the post is created
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
	if contentType == "github" {
		content, filename, err = generateWithOpenAI(ctx, apiKey, string(promptTemplate), repoData, readmeContent, tags, imageName, model)
	} else if contentType == "website" {
		content, filename, err = generateFromWebsite(ctx, apiKey, string(promptTemplate), topicURL, contentTitle, readmeContent, tags, imageName, model)
	} else {
		// Research topic
		content, filename, err = generateFromResearch(ctx, apiKey, string(promptTemplate), topicURL, contentTitle, readmeContent, tags, imageName, model)
	}
	if err != nil {
		logError("OpenAI generation failed: %v", err)
		return fmt.Errorf("failed to generate content: %w", err)
	}

	logInfo("Generated filename: %s", filename)

	// Validate we have content and filename before proceeding
	if content == "" {
		logError("Generated content is empty! Aborting.")
		return fmt.Errorf("content generation returned empty result")
	}
	if filename == "" {
		logError("Generated filename is empty! Using fallback.")
		filename = sanitizeFilename(contentTitle)
		if filename == "" {
			filename = "untitled-post"
		}
	}

	// Generate hero image if we don't have one yet
	if imageName == "" && !dryRun {
		logInfo("🎨 No image found, generating hero image with DALL-E...")
		generatedImageName, err := generateHeroImage(ctx, apiKey, content, filename, basePath)
		if err != nil {
			logError("Failed to generate image: %v", err)
			logInfo("Continuing without hero image...")
		} else {
			imageName = generatedImageName
			logSuccess("✨ Generated hero image: %s", imageName)

			// Update the content to include the generated image
			if contentType == "research" || contentType == "website" {
				content = updateContentWithImage(content, imageName)
			}
		}
	}

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

func detectContentType(input string) string {
	// Check if it's a GitHub URL
	if strings.Contains(input, "github.com") {
		return "github"
	}

	// Check if it's any URL (has http/https or common TLDs)
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return "website"
	}

	// Check for domain-like patterns (contains .com, .org, etc.)
	if strings.Contains(input, ".com") || strings.Contains(input, ".org") ||
		strings.Contains(input, ".net") || strings.Contains(input, ".io") ||
		strings.Contains(input, ".dev") || strings.Contains(input, ".co") {
		return "website"
	}

	// Otherwise, treat as a research topic
	return "research"
}

func selectPromptTemplate(contentType string, input string) string {
	// If GitHub, use the project template
	if contentType == "github" {
		return "prompts/github-project.txt"
	}

	// If research topic, use research template
	if contentType == "research" {
		return "prompts/research-topic.txt"
	}

	// For websites, detect content type based on URL patterns
	urlLower := strings.ToLower(input)

	// News sites and articles
	newsPatterns := []string{
		"cnn.com", "bbc.com", "reuters.com", "apnews.com",
		"nytimes.com", "wsj.com", "bloomberg.com", "techcrunch.com",
		"theverge.com", "arstechnica.com", "wired.com",
		"/news/", "/article/", "/story/",
	}

	for _, pattern := range newsPatterns {
		if strings.Contains(urlLower, pattern) {
			return "prompts/news-article.txt"
		}
	}

	// Technical documentation and tutorials
	technicalPatterns := []string{
		"stackoverflow.com", "dev.to", "medium.com",
		"docs.", "documentation", "/tutorial/", "/guide/",
		"/blog/", "hashnode.com", "substack.com",
	}

	for _, pattern := range technicalPatterns {
		if strings.Contains(urlLower, pattern) {
			return "prompts/technical-article.txt"
		}
	}

	// Default to news article template for general websites
	return "prompts/news-article.txt"
}

func fetchWebsiteContent(urlStr string) (content, title, htmlContent string, err error) {
	// Parse and validate URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}

	// Ensure we have a scheme
	if parsedURL.Scheme == "" {
		urlStr = "https://" + urlStr
	}

	// Fetch the webpage
	resp, err := http.Get(urlStr)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("HTTP error: %s", resp.Status)
	}

	// Read the body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read response: %w", err)
	}

	htmlContent = string(body)

	// Extract title from HTML
	title = extractTitle(htmlContent)
	if title == "" {
		title = parsedURL.Host
	}

	// Basic HTML to text conversion (strip tags)
	content = stripHTMLTags(htmlContent)

	return content, title, htmlContent, nil
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
	// Try to extract main article content first
	articleContent := extractArticleContent(html)
	if articleContent != "" {
		html = articleContent
	}

	// Remove script and style elements
	scriptRegex := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = scriptRegex.ReplaceAllString(html, "")
	styleRegex := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = styleRegex.ReplaceAllString(html, "")

	// Remove nav, header, footer, aside elements (separately since Go doesn't support backreferences)
	html = regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`).ReplaceAllString(html, "")

	// Remove HTML tags
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	text := tagRegex.ReplaceAllString(html, " ")

	// Clean up whitespace
	spaceRegex := regexp.MustCompile(`\s+`)
	text = spaceRegex.ReplaceAllString(text, " ")

	text = strings.TrimSpace(text)

	// Truncate if still too large (max ~50k characters = ~12.5k tokens roughly)
	maxChars := 50000
	if len(text) > maxChars {
		text = text[:maxChars] + "... [content truncated]"
	}

	return text
}

func extractArticleContent(html string) string {
	// Try common article content selectors
	patterns := []string{
		`(?is)<article[^>]*>(.*?)</article>`,
		`(?is)<div[^>]*class="[^"]*article-body[^"]*"[^>]*>(.*?)</div>`,
		`(?is)<div[^>]*class="[^"]*post-content[^"]*"[^>]*>(.*?)</div>`,
		`(?is)<div[^>]*class="[^"]*entry-content[^"]*"[^>]*>(.*?)</div>`,
		`(?is)<main[^>]*>(.*?)</main>`,
	}

	for _, pattern := range patterns {
		regex := regexp.MustCompile(pattern)
		matches := regex.FindStringSubmatch(html)
		if len(matches) > 1 && len(matches[1]) > 500 {
			return matches[1]
		}
	}

	return ""
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

func extractBestImage(html, baseURL string) string {
	// Try Open Graph image first (most reliable for hero images)
	ogImageRegex := regexp.MustCompile(`<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["']`)
	matches := ogImageRegex.FindStringSubmatch(html)
	if len(matches) > 1 {
		return makeAbsoluteURL(matches[1], baseURL)
	}

	// Try Twitter card image
	twitterImageRegex := regexp.MustCompile(`<meta[^>]*name=["']twitter:image["'][^>]*content=["']([^"']+)["']`)
	matches = twitterImageRegex.FindStringSubmatch(html)
	if len(matches) > 1 {
		return makeAbsoluteURL(matches[1], baseURL)
	}

	// Try to find large images in the article content
	// Look for images with common hero/featured image patterns
	heroPatterns := []string{
		`<img[^>]*class=["'][^"']*hero[^"']*["'][^>]*src=["']([^"']+)["']`,
		`<img[^>]*class=["'][^"']*featured[^"']*["'][^>]*src=["']([^"']+)["']`,
		`<img[^>]*class=["'][^"']*main[^"']*["'][^>]*src=["']([^"']+)["']`,
		`<img[^>]*src=["']([^"']+)["'][^>]*class=["'][^"']*hero[^"']*["']`,
		`<img[^>]*src=["']([^"']+)["'][^>]*class=["'][^"']*featured[^"']*["']`,
	}

	for _, pattern := range heroPatterns {
		regex := regexp.MustCompile(pattern)
		matches = regex.FindStringSubmatch(html)
		if len(matches) > 1 {
			return makeAbsoluteURL(matches[1], baseURL)
		}
	}

	// Fallback: Find first img tag in article content
	articleImgRegex := regexp.MustCompile(`<article[^>]*>.*?<img[^>]*src=["']([^"']+)["']`)
	matches = articleImgRegex.FindStringSubmatch(html)
	if len(matches) > 1 {
		imgURL := matches[1]
		// Filter out tracking pixels, icons, etc.
		if !isValidImageURL(imgURL) {
			return ""
		}
		return makeAbsoluteURL(imgURL, baseURL)
	}

	return ""
}

func makeAbsoluteURL(imageURL, baseURL string) string {
	// If already absolute, return as-is
	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		return imageURL
	}

	// Parse base URL
	base, err := url.Parse(baseURL)
	if err != nil {
		return imageURL
	}

	// If image URL starts with //, add scheme
	if strings.HasPrefix(imageURL, "//") {
		return base.Scheme + ":" + imageURL
	}

	// If image URL is relative, make it absolute
	if strings.HasPrefix(imageURL, "/") {
		return fmt.Sprintf("%s://%s%s", base.Scheme, base.Host, imageURL)
	}

	// Relative to current path
	return fmt.Sprintf("%s://%s%s/%s", base.Scheme, base.Host, filepath.Dir(base.Path), imageURL)
}

func isValidImageURL(imageURL string) bool {
	// Filter out common non-hero images
	lowerURL := strings.ToLower(imageURL)

	// Reject tracking pixels and tiny images
	if strings.Contains(lowerURL, "1x1") || strings.Contains(lowerURL, "pixel") {
		return false
	}

	// Reject icons and logos (usually small)
	if strings.Contains(lowerURL, "icon") || strings.Contains(lowerURL, "logo") {
		return false
	}

	// Reject social media share buttons
	if strings.Contains(lowerURL, "share") || strings.Contains(lowerURL, "social") {
		return false
	}

	// Must be a common image format
	validExts := []string{".jpg", ".jpeg", ".png", ".webp", ".gif"}
	hasValidExt := false
	for _, ext := range validExts {
		if strings.HasSuffix(lowerURL, ext) {
			hasValidExt = true
			break
		}
	}

	return hasValidExt
}

func downloadAndProcessWebImage(imageURL, baseName, basePath string) (string, error) {
	// Download the image
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error downloading image: %s", resp.Status)
	}

	// Read image data
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read image: %w", err)
	}

	// Determine file extension from URL or content-type
	ext := extractImageExtension(imageURL)
	if ext == "" {
		contentType := resp.Header.Get("Content-Type")
		switch contentType {
		case "image/jpeg", "image/jpg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		case "image/gif":
			ext = ".gif"
		default:
			ext = ".jpg" // default
		}
	}

	imageName := fmt.Sprintf("%s%s", baseName, ext)
	destPath := filepath.Join(basePath, "assets", "images", "site", imageName)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", err
	}

	// Write image file
	if err := os.WriteFile(destPath, imageData, 0644); err != nil {
		return "", err
	}

	return imageName, nil
}

func extractImageExtension(imageURL string) string {
	// Parse URL to get path without query params
	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return ""
	}

	// Get extension from path (before query params)
	ext := strings.ToLower(filepath.Ext(parsedURL.Path))

	// Validate it's an image extension
	validExts := []string{".jpg", ".jpeg", ".png", ".webp", ".gif"}
	for _, validExt := range validExts {
		if ext == validExt {
			return ext
		}
	}

	return ""
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

func researchTopic(ctx context.Context, apiKey, topic, model string) (researchContent, title string, err error) {
	client := openai.NewClient(apiKey)

	// Use OpenAI to research the topic and gather comprehensive information
	researchPrompt := fmt.Sprintf(`Research the following topic and provide comprehensive information that would be useful for writing a detailed blog post:

Topic: %s

Please provide:
1. Key concepts and definitions
2. Historical context or background
3. How it works (technical details if applicable)
4. Different approaches or perspectives
5. Practical applications and use cases
6. Common challenges or pitfalls
7. Best practices
8. Current trends or future directions
9. Real-world examples

Organize the information clearly and comprehensively. This will be used as research material for writing a blog post.`, topic)

	// Build request with model-specific parameters
	request := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a knowledgeable research assistant who provides comprehensive, accurate information on technical topics. Provide detailed, well-organized research material.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: researchPrompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   4000,
	}

	resp, err := client.CreateChatCompletion(ctx, request)

	if err != nil {
		return "", "", fmt.Errorf("research API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", "", fmt.Errorf("no research results from OpenAI")
	}

	researchContent = resp.Choices[0].Message.Content
	title = topic

	return researchContent, title, nil
}

func generateFromResearch(ctx context.Context, apiKey, promptTemplate, topic, title, researchContent, userTags, heroImage, model string) (postContent, filename string, err error) {
	client := openai.NewClient(apiKey)

	// Truncate research content if too large (keep first 12000 chars ~ 3000 tokens)
	maxResearchChars := 12000
	if len(researchContent) > maxResearchChars {
		logInfo("Research content is %d chars, truncating to %d chars", len(researchContent), maxResearchChars)
		researchContent = researchContent[:maxResearchChars] + "\n\n[Research content truncated for length]"
	}

	// Build context for the AI
	researchContext := fmt.Sprintf(`
Research Topic: %s

Research Material:
%s
`, topic, researchContent)

	// Get current date for the post
	currentDate := time.Now().Format("2006-01-02")

	heroImageInfo := ""
	if heroImage != "" {
		heroImageInfo = fmt.Sprintf("\nHero image available: %s (use path: /images/site/%s)", heroImage, heroImage)
	}

	userPrompt := fmt.Sprintf(`%s

Please generate a comprehensive blog post about this research topic:

%s
%s

User-provided tags: %s (suggest appropriate tags if none provided)

IMPORTANT: Your response must be ONLY valid markdown. Do not include any explanatory text before or after the markdown.
IMPORTANT: Use date: %s in the front matter.
IMPORTANT: Target 4-5 minute read time (approximately 800-1200 words).
%s

Generate a complete Hugo markdown post following the style guide above.
`, promptTemplate, researchContext, heroImageInfo, userTags, currentDate,
		func() string {
			if heroImage != "" {
				return fmt.Sprintf("IMPORTANT: Include 'hero: /images/site/%s' in the front matter.", heroImage)
			}
			return ""
		}())

	// Build request
	request := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a technical blog writer who creates comprehensive, well-researched posts. Follow the style guide precisely. Output ONLY the markdown content, no explanations.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   3000,
	}

	resp, err := client.CreateChatCompletion(ctx, request)

	if err != nil {
		return "", "", fmt.Errorf("OpenAI API error: %w\n\nTroubleshooting:\n- Check your API key is valid\n- Verify your OpenAI account has credits: https://platform.openai.com/usage\n- Try a different model with --model gpt-4o-mini\n- Check rate limits: https://platform.openai.com/account/limits", err)
	}

	if len(resp.Choices) == 0 {
		return "", "", fmt.Errorf("no response from OpenAI")
	}

	postContent = resp.Choices[0].Message.Content

	// Debug: Log response details
	logInfo("Response finish reason: %s", resp.Choices[0].FinishReason)
	logInfo("Content length: %d characters", len(postContent))

	// Check if content is empty
	if postContent == "" {
		logError("GPT-5 returned empty content!")
		logError("Finish reason: %s", resp.Choices[0].FinishReason)

		// Check if there are refusals
		if resp.Choices[0].Message.Refusal != "" {
			logError("Refusal message: %s", resp.Choices[0].Message.Refusal)
		}

		return "", "", fmt.Errorf("GPT-5 returned empty content (finish reason: %s)", resp.Choices[0].FinishReason)
	}

	// Generate filename from content
	filename, err = generateFilename(ctx, client, postContent, model)
	if err != nil {
		// Fallback to sanitized topic if filename generation fails
		logError("Failed to generate filename, using topic: %v", err)
		filename = sanitizeFilename(topic)
	}

	return postContent, filename, nil
}

func generateHeroImage(ctx context.Context, apiKey, postContent, filename, basePath string) (string, error) {
	client := openai.NewClient(apiKey)

	// Extract the title and key themes from the post to create a good prompt
	imagePrompt := createImagePrompt(postContent)

	logInfo("🖼️  Image prompt: %s", imagePrompt)

	// Generate image with DALL-E (landscape format)
	resp, err := client.CreateImage(ctx, openai.ImageRequest{
		Prompt:         imagePrompt,
		N:              1,
		Size:           openai.CreateImageSize1792x1024, // Landscape format
		ResponseFormat: openai.CreateImageResponseFormatURL,
		Model:          openai.CreateImageModelDallE3,
	})

	if err != nil {
		return "", fmt.Errorf("DALL-E API error: %w", err)
	}

	if len(resp.Data) == 0 {
		return "", fmt.Errorf("no image generated")
	}

	imageURL := resp.Data[0].URL

	// Download the generated image
	imgResp, err := http.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("failed to download generated image: %w", err)
	}
	defer imgResp.Body.Close()

	if imgResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error downloading generated image: %s", imgResp.Status)
	}

	// Read image data
	imageData, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read generated image: %w", err)
	}

	// Save with .png extension (DALL-E returns PNG)
	imageName := fmt.Sprintf("%s.png", filename)
	destPath := filepath.Join(basePath, "assets", "images", "site", imageName)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", err
	}

	// Write image file
	if err := os.WriteFile(destPath, imageData, 0644); err != nil {
		return "", err
	}

	return imageName, nil
}

func createImagePrompt(postContent string) string {
	// Extract title from front matter
	titleRegex := regexp.MustCompile(`title:\s*["']([^"']+)["']`)
	matches := titleRegex.FindStringSubmatch(postContent)
	title := ""
	if len(matches) > 1 {
		title = matches[1]
	}

	// Extract description if available
	descRegex := regexp.MustCompile(`description:\s*["']([^"']+)["']`)
	matches = descRegex.FindStringSubmatch(postContent)
	description := ""
	if len(matches) > 1 {
		description = matches[1]
	}

	// Create a clean, descriptive prompt for DALL-E
	prompt := "Create a modern, minimalist hero image for a technical blog post"

	if title != "" {
		// Remove common prefixes and clean up the title
		cleanTitle := strings.TrimPrefix(title, "Understanding ")
		cleanTitle = strings.TrimPrefix(cleanTitle, "How to ")
		cleanTitle = strings.TrimPrefix(cleanTitle, "A Guide to ")
		prompt += " about: " + cleanTitle
	}

	if description != "" {
		prompt += ". " + description
	}

	// Add style guidance for landscape format - emphasize NO TEXT and full bleed design
	prompt += ". Create a full-bleed design that fills the entire rectangular canvas edge to edge. Use flowing gradients, abstract waves, geometric patterns, or technical mesh backgrounds that cover the whole image. Modern tech aesthetic with rich colors suitable for a developer blog. Wide landscape format (16:9 aspect ratio). IMPORTANT: Absolutely no text, no words, no letters, no numbers, no symbols, no typography of any kind in the image. No floating shapes or objects - the design should fill the entire frame. Pure abstract visual design only."

	return prompt
}

func updateContentWithImage(content, imageName string) string {
	// Check if hero field already exists in front matter
	heroRegex := regexp.MustCompile(`(?m)^hero:\s*.*$`)
	if heroRegex.MatchString(content) {
		// Update existing hero field
		return heroRegex.ReplaceAllString(content, fmt.Sprintf("hero: /images/site/%s", imageName))
	}

	// Add hero field to front matter (after date line)
	dateRegex := regexp.MustCompile(`(?m)(^date:\s*.*$)`)
	return dateRegex.ReplaceAllString(content, fmt.Sprintf("$1\nhero: /images/site/%s", imageName))
}
