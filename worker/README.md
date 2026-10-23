# worker

Agent container image (phase 1): TypeScript on Node 22 with the Claude Agent SDK. Receives one task envelope from goald, runs the agent with its skills, tool policy and a single MCP gateway URL, streams events back, and returns a result that must satisfy the station contract.
