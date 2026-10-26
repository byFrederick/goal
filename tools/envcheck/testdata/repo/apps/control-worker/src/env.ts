const passed = ['CF_INDIRECT'];
export const settings = (env: Env) => ({ log: env.CF_LOG, agent: env.AGENT_DO, rest: passed.map((name) => env[name]) });
