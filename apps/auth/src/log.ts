// JSON lines on stdout (spec 3 §12): an event name and plain fields. Callers never pass emails,
// codes, tokens, cookies or secrets; errors are reduced to their message.

export type Level = 'debug' | 'info' | 'warn' | 'error';
export type Fields = Record<string, unknown>;

export interface Logger {
  log(level: Level, event: string, fields?: Fields): void;
}

export function jsonLogger(write: (line: string) => void = (l) => process.stdout.write(l)): Logger {
  return {
    log(level, event, fields = {}) {
      const entry: Fields = { time: new Date().toISOString(), level, event };
      for (const [k, v] of Object.entries(fields)) entry[k] = v instanceof Error ? v.message : v;
      write(`${JSON.stringify(entry)}\n`);
    },
  };
}
