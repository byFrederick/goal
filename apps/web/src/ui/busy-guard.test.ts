import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// A Button whose click waits on the server takes `busy` and `busyLabel`, never only `disabled` (MASTER.md, Busy).
// `disabled` may still name a pending request when it locks a control that did not start it (a sibling lock);
// those are listed below with the request that causes the lock.
const srcDir = fileURLToPath(new URL('../', import.meta.url));
const roots = ['features', 'shared', 'app'];

type Exception = { file: string; expression: string; why: string };

const exceptions: Exception[] = [
  // another fix's request is pending
  {
    file: 'features/factory-editor/frame/problem-fixes.tsx',
    expression: 'pending !== null && pending !== f',
    why: "another fix's request is pending",
  },
  // Remove is locked while Save's request is pending
  {
    file: 'features/factory-settings/people.tsx',
    expression: "pending === 'save'",
    why: "Remove is locked while Save's request is pending",
  },
  // Save is locked while Remove's request is pending
  {
    file: 'features/factory-settings/people.tsx',
    expression: "pending === 'remove'",
    why: "Save is locked while Remove's request is pending",
  },
  // Approve is locked while Reject's request is pending
  {
    file: 'features/floor/needs-you-card.tsx',
    expression: "approved || pending === 'reject'",
    why: "Approve is locked while Reject's request is pending",
  },
  // Deny opens a dialog; pending is Approve's request
  {
    file: 'features/needs-you/detail.tsx',
    expression: 'pending || approved',
    why: "Deny opens a dialog; pending is Approve's request",
  },
  // Save is locked while the clear request is pending
  {
    file: 'features/org-settings/notifications-section.tsx',
    expression: '!value || value === saved || (save.isPending && save.variables === null)',
    why: 'Save is locked while the clear request is pending',
  },
  // Clear is locked while Save's request is pending
  {
    file: 'features/org-settings/notifications-section.tsx',
    expression: 'save.isPending && save.variables !== null',
    why: "Clear is locked while Save's request is pending",
  },
  // Deny is locked while Approve's request is pending
  {
    file: 'features/sign-in/device-page.tsx',
    expression: 'approve.isPending',
    why: "Deny is locked while Approve's request is pending",
  },
  // Approve is locked while Deny's request is pending
  {
    file: 'features/sign-in/device-page.tsx',
    expression: 'deny.isPending',
    why: "Approve is locked while Deny's request is pending",
  },
  // Join is locked while Decline's request is pending
  {
    file: 'features/sign-in/invitation-page.tsx',
    expression: 'decline.isPending',
    why: "Join is locked while Decline's request is pending",
  },
  // Decline is locked while Join's request is pending
  {
    file: 'features/sign-in/invitation-page.tsx',
    expression: 'join.isPending || join.isSuccess',
    why: "Decline is locked while Join's request is pending",
  },
  // Approve is locked while another decision's request is pending
  {
    file: 'shared/item-actions.tsx',
    expression: "mayDecide ? (decide.isPending && pending !== 'approve') || held === 'approve' : undefined",
    why: "Approve is locked while another decision's request is pending",
  },
  // Pause is locked while another decision's request is pending
  {
    file: 'shared/item-actions.tsx',
    expression: "decide.isPending && pending !== 'pause'",
    why: "Pause is locked while another decision's request is pending",
  },
  // Move opens a dialog; pending is another decision's request
  {
    file: 'shared/item-actions.tsx',
    expression: 'decide.isPending',
    why: "Move opens a dialog; pending is another decision's request",
  },
];

const pendingName = /\b(isPending|pending|busy)\b/;

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.tsx$/.test(name) && !/\.test\.tsx$/.test(name) ? [path] : [];
  });
}

function rel(path: string): string {
  return relative(srcDir, path).split('\\').join('/');
}

type Found = { line: number; expression: string };

// Returns the index just past the balanced `{…}` or quoted string that starts at `i`.
function skipGroup(src: string, i: number): number {
  const open = src[i];
  if (open === '"' || open === "'" || open === '`') {
    for (let j = i + 1; j < src.length; j++) {
      if (src[j] === '\\') j++;
      else if (src[j] === open) return j + 1;
    }
    return src.length;
  }
  let depth = 0;
  for (let j = i; j < src.length; j++) {
    const c = src[j];
    if (c === '"' || c === "'" || c === '`') j = skipGroup(src, j) - 1;
    else if (c === '{') depth++;
    else if (c === '}' && --depth === 0) return j + 1;
  }
  return src.length;
}

/** Every `disabled={…}` expression, on a `<Button` tag, that names a pending request. */
function pendingDisabled(src: string): Found[] {
  const found: Found[] = [];
  for (const tag of src.matchAll(/<Button(?=[\s>/])/g)) {
    let i = tag.index + tag[0].length;
    while (i < src.length && src[i] !== '>') {
      const c = src[i] as string;
      if (c === '{' || c === '"' || c === "'") {
        i = skipGroup(src, i);
        continue;
      }
      const attr = /^disabled=\{/.exec(src.slice(i, i + 10));
      if (attr && /\s/.test(src[i - 1] ?? '')) {
        const end = skipGroup(src, i + attr[0].length - 1);
        const expression = src.slice(i + attr[0].length, end - 1).trim();
        if (pendingName.test(expression)) {
          found.push({ line: src.slice(0, i).split('\n').length, expression });
        }
        i = end;
        continue;
      }
      i++;
    }
  }
  return found;
}

describe('busy guard scanner', () => {
  it('flags a multi-line Button whose disabled names a pending request', () => {
    const src = `<Button\n  busy={a.isPending}\n  onClick={() => x > 1}\n  disabled={b.isPending}\n>`;
    expect(pendingDisabled(src)).toEqual([{ line: 4, expression: 'b.isPending' }]);
  });

  it('ignores other components and conditions that are not a pending request', () => {
    expect(pendingDisabled('<MenuItem disabled={x.isPending}>')).toEqual([]);
    expect(pendingDisabled('<Button disabled={!ready}>')).toEqual([]);
  });
});

describe('busy guard', () => {
  const all = roots
    .flatMap((root) => sources(join(srcDir, root)))
    .flatMap((path) => pendingDisabled(readFileSync(path, 'utf8')).map((f) => ({ file: rel(path), ...f })));
  const allowed = (f: { file: string; expression: string }) =>
    exceptions.some((e) => e.file === f.file && e.expression === f.expression);

  it('finds no Button that shows a pending request only as disabled', () => {
    const offenders = all.filter((f) => !allowed(f)).map((f) => `${f.file}:${f.line}: ${f.expression}`);
    expect(offenders).toEqual([]);
  });

  it('keeps the exception list honest: each entry still matches a Button', () => {
    const stale = exceptions.filter((e) => !all.some((f) => f.file === e.file && f.expression === e.expression));
    expect(stale.map((e) => `${e.file}: ${e.expression}`)).toEqual([]);
  });
});
