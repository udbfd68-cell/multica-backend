// Usage: node get-code.cjs [email]
// Fetches the latest verification code from Render service logs
const https = require('https');

const email = process.argv[2];
if (!email) {
  console.log('Usage: node get-code.cjs <email>');
  console.log('Example: node get-code.cjs user@example.com');
  process.exit(1);
}

const url = new URL('https://api.render.com/v1/logs');
url.searchParams.set('ownerId', 'tea-d752tfs50q8c739rgv20');
url.searchParams.set('resource', 'srv-d7h1pk0sfn5c73e59ldg');
url.searchParams.set('direction', 'backward');
url.searchParams.set('limit', '100');
url.searchParams.set('type', 'app');
url.searchParams.set('text', 'Verification code');

const req = https.request(url, {
  method: 'GET',
  headers: {
    'Authorization': 'Bearer rnd_XkCpESec74QD1YQNV87ZwUehVIA8',
    'Accept': 'application/json',
  },
}, (res) => {
  let body = '';
  res.on('data', (chunk) => body += chunk);
  res.on('end', () => {
    if (res.statusCode !== 200) {
      console.error('API error:', res.statusCode, body);
      process.exit(1);
    }
    const data = JSON.parse(body);
    const codes = data.logs
      .filter(l => l.message.includes(email))
      .map(l => {
        const match = l.message.match(/Verification code for .+: (\d+)/);
        return match ? { code: match[1], time: l.timestamp } : null;
      })
      .filter(Boolean)
      .sort((a, b) => new Date(b.time) - new Date(a.time));

    if (codes.length === 0) {
      console.log(`No verification code found for ${email} in recent logs.`);
      console.log('Make sure you requested a code first on https://aurion-main.vercel.app');
    } else {
      console.log(`Latest verification code for ${email}:`);
      console.log(`  Code: ${codes[0].code}`);
      console.log(`  Time: ${codes[0].time}`);
      if (codes.length > 1) {
        console.log(`\nOlder codes:`);
        codes.slice(1, 5).forEach(c => console.log(`  ${c.code} at ${c.time}`));
      }
    }
  });
});
req.on('error', (e) => console.error('Error:', e.message));
req.end();
