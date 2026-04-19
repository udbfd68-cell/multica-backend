#!/bin/sh
set -e

echo "Running database migrations..."
./migrate up

# Start the daemon in background if auth token is provided and at least
# one agent CLI is available. The daemon connects to the server running
# in this same container via localhost.
if [ -n "$AURION_AUTH_TOKEN" ]; then
  HAS_AGENT=false
  command -v claude >/dev/null 2>&1 && HAS_AGENT=true
  command -v codex  >/dev/null 2>&1 && HAS_AGENT=true

  if [ "$HAS_AGENT" = "true" ]; then
    echo "Starting agent daemon (will wait for server)..."
    # Run daemon in a subshell that waits for the server to be ready first.
    (
      SERVER_PORT="${PORT:-8080}"
      echo "Daemon: waiting for server on port $SERVER_PORT..."
      for i in $(seq 1 30); do
        if wget -q -O /dev/null "http://localhost:$SERVER_PORT/health" 2>/dev/null; then
          echo "Daemon: server is ready, starting..."
          AURION_SERVER_URL="http://localhost:$SERVER_PORT" \
            exec ./aurion daemon start --foreground
        fi
        sleep 1
      done
      echo "Daemon: server did not become ready in 30s, giving up"
    ) &
    DAEMON_PID=$!
    echo "Daemon launcher started (PID=$DAEMON_PID)"
  else
    echo "WARN: AURION_AUTH_TOKEN set but no agent CLI found (install claude or codex)"
  fi
else
  echo "INFO: Daemon disabled — set AURION_AUTH_TOKEN to enable agent execution"
fi

echo "Starting server..."
exec ./server
