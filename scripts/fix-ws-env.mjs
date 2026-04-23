import { execSync } from 'node:child_process';

const tok = 'vca_3bGm0uZl2rwTGxEpXA1JIejPX7OggkYGBO1y5I3pFaaM7BdubJ1mZtGc';
const proj = 'prj_bfdi39k2UJaUeoR5B0r557OVzX8Y';
const team = 'team_C2vaCqL89CLCvwK17DSw6SoY';

// Get current env vars
const list = await fetch(
  `https://api.vercel.com/v10/projects/${proj}/env?teamId=${team}&decrypt=true`,
  { headers: { Authorization: `Bearer ${tok}` } }
).then((r) => r.json());

const wsVar = list.envs.find((e) => e.key === 'NEXT_PUBLIC_WS_URL');
console.log('current NEXT_PUBLIC_WS_URL id:', wsVar?.id, 'value:', wsVar?.value);

// Patch with new value
const patchRes = await fetch(
  `https://api.vercel.com/v10/projects/${proj}/env/${wsVar.id}?teamId=${team}`,
  {
    method: 'PATCH',
    headers: {
      Authorization: `Bearer ${tok}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      value: 'wss://multica-backend.onrender.com/ws',
      target: ['production', 'preview', 'development'],
      type: 'plain',
    }),
  }
);
const patched = await patchRes.json();
console.log('patch status:', patchRes.status, JSON.stringify(patched).slice(0, 300));
