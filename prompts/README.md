# Megafone Prompt Templates

This directory contains prompt templates for different content types. Megafone automatically selects the appropriate template based on the input you provide (URL or research topic).

## Available Templates

### 1. `research-topic.txt`
**Used for:** Research topics and general inquiries

**Auto-selected when:**
- Input is not a URL (doesn't contain http://, https://, or domain extensions)
- You provide a topic string instead of a URL

**Style:** Comprehensive research-based post with fundamentals, deep-dive sections, and practical examples. Targets 4-5 minute read time (800-1200 words).

**How it works:**
1. First API call: Researches the topic and gathers comprehensive information
2. Second API call: Synthesizes research into a well-structured blog post

**Example usage:**
```bash
./megafone generate -t "kubernetes security best practices" -s ~/hugo
./megafone generate -t "how LLMs work" -s ~/hugo
./megafone generate -t "docker vs podman comparison" -s ~/hugo
```

---

### 2. `github-project.txt`
**Used for:** GitHub repositories and open-source projects

**Auto-selected when:**
- URL contains `github.com`

**Style:** Technical deep-dive focused on architecture, implementation, and technical stack

**Example usage:**
```bash
./megafone generate -t https://github.com/user/repo -s ~/hugo
```

---

### 3. `news-article.txt`
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

### 4. `technical-article.txt`
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
1. **Research topics** (non-URL strings) → `research-topic.txt`
2. **GitHub URLs** → `github-project.txt`
3. **News sites** → `news-article.txt`
4. **Technical sites** → `technical-article.txt`
5. **Other URLs** → `news-article.txt` (default fallback)

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
