# --- Build stage ---
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Cache dependencies
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download

# Copy server source
COPY server/ ./server/

# Build binaries
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/server ./cmd/server
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/aurion ./cmd/aurion
RUN cd server && CGO_ENABLED=0 go build -ldflags "-s -w" -o bin/migrate ./cmd/migrate

# --- Runtime stage ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata nodejs npm git \
    chromium chromium-chromedriver nss freetype harfbuzz ttf-freefont font-noto-emoji \
    dbus-x11 xvfb ffmpeg

# Install Claude Code CLI for cloud daemon
RUN npm install -g @anthropic-ai/claude-code@latest 2>/dev/null || true

# Pre-install Playwright MCP + tell Playwright to reuse Alpine's system Chromium
# (Playwright's downloaded Chromium builds are glibc-only and don't run on Alpine)
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true \
    CHROME_BIN=/usr/bin/chromium
# Do NOT swallow install failures — if @playwright/mcp can't install, the
# browser-mode agents will silently fall back to text-only, which is the
# opposite of what we want. Fail the build instead.
RUN npm install -g @playwright/mcp@latest && which playwright-mcp

WORKDIR /app

COPY --from=builder /src/server/bin/server .
COPY --from=builder /src/server/bin/aurion .
COPY --from=builder /src/server/bin/migrate .
COPY server/migrations/ ./migrations/
COPY docker/entrypoint.sh .
RUN sed -i 's/\r$//' entrypoint.sh && chmod +x entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["./entrypoint.sh"]
