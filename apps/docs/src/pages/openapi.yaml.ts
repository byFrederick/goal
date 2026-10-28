// The API contract, published as /docs/openapi.yaml. It is read at build time from
// contracts/api/openapi.yaml, the one copy, so nothing here is committed twice.
import contract from '../../../../contracts/api/openapi.yaml?raw';

export const prerender = true;

export function GET() {
  return new Response(contract, { headers: { 'Content-Type': 'application/yaml; charset=utf-8' } });
}
