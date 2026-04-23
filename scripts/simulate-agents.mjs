// E2E simulation harness — creates multiple preconfigured agents, seeds MCP
// registry, then triggers several "real-world" scenarios to prove the full
// agentic stack (browser mode + routine mode + Playwright MCP + sub-agents).
const BE = 'https://multica-backend.onrender.com';
const EMAIL = 'admin@aurion.studio';

async function jfetch(u, o = {}) {
  const r = await fetch(u, o);
  const t = await r.text();
  try { return { status: r.status, body: JSON.parse(t) }; } catch { return { status: r.status, body: t }; }
}

function authHeaders(tok) {
  return { 'Content-Type': 'application/json', Authorization: `Bearer ${tok}`, 'X-Workspace-Slug': 'main' };
}

async function login() {
  await jfetch(`${BE}/auth/send-code`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: EMAIL }) });
  await new Promise(r => setTimeout(r, 2000));
  const v = await jfetch(`${BE}/auth/verify-code`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: EMAIL, code: '888888' }) });
  if (v.status !== 200) throw new Error('login failed: ' + JSON.stringify(v));
  return v.body.token;
}

async function seedRegistry(tok) {
  const r = await jfetch(`${BE}/api/v1/workspaces/main/mcp-registry/seed-all`, { method: 'POST', headers: authHeaders(tok) });
  console.log('seed registry:', r.status, JSON.stringify(r.body).slice(0, 200));
}

async function createAgent(tok, spec) {
  const r = await jfetch(`${BE}/api/v1/agents`, {
    method: 'POST',
    headers: authHeaders(tok),
    body: JSON.stringify(spec),
  });
  console.log(`create ${spec.name}:`, r.status);
  return r.body;
}

async function runSession(tok, agentId, prompt, execMode, label) {
  console.log(`\n━━━ ${label} [${execMode}] ━━━`);
  console.log('> prompt:', prompt.slice(0, 120));
  const trig = await jfetch(`${BE}/api/v1/agents/${agentId}/trigger`, {
    method: 'POST',
    headers: authHeaders(tok),
    body: JSON.stringify({ prompt, execution_mode: execMode }),
  });
  if (trig.status !== 201) { console.log('trigger FAILED', trig.status, trig.body); return; }
  const sid = trig.body.session?.id;
  console.log('  session:', sid);

  for (let i = 0; i < 30; i++) {
    await new Promise(r => setTimeout(r, 6000));
    const s = await jfetch(`${BE}/api/v1/sessions/${sid}`, { headers: authHeaders(tok) });
    const ev = await jfetch(`${BE}/api/v1/sessions/${sid}/events?limit=200`, { headers: authHeaders(tok) });
    const evs = Array.isArray(ev.body?.events) ? ev.body.events : [];
    const toolCalls = evs.filter(e => e.event_type === 'tool_use' || e.event_type === 'tool_call');
    const status = s.body.status;
    process.stdout.write(`\r  [${i}s] status=${status} events=${evs.length} tools=${toolCalls.length}  `);
    if (['completed', 'failed', 'terminated', 'idle'].includes(status) && evs.length > 0) {
      console.log();
      const outputs = evs.filter(e => e.event_type === 'text' || e.event_type === 'message' || e.event_type === 'assistant_text').slice(-3);
      const tools = evs.filter(e => e.event_type === 'tool_use').map(e => {
        try { const c = typeof e.content === 'string' ? JSON.parse(e.content) : e.content; return c.tool || c.name; } catch { return '?'; }
      });
      console.log('  tools used:', tools.length ? tools.join(', ') : '(none)');
      outputs.forEach(e => console.log('  out:', (typeof e.content === 'string' ? e.content : JSON.stringify(e.content)).slice(0, 200)));
      return;
    }
    if (status === 'terminated' && evs.length === 0) {
      console.log('\n  → terminated with 0 events (likely API error, check render logs)');
      return;
    }
  }
  console.log('\n  → timeout waiting for completion');
}

const tok = await login();
console.log('✓ logged in');

await seedRegistry(tok);

// Pre-existing execution agent (web scraper)
const EXEC_AGENT = '5bbcf16e-22d1-4ad8-909d-b3456259b13b';

// Create a few scenario agents
const prospecter = await createAgent(tok, {
  name: 'Email Prospecter',
  description: 'Finds prospects online, drafts personalized outreach emails',
  system_prompt: 'You are an expert B2B email prospecter. Find leads, research their company, craft hyper-personalized outreach.',
  model: { id: 'anthropic/claude-opus-4.7', speed: 'standard' },
  metadata: { execution_mode: 'browser' },
});

const researcher = await createAgent(tok, {
  name: 'Web Researcher',
  description: 'Deep research on any topic by browsing the web',
  system_prompt: 'You are a meticulous research analyst. Use the browser to gather primary sources, cross-check facts, and produce structured briefs.',
  model: { id: 'anthropic/claude-opus-4.7', speed: 'standard' },
  metadata: { execution_mode: 'browser' },
});

const coder = await createAgent(tok, {
  name: 'Routine Coder',
  description: 'Writes/fixes code without opening a browser',
  system_prompt: 'You are a senior Go/TypeScript engineer. Read code with read_file, edit precisely, run tests with bash.',
  model: { id: 'anthropic/claude-opus-4.7', speed: 'standard' },
  metadata: { execution_mode: 'routine' },
});

console.log('\n✓ agents ready\n');

// === Scenarios ===

// 1. Browser mode: navigate, snapshot, extract
await runSession(tok, researcher.id,
  'Open example.com and tell me the exact heading text on the page. Use browser_navigate then browser_snapshot.',
  'browser', '1️⃣  Basic browser navigation');

// 2. Browser mode: real search
await runSession(tok, researcher.id,
  'Go to duckduckgo.com, search for "Anthropic Claude Opus 4.7 release date", and return the top 3 result titles.',
  'browser', '2️⃣  Real Google-style search');

// 3. Routine mode: HTTP only
await runSession(tok, coder.id,
  'Use http_request to GET https://api.github.com/repos/anthropics/anthropic-sdk-typescript and return the star count and description as JSON.',
  'routine', '3️⃣  Routine HTTP API call');

// 4. Email prospecting (simulation — no real send)
await runSession(tok, prospecter.id,
  'Draft a cold email to the CTO of Vercel (Malte Ubl). Research him briefly with web_search, then write a 4-line personalized email pitching Aurion (an AI agent orchestration platform). DO NOT actually send — just output the draft.',
  'hybrid', '4️⃣  Email prospecting draft');

// 5. Code task with file ops
await runSession(tok, coder.id,
  'Write a Python function called fizzbuzz(n) that prints 1..n with standard fizzbuzz rules. Output the final code only, no preamble.',
  'routine', '5️⃣  Simple code generation');

// 6. Use pre-existing execution agent
await runSession(tok, EXEC_AGENT,
  'What tools do you have available? List them briefly.',
  'browser', '6️⃣  Capability introspection');

console.log('\n\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━');
console.log('DONE — all scenarios attempted');
