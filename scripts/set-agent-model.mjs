const BE = 'https://multica-backend.onrender.com';
const EMAIL = 'admin@aurion.studio';
const AGENT = '5bbcf16e-22d1-4ad8-909d-b3456259b13b';
const MODEL = process.argv[2] || 'anthropic/claude-opus-4.7';

async function jfetch(u, o = {}) {
  const r = await fetch(u, o);
  const t = await r.text();
  try { return { status: r.status, body: JSON.parse(t) }; } catch { return { status: r.status, body: t }; }
}

await jfetch(`${BE}/auth/send-code`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: EMAIL }) });
const login = await jfetch(`${BE}/auth/verify-code`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: EMAIL, code: '888888' }) });
const tok = login.body.token;
console.log('token ok');

const upd = await jfetch(`${BE}/api/v1/agents/${AGENT}`, {
  method: 'PUT',
  headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${tok}`, 'X-Workspace-Slug': 'main' },
  body: JSON.stringify({ model: { id: MODEL, speed: 'standard' } }),
});
console.log('update', upd.status, JSON.stringify(upd.body).slice(0, 400));
