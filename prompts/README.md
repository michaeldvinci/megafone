# Megafone Prompt Templates

This directory contains prompt templates for different content types. Megafone automatically selects the appropriate template based on the URL you provide.

## Available Templates

### 1. `github-project.txt`
**Used for:** GitHub repositories and open-source projects

**Auto-selected when:**
- URL contains `github.com`

**Style:** Technical deep-dive focused on architecture, implementation, and technical stack

**Example usage:**
```bash
./megafone generate -t https://github.com/user/repo -s ~/hugo
```

---

### 2. `news-article.txt`
**Used for:** News articles, current events, industry coverage

**Auto-selected when URL contains:**
- News sites: CNN, BBC, Reuters, AP News, NY Times, WSJ, Bloomberg
- Tech news: TechCrunch, The Verge, Ars Technica, Wired
- URL patterns: `/news/`, `/article/`, `/story/`

**Style:** Analytical commentary with context, multiple perspectives, and personal take

**Example usage:**
```bash
./megafone generate -t https://www.cnn.com/2025/10/19/article -s ~/hugo
```

---

### 3. `technical-article.txt`
**Used for:** Technical tutorials, documentation, deep-dives, how-tos

**Auto-selected when URL contains:**
- Tech platforms: Stack Overflow, Dev.to, Medium, Hashnode, Substack
- Documentation: `docs.`, `documentation`
- Content types: `/tutorial/`, `/guide/`, `/blog/`

**Style:** Educational walkthrough with code examples, explanations, and practical applications

**Example usage:**
```bash
./megafone generate -t https://dev.to/article -s ~/hugo
```

---

## Manual Template Selection

You can override the auto-selection by specifying a template:

```bash
./megafone generate \
  -t https://example.com/article \
  -s ~/hugo \
  -p prompts/technical-article.txt
```

Or use a completely custom template:

```bash
./megafone generate \
  -t https://example.com \
  -s ~/hugo \
  -p /path/to/my-custom-template.txt
```

## Default Behavior

If no template is specified:
1. **GitHub URLs** → `github-project.txt`
2. **News sites** → `news-article.txt`
3. **Technical sites** → `technical-article.txt`
4. **Everything else** → `news-article.txt` (default fallback)

## Creating Custom Templates

Templates are plain text files that contain instructions for the AI. See existing templates for examples.

Key sections to include:
- Writing style & tone
- Post structure
- Content requirements
- Tag selection guidelines
- Front matter format
- Common patterns to follow/avoid
- Output format requirements

The template content is passed directly to OpenAI along with the source material (GitHub repo data or website content).
