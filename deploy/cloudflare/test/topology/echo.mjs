import { createServer } from 'node:http';

for (const port of [8080, 8081, 8090]) {
  createServer((req, res) => {
    res.setHeader('content-type', 'application/json');
    res.end(
      JSON.stringify({
        port,
        path: req.url,
        host: req.headers.host,
        forwardedFor: req.headers['x-forwarded-for'] ?? null,
        peer: req.socket.remoteAddress,
        env: Object.keys(process.env).sort(),
      }),
    );
  }).listen(port, '0.0.0.0');
}
