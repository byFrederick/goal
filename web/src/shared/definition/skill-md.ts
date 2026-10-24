// What a skill says it is for: the `description` of SKILL.md's front matter (the field agents
// read to decide when to use it), else the file's first paragraph. Shown as plain text.

const frontMatter = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/;

function unquote(value: string): string {
  if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) return value.slice(1, -1).replace(/\\"/g, '"');
  if (value.length >= 2 && value.startsWith("'") && value.endsWith("'")) return value.slice(1, -1).replace(/''/g, "'");
  return value;
}

/** The front matter's description: a plain, quoted or block scalar (> or |) on its own lines. */
function described(header: string): string | undefined {
  const lines = header.split(/\r?\n/);
  const at = lines.findIndex((l) => /^description:/.test(l));
  if (at < 0) return undefined;
  const value = (lines[at] ?? '').replace(/^description:\s*/, '').trim();
  if (!/^[>|][+-]?$/.test(value)) return unquote(value) || undefined;
  const block: string[] = [];
  for (const line of lines.slice(at + 1)) {
    if (line.trim() !== '' && !/^\s/.test(line)) break;
    block.push(line.trim());
  }
  return block.filter(Boolean).join(' ') || undefined;
}

/** The first paragraph that is not a heading, on one line. */
function firstParagraph(body: string): string | undefined {
  for (const paragraph of body.split(/\r?\n\s*\r?\n/)) {
    const text = paragraph
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l !== '' && !l.startsWith('#'))
      .join(' ');
    if (text) return text;
  }
  return undefined;
}

export function skillDescription(text: string): string | undefined {
  const match = frontMatter.exec(text);
  if (!match) return firstParagraph(text);
  return described(match[1] ?? '') ?? firstParagraph(text.slice(match[0].length));
}
