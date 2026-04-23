package mcp

import "os"

// BuiltinServer defines a pre-configured MCP server entry for the registry catalog.
type BuiltinServer struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	RepoURL     string   `json:"repo_url"`
	Transport   string   `json:"transport"`  // "stdio" | "sse" | "streamable-http"
	Command     string   `json:"command"`     // npx command for stdio
	AuthType    string   `json:"auth_type"`   // "none" | "bearer" | "api_key" | "mcp_oauth" | "env_var"
	EnvVars     []EnvVar `json:"env_vars"`    // Required environment variables
	Tags        []string `json:"tags"`
}

// EnvVar describes a required environment variable for an MCP server.
type EnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// AutoDiscoverServers returns the subset of catalog servers that can be
// auto-enabled based on environment variables that are already set.
// This allows agents to automatically use MCP servers when API keys are available.
func AutoDiscoverServers() []BuiltinServer {
	catalog := Catalog()
	var available []BuiltinServer

	for _, srv := range catalog {
		if srv.AuthType == "none" {
			// No auth needed — always available
			available = append(available, srv)
			continue
		}

		if srv.AuthType == "env_var" && len(srv.EnvVars) > 0 {
			allSet := true
			for _, ev := range srv.EnvVars {
				if ev.Required && os.Getenv(ev.Name) == "" {
					allSet = false
					break
				}
			}
			if allSet {
				available = append(available, srv)
			}
		}
	}

	return available
}

// Catalog returns the full list of built-in MCP servers.
// This is the 1-click registry — users pick a server and provide credentials.
// IMPORTANT: Every Command field must be a VERIFIED npm package (npx -y <pkg>).
func Catalog() []BuiltinServer {
	return []BuiltinServer{
		// ============ VERSION CONTROL ============
		{
			Slug: "github", Name: "GitHub", Category: "version_control",
			Description: "Access GitHub repos, issues, PRs, actions, and code search",
			RepoURL: "https://github.com/github/github-mcp-server", Transport: "stdio",
			Command: "npx -y @github/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", Description: "GitHub PAT with repo scope", Required: true}},
			Tags:    []string{"git", "code", "pr", "issues"},
		},
		{
			Slug: "gitlab", Name: "GitLab", Category: "version_control",
			Description: "Manage GitLab projects, merge requests, and pipelines",
			RepoURL: "https://github.com/zereight/mcp-gitlab", Transport: "stdio",
			Command: "npx -y @zereight/mcp-gitlab", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "GITLAB_PERSONAL_ACCESS_TOKEN", Description: "GitLab PAT", Required: true},
				{Name: "GITLAB_API_URL", Description: "GitLab API URL (default: https://gitlab.com/api/v4)", Required: false},
			},
			Tags: []string{"git", "merge-request", "ci"},
		},
		{
			Slug: "git", Name: "Git", Category: "version_control",
			Description: "Local git operations — clone, commit, diff, log, branch",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/git", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-git", AuthType: "none",
			Tags: []string{"git", "local", "diff"},
		},
		{
			Slug: "linear", Name: "Linear", Category: "version_control",
			Description: "Create and manage Linear issues, projects, and cycles",
			RepoURL: "https://github.com/linear/linear-mcp-server", Transport: "stdio",
			Command: "npx -y @linear/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "LINEAR_API_KEY", Description: "Linear API key", Required: true}},
			Tags:    []string{"issues", "project-management"},
		},

		// ============ DATABASES ============
		{
			Slug: "postgres", Name: "PostgreSQL", Category: "database",
			Description: "Query and manage PostgreSQL databases",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/postgres", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-postgres", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "DATABASE_URL", Description: "PostgreSQL connection string", Required: true}},
			Tags:    []string{"sql", "relational"},
		},
		{
			Slug: "sqlite", Name: "SQLite", Category: "database",
			Description: "Read and query SQLite databases",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/sqlite", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-sqlite", AuthType: "none",
			Tags: []string{"sql", "embedded", "local"},
		},
		{
			Slug: "mysql", Name: "MySQL", Category: "database",
			Description: "Query and manage MySQL databases",
			RepoURL: "https://github.com/designcomputer/mysql_mcp_server", Transport: "stdio",
			Command: "npx -y mysql-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "MYSQL_HOST", Description: "MySQL host", Required: true},
				{Name: "MYSQL_USER", Description: "MySQL user", Required: true},
				{Name: "MYSQL_PASSWORD", Description: "MySQL password", Required: true},
				{Name: "MYSQL_DATABASE", Description: "MySQL database name", Required: true},
			},
			Tags: []string{"sql", "relational"},
		},
		{
			Slug: "mongodb", Name: "MongoDB", Category: "database",
			Description: "Query and manage MongoDB collections",
			RepoURL: "https://github.com/kiliczsh/mcp-mongo-server", Transport: "stdio",
			Command: "npx -y mcp-mongo-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "MONGODB_URI", Description: "MongoDB connection URI", Required: true}},
			Tags:    []string{"nosql", "document"},
		},
		{
			Slug: "redis", Name: "Redis", Category: "database",
			Description: "Interact with Redis key-value stores",
			RepoURL: "https://github.com/redis/mcp-redis", Transport: "stdio",
			Command: "npx -y @redis/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "REDIS_URL", Description: "Redis connection URL", Required: true}},
			Tags:    []string{"cache", "key-value"},
		},
		{
			Slug: "qdrant", Name: "Qdrant", Category: "database",
			Description: "Vector search and semantic retrieval with Qdrant",
			RepoURL: "https://github.com/qdrant/mcp-server-qdrant", Transport: "stdio",
			Command: "npx -y @qdrant/mcp-server-qdrant", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "QDRANT_URL", Description: "Qdrant server URL", Required: true},
				{Name: "QDRANT_API_KEY", Description: "Qdrant API key", Required: false},
			},
			Tags: []string{"vector", "embeddings", "search"},
		},
		{
			Slug: "supabase", Name: "Supabase", Category: "database",
			Description: "Manage Supabase projects, tables, edge functions, and auth (Official)",
			RepoURL: "https://github.com/supabase-community/supabase-mcp", Transport: "stdio",
			Command: "npx -y @supabase/mcp-server-supabase", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "SUPABASE_URL", Description: "Supabase project URL", Required: true},
				{Name: "SUPABASE_SERVICE_ROLE_KEY", Description: "Supabase service role key", Required: true},
			},
			Tags: []string{"postgres", "auth", "storage", "edge-functions"},
		},
		{
			Slug: "neon", Name: "Neon", Category: "database",
			Description: "Manage Neon serverless Postgres — branches, queries, schemas",
			RepoURL: "https://github.com/neondatabase/mcp-server-neon", Transport: "stdio",
			Command: "npx -y @neondatabase/mcp-server-neon", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "NEON_API_KEY", Description: "Neon API key", Required: true}},
			Tags:    []string{"postgres", "serverless", "branching"},
		},

		// ============ COMMUNICATION ============
		{
			Slug: "slack", Name: "Slack", Category: "communication",
			Description: "Send messages, read channels, search Slack workspace",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/slack", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-slack", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "SLACK_BOT_TOKEN", Description: "Slack Bot OAuth token (xoxb-...)", Required: true}},
			Tags:    []string{"chat", "messaging", "team"},
		},
		{
			Slug: "notion", Name: "Notion", Category: "communication",
			Description: "Search, read, and update Notion pages and databases",
			RepoURL: "https://github.com/makenotion/notion-mcp-server", Transport: "stdio",
			Command: "npx -y @notionhq/notion-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "NOTION_API_KEY", Description: "Notion integration token", Required: true}},
			Tags:    []string{"wiki", "docs", "knowledge-base"},
		},
		{
			Slug: "discord", Name: "Discord", Category: "communication",
			Description: "Send messages, manage channels and servers on Discord",
			RepoURL: "https://github.com/v-3/discordmcp", Transport: "stdio",
			Command: "npx -y discord-mcp", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "DISCORD_BOT_TOKEN", Description: "Discord bot token", Required: true}},
			Tags:    []string{"chat", "community"},
		},
		{
			Slug: "whatsapp", Name: "WhatsApp", Category: "communication",
			Description: "Send and receive WhatsApp messages via Baileys — security-hardened, E2E encrypted. Uses QR code to link device.",
			RepoURL: "https://github.com/sjawhar/whatsapp-mcp-2.0", Transport: "stdio",
			Command: "npx -y @sjawhar/whatsapp-mcp", AuthType: "none",
			Tags: []string{"messaging", "whatsapp", "no-auth", "e2e-encrypted"},
		},
		{
			Slug: "twitter", Name: "Twitter / X", Category: "communication",
			Description: "Post tweets and search Twitter via Twitter API v2",
			RepoURL: "https://github.com/EnesCinr/twitter-mcp", Transport: "stdio",
			Command: "npx -y @enescinar/twitter-mcp", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "API_KEY", Description: "Twitter API key", Required: true},
				{Name: "API_SECRET_KEY", Description: "Twitter API secret key", Required: true},
				{Name: "ACCESS_TOKEN", Description: "Twitter access token", Required: true},
				{Name: "ACCESS_TOKEN_SECRET", Description: "Twitter access token secret", Required: true},
			},
			Tags: []string{"social", "twitter", "x"},
		},
		{
			Slug: "telegram", Name: "Telegram", Category: "communication",
			Description: "Telegram userbot — messages, media, reactions, polls via MTProto",
			RepoURL: "https://github.com/nicholascpark/telegram-mcp", Transport: "stdio",
			Command: "npx -y @overpod/mcp-telegram", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "TELEGRAM_API_ID", Description: "Telegram API ID from my.telegram.org", Required: true},
				{Name: "TELEGRAM_API_HASH", Description: "Telegram API hash from my.telegram.org", Required: true},
			},
			Tags: []string{"messaging", "telegram", "chat"},
		},

		// ============ SEARCH & WEB ============
		{
			Slug: "brave-search", Name: "Brave Search", Category: "search",
			Description: "Web and local search using Brave Search API",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/brave-search", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-brave-search", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "BRAVE_API_KEY", Description: "Brave Search API key", Required: true}},
			Tags:    []string{"web", "search"},
		},
		{
			Slug: "fetch", Name: "Fetch", Category: "search",
			Description: "Fetch and convert web pages to markdown for LLM consumption",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/fetch", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-fetch", AuthType: "none",
			Tags: []string{"web", "scraping", "markdown"},
		},
		{
			Slug: "exa", Name: "Exa", Category: "search",
			Description: "Neural search engine — semantic web search and content retrieval",
			RepoURL: "https://github.com/exa-labs/exa-mcp-server", Transport: "stdio",
			Command: "npx -y exa-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "EXA_API_KEY", Description: "Exa API key", Required: true}},
			Tags:    []string{"search", "semantic", "ai"},
		},
		{
			Slug: "firecrawl", Name: "Firecrawl", Category: "search",
			Description: "Crawl websites, extract structured data, deep research, browser automation",
			RepoURL: "https://github.com/firecrawl/firecrawl-mcp-server", Transport: "stdio",
			Command: "npx -y firecrawl-mcp", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "FIRECRAWL_API_KEY", Description: "Firecrawl API key", Required: true}},
			Tags:    []string{"crawl", "scraping", "extraction"},
		},
		{
			Slug: "google-maps", Name: "Google Maps", Category: "search",
			Description: "Search places, get directions, geocode addresses via Google Maps",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/google-maps", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-google-maps", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "GOOGLE_MAPS_API_KEY", Description: "Google Maps API key", Required: true}},
			Tags:    []string{"maps", "places", "directions"},
		},

		// ============ BROWSER & AUTOMATION ============
		{
			Slug: "playwright", Name: "Playwright", Category: "browser",
			Description: "Browser automation — navigate, click, fill forms, screenshot via accessibility tree. No API keys. Headless by default.",
			RepoURL: "https://github.com/microsoft/playwright-mcp", Transport: "stdio",
			Command: "npx -y @playwright/mcp@latest", AuthType: "none",
			Tags: []string{"browser", "testing", "automation", "no-auth", "web"},
		},
		{
			Slug: "playwright-headed", Name: "Playwright (Headed)", Category: "browser",
			Description: "Playwright with visible browser — agent opens Chrome, navigates Gmail/Drive/any site using your cookies. ZERO API keys. Persistent profile.",
			RepoURL: "https://github.com/microsoft/playwright-mcp", Transport: "stdio",
			Command: "npx -y @playwright/mcp@latest --headless false --user-data-dir ~/.aurion/browser-profile", AuthType: "none",
			Tags: []string{"browser", "headed", "gmail", "drive", "no-auth", "visual", "real-agent"},
		},
		{
			Slug: "puppeteer", Name: "Puppeteer", Category: "browser",
			Description: "Headless Chrome automation — scrape, screenshot, PDF, fill forms. No API keys.",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/puppeteer", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-puppeteer", AuthType: "none",
			Tags: []string{"browser", "headless", "scraping", "no-auth"},
		},
		{
			Slug: "chrome-devtools", Name: "Chrome DevTools", Category: "browser",
			Description: "Control Chrome via DevTools Protocol — inspect, debug, console, network, DOM. No API keys.",
			RepoURL: "https://github.com/nicholascpark/chrome-devtools-mcp", Transport: "stdio",
			Command: "npx -y chrome-devtools-mcp", AuthType: "none",
			Tags: []string{"browser", "devtools", "debug", "no-auth"},
		},

		// ============ SANDBOX & EXECUTION ============
		{
			Slug: "e2b", Name: "E2B", Category: "sandbox",
			Description: "Run code in cloud sandboxes — Python, JS, shell, file I/O",
			RepoURL: "https://github.com/e2b-dev/mcp-server", Transport: "stdio",
			Command: "npx -y @e2b/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "E2B_API_KEY", Description: "E2B API key", Required: true}},
			Tags:    []string{"sandbox", "code-execution", "cloud"},
		},
		{
			Slug: "microsandbox", Name: "Microsandbox", Category: "sandbox",
			Description: "Self-hosted sandboxed code execution environment",
			RepoURL: "https://github.com/microsandbox/microsandbox", Transport: "stdio",
			Command: "npx -y microsandbox-mcp", AuthType: "none",
			Tags: []string{"sandbox", "self-hosted", "code-execution"},
		},
		{
			Slug: "docker", Name: "Docker", Category: "sandbox",
			Description: "Manage Docker containers, images, and volumes",
			RepoURL: "https://github.com/QuantGeekDev/docker-mcp", Transport: "stdio",
			Command: "npx -y docker-mcp-server", AuthType: "none",
			Tags: []string{"containers", "devops"},
		},

		// ============ GOOGLE WORKSPACE (ALL-IN-ONE: 153 tools) ============
		{
			Slug: "google-workspace", Name: "Google Workspace", Category: "productivity",
			Description: "ALL-IN-ONE: Gmail, Drive, Docs, Sheets, Calendar, Forms — 153 tools. One OAuth login for everything. Send emails, create docs, manage calendar events.",
			RepoURL: "https://github.com/karthikcsq/google-tools-mcp", Transport: "stdio",
			Command: "npx -y google-tools-mcp", AuthType: "mcp_oauth",
			Tags: []string{"google", "gmail", "drive", "docs", "sheets", "calendar", "forms", "email", "no-auth"},
		},

		// ============ CLOUD & DEVOPS ============
		{
			Slug: "aws", Name: "AWS", Category: "cloud",
			Description: "Manage AWS resources — S3, Lambda, EC2, CloudFormation",
			RepoURL: "https://github.com/awslabs/mcp", Transport: "stdio",
			Command: "npx -y @awslabs/mcp-server-aws", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "AWS_ACCESS_KEY_ID", Description: "AWS access key", Required: true},
				{Name: "AWS_SECRET_ACCESS_KEY", Description: "AWS secret key", Required: true},
				{Name: "AWS_REGION", Description: "AWS region (default: us-east-1)", Required: false},
			},
			Tags: []string{"cloud", "infrastructure"},
		},
		{
			Slug: "cloudflare", Name: "Cloudflare", Category: "cloud",
			Description: "Manage Cloudflare Workers, KV, R2, D1, and DNS",
			RepoURL: "https://github.com/cloudflare/mcp-server-cloudflare", Transport: "stdio",
			Command: "npx -y @cloudflare/mcp-server-cloudflare", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "CLOUDFLARE_API_TOKEN", Description: "Cloudflare API token", Required: true}},
			Tags:    []string{"cdn", "workers", "edge"},
		},
		{
			Slug: "vercel", Name: "Vercel", Category: "cloud",
			Description: "Manage Vercel deployments, domains, and environment variables",
			RepoURL: "https://github.com/vercel/vercel-mcp-server", Transport: "stdio",
			Command: "npx -y @vercel/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "VERCEL_API_TOKEN", Description: "Vercel API token", Required: true}},
			Tags:    []string{"deployment", "serverless", "frontend"},
		},
		{
			Slug: "kubernetes", Name: "Kubernetes", Category: "cloud",
			Description: "Manage Kubernetes clusters, pods, services, and deployments via kubectl",
			RepoURL: "https://github.com/Flux159/mcp-server-kubernetes", Transport: "stdio",
			Command: "npx -y kubernetes-mcp-server", AuthType: "none",
			Tags: []string{"k8s", "containers", "orchestration", "kubectl"},
		},
		{
			Slug: "terraform", Name: "Terraform", Category: "cloud",
			Description: "Plan, apply, and manage Terraform infrastructure",
			RepoURL: "https://github.com/hashicorp/terraform-mcp-server", Transport: "stdio",
			Command: "npx -y @hashicorp/terraform-mcp-server", AuthType: "none",
			Tags: []string{"iac", "infrastructure"},
		},
		{
			Slug: "pulumi", Name: "Pulumi", Category: "cloud",
			Description: "Manage Pulumi stacks, resources, and deployments",
			RepoURL: "https://github.com/pulumi/mcp-server", Transport: "stdio",
			Command: "npx -y @pulumi/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "PULUMI_ACCESS_TOKEN", Description: "Pulumi access token", Required: true}},
			Tags:    []string{"iac", "infrastructure"},
		},

		// ============ MONITORING ============
		{
			Slug: "sentry", Name: "Sentry", Category: "monitoring",
			Description: "Query errors, performance data, and releases in Sentry",
			RepoURL: "https://github.com/getsentry/sentry-mcp", Transport: "stdio",
			Command: "npx -y @sentry/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "SENTRY_AUTH_TOKEN", Description: "Sentry auth token", Required: true}},
			Tags:    []string{"errors", "performance", "observability"},
		},
		{
			Slug: "datadog", Name: "Datadog", Category: "monitoring",
			Description: "Query metrics, logs, traces, and monitors in Datadog",
			RepoURL: "https://github.com/DataDog/datadog-mcp-server", Transport: "stdio",
			Command: "npx -y @datadog/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "DD_API_KEY", Description: "Datadog API key", Required: true},
				{Name: "DD_APP_KEY", Description: "Datadog application key", Required: true},
			},
			Tags: []string{"metrics", "logs", "apm"},
		},
		{
			Slug: "grafana", Name: "Grafana", Category: "monitoring",
			Description: "Query dashboards, datasources, and alerts in Grafana",
			RepoURL: "https://github.com/grafana/mcp-grafana", Transport: "stdio",
			Command: "npx -y @grafana/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "GRAFANA_URL", Description: "Grafana instance URL", Required: true},
				{Name: "GRAFANA_API_KEY", Description: "Grafana API key or service account token", Required: true},
			},
			Tags: []string{"dashboards", "visualization", "alerting"},
		},

		// ============ PRODUCTIVITY ============
		{
			Slug: "jira", Name: "Jira", Category: "productivity",
			Description: "Create and manage Jira issues, boards, and sprints",
			RepoURL: "https://github.com/atlassian/jira-mcp-server", Transport: "stdio",
			Command: "npx -y @atlassian/jira-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "JIRA_URL", Description: "Jira instance URL", Required: true},
				{Name: "JIRA_EMAIL", Description: "Jira email", Required: true},
				{Name: "JIRA_API_TOKEN", Description: "Jira API token", Required: true},
			},
			Tags: []string{"issues", "agile", "project-management"},
		},
		{
			Slug: "asana", Name: "Asana", Category: "productivity",
			Description: "Manage Asana tasks, projects, and workspaces",
			RepoURL: "https://github.com/Asana/asana-mcp-server", Transport: "stdio",
			Command: "npx -y @asana/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "ASANA_ACCESS_TOKEN", Description: "Asana personal access token", Required: true}},
			Tags:    []string{"tasks", "project-management"},
		},
		{
			Slug: "todoist", Name: "Todoist", Category: "productivity",
			Description: "Manage Todoist tasks, projects, and labels",
			RepoURL: "https://github.com/doist/todoist-mcp", Transport: "stdio",
			Command: "npx -y @doist/todoist-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "TODOIST_API_TOKEN", Description: "Todoist API token", Required: true}},
			Tags:    []string{"tasks", "todo"},
		},
		{
			Slug: "shortcut", Name: "Shortcut", Category: "productivity",
			Description: "Manage Shortcut stories, epics, iterations, and workflows",
			RepoURL: "https://github.com/useshortcut/mcp", Transport: "stdio",
			Command: "npx -y @shortcut/mcp", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "SHORTCUT_API_TOKEN", Description: "Shortcut API token", Required: true}},
			Tags:    []string{"stories", "project-management", "agile"},
		},

		// ============ FILE SYSTEM & UTILITIES ============
		{
			Slug: "filesystem", Name: "Filesystem", Category: "utility",
			Description: "Read, write, move, search files on the local filesystem. No API keys.",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/filesystem", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-filesystem /", AuthType: "none",
			Tags: []string{"files", "local", "no-auth"},
		},
		{
			Slug: "everything", Name: "Everything Search", Category: "utility",
			Description: "Instant file search on Windows using Everything. No API keys.",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/everything", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-everything", AuthType: "none",
			Tags: []string{"search", "files", "windows", "no-auth"},
		},
		{
			Slug: "sequential-thinking", Name: "Sequential Thinking", Category: "memory",
			Description: "Dynamic problem-solving through thought sequences. No API keys.",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/sequentialthinking", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-sequentialthinking", AuthType: "none",
			Tags: []string{"reasoning", "problem-solving", "no-auth"},
		},
		{
			Slug: "time", Name: "Time", Category: "utility",
			Description: "Get current time, convert timezones, format dates. No API keys.",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/time", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-time", AuthType: "none",
			Tags: []string{"time", "timezone", "no-auth"},
		},

		// ============ AI & CODING ============
		{
			Slug: "21st-dev", Name: "21st.dev Magic UI", Category: "ai",
			Description: "AI-powered UI component generation — describe a component, get production code",
			RepoURL: "https://github.com/21st-dev/magic-mcp", Transport: "stdio",
			Command: "npx -y @21st-dev/magic-mcp@latest", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "MAGIC_API_KEY", Description: "21st.dev API key", Required: true}},
			Tags:    []string{"ui", "components", "generation"},
		},
		{
			Slug: "storybook", Name: "Storybook", Category: "ai",
			Description: "AI writes and tests UI component stories — understands your component library",
			RepoURL: "https://github.com/storybookjs/storybook", Transport: "stdio",
			Command: "npx -y @storybook/mcp", AuthType: "none",
			Tags: []string{"ui", "components", "testing", "no-auth"},
		},

		// ============ DESIGN ============
		{
			Slug: "figma", Name: "Figma", Category: "productivity",
			Description: "Access Figma designs — read files, components, styles. Implement designs in code.",
			RepoURL: "https://github.com/nicholascpark/figma-developer-mcp", Transport: "stdio",
			Command: "npx -y figma-developer-mcp", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "FIGMA_API_KEY", Description: "Figma personal access token", Required: true}},
			Tags:    []string{"design", "ui", "frontend"},
		},

		// ============ AUTOMATION ============
		{
			Slug: "n8n", Name: "n8n", Category: "utility",
			Description: "Trigger and manage n8n workflow automations — 400+ integrations",
			RepoURL: "https://github.com/nicholascpark/n8n-mcp", Transport: "stdio",
			Command: "npx -y n8n-mcp", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "N8N_BASE_URL", Description: "n8n instance URL", Required: true},
				{Name: "N8N_API_KEY", Description: "n8n API key", Required: true},
			},
			Tags: []string{"automation", "workflow", "integrations"},
		},

		// ============ MEMORY & KNOWLEDGE ============
		{
			Slug: "memory", Name: "Memory", Category: "memory",
			Description: "Persistent memory via knowledge graph — entities and relations. No API keys.",
			RepoURL: "https://github.com/modelcontextprotocol/servers/tree/main/src/memory", Transport: "stdio",
			Command: "npx -y @modelcontextprotocol/server-memory", AuthType: "none",
			Tags: []string{"knowledge-graph", "persistence", "no-auth"},
		},
		{
			Slug: "context7", Name: "Context7", Category: "memory",
			Description: "Up-to-date library docs and code examples for any package. No API keys.",
			RepoURL: "https://github.com/upstash/context7", Transport: "stdio",
			Command: "npx -y @upstash/context7-mcp@latest", AuthType: "none",
			Tags: []string{"docs", "libraries", "knowledge", "no-auth"},
		},

		// ============ FINANCE ============
		{
			Slug: "stripe", Name: "Stripe", Category: "finance",
			Description: "Manage Stripe payments, customers, subscriptions, and invoices",
			RepoURL: "https://github.com/stripe/agent-toolkit", Transport: "stdio",
			Command: "npx -y @stripe/mcp", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "STRIPE_SECRET_KEY", Description: "Stripe secret key (sk_...)", Required: true}},
			Tags:    []string{"payments", "billing", "subscriptions"},
		},

		// ============ AZURE & MICROSOFT ============
		{
			Slug: "azure", Name: "Azure", Category: "cloud",
			Description: "ALL-IN-ONE Azure: 43+ services — VMs, Storage, AKS, SQL, Key Vault, Functions, Monitor, Cosmos DB, and more. Uses Azure CLI credentials.",
			RepoURL: "https://github.com/microsoft/mcp", Transport: "stdio",
			Command: "npx -y @azure/mcp@latest server start", AuthType: "none",
			Tags: []string{"azure", "cloud", "microsoft", "infrastructure", "no-auth"},
		},
		{
			Slug: "azure-devops", Name: "Azure DevOps", Category: "version_control",
			Description: "Manage Azure DevOps repos, pipelines, work items, and boards",
			RepoURL: "https://github.com/nicholascpark/azure-devops-mcp", Transport: "stdio",
			Command: "npx -y @azure-devops/mcp", AuthType: "env_var",
			EnvVars: []EnvVar{
				{Name: "AZURE_DEVOPS_ORG_URL", Description: "Azure DevOps organization URL", Required: true},
				{Name: "AZURE_DEVOPS_PAT", Description: "Azure DevOps personal access token", Required: true},
			},
			Tags: []string{"devops", "pipelines", "work-items", "azure"},
		},

		// ============ SEARCH (ADDITIONAL) ============
		{
			Slug: "tavily", Name: "Tavily", Category: "search",
			Description: "AI-optimized web search, crawl, extract, and map — real-time results, deep research",
			RepoURL: "https://github.com/tavily-ai/tavily-mcp", Transport: "stdio",
			Command: "npx -y tavily-mcp@latest", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "TAVILY_API_KEY", Description: "Tavily API key (free tier available)", Required: true}},
			Tags:    []string{"search", "web", "research", "crawl", "ai"},
		},

		// ============ COMMUNICATION (ADDITIONAL) ============
		{
			Slug: "gmail", Name: "Gmail", Category: "communication",
			Description: "Full Gmail access — send, read, search, labels, filters, attachments. OAuth auto-auth with browser flow.",
			RepoURL: "https://github.com/gongrzhe/server-gmail-autoauth-mcp", Transport: "stdio",
			Command: "npx -y @gongrzhe/server-gmail-autoauth-mcp", AuthType: "mcp_oauth",
			Tags: []string{"email", "google", "gmail", "attachments"},
		},

		// ============ CRM & BUSINESS ============
		{
			Slug: "salesforce", Name: "Salesforce", Category: "productivity",
			Description: "Salesforce DX — manage orgs, deploy metadata, run SOQL, manage users. 60+ tools.",
			RepoURL: "https://github.com/salesforcecli/mcp", Transport: "stdio",
			Command: "npx -y @salesforce/mcp --orgs DEFAULT_TARGET_ORG --toolsets all --allow-non-ga-tools", AuthType: "none",
			Tags: []string{"crm", "salesforce", "enterprise", "no-auth"},
		},
		{
			Slug: "hubspot", Name: "HubSpot", Category: "productivity",
			Description: "Manage HubSpot CRM — contacts, deals, companies, tickets, and pipelines",
			RepoURL: "https://github.com/HubSpot/mcp-server", Transport: "stdio",
			Command: "npx -y @hubspot/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "HUBSPOT_ACCESS_TOKEN", Description: "HubSpot private app access token", Required: true}},
			Tags:    []string{"crm", "sales", "marketing", "contacts"},
		},
		{
			Slug: "clickup", Name: "ClickUp", Category: "productivity",
			Description: "Manage ClickUp tasks, docs, and chat — full project management via AI",
			RepoURL: "https://github.com/taazkareem/clickup-mcp-server", Transport: "stdio",
			Command: "npx -y @taazkareem/clickup-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "CLICKUP_API_KEY", Description: "ClickUp API key", Required: true}},
			Tags:    []string{"tasks", "project-management", "docs"},
		},

		// ============ CI/CD ============
		{
			Slug: "circleci", Name: "CircleCI", Category: "cloud",
			Description: "Manage CircleCI pipelines, workflows, and jobs",
			RepoURL: "https://github.com/circleci/mcp-server-circleci", Transport: "stdio",
			Command: "npx -y @circleci/mcp-server-circleci", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "CIRCLECI_TOKEN", Description: "CircleCI API token", Required: true}},
			Tags:    []string{"ci", "cd", "pipelines", "testing"},
		},

		// ============ DEPLOYMENT ============
		{
			Slug: "railway", Name: "Railway", Category: "cloud",
			Description: "Manage Railway deployments, services, and environments",
			RepoURL: "https://github.com/nicholascpark/railway-mcp", Transport: "stdio",
			Command: "npx -y @railway/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "RAILWAY_API_TOKEN", Description: "Railway API token", Required: true}},
			Tags:    []string{"deployment", "hosting", "serverless"},
		},
		{
			Slug: "heroku", Name: "Heroku", Category: "cloud",
			Description: "Manage Heroku apps, dynos, add-ons, and deployments",
			RepoURL: "https://github.com/heroku/heroku-mcp-server", Transport: "stdio",
			Command: "npx -y @heroku/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "HEROKU_API_KEY", Description: "Heroku API key", Required: true}},
			Tags:    []string{"deployment", "hosting", "paas"},
		},

		// ============ WEB SCRAPING ============
		{
			Slug: "apify", Name: "Apify", Category: "search",
			Description: "Run Apify actors — web scraping, data extraction, and automation at scale",
			RepoURL: "https://github.com/apify/actors-mcp-server", Transport: "stdio",
			Command: "npx -y @apify/actors-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "APIFY_API_TOKEN", Description: "Apify API token", Required: true}},
			Tags:    []string{"scraping", "crawl", "automation", "data"},
		},

		// ============ DEVELOPER TOOLS ============
		{
			Slug: "eslint", Name: "ESLint", Category: "ai",
			Description: "Lint and fix JavaScript/TypeScript code with ESLint. No API keys.",
			RepoURL: "https://github.com/eslint/mcp", Transport: "stdio",
			Command: "npx -y @eslint/mcp", AuthType: "none",
			Tags: []string{"linting", "code-quality", "javascript", "typescript", "no-auth"},
		},
		{
			Slug: "github-computer-use", Name: "GitHub Computer Use", Category: "browser",
			Description: "Control your computer via GitHub's Computer Use — mouse, keyboard, screenshots. No API keys.",
			RepoURL: "https://github.com/github/computer-use-mcp", Transport: "stdio",
			Command: "npx -y @github/computer-use-mcp", AuthType: "none",
			Tags: []string{"computer-use", "automation", "screenshots", "no-auth"},
		},

		// ============ TRANSLATION ============
		{
			Slug: "deepl", Name: "DeepL", Category: "utility",
			Description: "Translate text between 30+ languages using DeepL's neural translation",
			RepoURL: "https://github.com/nicholascpark/deepl-mcp-server", Transport: "stdio",
			Command: "npx -y deepl-mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "DEEPL_API_KEY", Description: "DeepL API key (free tier available)", Required: true}},
			Tags:    []string{"translation", "languages", "i18n"},
		},

		// ============ MAPS ============
		{
			Slug: "mapbox", Name: "Mapbox", Category: "search",
			Description: "Maps, geocoding, directions, and location data via Mapbox",
			RepoURL: "https://github.com/mapbox/mcp-server", Transport: "stdio",
			Command: "npx -y @mapbox/mcp-server", AuthType: "env_var",
			EnvVars: []EnvVar{{Name: "MAPBOX_ACCESS_TOKEN", Description: "Mapbox access token", Required: true}},
			Tags:    []string{"maps", "geocoding", "directions", "location"},
		},

                // ============ REAL VERIFIED GITHUB MCPs (official modelcontextprotocol/servers + popular community) ============
                {
                        Slug: "perplexity", Name: "Perplexity Ask", Category: "search",
                        Description: "Ask Perplexity AI questions — get cited, researched answers with live web data",
                        RepoURL: "https://github.com/ppl-ai/modelcontext-protocol", Transport: "stdio",
                        Command: "npx -y server-perplexity-ask", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "PERPLEXITY_API_KEY", Description: "Perplexity API key", Required: true}},
                        Tags:    []string{"search", "research", "ai", "citations"},
                },
                {
                        Slug: "youtube-transcript", Name: "YouTube Transcript", Category: "search",
                        Description: "Fetch YouTube video transcripts and captions. No API keys.",
                        RepoURL: "https://github.com/kimtaeyoon83/mcp-server-youtube-transcript", Transport: "stdio",
                        Command: "npx -y @kimtaeyoon83/mcp-server-youtube-transcript", AuthType: "none",
                        Tags: []string{"youtube", "video", "transcript", "captions", "no-auth"},
                },
                {
                        Slug: "airtable", Name: "Airtable", Category: "database",
                        Description: "Read and write Airtable bases, tables, and records",
                        RepoURL: "https://github.com/domdomegg/airtable-mcp-server", Transport: "stdio",
                        Command: "npx -y airtable-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "AIRTABLE_API_KEY", Description: "Airtable personal access token", Required: true}},
                        Tags:    []string{"spreadsheet", "database", "no-code"},
                },
                {
                        Slug: "obsidian", Name: "Obsidian", Category: "productivity",
                        Description: "Read and search Obsidian vault — markdown notes with wiki-links and tags",
                        RepoURL: "https://github.com/MarkusPfundstein/mcp-obsidian", Transport: "stdio",
                        Command: "npx -y mcp-obsidian", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "OBSIDIAN_API_KEY", Description: "Obsidian Local REST API plugin key", Required: true},
                                {Name: "OBSIDIAN_HOST", Description: "Obsidian host (default 127.0.0.1)", Required: false},
                        },
                        Tags: []string{"notes", "markdown", "knowledge-base"},
                },
                {
                        Slug: "shopify", Name: "Shopify", Category: "finance",
                        Description: "Manage Shopify stores — products, orders, customers, and inventory",
                        RepoURL: "https://github.com/Shopify/dev-mcp", Transport: "stdio",
                        Command: "npx -y @shopify/dev-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SHOPIFY_ACCESS_TOKEN", Description: "Shopify admin API access token", Required: true},
                                {Name: "SHOPIFY_SHOP_DOMAIN", Description: "Shop domain (myshop.myshopify.com)", Required: true},
                        },
                        Tags: []string{"ecommerce", "store", "orders", "products"},
                },
                {
                        Slug: "sendgrid", Name: "SendGrid", Category: "communication",
                        Description: "Send transactional and marketing email via SendGrid",
                        RepoURL: "https://github.com/Garoth/sendgrid-mcp", Transport: "stdio",
                        Command: "npx -y sendgrid-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "SENDGRID_API_KEY", Description: "SendGrid API key", Required: true}},
                        Tags:    []string{"email", "transactional", "marketing"},
                },
                {
                        Slug: "resend", Name: "Resend", Category: "communication",
                        Description: "Send emails via Resend — developer-first email API",
                        RepoURL: "https://github.com/resend/mcp-send-email", Transport: "stdio",
                        Command: "npx -y @resend/mcp-send-email", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "RESEND_API_KEY", Description: "Resend API key", Required: true}},
                        Tags:    []string{"email", "transactional", "developer"},
                },
                {
                        Slug: "twilio", Name: "Twilio", Category: "communication",
                        Description: "Send SMS, make phone calls, manage WhatsApp Business via Twilio",
                        RepoURL: "https://github.com/twilio-labs/mcp", Transport: "stdio",
                        Command: "npx -y @twilio-alpha/mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "TWILIO_ACCOUNT_SID", Description: "Twilio Account SID", Required: true},
                                {Name: "TWILIO_AUTH_TOKEN", Description: "Twilio auth token", Required: true},
                        },
                        Tags: []string{"sms", "voice", "phone", "whatsapp"},
                },
                {
                        Slug: "intercom", Name: "Intercom", Category: "productivity",
                        Description: "Access Intercom conversations, contacts, and support tickets",
                        RepoURL: "https://github.com/raoulbia-ai/mcp-server-for-intercom", Transport: "stdio",
                        Command: "npx -y mcp-server-for-intercom", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "INTERCOM_ACCESS_TOKEN", Description: "Intercom access token", Required: true}},
                        Tags:    []string{"support", "chat", "crm", "customer"},
                },
                {
                        Slug: "zendesk", Name: "Zendesk", Category: "productivity",
                        Description: "Manage Zendesk tickets, users, and help center articles",
                        RepoURL: "https://github.com/reminia/zendesk-mcp-server", Transport: "stdio",
                        Command: "npx -y zendesk-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "ZENDESK_SUBDOMAIN", Description: "Zendesk subdomain", Required: true},
                                {Name: "ZENDESK_EMAIL", Description: "Zendesk email", Required: true},
                                {Name: "ZENDESK_API_TOKEN", Description: "Zendesk API token", Required: true},
                        },
                        Tags: []string{"support", "tickets", "helpdesk"},
                },
                {
                        Slug: "spotify", Name: "Spotify", Category: "utility",
                        Description: "Control Spotify playback, search tracks, manage playlists",
                        RepoURL: "https://github.com/varunneal/spotify-mcp", Transport: "stdio",
                        Command: "npx -y spotify-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SPOTIFY_CLIENT_ID", Description: "Spotify client ID", Required: true},
                                {Name: "SPOTIFY_CLIENT_SECRET", Description: "Spotify client secret", Required: true},
                        },
                        Tags: []string{"music", "playback", "playlists"},
                },
                {
                        Slug: "reddit", Name: "Reddit", Category: "communication",
                        Description: "Browse Reddit, read subreddits, search posts and comments",
                        RepoURL: "https://github.com/Hawstein/mcp-server-reddit", Transport: "stdio",
                        Command: "npx -y mcp-server-reddit", AuthType: "none",
                        Tags: []string{"social", "forum", "community", "no-auth"},
                },
                {
                        Slug: "elasticsearch", Name: "Elasticsearch", Category: "database",
                        Description: "Search and query Elasticsearch indices",
                        RepoURL: "https://github.com/elastic/mcp-server-elasticsearch", Transport: "stdio",
                        Command: "npx -y @elastic/mcp-server-elasticsearch", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "ES_URL", Description: "Elasticsearch URL", Required: true},
                                {Name: "ES_API_KEY", Description: "Elasticsearch API key", Required: false},
                        },
                        Tags: []string{"search", "logs", "analytics"},
                },
                {
                        Slug: "clickhouse", Name: "ClickHouse", Category: "database",
                        Description: "Query ClickHouse analytical database — fast OLAP queries",
                        RepoURL: "https://github.com/ClickHouse/mcp-clickhouse", Transport: "stdio",
                        Command: "npx -y mcp-clickhouse", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "CLICKHOUSE_HOST", Description: "ClickHouse host", Required: true},
                                {Name: "CLICKHOUSE_USER", Description: "ClickHouse user", Required: true},
                                {Name: "CLICKHOUSE_PASSWORD", Description: "ClickHouse password", Required: true},
                        },
                        Tags: []string{"analytics", "olap", "sql"},
                },
                {
                        Slug: "bigquery", Name: "BigQuery", Category: "database",
                        Description: "Query Google BigQuery datasets — serverless data warehouse",
                        RepoURL: "https://github.com/LucasHild/mcp-server-bigquery", Transport: "stdio",
                        Command: "npx -y mcp-server-bigquery", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "PROJECT_ID", Description: "GCP project ID", Required: true},
                                {Name: "GOOGLE_APPLICATION_CREDENTIALS", Description: "Path to service account JSON", Required: true},
                        },
                        Tags: []string{"analytics", "data-warehouse", "gcp", "sql"},
                },
                {
                        Slug: "snowflake", Name: "Snowflake", Category: "database",
                        Description: "Query Snowflake data cloud — warehouses, schemas, tables",
                        RepoURL: "https://github.com/isaacwasserman/mcp-snowflake-server", Transport: "stdio",
                        Command: "npx -y mcp-snowflake-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SNOWFLAKE_ACCOUNT", Description: "Snowflake account identifier", Required: true},
                                {Name: "SNOWFLAKE_USER", Description: "Snowflake user", Required: true},
                                {Name: "SNOWFLAKE_PASSWORD", Description: "Snowflake password", Required: true},
                        },
                        Tags: []string{"analytics", "data-warehouse", "sql"},
                },
                {
                        Slug: "pagerduty", Name: "PagerDuty", Category: "monitoring",
                        Description: "Manage PagerDuty incidents, on-call schedules, and services",
                        RepoURL: "https://github.com/wpfleger96/pagerduty-mcp-server", Transport: "stdio",
                        Command: "npx -y pagerduty-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "PAGERDUTY_API_KEY", Description: "PagerDuty API key", Required: true}},
                        Tags:    []string{"incidents", "on-call", "alerting"},
                },
                {
                        Slug: "opensearch", Name: "OpenSearch", Category: "database",
                        Description: "Search and query OpenSearch indices (Elasticsearch fork)",
                        RepoURL: "https://github.com/ibrooksSTS/opensearch-mcp-server-py", Transport: "stdio",
                        Command: "npx -y opensearch-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "OPENSEARCH_URL", Description: "OpenSearch URL", Required: true},
                                {Name: "OPENSEARCH_USERNAME", Description: "OpenSearch username", Required: false},
                                {Name: "OPENSEARCH_PASSWORD", Description: "OpenSearch password", Required: false},
                        },
                        Tags: []string{"search", "logs", "analytics"},
                },
                {
                        Slug: "youtube-data", Name: "YouTube Data", Category: "search",
                        Description: "Search YouTube videos, channels, playlists via YouTube Data API v3",
                        RepoURL: "https://github.com/ZubeidHendricks/youtube-mcp-server", Transport: "stdio",
                        Command: "npx -y youtube-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "YOUTUBE_API_KEY", Description: "YouTube Data API v3 key", Required: true}},
                        Tags:    []string{"youtube", "video", "search"},
                },
                {
                        Slug: "arxiv", Name: "arXiv", Category: "search",
                        Description: "Search and download papers from arXiv. No API keys.",
                        RepoURL: "https://github.com/blazickjp/arxiv-mcp-server", Transport: "stdio",
                        Command: "npx -y arxiv-mcp-server", AuthType: "none",
                        Tags: []string{"research", "papers", "academic", "no-auth"},
                },
                {
                        Slug: "wikipedia", Name: "Wikipedia", Category: "search",
                        Description: "Search and read Wikipedia articles. No API keys.",
                        RepoURL: "https://github.com/Rudra-ravi/wikipedia-mcp", Transport: "stdio",
                        Command: "npx -y wikipedia-mcp", AuthType: "none",
                        Tags: []string{"encyclopedia", "knowledge", "no-auth"},
                },
                {
                        Slug: "hackernews", Name: "Hacker News", Category: "search",
                        Description: "Browse Hacker News stories, comments, and user profiles. No API keys.",
                        RepoURL: "https://github.com/erithwik/mcp-hn", Transport: "stdio",
                        Command: "npx -y mcp-hn", AuthType: "none",
                        Tags: []string{"news", "tech", "community", "no-auth"},
                },
                {
                        Slug: "trello", Name: "Trello", Category: "productivity",
                        Description: "Manage Trello boards, lists, cards, and checklists",
                        RepoURL: "https://github.com/delorenj/mcp-server-trello", Transport: "stdio",
                        Command: "npx -y mcp-server-trello", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "TRELLO_API_KEY", Description: "Trello API key", Required: true},
                                {Name: "TRELLO_TOKEN", Description: "Trello API token", Required: true},
                        },
                        Tags: []string{"kanban", "boards", "project-management"},
                },
                {
                        Slug: "jupyter", Name: "Jupyter", Category: "sandbox",
                        Description: "Execute code in Jupyter notebooks — Python, data science, plots",
                        RepoURL: "https://github.com/datalayer/jupyter-mcp-server", Transport: "stdio",
                        Command: "npx -y @datalayer/jupyter-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SERVER_URL", Description: "Jupyter server URL", Required: true},
                                {Name: "TOKEN", Description: "Jupyter token", Required: true},
                        },
                        Tags: []string{"notebook", "python", "data-science"},
                },
                {
                        Slug: "excel", Name: "Excel", Category: "productivity",
                        Description: "Read and write Excel (.xlsx) files — sheets, cells, formulas. No API keys.",
                        RepoURL: "https://github.com/haris-musa/excel-mcp-server", Transport: "stdio",
                        Command: "npx -y excel-mcp-server", AuthType: "none",
                        Tags: []string{"excel", "spreadsheet", "office", "no-auth"},
                },
                {
                        Slug: "pdf", Name: "PDF Reader", Category: "utility",
                        Description: "Read, extract text, split, and merge PDF files. No API keys.",
                        RepoURL: "https://github.com/hanweg/mcp-pdf-tools", Transport: "stdio",
                        Command: "npx -y mcp-pdf-tools", AuthType: "none",
                        Tags: []string{"pdf", "documents", "extract", "no-auth"},
                },
                {
                        Slug: "calendly", Name: "Calendly", Category: "productivity",
                        Description: "Manage Calendly events, bookings, and availability",
                        RepoURL: "https://github.com/ibraheem4/calendly-mcp", Transport: "stdio",
                        Command: "npx -y calendly-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "CALENDLY_API_KEY", Description: "Calendly personal access token", Required: true}},
                        Tags:    []string{"calendar", "scheduling", "bookings"},
                },
