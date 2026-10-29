// The same API contract as /docs/openapi.json, parsed from contracts/api/openapi.yaml at build time.
import { parse } from 'yaml';
import contract from '../../../../contracts/api/openapi.yaml?raw';

export const prerender = true;

export function GET() {
  return new Response(`${JSON.stringify(parse(contract), null, 2)}\n`, {
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  });
}
