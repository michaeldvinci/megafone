# megafone 📣

AI-powered content generation tool for technical blogs. Generate comprehensive blog posts from GitHub repositories, web articles, or research topics.

## Features

- 🤖 **AI-Powered Content**: Uses OpenAI GPT-4 to write complete blog posts
- 🔬 **Research Mode**: Provide a topic string and get a comprehensive 4-5 minute read
- 📊 **GitHub Integration**: Fetches repo metadata and README automatically
- 🌐 **Website Scraping**: Extract content and images from any web article
- 🎨 **Style Matching**: Multiple prompt templates for different content types
- 🖼️ **Auto Image Detection**: Finds and downloads hero images from websites and repos
- 🏷️ **Smart Tagging**: AI suggests relevant tags based on content
- 📝 **Customizable Prompts**: Multiple templates that auto-select based on content type
- 🎯 **Smart Content Type Detection**: Automatically picks the right template

## Prerequisites

- Go 1.23+
- OpenAI API key ([get one here](https://platform.openai.com/api-keys))

## Installation

```bash
cd _companion
go mod download
go build -o megafone
```

## Usage

### Basic Usage

```bash
# Set your OpenAI API key
export OPENAI_API_KEY="sk-..."

# Generate from GitHub repo
./megafone generate \
  --topic https://github.com/user/repo \
  --site-source ~/code/hugo

# Generate from web article (auto-extracts content and image)
./megafone generate \
  --topic https://www.cnn.com/2025/10/19/article \
  --site-source ~/code/hugo

# Research and write about a topic (4-5 min read)
./megafone generate \
  --topic "kubernetes security best practices" \
  --site-source ~/code/hugo

# With custom tags
./megafone generate \
  --topic "how LLMs work" \
  --site-source ~/code/hugo \
  --tags "ai,machine-learning,deep-dive"

# With manual image
./megafone generate \
  --topic https://dev.to/article \
  --site-source ~/code/hugo \
  --image ~/Desktop/screenshot.png
```

### Dry Run Mode

Preview generated content without writing files:

```bash
./hugo-companion generate \
  --repo https://github.com/user/repo \
  --dry-run
```

### Custom Prompt Template

Use a different prompt file:

```bash
./hugo-companion generate \
  --repo https://github.com/user/repo \
  --prompt my-custom-prompt.txt
```

### GitHub Actions

Trigger via GitHub Actions UI:

1. Go to **Actions** tab
2. Select **"Create New Post"** workflow
3. Click **"Run workflow"**
4. Fill in:
   - **Repository URL**: `https://github.com/user/repo`
   - **Image URL**: (optional) Direct URL to image
   - **Tags**: `homelab,docker,go`

The workflow will:
1. Fetch repo metadata
2. Download image (if provided)
3. Generate markdown post
4. Create a Pull Request for review

### Build Binary

```bash
cd _companion
go build -o hugo-post
./hugo-post --repo https://github.com/michaeldvinci/MyProject
```

## How It Works

1. **Fetches GitHub Data**: Retrieves repo metadata, README, language, and stats
2. **Loads Style Guide**: Reads `prompt.txt` with your writing style and structure preferences
3. **Generates Content**: Sends context to OpenAI GPT-4 to write a complete blog post
4. **Processes Images**: Copies hero images to the correct Hugo directory
5. **Creates Post**: Writes Hugo-compatible markdown to `content/posts/en/`

## Customizing the Writing Style

Edit `prompt.txt` to adjust:
- Tone and voice
- Post structure and sections
- Technical depth
- Tag selection criteria
- Common phrases and patterns

The AI will follow your prompt template precisely.

## GitHub Actions Integration

The included workflow allows you to generate posts via GitHub UI:

1. Go to **Actions** tab
2. Select **"Create New Post"** workflow
3. Click **"Run workflow"**
4. Provide repo URL, optional image URL, and tags

A Pull Request will be created with the generated post for review.

## Configuration

### Environment Variables

- `OPENAI_API_KEY` - Your OpenAI API key (required)

### File Locations

- **Posts**: Written to `../content/posts/en/`
- **Images**: Copied to `../assets/images/site/`
- **Prompt**: Reads from `prompt.txt` (customizable via `--prompt`)

## Dependencies

- `github.com/spf13/cobra` - CLI framework
- `github.com/google/go-github/v57` - GitHub API client
- `github.com/sashabaranov/go-openai` - OpenAI API client

## Examples

```bash
# Generate post with AI-suggested tags
./hugo-companion generate --repo https://github.com/michaeldvinci/Syllabus

# Include screenshot
./hugo-companion generate \
  --repo https://github.com/user/project \
  --image ~/screenshots/app.png

# Preview without writing files
./hugo-companion generate \
  --repo https://github.com/user/project \
  --dry-run

# Use custom prompt template
./hugo-companion generate \
  --repo https://github.com/user/project \
  --prompt prompts/technical-deep-dive.txt
```

## Cost Considerations

Uses OpenAI GPT-4o. Each post generation costs approximately $0.05-0.15 depending on README length and complexity.
