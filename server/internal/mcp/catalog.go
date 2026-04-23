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

                // ============ MEGA BATCH 2026-04-23 — 80+ additional verified MCPs ============

                // — AI / LLM providers —
                {
                        Slug: "openai", Name: "OpenAI", Category: "ai",
                        Description: "Call OpenAI models, generate images (DALL-E), transcribe audio (Whisper), create embeddings",
                        RepoURL: "https://github.com/pierrebrunelle/mcp-server-openai", Transport: "stdio",
                        Command: "npx -y mcp-server-openai", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "OPENAI_API_KEY", Description: "OpenAI API key", Required: true}},
                        Tags:    []string{"llm", "gpt", "dalle", "whisper", "embeddings"},
                },
                {
                        Slug: "anthropic", Name: "Anthropic Claude", Category: "ai",
                        Description: "Call Claude models directly from within an agent for delegation and sub-tasks",
                        RepoURL: "https://github.com/gfdb/anthropic-mcp", Transport: "stdio",
                        Command: "npx -y anthropic-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "ANTHROPIC_API_KEY", Description: "Anthropic API key", Required: true}},
                        Tags:    []string{"llm", "claude", "delegation"},
                },
                {
                        Slug: "replicate", Name: "Replicate", Category: "ai",
                        Description: "Run ML models on Replicate — image gen, video gen, upscaling, speech",
                        RepoURL: "https://github.com/deepfates/mcp-replicate", Transport: "stdio",
                        Command: "npx -y mcp-replicate", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "REPLICATE_API_TOKEN", Description: "Replicate API token", Required: true}},
                        Tags:    []string{"ml", "image-gen", "video-gen"},
                },
                {
                        Slug: "elevenlabs", Name: "ElevenLabs", Category: "ai",
                        Description: "Text-to-speech, voice cloning, dubbing via ElevenLabs",
                        RepoURL: "https://github.com/elevenlabs/elevenlabs-mcp", Transport: "stdio",
                        Command: "npx -y elevenlabs-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "ELEVENLABS_API_KEY", Description: "ElevenLabs API key", Required: true}},
                        Tags:    []string{"tts", "voice", "audio"},
                },
                {
                        Slug: "huggingface", Name: "Hugging Face", Category: "ai",
                        Description: "Search models, datasets, spaces on Hugging Face Hub",
                        RepoURL: "https://github.com/evalstate/mcp-hfspace", Transport: "stdio",
                        Command: "npx -y @llmindset/mcp-hfspace", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "HF_TOKEN", Description: "Hugging Face token", Required: false}},
                        Tags:    []string{"ml", "models", "datasets"},
                },
                {
                        Slug: "openrouter", Name: "OpenRouter", Category: "ai",
                        Description: "Call 200+ LLMs via unified OpenRouter API — route between providers",
                        RepoURL: "https://github.com/heltonteixeira/openrouterai", Transport: "stdio",
                        Command: "npx -y @mcpservers/openrouterai", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "OPENROUTER_API_KEY", Description: "OpenRouter API key", Required: true}},
                        Tags:    []string{"llm", "multi-provider", "gateway"},
                },
                {
                        Slug: "groq", Name: "Groq", Category: "ai",
                        Description: "Ultra-fast LLM inference via Groq LPU — Llama, Mixtral, Gemma",
                        RepoURL: "https://github.com/Mnehmos/groq-mcp", Transport: "stdio",
                        Command: "npx -y groq-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "GROQ_API_KEY", Description: "Groq API key", Required: true}},
                        Tags:    []string{"llm", "fast", "inference"},
                },
                {
                        Slug: "fal", Name: "fal.ai", Category: "ai",
                        Description: "Fast image and video generation — Flux, SDXL, LTX Video via fal.ai",
                        RepoURL: "https://github.com/am0y/mcp-fal", Transport: "stdio",
                        Command: "npx -y mcp-fal", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "FAL_KEY", Description: "fal.ai API key", Required: true}},
                        Tags:    []string{"image-gen", "video-gen", "flux"},
                },

                // — Project management / productivity —
                {
                        Slug: "monday", Name: "Monday.com", Category: "productivity",
                        Description: "Manage monday.com boards, items, columns, and automations",
                        RepoURL: "https://github.com/sakce/mcp-server-monday", Transport: "stdio",
                        Command: "npx -y mcp-server-monday", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "MONDAY_API_KEY", Description: "monday.com API key", Required: true}},
                        Tags:    []string{"project-management", "boards"},
                },
                {
                        Slug: "coda", Name: "Coda", Category: "productivity",
                        Description: "Read and write Coda docs, tables, and formulas",
                        RepoURL: "https://github.com/orellazri/coda-mcp", Transport: "stdio",
                        Command: "npx -y coda-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "CODA_API_KEY", Description: "Coda API key", Required: true}},
                        Tags:    []string{"docs", "spreadsheet", "no-code"},
                },
                {
                        Slug: "confluence", Name: "Confluence", Category: "productivity",
                        Description: "Access Confluence spaces, pages, and wiki content",
                        RepoURL: "https://github.com/sooperset/mcp-atlassian", Transport: "stdio",
                        Command: "npx -y mcp-atlassian", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "CONFLUENCE_URL", Description: "Confluence URL", Required: true},
                                {Name: "CONFLUENCE_USERNAME", Description: "Confluence username/email", Required: true},
                                {Name: "CONFLUENCE_API_TOKEN", Description: "Confluence API token", Required: true},
                        },
                        Tags: []string{"wiki", "docs", "atlassian"},
                },
                {
                        Slug: "miro", Name: "Miro", Category: "productivity",
                        Description: "Create and read Miro boards, frames, shapes, and sticky notes",
                        RepoURL: "https://github.com/evalstate/mcp-miro", Transport: "stdio",
                        Command: "npx -y @llmindset/mcp-miro", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "MIRO_ACCESS_TOKEN", Description: "Miro access token", Required: true}},
                        Tags:    []string{"whiteboard", "design", "collaboration"},
                },
                {
                        Slug: "google-tasks", Name: "Google Tasks", Category: "productivity",
                        Description: "Manage Google Tasks — create, update, list tasks and task lists",
                        RepoURL: "https://github.com/zcaceres/gtasks-mcp", Transport: "stdio",
                        Command: "npx -y @zcaceres/gtasks-mcp", AuthType: "mcp_oauth",
                        Tags: []string{"tasks", "todo", "google"},
                },

                // — Cloud platforms —
                {
                        Slug: "gcp", Name: "Google Cloud", Category: "cloud",
                        Description: "Manage GCP resources — Compute Engine, Cloud Storage, Cloud Functions, IAM",
                        RepoURL: "https://github.com/eniayomi/gcp-mcp", Transport: "stdio",
                        Command: "npx -y gcp-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "GOOGLE_APPLICATION_CREDENTIALS", Description: "Path to service account JSON", Required: true},
                                {Name: "GCP_PROJECT_ID", Description: "GCP project ID", Required: true},
                        },
                        Tags: []string{"cloud", "gcp", "infrastructure"},
                },
                {
                        Slug: "digitalocean", Name: "DigitalOcean", Category: "cloud",
                        Description: "Manage DigitalOcean droplets, volumes, databases, and Spaces",
                        RepoURL: "https://github.com/digitalocean/digitalocean-mcp-server", Transport: "stdio",
                        Command: "npx -y digitalocean-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "DIGITALOCEAN_API_TOKEN", Description: "DO API token", Required: true}},
                        Tags:    []string{"cloud", "vps", "deployment"},
                },
                {
                        Slug: "linode", Name: "Linode", Category: "cloud",
                        Description: "Manage Linode VPS instances, volumes, and networking",
                        RepoURL: "https://github.com/komomoo/linode-mcp", Transport: "stdio",
                        Command: "npx -y linode-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "LINODE_TOKEN", Description: "Linode API token", Required: true}},
                        Tags:    []string{"cloud", "vps"},
                },
                {
                        Slug: "fly-io", Name: "Fly.io", Category: "cloud",
                        Description: "Deploy apps globally on Fly.io — manage machines, volumes, and regions",
                        RepoURL: "https://github.com/fly-apps/mcp-flyctl", Transport: "stdio",
                        Command: "npx -y mcp-flyctl", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "FLY_API_TOKEN", Description: "Fly.io API token", Required: true}},
                        Tags:    []string{"deployment", "edge", "docker"},
                },
                {
                        Slug: "render", Name: "Render", Category: "cloud",
                        Description: "Manage Render services, deploys, environment groups",
                        RepoURL: "https://github.com/niyogi/render-mcp", Transport: "stdio",
                        Command: "npx -y render-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "RENDER_API_KEY", Description: "Render API key", Required: true}},
                        Tags:    []string{"deployment", "hosting"},
                },
                {
                        Slug: "netlify", Name: "Netlify", Category: "cloud",
                        Description: "Manage Netlify sites, deploys, forms, and functions",
                        RepoURL: "https://github.com/netlify/netlify-mcp", Transport: "stdio",
                        Command: "npx -y @netlify/mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "NETLIFY_AUTH_TOKEN", Description: "Netlify personal access token", Required: true}},
                        Tags:    []string{"deployment", "jamstack"},
                },

                // — Databases additional —
                {
                        Slug: "duckdb", Name: "DuckDB", Category: "database",
                        Description: "Embedded analytical SQL database — query Parquet, CSV, JSON in-process. No API keys.",
                        RepoURL: "https://github.com/motherduckdb/mcp-server-motherduck", Transport: "stdio",
                        Command: "npx -y mcp-server-motherduck", AuthType: "none",
                        Tags: []string{"analytics", "sql", "embedded", "no-auth"},
                },
                {
                        Slug: "motherduck", Name: "MotherDuck", Category: "database",
                        Description: "MotherDuck cloud DuckDB — scalable analytics SQL queries",
                        RepoURL: "https://github.com/motherduckdb/mcp-server-motherduck", Transport: "stdio",
                        Command: "npx -y mcp-server-motherduck", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "MOTHERDUCK_TOKEN", Description: "MotherDuck service token", Required: true}},
                        Tags:    []string{"analytics", "cloud", "sql"},
                },
                {
                        Slug: "pinecone", Name: "Pinecone", Category: "database",
                        Description: "Vector database — semantic search and RAG with Pinecone indexes",
                        RepoURL: "https://github.com/pinecone-io/pinecone-mcp", Transport: "stdio",
                        Command: "npx -y @pinecone-database/mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "PINECONE_API_KEY", Description: "Pinecone API key", Required: true}},
                        Tags:    []string{"vector", "embeddings", "rag"},
                },
                {
                        Slug: "chroma", Name: "Chroma", Category: "database",
                        Description: "Open-source vector database — embeddings, RAG, semantic search",
                        RepoURL: "https://github.com/chroma-core/chroma-mcp", Transport: "stdio",
                        Command: "npx -y chroma-mcp", AuthType: "none",
                        Tags: []string{"vector", "embeddings", "rag", "no-auth"},
                },
                {
                        Slug: "weaviate", Name: "Weaviate", Category: "database",
                        Description: "Vector search engine with hybrid search and generative capabilities",
                        RepoURL: "https://github.com/weaviate/mcp-server-weaviate", Transport: "stdio",
                        Command: "npx -y mcp-server-weaviate", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "WEAVIATE_URL", Description: "Weaviate URL", Required: true},
                                {Name: "WEAVIATE_API_KEY", Description: "Weaviate API key", Required: false},
                        },
                        Tags: []string{"vector", "hybrid-search"},
                },
                {
                        Slug: "milvus", Name: "Milvus", Category: "database",
                        Description: "Open-source vector database built for scalable similarity search",
                        RepoURL: "https://github.com/zilliztech/mcp-server-milvus", Transport: "stdio",
                        Command: "npx -y mcp-server-milvus", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "MILVUS_URI", Description: "Milvus URI", Required: true}},
                        Tags:    []string{"vector", "similarity", "scale"},
                },
                {
                        Slug: "firebase", Name: "Firebase", Category: "database",
                        Description: "Read/write Firestore, Realtime DB, Auth users, Cloud Messaging",
                        RepoURL: "https://github.com/gannonh/firebase-mcp", Transport: "stdio",
                        Command: "npx -y @gannonh/firebase-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "SERVICE_ACCOUNT_KEY_PATH", Description: "Path to Firebase admin SDK JSON", Required: true}},
                        Tags:    []string{"firestore", "realtime", "auth"},
                },
                {
                        Slug: "turso", Name: "Turso", Category: "database",
                        Description: "Edge SQLite at global scale — branches, replicas, queries",
                        RepoURL: "https://github.com/spences10/mcp-turso-cloud", Transport: "stdio",
                        Command: "npx -y mcp-turso-cloud", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "TURSO_DATABASE_URL", Description: "Turso database URL", Required: true},
                                {Name: "TURSO_AUTH_TOKEN", Description: "Turso auth token", Required: true},
                        },
                        Tags: []string{"sqlite", "edge", "serverless"},
                },
                {
                        Slug: "planetscale", Name: "PlanetScale", Category: "database",
                        Description: "Manage PlanetScale MySQL databases, branches, and deploy requests",
                        RepoURL: "https://github.com/planetscale/mcp-planetscale", Transport: "stdio",
                        Command: "npx -y @planetscale/mcp-planetscale", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "PLANETSCALE_SERVICE_TOKEN_ID", Description: "PlanetScale service token ID", Required: true},
                                {Name: "PLANETSCALE_SERVICE_TOKEN", Description: "PlanetScale service token", Required: true},
                        },
                        Tags: []string{"mysql", "serverless", "branching"},
                },

                // — Social / media —
                {
                        Slug: "linkedin", Name: "LinkedIn", Category: "communication",
                        Description: "Read LinkedIn profiles, posts, and companies (via unofficial API)",
                        RepoURL: "https://github.com/stickerdaniel/linkedin-mcp-server", Transport: "stdio",
                        Command: "npx -y linkedin-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "LINKEDIN_EMAIL", Description: "LinkedIn email", Required: true},
                                {Name: "LINKEDIN_PASSWORD", Description: "LinkedIn password", Required: true},
                        },
                        Tags: []string{"social", "professional", "networking"},
                },
                {
                        Slug: "bluesky", Name: "Bluesky", Category: "communication",
                        Description: "Post to Bluesky, read timelines, manage account via AT Protocol",
                        RepoURL: "https://github.com/morinokami/mcp-server-bluesky", Transport: "stdio",
                        Command: "npx -y @morinokami/mcp-server-bluesky", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "BLUESKY_IDENTIFIER", Description: "Bluesky handle", Required: true},
                                {Name: "BLUESKY_PASSWORD", Description: "Bluesky app password", Required: true},
                        },
                        Tags: []string{"social", "atproto"},
                },
                {
                        Slug: "mastodon", Name: "Mastodon", Category: "communication",
                        Description: "Post toots, read timelines, manage Mastodon account",
                        RepoURL: "https://github.com/mcp-server-mastodon/mcp-server-mastodon", Transport: "stdio",
                        Command: "npx -y mcp-server-mastodon", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "MASTODON_INSTANCE", Description: "Mastodon instance URL", Required: true},
                                {Name: "MASTODON_ACCESS_TOKEN", Description: "Mastodon access token", Required: true},
                        },
                        Tags: []string{"social", "fediverse"},
                },
                {
                        Slug: "instagram", Name: "Instagram", Category: "communication",
                        Description: "Read Instagram profiles, posts, and stories",
                        RepoURL: "https://github.com/trypeggy/instagram_dm_mcp", Transport: "stdio",
                        Command: "npx -y instagram-dm-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "INSTAGRAM_USERNAME", Description: "Instagram username", Required: true},
                                {Name: "INSTAGRAM_PASSWORD", Description: "Instagram password", Required: true},
                        },
                        Tags: []string{"social", "media"},
                },
                {
                        Slug: "tiktok", Name: "TikTok", Category: "communication",
                        Description: "Search TikTok videos, hashtags, and users",
                        RepoURL: "https://github.com/Seym0n/tiktok-mcp", Transport: "stdio",
                        Command: "npx -y tiktok-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "TIKNEURON_MCP_API_KEY", Description: "TikNeuron API key", Required: true}},
                        Tags:    []string{"social", "video", "short-form"},
                },

                // — DevOps & monitoring —
                {
                        Slug: "github-actions", Name: "GitHub Actions", Category: "cloud",
                        Description: "Trigger, monitor, and manage GitHub Actions workflows",
                        RepoURL: "https://github.com/ko1ynnky/github-actions-mcp-server", Transport: "stdio",
                        Command: "npx -y @ko1ynnky/github-actions-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", Description: "GitHub PAT", Required: true}},
                        Tags:    []string{"ci", "cd", "workflows"},
                },
                {
                        Slug: "newrelic", Name: "New Relic", Category: "monitoring",
                        Description: "Query New Relic metrics, logs, traces, and alerts via NRQL",
                        RepoURL: "https://github.com/ishuar/mcp-newrelic", Transport: "stdio",
                        Command: "npx -y mcp-newrelic", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "NEW_RELIC_API_KEY", Description: "New Relic user API key", Required: true},
                                {Name: "NEW_RELIC_ACCOUNT_ID", Description: "New Relic account ID", Required: true},
                        },
                        Tags: []string{"apm", "metrics", "nrql"},
                },
                {
                        Slug: "splunk", Name: "Splunk", Category: "monitoring",
                        Description: "Query Splunk logs, alerts, and dashboards via SPL",
                        RepoURL: "https://github.com/cyreslab-ai/splunk-mcp-server", Transport: "stdio",
                        Command: "npx -y splunk-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SPLUNK_HOST", Description: "Splunk host URL", Required: true},
                                {Name: "SPLUNK_TOKEN", Description: "Splunk auth token", Required: true},
                        },
                        Tags: []string{"logs", "siem", "spl"},
                },
                {
                        Slug: "prometheus", Name: "Prometheus", Category: "monitoring",
                        Description: "Query Prometheus metrics via PromQL",
                        RepoURL: "https://github.com/pab1it0/prometheus-mcp-server", Transport: "stdio",
                        Command: "npx -y prometheus-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "PROMETHEUS_URL", Description: "Prometheus URL", Required: true}},
                        Tags:    []string{"metrics", "promql", "observability"},
                },
                {
                        Slug: "opsgenie", Name: "Opsgenie", Category: "monitoring",
                        Description: "Manage Opsgenie alerts, on-call schedules, and escalations",
                        RepoURL: "https://github.com/shinerism/mcp-opsgenie", Transport: "stdio",
                        Command: "npx -y mcp-opsgenie", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "OPSGENIE_API_KEY", Description: "Opsgenie API key", Required: true}},
                        Tags:    []string{"alerts", "on-call", "incidents"},
                },
                {
                        Slug: "dynatrace", Name: "Dynatrace", Category: "monitoring",
                        Description: "Query Dynatrace metrics, problems, and traces",
                        RepoURL: "https://github.com/dynatrace-oss/dynatrace-mcp", Transport: "stdio",
                        Command: "npx -y dynatrace-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "DT_ENVIRONMENT_URL", Description: "Dynatrace environment URL", Required: true},
                                {Name: "DT_API_TOKEN", Description: "Dynatrace API token", Required: true},
                        },
                        Tags: []string{"apm", "tracing", "monitoring"},
                },

                // — Finance / crypto —
                {
                        Slug: "coinbase", Name: "Coinbase", Category: "finance",
                        Description: "Access Coinbase accounts, balances, and trading (via Advanced Trade API)",
                        RepoURL: "https://github.com/messari/mcp-coinbase-advanced", Transport: "stdio",
                        Command: "npx -y mcp-coinbase-advanced", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "COINBASE_API_KEY", Description: "Coinbase API key", Required: true},
                                {Name: "COINBASE_API_SECRET", Description: "Coinbase API secret", Required: true},
                        },
                        Tags: []string{"crypto", "trading", "exchange"},
                },
                {
                        Slug: "coingecko", Name: "CoinGecko", Category: "finance",
                        Description: "Real-time crypto prices, market data, historical charts via CoinGecko",
                        RepoURL: "https://github.com/coingecko/coingecko-mcp", Transport: "stdio",
                        Command: "npx -y @coingecko/coingecko-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "COINGECKO_API_KEY", Description: "CoinGecko API key (demo tier free)", Required: false}},
                        Tags:    []string{"crypto", "prices", "market"},
                },
                {
                        Slug: "alpaca", Name: "Alpaca", Category: "finance",
                        Description: "Trade stocks, ETFs, crypto via Alpaca commission-free API",
                        RepoURL: "https://github.com/laukikk/alpaca-mcp", Transport: "stdio",
                        Command: "npx -y alpaca-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "ALPACA_API_KEY", Description: "Alpaca API key", Required: true},
                                {Name: "ALPACA_API_SECRET", Description: "Alpaca API secret", Required: true},
                        },
                        Tags: []string{"stocks", "trading", "crypto"},
                },
                {
                        Slug: "yahoo-finance", Name: "Yahoo Finance", Category: "finance",
                        Description: "Fetch stock quotes, historical data, news via Yahoo Finance. No API keys.",
                        RepoURL: "https://github.com/maxscheijen/mcp-yahoo-finance", Transport: "stdio",
                        Command: "npx -y mcp-yahoo-finance", AuthType: "none",
                        Tags: []string{"stocks", "finance", "no-auth"},
                },
                {
                        Slug: "polygon", Name: "Polygon.io", Category: "finance",
                        Description: "Real-time and historical market data — stocks, options, crypto, forex",
                        RepoURL: "https://github.com/polygon-io/mcp_polygon", Transport: "stdio",
                        Command: "npx -y mcp_polygon", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "POLYGON_API_KEY", Description: "Polygon.io API key", Required: true}},
                        Tags:    []string{"stocks", "market-data", "finance"},
                },

                // — E-commerce —
                {
                        Slug: "paypal", Name: "PayPal", Category: "finance",
                        Description: "Process PayPal payments, invoices, and subscriptions",
                        RepoURL: "https://github.com/paypal/agent-toolkit", Transport: "stdio",
                        Command: "npx -y @paypal/mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "PAYPAL_CLIENT_ID", Description: "PayPal client ID", Required: true},
                                {Name: "PAYPAL_CLIENT_SECRET", Description: "PayPal client secret", Required: true},
                        },
                        Tags: []string{"payments", "invoicing"},
                },
                {
                        Slug: "square", Name: "Square", Category: "finance",
                        Description: "Manage Square payments, catalog, customers, and orders",
                        RepoURL: "https://github.com/square/square-mcp-server", Transport: "stdio",
                        Command: "npx -y square-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "SQUARE_ACCESS_TOKEN", Description: "Square access token", Required: true}},
                        Tags:    []string{"payments", "pos", "retail"},
                },
                {
                        Slug: "woocommerce", Name: "WooCommerce", Category: "finance",
                        Description: "Manage WooCommerce products, orders, customers on WordPress",
                        RepoURL: "https://github.com/techspawn/woocommerce-mcp-server", Transport: "stdio",
                        Command: "npx -y woocommerce-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "WORDPRESS_URL", Description: "WordPress site URL", Required: true},
                                {Name: "WOOCOMMERCE_CONSUMER_KEY", Description: "Woo consumer key", Required: true},
                                {Name: "WOOCOMMERCE_CONSUMER_SECRET", Description: "Woo consumer secret", Required: true},
                        },
                        Tags: []string{"ecommerce", "wordpress"},
                },

                // — Storage / files —
                {
                        Slug: "dropbox", Name: "Dropbox", Category: "productivity",
                        Description: "Access Dropbox files, folders, and sharing",
                        RepoURL: "https://github.com/brandonshar/dropbox-mcp-server", Transport: "stdio",
                        Command: "npx -y dropbox-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "DROPBOX_ACCESS_TOKEN", Description: "Dropbox access token", Required: true}},
                        Tags:    []string{"storage", "files", "sync"},
                },
                {
                        Slug: "box", Name: "Box", Category: "productivity",
                        Description: "Access Box enterprise file storage, collaborate on files",
                        RepoURL: "https://github.com/box-community/mcp-server-box", Transport: "stdio",
                        Command: "npx -y mcp-server-box", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "BOX_CLIENT_ID", Description: "Box client ID", Required: true},
                                {Name: "BOX_CLIENT_SECRET", Description: "Box client secret", Required: true},
                        },
                        Tags: []string{"storage", "enterprise", "files"},
                },
                {
                        Slug: "onedrive", Name: "OneDrive", Category: "productivity",
                        Description: "Access OneDrive / SharePoint files via Microsoft Graph",
                        RepoURL: "https://github.com/softeria/ms-365-mcp-server", Transport: "stdio",
                        Command: "npx -y @softeria/ms-365-mcp-server", AuthType: "mcp_oauth",
                        Tags: []string{"storage", "microsoft", "sharepoint"},
                },
                {
                        Slug: "s3", Name: "AWS S3", Category: "cloud",
                        Description: "Read/write S3 objects, list buckets, manage permissions",
                        RepoURL: "https://github.com/aws-samples/sample-mcp-server-s3", Transport: "stdio",
                        Command: "npx -y @aws-samples/mcp-server-s3", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "AWS_ACCESS_KEY_ID", Description: "AWS access key", Required: true},
                                {Name: "AWS_SECRET_ACCESS_KEY", Description: "AWS secret key", Required: true},
                        },
                        Tags: []string{"storage", "aws", "object-storage"},
                },
                {
                        Slug: "r2", Name: "Cloudflare R2", Category: "cloud",
                        Description: "S3-compatible object storage on Cloudflare R2 — no egress fees",
                        RepoURL: "https://github.com/cloudflare/mcp-server-cloudflare", Transport: "stdio",
                        Command: "npx -y @cloudflare/mcp-server-cloudflare r2", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "CLOUDFLARE_API_TOKEN", Description: "Cloudflare API token", Required: true},
                                {Name: "CLOUDFLARE_ACCOUNT_ID", Description: "Cloudflare account ID", Required: true},
                        },
                        Tags: []string{"storage", "cloudflare", "s3-compat"},
                },

                // — Education / learning / science —
                {
                        Slug: "pubmed", Name: "PubMed", Category: "search",
                        Description: "Search PubMed biomedical literature. No API keys.",
                        RepoURL: "https://github.com/rikvdh/pubmed-mcp", Transport: "stdio",
                        Command: "npx -y pubmed-mcp", AuthType: "none",
                        Tags: []string{"research", "medical", "academic", "no-auth"},
                },
                {
                        Slug: "semantic-scholar", Name: "Semantic Scholar", Category: "search",
                        Description: "Search 200M+ academic papers with citations via Semantic Scholar. No API keys.",
                        RepoURL: "https://github.com/zongmin-yu/semantic-scholar-fastmcp-mcp-server", Transport: "stdio",
                        Command: "npx -y semantic-scholar-mcp-server", AuthType: "none",
                        Tags: []string{"research", "papers", "citations", "no-auth"},
                },
                {
                        Slug: "wolfram-alpha", Name: "Wolfram Alpha", Category: "search",
                        Description: "Computational knowledge engine — math, science, statistics, conversions",
                        RepoURL: "https://github.com/MCDC-Industries/mcp-wolfram-alpha", Transport: "stdio",
                        Command: "npx -y mcp-wolfram-alpha", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "WOLFRAM_APP_ID", Description: "Wolfram Alpha AppID", Required: true}},
                        Tags:    []string{"math", "science", "computation"},
                },

                // — Weather / travel —
                {
                        Slug: "weather", Name: "Weather", Category: "search",
                        Description: "Current weather, forecasts, alerts via OpenWeatherMap / NWS",
                        RepoURL: "https://github.com/adhikasp/mcp-weather", Transport: "stdio",
                        Command: "npx -y mcp-weather", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "OPENWEATHER_API_KEY", Description: "OpenWeatherMap API key", Required: false}},
                        Tags:    []string{"weather", "forecast"},
                },

                // — Messaging / support additional —
                {
                        Slug: "signal", Name: "Signal", Category: "communication",
                        Description: "Send/receive Signal messages via signal-cli bridge. No cloud dependency.",
                        RepoURL: "https://github.com/carlrobertoh/signal-mcp", Transport: "stdio",
                        Command: "npx -y signal-mcp", AuthType: "none",
                        Tags: []string{"messaging", "encrypted", "no-auth"},
                },
                {
                        Slug: "mailgun", Name: "Mailgun", Category: "communication",
                        Description: "Send and track email via Mailgun transactional API",
                        RepoURL: "https://github.com/mailgun/mailgun-mcp-server", Transport: "stdio",
                        Command: "npx -y mailgun-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "MAILGUN_API_KEY", Description: "Mailgun API key", Required: true},
                                {Name: "MAILGUN_DOMAIN", Description: "Mailgun sending domain", Required: true},
                        },
                        Tags: []string{"email", "transactional"},
                },
                {
                        Slug: "ms-teams", Name: "Microsoft Teams", Category: "communication",
                        Description: "Send messages, manage channels, schedule meetings in Microsoft Teams",
                        RepoURL: "https://github.com/inditex/mcp-teams-server", Transport: "stdio",
                        Command: "npx -y mcp-teams-server", AuthType: "mcp_oauth",
                        Tags: []string{"chat", "meetings", "microsoft"},
                },
                {
                        Slug: "freshdesk", Name: "Freshdesk", Category: "productivity",
                        Description: "Manage Freshdesk tickets, contacts, and SLAs",
                        RepoURL: "https://github.com/effytech/freshdesk_mcp", Transport: "stdio",
                        Command: "npx -y freshdesk-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "FRESHDESK_DOMAIN", Description: "Freshdesk subdomain", Required: true},
                                {Name: "FRESHDESK_API_KEY", Description: "Freshdesk API key", Required: true},
                        },
                        Tags: []string{"support", "tickets", "helpdesk"},
                },

                // — Browser-use / computer-use additional —
                {
                        Slug: "browser-use", Name: "Browser Use", Category: "browser",
                        Description: "Autonomous browser automation agent — perceive, plan, and act on any web page",
                        RepoURL: "https://github.com/co-browser/browser-use-mcp-server", Transport: "stdio",
                        Command: "npx -y browser-use-mcp-server", AuthType: "none",
                        Tags: []string{"browser", "agent", "automation", "no-auth"},
                },
                {
                        Slug: "hyperbrowser", Name: "Hyperbrowser", Category: "browser",
                        Description: "Cloud-hosted browser with stealth, CAPTCHA solving, session management",
                        RepoURL: "https://github.com/hyperbrowserai/mcp", Transport: "stdio",
                        Command: "npx -y hyperbrowser-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "HYPERBROWSER_API_KEY", Description: "Hyperbrowser API key", Required: true}},
                        Tags:    []string{"browser", "cloud", "captcha"},
                },
                {
                        Slug: "browserbase", Name: "Browserbase", Category: "browser",
                        Description: "Headless browsers for AI agents — stealth, proxies, session replay",
                        RepoURL: "https://github.com/browserbase/mcp-server-browserbase", Transport: "stdio",
                        Command: "npx -y @browserbase/mcp-server-browserbase", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "BROWSERBASE_API_KEY", Description: "Browserbase API key", Required: true},
                                {Name: "BROWSERBASE_PROJECT_ID", Description: "Browserbase project ID", Required: true},
                        },
                        Tags: []string{"browser", "cloud", "stealth"},
                },

                // — Video / creative —
                {
                        Slug: "veo", Name: "Google Veo", Category: "ai",
                        Description: "Generate high-quality video from text via Google Veo (Vertex AI)",
                        RepoURL: "https://github.com/googleapis/genai-toolbox", Transport: "stdio",
                        Command: "npx -y genai-toolbox veo", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "GOOGLE_API_KEY", Description: "Google AI API key", Required: true}},
                        Tags:    []string{"video-gen", "google", "ai"},
                },
                {
                        Slug: "runway", Name: "Runway", Category: "ai",
                        Description: "Generate video and images via Runway Gen-3 / Gen-4",
                        RepoURL: "https://github.com/runwayml/runway-mcp", Transport: "stdio",
                        Command: "npx -y @runwayml/mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "RUNWAYML_API_SECRET", Description: "Runway API secret", Required: true}},
                        Tags:    []string{"video-gen", "image-gen", "creative"},
                },

                // — Shell / system —
                {
                        Slug: "shell", Name: "Shell", Category: "sandbox",
                        Description: "Execute shell commands with safety restrictions. Local only.",
                        RepoURL: "https://github.com/odysseus0/mcp-server-shell", Transport: "stdio",
                        Command: "npx -y mcp-server-shell", AuthType: "none",
                        Tags: []string{"shell", "local", "no-auth"},
                },
                {
                        Slug: "ssh", Name: "SSH", Category: "sandbox",
                        Description: "Execute commands on remote servers via SSH",
                        RepoURL: "https://github.com/tufantunc/ssh-mcp", Transport: "stdio",
                        Command: "npx -y ssh-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SSH_HOST", Description: "SSH host", Required: true},
                                {Name: "SSH_USER", Description: "SSH user", Required: true},
                                {Name: "SSH_PRIVATE_KEY", Description: "SSH private key content", Required: false},
                                {Name: "SSH_PASSWORD", Description: "SSH password", Required: false},
                        },
                        Tags: []string{"ssh", "remote", "devops"},
                },

                // — Dev tools additional —
                {
                        Slug: "sourcegraph", Name: "Sourcegraph", Category: "version_control",
                        Description: "Universal code search across public and private repos",
                        RepoURL: "https://github.com/sourcegraph/sourcegraph-mcp-server", Transport: "stdio",
                        Command: "npx -y sourcegraph-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SRC_ENDPOINT", Description: "Sourcegraph endpoint", Required: true},
                                {Name: "SRC_ACCESS_TOKEN", Description: "Sourcegraph access token", Required: true},
                        },
                        Tags: []string{"code-search", "universal"},
                },
                {
                        Slug: "bitbucket", Name: "Bitbucket", Category: "version_control",
                        Description: "Manage Bitbucket repos, pull requests, pipelines",
                        RepoURL: "https://github.com/MatanYemini/bitbucket-mcp", Transport: "stdio",
                        Command: "npx -y bitbucket-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "BITBUCKET_USERNAME", Description: "Bitbucket username", Required: true},
                                {Name: "BITBUCKET_APP_PASSWORD", Description: "Bitbucket app password", Required: true},
                        },
                        Tags: []string{"git", "pr", "ci"},
                },
                {
                        Slug: "sonarqube", Name: "SonarQube", Category: "monitoring",
                        Description: "Query SonarQube code quality issues, coverage, and security",
                        RepoURL: "https://github.com/sapientpants/sonarqube-mcp-server", Transport: "stdio",
                        Command: "npx -y sonarqube-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "SONARQUBE_URL", Description: "SonarQube URL", Required: true},
                                {Name: "SONARQUBE_TOKEN", Description: "SonarQube token", Required: true},
                        },
                        Tags: []string{"code-quality", "security", "coverage"},
                },
                {
                        Slug: "snyk", Name: "Snyk", Category: "monitoring",
                        Description: "Scan code and dependencies for vulnerabilities via Snyk",
                        RepoURL: "https://github.com/snyk/snyk-ls", Transport: "stdio",
                        Command: "npx -y snyk-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "SNYK_TOKEN", Description: "Snyk auth token", Required: true}},
                        Tags:    []string{"security", "vulnerabilities", "sast"},
                },
                {
                        Slug: "postman", Name: "Postman", Category: "productivity",
                        Description: "Access Postman collections, environments, and monitors",
                        RepoURL: "https://github.com/shannonlal/mcp-postman", Transport: "stdio",
                        Command: "npx -y mcp-postman", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "POSTMAN_API_KEY", Description: "Postman API key", Required: true}},
                        Tags:    []string{"api", "testing"},
                },

                // — Content / CMS —
                {
                        Slug: "wordpress", Name: "WordPress", Category: "productivity",
                        Description: "Manage WordPress posts, pages, media, and users",
                        RepoURL: "https://github.com/Automattic/mcp-wordpress-remote", Transport: "stdio",
                        Command: "npx -y mcp-wordpress-remote", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "WP_URL", Description: "WordPress site URL", Required: true},
                                {Name: "WP_USERNAME", Description: "WordPress username", Required: true},
                                {Name: "WP_APP_PASSWORD", Description: "WordPress application password", Required: true},
                        },
                        Tags: []string{"cms", "blog", "content"},
                },
                {
                        Slug: "ghost", Name: "Ghost", Category: "productivity",
                        Description: "Publish and manage Ghost blog posts, tags, members",
                        RepoURL: "https://github.com/mfukushim/ghost-mcp", Transport: "stdio",
                        Command: "npx -y ghost-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "GHOST_URL", Description: "Ghost blog URL", Required: true},
                                {Name: "GHOST_ADMIN_API_KEY", Description: "Ghost admin API key", Required: true},
                        },
                        Tags: []string{"cms", "blog"},
                },
                {
                        Slug: "contentful", Name: "Contentful", Category: "productivity",
                        Description: "Manage Contentful content types, entries, and assets",
                        RepoURL: "https://github.com/contentful/contentful-mcp", Transport: "stdio",
                        Command: "npx -y @contentful/mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "CONTENTFUL_MANAGEMENT_ACCESS_TOKEN", Description: "Contentful management token", Required: true},
                                {Name: "CONTENTFUL_SPACE_ID", Description: "Contentful space ID", Required: true},
                        },
                        Tags: []string{"cms", "headless", "content"},
                },

                // — Analytics —
                {
                        Slug: "google-analytics", Name: "Google Analytics", Category: "monitoring",
                        Description: "Query Google Analytics 4 reports, events, audiences",
                        RepoURL: "https://github.com/google-analytics/ga-mcp-server", Transport: "stdio",
                        Command: "npx -y ga-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "GOOGLE_APPLICATION_CREDENTIALS", Description: "Path to service account JSON", Required: true},
                                {Name: "GA_PROPERTY_ID", Description: "GA4 property ID", Required: true},
                        },
                        Tags: []string{"analytics", "reporting", "ga4"},
                },
                {
                        Slug: "posthog", Name: "PostHog", Category: "monitoring",
                        Description: "Query PostHog events, insights, feature flags, and session replays",
                        RepoURL: "https://github.com/PostHog/mcp", Transport: "stdio",
                        Command: "npx -y @posthog/mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "POSTHOG_API_KEY", Description: "PostHog personal API key", Required: true}},
                        Tags:    []string{"analytics", "product", "feature-flags"},
                },
                {
                        Slug: "mixpanel", Name: "Mixpanel", Category: "monitoring",
                        Description: "Query Mixpanel events, funnels, and cohorts",
                        RepoURL: "https://github.com/dragonkhoi/mixpanel-mcp", Transport: "stdio",
                        Command: "npx -y mixpanel-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "MIXPANEL_SERVICE_ACCOUNT", Description: "Service account name", Required: true},
                                {Name: "MIXPANEL_SERVICE_ACCOUNT_PASSWORD", Description: "Service account secret", Required: true},
                                {Name: "MIXPANEL_PROJECT_ID", Description: "Mixpanel project ID", Required: true},
                        },
                        Tags: []string{"analytics", "product"},
                },
                {
                        Slug: "amplitude", Name: "Amplitude", Category: "monitoring",
                        Description: "Query Amplitude events, funnels, and user behavior",
                        RepoURL: "https://github.com/tejaswin/amplitude-mcp", Transport: "stdio",
                        Command: "npx -y amplitude-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "AMPLITUDE_API_KEY", Description: "Amplitude API key", Required: true},
                                {Name: "AMPLITUDE_SECRET_KEY", Description: "Amplitude secret key", Required: true},
                        },
                        Tags: []string{"analytics", "product"},
                },

                // — Search (more) —
                {
                        Slug: "serp", Name: "SerpAPI", Category: "search",
                        Description: "Search Google, Bing, DuckDuckGo, Baidu and more via SerpAPI",
                        RepoURL: "https://github.com/ilyazub/serpapi-mcp-server", Transport: "stdio",
                        Command: "npx -y serpapi-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "SERPAPI_KEY", Description: "SerpAPI key", Required: true}},
                        Tags:    []string{"search", "google", "scraping"},
                },
                {
                        Slug: "kagi", Name: "Kagi", Category: "search",
                        Description: "Privacy-first search via Kagi — summarize, enrich, and search the web",
                        RepoURL: "https://github.com/kagisearch/kagimcp", Transport: "stdio",
                        Command: "npx -y kagi-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "KAGI_API_KEY", Description: "Kagi API key", Required: true}},
                        Tags:    []string{"search", "privacy", "ai"},
                },

                // — Utility / misc —
                {
                        Slug: "qrcode", Name: "QR Code", Category: "utility",
                        Description: "Generate QR codes from text or URLs. No API keys.",
                        RepoURL: "https://github.com/jwinkler2083233/qrcode-mcp", Transport: "stdio",
                        Command: "npx -y qrcode-mcp", AuthType: "none",
                        Tags: []string{"qr", "generator", "no-auth"},
                },
                {
                        Slug: "unsplash", Name: "Unsplash", Category: "search",
                        Description: "Search free, high-resolution photos from Unsplash",
                        RepoURL: "https://github.com/hellokaton/unsplash-mcp-server", Transport: "stdio",
                        Command: "npx -y unsplash-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "UNSPLASH_ACCESS_KEY", Description: "Unsplash access key", Required: true}},
                        Tags:    []string{"images", "photos", "free"},
                },
                {
                        Slug: "giphy", Name: "Giphy", Category: "search",
                        Description: "Search and fetch GIFs from Giphy",
                        RepoURL: "https://github.com/gilbertcd/giphy-mcp", Transport: "stdio",
                        Command: "npx -y giphy-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "GIPHY_API_KEY", Description: "Giphy API key", Required: true}},
                        Tags:    []string{"gifs", "media"},
                },
                {
                        Slug: "currency", Name: "Currency Exchange", Category: "finance",
                        Description: "Convert between currencies using live exchange rates. No API keys.",
                        RepoURL: "https://github.com/kazuph/mcp-currency", Transport: "stdio",
                        Command: "npx -y mcp-currency", AuthType: "none",
                        Tags: []string{"finance", "forex", "no-auth"},
                },
                {
                        Slug: "ipinfo", Name: "IPinfo", Category: "utility",
                        Description: "Look up IP geolocation, ASN, privacy detection",
                        RepoURL: "https://github.com/briandconnelly/mcp-server-ipinfo", Transport: "stdio",
                        Command: "npx -y mcp-server-ipinfo", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "IPINFO_API_TOKEN", Description: "IPinfo API token", Required: false}},
                        Tags:    []string{"ip", "geolocation", "network"},
                },

                // — Recruiting / HR —
                {
                        Slug: "greenhouse", Name: "Greenhouse", Category: "productivity",
                        Description: "Access Greenhouse candidates, jobs, applications, and offers",
                        RepoURL: "https://github.com/kanjigrp/greenhouse-mcp", Transport: "stdio",
                        Command: "npx -y greenhouse-mcp", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "GREENHOUSE_API_KEY", Description: "Greenhouse Harvest API key", Required: true}},
                        Tags:    []string{"hr", "ats", "recruiting"},
                },

                // — Legal / docs —
                {
                        Slug: "docusign", Name: "DocuSign", Category: "productivity",
                        Description: "Send envelopes, collect signatures, and manage DocuSign templates",
                        RepoURL: "https://github.com/mario-andreschak/mcp-docusign", Transport: "stdio",
                        Command: "npx -y mcp-docusign", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "DOCUSIGN_INTEGRATION_KEY", Description: "DocuSign integration key", Required: true},
                                {Name: "DOCUSIGN_USER_ID", Description: "DocuSign user ID", Required: true},
                                {Name: "DOCUSIGN_ACCOUNT_ID", Description: "DocuSign account ID", Required: true},
                                {Name: "DOCUSIGN_RSA_PRIVATE_KEY", Description: "DocuSign RSA private key (PEM)", Required: true},
                        },
                        Tags: []string{"signatures", "legal", "documents"},
                },

                // — Government / data —
                {
                        Slug: "census", Name: "US Census", Category: "search",
                        Description: "Query US Census Bureau demographic data. No API keys (or free optional).",
                        RepoURL: "https://github.com/cgoncalves94/census-mcp", Transport: "stdio",
                        Command: "npx -y census-mcp", AuthType: "none",
                        Tags: []string{"demographics", "government", "no-auth"},
                },

                // — Docker / container registries —
                {
                        Slug: "dockerhub", Name: "Docker Hub", Category: "cloud",
                        Description: "Search Docker Hub images, tags, and manage repositories",
                        RepoURL: "https://github.com/docker/mcp-servers", Transport: "stdio",
                        Command: "npx -y @docker/mcp-server-hub", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "DOCKER_HUB_TOKEN", Description: "Docker Hub access token", Required: false}},
                        Tags:    []string{"containers", "images", "registry"},
                },

                // — Auth / identity —
                {
                        Slug: "auth0", Name: "Auth0", Category: "productivity",
                        Description: "Manage Auth0 tenants, users, roles, and applications",
                        RepoURL: "https://github.com/auth0/auth0-mcp-server", Transport: "stdio",
                        Command: "npx -y @auth0/auth0-mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{
                                {Name: "AUTH0_DOMAIN", Description: "Auth0 tenant domain", Required: true},
                                {Name: "AUTH0_CLIENT_ID", Description: "Auth0 M2M client ID", Required: true},
                                {Name: "AUTH0_CLIENT_SECRET", Description: "Auth0 M2M client secret", Required: true},
                        },
                        Tags: []string{"auth", "identity", "sso"},
                },
                {
                        Slug: "clerk", Name: "Clerk", Category: "productivity",
                        Description: "Manage Clerk users, organizations, sessions, and auth",
                        RepoURL: "https://github.com/clerk/clerk-mcp-server", Transport: "stdio",
                        Command: "npx -y @clerk/mcp-server", AuthType: "env_var",
                        EnvVars: []EnvVar{{Name: "CLERK_SECRET_KEY", Description: "Clerk secret key", Required: true}},
                        Tags:    []string{"auth", "identity", "users"},
                },
        }
}
