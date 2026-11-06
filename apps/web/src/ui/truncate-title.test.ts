import { mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

// Text truncates only where the design says so, and a truncated element carries a title with its full
// text (MASTER.md, Components). This scans the JSX opening tags for `truncate` without a `title`.
// Allowed: an `aria-label`, an enclosing <Tooltip>, an `sr-only` element, and the files listed below.
const srcDir = fileURLToPath(new URL('../', import.meta.url));

const shown = 'the full text is shown elsewhere on the row, or the text is short by design';
// Untitled truncate uses that predate the rule: file -> how many tags may stay. A file may not gain one,
// and a count above the measured number fails: lower it, or delete the entry, when a screen gets titles.
const allowed: Record<string, { count: number; reason: string }> = {
  'app/account-menu.tsx': { count: 3, reason: shown },
  'app/org-menu.tsx': { count: 1, reason: shown },
  'app/sidebar-factories.tsx': { count: 3, reason: shown },
  'app/sidebar-search.tsx': { count: 8, reason: shown },
  'app/sidebar.tsx': { count: 4, reason: shown },
  'features/board/board-page.tsx': { count: 1, reason: shown },
  'features/factories/spend-week.tsx': { count: 1, reason: shown },
  'features/factory-editor/agents/agents-section.tsx': { count: 2, reason: shown },
  'features/factory-editor/agents/prompt-editor.tsx': { count: 1, reason: shown },
  'features/factory-editor/frame/wizard-steps.tsx': { count: 1, reason: shown },
  'features/factory-editor/line/station-dnd.tsx': { count: 1, reason: shown },
  'features/factory-editor/line/station-rules.tsx': { count: 1, reason: shown },
  'features/factory-editor/review/changes-card.tsx': { count: 1, reason: shown },
  'features/factory-editor/yaml/file-tree.tsx': { count: 3, reason: shown },
  'features/factory-editor/yaml/panels.tsx': { count: 1, reason: shown },
  'features/factory-editor/yaml/yaml-view.tsx': { count: 1, reason: shown },
  'features/floor/needs-you-card.tsx': { count: 1, reason: shown },
  'features/floor/waits-card.tsx': { count: 1, reason: shown },
  'features/items/add-part-dialog.tsx': { count: 1, reason: shown },
  'features/members/members-page.tsx': { count: 6, reason: shown },
  'features/onboarding/factory-preview.tsx': { count: 1, reason: shown },
  'features/onboarding/first-item-step.tsx': { count: 1, reason: shown },
  'features/sign-in/invitation-page.tsx': { count: 2, reason: shown },
  'features/spend/bar-list.tsx': { count: 1, reason: shown },
  'shared/new-item.tsx': { count: 1, reason: shown },
  'shared/tokens/token-list.tsx': { count: 2, reason: shown },
  'ui/select.tsx': { count: 1, reason: shown },
  'ui/stepper.tsx': { count: 2, reason: shown },
};

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx$/.test(name) && !/\.(test|gen)\.tsx$/.test(name) ? [path] : [];
  });
}

/** The end of the opening tag that starts at `from`: the first `>` outside braces and quotes. */
function tagEnd(text: string, from: number): number {
  let depth = 0;
  let quote = '';
  for (let i = from; i < text.length; i++) {
    const c = text[i] as string;
    if (quote) {
      if (c === '\\' && quote !== '"') i++;
      else if (c === quote) quote = '';
    } else if (c === '"' || c === "'" || (c === '`' && depth > 0)) quote = c;
    else if (c === '{') depth++;
    else if (c === '}') depth--;
    else if (c === '>' && depth === 0 && text[i - 1] !== '=') return i;
  }
  return -1;
}

/** The lines of the opening tags that truncate with no title, no aria-label, no Tooltip around and no sr-only. */
function untitled(text: string): number[] {
  const lines: number[] = [];
  const tag = /<([A-Za-z][\w.]*)(?=[\s/>])/g;
  for (let m = tag.exec(text); m; m = tag.exec(text)) {
    if (/[\w)\]]/.test(text[m.index - 1] ?? '')) continue;
    const end = tagEnd(text, m.index + m[0].length);
    if (end < 0) continue;
    const opening = text.slice(m.index, end + 1);
    if (!/(?<![\w-])truncate(?![\w-])/.test(opening)) continue;
    if (/(?:^|\s)(?:title|aria-label)=/.test(opening) || /(?<![\w-])sr-only(?![\w-])/.test(opening)) continue;
    const before = text.slice(0, m.index);
    const open = (before.match(/<Tooltip[\s>]/g) ?? []).length;
    const closed = (before.match(/<\/Tooltip>/g) ?? []).length;
    if (open > closed) continue;
    lines.push(before.split('\n').length);
  }
  return lines;
}

function scan(dir: string): Record<string, number[]> {
  const found: Record<string, number[]> = {};
  for (const path of sources(dir)) {
    const lines = untitled(readFileSync(path, 'utf8'));
    if (lines.length > 0) found[relative(dir, path).split('\\').join('/')] = lines;
  }
  return found;
}

/** What breaks the rule in `found`, by file and line. */
function violations(found: Record<string, number[]>, list: typeof allowed): string[] {
  return Object.entries(found).flatMap(([file, lines]) => {
    const count = list[file]?.count ?? 0;
    return lines.length > count
      ? [
          `${file}:${lines.join(',')}: className has truncate and no title with the full text (${lines.length} found, ${count} allowed)`,
        ]
      : [];
  });
}

/** Allowlist entries that allow more than the file has. */
function slack(found: Record<string, number[]>, list: typeof allowed): string[] {
  return Object.entries(list).flatMap(([file, { count }]) => {
    const have = found[file]?.length ?? 0;
    return count > have ? [`${file}: allows ${count}, has ${have}`] : [];
  });
}

describe('truncated labels', () => {
  it('carry a title with their full text', () => {
    expect(violations(scan(srcDir), allowed)).toEqual([]);
  });

  it('keeps the allowlist from growing: each entry matches what the file has', () => {
    expect(slack(scan(srcDir), allowed)).toEqual([]);
  });

  it('gives every allowlist entry a reason', () => {
    expect(Object.entries(allowed).filter(([, e]) => e.reason.trim() === '')).toEqual([]);
  });
});

describe('the scan', () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), 'truncate-title-'));
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  function source(path: string, text: string): void {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), text);
  }

  it('fails a truncated tag with no title, naming the file and line', () => {
    source('a.tsx', 'const a = (\n  <span className="truncate">x</span>\n);\n');

    expect(violations(scan(root), {})).toEqual([
      'a.tsx:2: className has truncate and no title with the full text (1 found, 0 allowed)',
    ]);
  });

  it('passes a truncated tag with a title', () => {
    source('a.tsx', '<span className="truncate" title="x">x</span>');

    expect(violations(scan(root), {})).toEqual([]);
  });

  it('finds truncate in a multi-line className={cn(...)} and ignores => inside it', () => {
    source(
      'a.tsx',
      `<a
  onClick={() => go()}
  className={cn(
    'flex',
    current ? 'truncate' : 'x',
  )}
>
  x
</a>`,
    );

    expect(Object.keys(scan(root))).toEqual(['a.tsx']);
  });

  it('passes an aria-label, an sr-only element and a tag inside a Tooltip', () => {
    source('a.tsx', '<span className="truncate" aria-label="x">x</span>');
    source('b.tsx', '<span className="truncate sr-only">x</span>');
    source('c.tsx', '<Tooltip label="x"><button className="truncate">x</button></Tooltip>');

    expect(scan(root)).toEqual({});
  });

  it('does not treat truncate-like class names as truncate', () => {
    source('a.tsx', '<span className="text-truncate-x">x</span>');

    expect(scan(root)).toEqual({});
  });

  it('fails a file over its allowlist count', () => {
    source('a.tsx', '<i className="truncate" />\n<b className="truncate" />');

    expect(violations(scan(root), { 'a.tsx': { count: 1, reason: 'x' } })).toHaveLength(1);
    expect(violations(scan(root), { 'a.tsx': { count: 2, reason: 'x' } })).toEqual([]);
  });

  it('fails an allowlist count above the measured number', () => {
    source('a.tsx', '<i className="truncate" />');

    expect(slack(scan(root), { 'a.tsx': { count: 2, reason: 'x' } })).toEqual(['a.tsx: allows 2, has 1']);
    expect(slack(scan(root), { 'gone.tsx': { count: 1, reason: 'x' } })).toEqual(['gone.tsx: allows 1, has 0']);
  });
});
