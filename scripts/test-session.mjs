const BE = 'https://multica-backend.onrender.com';

async function jfetch(url, opts = {}) {
  const r = await fetch(url, opts);
  const t = await r.text();
  try { return { status: r.status, body: JSON.parse(t) }; } catch { return { status: r.status, body: t }; }
}

const EMAIL = 'admin@aurion.studio';
const send = await jfetch(`${BE}/auth/send-code`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email: EMAIL }),
});
console.log('send-code', send.status, JSON.stringify(send.body));
const login = await jfetch(`${BE}/auth/verify-code`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email: EMAIL, code: '888888' }),
});
console.log('verify', login.status, typeof login.body === 'object' ? Object.keys(login.body) : login.body);
const tok = login.body.token || login.body.access_token;
if (!tok) { console.error('no token', login); process.exit(1); }

const trig = await jfetch(
  `${BE}/api/v1/agents/5bbcf16e-22d1-4ad8-909d-b3456259b13b/trigger`,
  {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${tok}`,
      'X-Workspace-Slug': 'main',
    },
    body: JSON.stringify({
      prompt: 'What is 2+2? Answer in one word.',
      execution_mode: 'routine',
    }),
  }
);
console.log('trigger', trig.status, JSON.stringify(trig.body));
const sid = trig.body.session_id || trig.body.session?.id;
if (!sid) { console.error('no sid'); process.exit(1); }
console.log('sid', sid);

for (let i = 0; i < 12; i++) {
  await new Promise((r) => setTimeout(r, 10000));
  const s = await jfetch(`${BE}/api/v1/sessions/${sid}`, {
    headers: { Authorization: `Bearer ${tok}`, 'X-Workspace-Slug': 'main' },
  });
  const ev = await jfetch(`${BE}/api/v1/sessions/${sid}/events?limit=50`, {
    headers: { Authorization: `Bearer ${tok}`, 'X-Workspace-Slug': 'main' },
  });
  const evs = Array.isArray(ev.body?.events) ? ev.body.events : Array.isArray(ev.body) ? ev.body : [];
  console.log(`[${i}] status=${s.body.status} cost=${s.body.total_cost_usd} tokens_in=${s.body.total_tokens_in} tokens_out=${s.body.total_tokens_out} events=${evs.length}`);
  if (evs.length) {
    const last = evs[evs.length - 1];
    console.log('  last event:', last.event_type, JSON.stringify(last.content || last.payload || '').slice(0, 200));
  }
  if (['completed', 'failed', 'terminated'].includes(s.body.status)) {
    console.log('\nFINAL ALL EVENTS:');
    for (const e of evs) console.log(' -', e.event_type, JSON.stringify(e.content || e.payload || '').slice(0, 300));
    break;
  }
}
