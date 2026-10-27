// The mock's definitions: each seeded factory's published versions and goal's open draft as
// file trees (factory.yaml with comments, agent prompts, a local skill), the template catalog
// (catalog/templates/) and the skills.sh skills catalog (catalog/skills/). Illustrative content
// only: goald reads real definitions from the bucket.
import type { Actor, VersionSource } from '@goal/api-client';

/** A definition's files by path. */
export type Tree = Map<string, Uint8Array>;

export interface MockVersion {
  id: string;
  /** null for a draft. */
  number: number | null;
  revision: number;
  state: 'draft' | 'published' | 'archived';
  message: string;
  source: VersionSource;
  files: Tree;
  /** The published version a draft started from (its id). */
  base: string | null;
  /** The tree a draft without a base started from (a template, clone or import). */
  start: Tree | null;
  created_at: string;
  created_by: Actor;
  updated_at: string;
  updated_by: Actor;
  published_at: string | null;
  published_by: Actor | null;
}

export interface MockDefinition {
  /** Published versions, oldest first. */
  versions: MockVersion[];
  draft: MockVersion | null;
  /** The version that runs (its id); null before the first publish. */
  current: string | null;
}

export interface MockTemplate {
  name: string;
  version: number;
  title: string;
  summary: string;
  description: string;
  connectors: string[];
  changed: string;
  files: Tree;
}

export interface MockSkill {
  owner: string;
  repo: string;
  skill: string;
  commit: string;
  description: string;
  installs: number;
  files: Record<string, string>;
}

export const localUser: Actor = { kind: 'user', id: 'local', name: 'Local user' };

const encoder = new TextEncoder();

export function tree(files: Record<string, string>): Tree {
  return new Map(Object.entries(files).map(([p, text]) => [p, encoder.encode(text)]));
}

// goal: a factory building this platform. v8 runs; the draft of v9 adds a check, raises the
// review's rework limit and asks before frontend-engineer fetches web pages (a warning until
// tool approvals arrive): 3 changes in 2 sections, 1 problem.
const goalYaml = (o: {
  merge: boolean;
  draft: boolean;
}) => `# Goal builds itself: specs, code and reviews for this repository.
apiVersion: goal/v1
kind: Factory
metadata:
  id: 6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60
  name: goal
  title: Goal
  purpose: "Builds the Goal platform from this repository: engine, UI, infrastructure and its own factories."
spec:
  runtime:
    model: claude-sonnet-5-5
    modelConnection: claude
    size: large
    tools: [golangci-lint@2.1, sqlc@1.27]
    repository: { url: "https://github.com/acme/goal", connection: github }
    budget: { weeklyUsd: 1000, perItemUsd: 4, onExhausted: drain }
  connections:
    # pull requests and issues of the repository
    - github
    - playwright
  skills:
    - { name: acme-conventions, source: ./skills/acme-conventions }
  agents:
    - name: spec-writer
      description: Turns an item into a spec with acceptance criteria.
      prompt: ./agents/spec-writer.md
      model: claude-opus-5-5
      skills: [acme-conventions]
    - name: backend-engineer
      description: Builds Go changes from the spec.
      prompt: ./agents/backend-engineer.md
      maxTurns: 60
      skills: [acme-conventions]
      builtins: { read: allowed, edit: allowed, run: { mode: allowed, commands: [go, task, git] } }
    - name: frontend-engineer
      description: Builds web changes from the spec.
      prompt: ./agents/frontend-engineer.md
      skills: [acme-conventions]
      connections: [playwright]
      builtins: { read: allowed, edit: allowed, run: { mode: allowed, commands: [pnpm, git] }, web: ${o.draft ? 'ask' : 'off'} }
    - name: reviewer
      description: Reviews the pull request against the spec.
      prompt: ./agents/reviewer.md
      connections: [github]
  board:
    workTypes: [feature, fix, refactor, infra, design]
    stations:
      - { id: inbox, name: Inbox, kind: queue }
      - { id: spec, name: Spec, kind: agent, agent: spec-writer, wip: 2 }
      - { id: approve-spec, name: Approve spec, kind: human }
      - id: build
        name: Build
        kind: agent
        # the spec's lane picks the engineer
        route: { backend: backend-engineer, frontend: frontend-engineer }
        agents: 2
        wip: 2
        checks: [task check${o.draft ? ', pnpm test' : ''}]
      - { id: review, name: Review, kind: agent, agent: reviewer, wip: 1, gate: { onReject: build, maxRework: ${o.draft ? 2 : 1} } }
      - { id: merge, name: Merge, kind: human${o.merge ? ', action: merge' : ''} }
      - { id: done, name: Done, kind: done }
    flow:
      constraint: build
      andon: { reworkRate: 0.4, window: 10 }
`;

const goalFiles = (o: { merge: boolean; draft: boolean }) =>
  tree({
    'factory.yaml': goalYaml(o),
    'agents/spec-writer.md': `# spec-writer

Read the item and write a spec: the problem, the change, and acceptance criteria a reviewer can check.
Name the lane (backend or frontend) that builds it.
`,
    'agents/backend-engineer.md': `# backend-engineer

Build the change the spec describes in Go. Keep the hexagonal boundaries, add tests that fail without
the change, and run task check before you open the pull request.
`,
    'agents/frontend-engineer.md': `# frontend-engineer

Build the change the spec describes in web/. Follow the design system, keep every action reachable by
keyboard, and run pnpm test before you open the pull request.
`,
    'agents/reviewer.md': `# reviewer

Review the pull request against the spec's acceptance criteria. Approve it, or send it back with the
criteria it misses.
`,
    'skills/acme-conventions/SKILL.md': `---
name: acme-conventions
description: How acme names things, writes commits and structures pull requests.
---

# Acme conventions

- Commit messages: \`<area>: <what>\`.
- One change per pull request.
`,
  });

const sreYaml = `# SRE: triage alerts, investigate, and ask a person before changing anything.
apiVersion: goal/v1
kind: Factory
metadata:
  id: 0b7d9e21-58a4-4f0e-b6c3-2a9d8e7f6a51
  name: sre
  title: SRE
  purpose: Triage, investigate and document production issues and SRE toil.
spec:
  runtime:
    model: claude-sonnet-5-5
    modelConnection: claude
    budget: { weeklyUsd: 400, onExhausted: stop }
  connections: [aws-prod, datadog, pagerduty]
  knowledge: { sources: [handbook] }
  skills:
    - { name: aws-investigation, source: ./skills/aws-investigation }
    - { name: incident-triage, source: ./skills/incident-triage }
    - name: postmortem-writing
      source: ./skills/postmortem-writing
      origin: "skills.sh:acme/agent-skills@postmortem-writing#${'5e'.repeat(20)}"
  agents:
    - name: triager
      description: Sorts new alerts and requests.
      prompt: ./agents/triager.md
      skills: [incident-triage]
    - name: investigator
      description: Gathers evidence from AWS and the repository and proposes a diagnosis.
      prompt: ./agents/investigator.md
      maxTurns: 40
      skills: [aws-investigation, incident-triage, postmortem-writing]
      connections: [aws-prod, datadog, pagerduty]
      # rules from before the built-in settings: read and run, never change anything
      tools:
        allow: [Read, Grep, Glob, Bash, WebFetch]
        deny: ["Bash(aws * delete*)", "Bash(aws * put*)", "Bash(terraform apply*)", "Bash(kubectl delete*)"]
  board:
    workTypes: [incident, change, toil]
    stations:
      - { id: inbox, name: Inbox, kind: queue }
      - { id: triage, name: Triage, kind: agent, agent: triager, wip: 4 }
      - { id: investigate, name: Investigate, kind: agent, agent: investigator, agents: 3, wip: 3 }
      - { id: approve, name: Approve, kind: human }
      - { id: done, name: Done, kind: done }
`;

const sreFiles = () =>
  tree({
    'factory.yaml': sreYaml,
    'agents/triager.md': '# triager\n\nSort the alert: incident, change or toil, and how urgent it is.\n',
    'agents/investigator.md':
      '# investigator\n\nFind the cause. Propose the smallest safe change and how to roll it back.\n',
    'skills/aws-investigation/SKILL.md':
      '---\nname: aws-investigation\ndescription: Read-only playbook for ECS, RDS, ALB, Lambda\n---\n\n# AWS investigation\n\nDescribe before you change: logs, metrics, recent deploys.\n',
    'skills/incident-triage/SKILL.md':
      '---\nname: incident-triage\ndescription: Severity and sizing rules\n---\n\n# Incident triage\n\nSev1 when customers cannot pay; sev2 when they wait.\n',
    'skills/postmortem-writing/SKILL.md':
      '---\nname: postmortem-writing\ndescription: Used when the item becomes a postmortem\n---\n\n# Postmortem writing\n\nTimeline, impact, causes, actions. No blame.\n',
  });

const supportYaml = `apiVersion: goal/v1
kind: Factory
metadata:
  id: 5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01
  name: support
  title: Support
  purpose: Answers questions from the acme shop, citing runbooks.
spec:
  runtime: { modelConnection: claude }
  agents:
    - name: responder
      prompt: ./agents/responder.md
  board:
    workTypes: [question]
    stations:
      - { id: inbox, kind: queue }
      - { id: answer, kind: agent, agent: responder, wip: 2 }
      - { id: approve, kind: human }
      - { id: done, kind: done }
`;

const supportFiles = () =>
  tree({
    'factory.yaml': supportYaml,
    'agents/responder.md': '# responder\n\nAnswer the question and cite the runbook.\n',
  });

const researchYaml = `apiVersion: goal/v1
kind: Factory
metadata:
  id: 9e8d7c6b-5a49-4382-9172-6a5b4c3d2e1f
  name: research
  title: Research
  purpose: Reads papers and writes short notes for the Goal team.
spec:
  agents:
    - name: reader
      prompt: ./agents/reader.md
  board:
    workTypes: [question]
    stations:
      - { id: inbox, kind: queue }
      - { id: read, kind: agent, agent: reader, wip: 1 }
      - { id: done, kind: done }
`;

const researchFiles = () =>
  tree({ 'factory.yaml': researchYaml, 'agents/reader.md': '# reader\n\nRead the paper and write a one-page note.\n' });

interface History {
  /** The version ids (from VersionRef) and when they were published, oldest first. */
  versions: { id: string; message: string; at: string; files: Tree }[];
}

function published(v: History['versions'][number], number: number): MockVersion {
  return {
    id: v.id,
    number,
    revision: 1,
    state: 'published',
    message: v.message,
    source: number === 1 ? { kind: 'legacy' } : { kind: 'edit', version: String(number - 1) },
    files: v.files,
    base: null,
    start: null,
    created_at: v.at,
    created_by: localUser,
    updated_at: v.at,
    updated_by: localUser,
    published_at: v.at,
    published_by: localUser,
  };
}

function definition(history: History): MockDefinition {
  const versions = history.versions.map((v, i) => published(v, i + 1));
  return { versions, draft: null, current: versions.at(-1)?.id ?? null };
}

const day = (d: number, hour = '10:00') => `2026-10-${String(d).padStart(2, '0')}T${hour}:00Z`;

/** Version ids: goal's v8 and draft, sre's v3, support's v1 and research's v2 are the fixtures' VersionRefs. */
const versionId = (prefix: string, n: number) => `019a1c00-0000-7000-8000-${prefix}${n.toString(16).padStart(2, '0')}`;

export function seedDefinitions(): Record<string, MockDefinition> {
  const goalMessages = [
    'Imported from the bucket',
    'Spec and build stations',
    'Review sends work back to build',
    'Frontend lane',
    'Approve the spec before building',
    'Constraint at build',
    'Andon at 40% rework',
    'Merge step on Merge',
  ];
  const goal = definition({
    versions: goalMessages.map((message, i) => ({
      id: i === 7 ? '019a1c00-0000-7000-8000-0000000000d8' : versionId('0000000000', 0xd0 + i),
      message,
      at: day(13 + i, '14:32'),
      files: goalFiles({ merge: i === 7, draft: false }),
    })),
  });
  const base = goal.versions.at(-1) as MockVersion;
  goal.draft = {
    ...base,
    id: '019a1c00-0000-7000-8000-0000000000d9',
    number: null,
    revision: 42,
    state: 'draft',
    message: '',
    source: { kind: 'edit', version: '8' },
    files: goalFiles({ merge: true, draft: true }),
    base: base.id,
    created_at: '2026-10-21T09:12:00Z',
    updated_at: '2026-10-23T14:32:00Z',
    published_at: null,
    published_by: null,
  };

  const sre = definition({
    versions: ['Imported from the bucket', 'Investigators at three', 'Stop when the week is spent'].map(
      (message, i) => ({
        id: i === 2 ? '019a1c00-0000-7000-8000-0000000000e3' : versionId('0000000000', 0xe1 + i),
        message,
        at: day(4 + i * 4),
        files: sreFiles(),
      }),
    ),
  });
  const support = definition({
    versions: [
      {
        id: '019a1c00-0000-7000-8000-0000000000f1',
        message: 'First version',
        at: day(18, '16:00'),
        files: supportFiles(),
      },
    ],
  });
  const research = definition({
    versions: ['First version', 'Notes of one page'].map((message, i) => ({
      id: i === 1 ? '019a1c00-0000-7000-8000-0000000000b1' : versionId('0000000000', 0xb0),
      message,
      at: day(2 + i * 3, '11:00'),
      files: researchFiles(),
    })),
  });

  return {
    '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60': goal,
    '0b7d9e21-58a4-4f0e-b6c3-2a9d8e7f6a51': sre,
    '5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01': support,
    '9e8d7c6b-5a49-4382-9172-6a5b4c3d2e1f': research,
  };
}

// Templates: the catalog's latest versions, "5 in the bucket".

interface TemplateLine {
  id: string;
  name: string;
  kind: 'queue' | 'agent' | 'human' | 'done';
  agent?: string;
  agents?: number;
  wip?: number;
  extra?: string;
}

interface TemplateSpec {
  name: string;
  version: number;
  title: string;
  summary: string;
  description: string;
  purpose: string;
  connectors: string[];
  changed: string;
  workTypes: string[];
  /** The organization connections it uses, by name. */
  connections?: string[];
  agents: { name: string; description: string; prompt: string }[];
  stations: TemplateLine[];
}

function templateYaml(t: TemplateSpec): string {
  const station = (s: TemplateLine) =>
    `      - { ${[
      `id: ${s.id}`,
      `name: ${s.name}`,
      `kind: ${s.kind}`,
      ...(s.agent ? [`agent: ${s.agent}`] : []),
      ...(s.agents ? [`agents: ${s.agents}`] : []),
      ...(s.wip ? [`wip: ${s.wip}`] : []),
      ...(s.extra ? [s.extra] : []),
    ].join(', ')} }`;
  return `# ${t.title} template v${t.version}. Templates never include secrets.
apiVersion: goal/v1
kind: Factory
metadata:
  id: 00000000-0000-4000-8000-000000000000
  name: ${t.name}
  title: ${t.title}
  purpose: ${t.purpose}
spec:
${t.connections ? `  connections:\n${t.connections.map((c) => `    - ${c}`).join('\n')}\n` : ''}  agents:
${t.agents.map((a) => `    - name: ${a.name}\n      description: ${a.description}\n      prompt: ./agents/${a.name}.md`).join('\n')}
  board:
    workTypes: [${t.workTypes.join(', ')}]
    stations:
${t.stations.map(station).join('\n')}
`;
}

function template(t: TemplateSpec): MockTemplate {
  return {
    name: t.name,
    version: t.version,
    title: t.title,
    summary: t.summary,
    description: t.description,
    connectors: t.connectors,
    changed: t.changed,
    files: tree({
      'factory.yaml': templateYaml(t),
      ...Object.fromEntries(t.agents.map((a) => [`agents/${a.name}.md`, `# ${a.name}\n\n${a.prompt}\n`])),
    }),
  };
}

const templateSpecs: TemplateSpec[] = [
  {
    name: 'support-desk',
    version: 1,
    title: 'Support desk',
    summary: 'Drafts replies to tickets, a person sends them',
    description: 'An agent drafts each reply from the runbooks; a person reads it and sends it.',
    purpose: 'Drafts replies to support tickets for a person to send.',
    connectors: [],
    changed: '2026-09-02',
    workTypes: ['ticket'],
    agents: [{ name: 'drafter', description: 'Drafts the reply.', prompt: 'Draft a reply to the ticket.' }],
    stations: [
      { id: 'inbox', name: 'Inbox', kind: 'queue' },
      { id: 'draft', name: 'Draft', kind: 'agent', agent: 'drafter', wip: 2 },
      { id: 'send', name: 'Send', kind: 'human' },
      { id: 'done', name: 'Done', kind: 'done' },
    ],
  },
  {
    name: 'cost-review',
    version: 2,
    title: 'Cost review',
    summary: 'Finds cloud spend to cut and proposes changes for a person to approve.',
    description:
      'Analysts read the cost report, a proposer writes the change, a reviewer checks it and you approve the merge.',
    purpose: 'Finds cloud spend to cut and proposes infrastructure changes.',
    connectors: ['aws', 'github'],
    changed: '2026-10-09',
    workTypes: ['cost-review'],
    connections: ['github'],
    agents: [
      { name: 'cost-analyst', description: 'Reads the cost report.', prompt: 'Find the largest avoidable costs.' },
      {
        name: 'proposer',
        description: 'Writes the change.',
        prompt: 'Write the infrastructure change that cuts the cost.',
      },
      {
        name: 'reviewer',
        description: 'Checks the change.',
        prompt: 'Check the change is safe and saves what it says.',
      },
    ],
    stations: [
      { id: 'inbox', name: 'Inbox', kind: 'queue' },
      { id: 'analyze', name: 'Analyze', kind: 'agent', agent: 'cost-analyst', agents: 2, wip: 2 },
      { id: 'propose', name: 'Propose', kind: 'agent', agent: 'proposer', wip: 1 },
      {
        id: 'review',
        name: 'Review',
        kind: 'agent',
        agent: 'reviewer',
        wip: 1,
        extra: 'gate: { onReject: propose, maxRework: 2 }',
      },
      { id: 'approve', name: 'Approve', kind: 'human' },
      { id: 'done', name: 'Done', kind: 'done' },
    ],
  },
  {
    name: 'code-review',
    version: 1,
    title: 'Code review',
    summary: 'Reviews pull requests and asks a person to merge.',
    description: 'A reviewer reads each pull request, then you decide.',
    purpose: 'Reviews pull requests against the team conventions.',
    connectors: ['github'],
    changed: '2026-09-20',
    workTypes: ['review'],
    agents: [{ name: 'reviewer', description: 'Reviews the change.', prompt: 'Review the pull request.' }],
    stations: [
      { id: 'inbox', name: 'Inbox', kind: 'queue' },
      { id: 'review', name: 'Review', kind: 'agent', agent: 'reviewer', wip: 2 },
      { id: 'approve', name: 'Approve', kind: 'human' },
      { id: 'done', name: 'Done', kind: 'done' },
    ],
  },
  {
    name: 'docs',
    version: 1,
    title: 'Docs',
    summary: 'Writes and updates documentation pages.',
    description: 'A writer drafts the page and an editor tightens it.',
    purpose: 'Keeps the documentation current.',
    connectors: [],
    changed: '2026-09-28',
    workTypes: ['page'],
    agents: [
      { name: 'writer', description: 'Drafts the page.', prompt: 'Write the page.' },
      { name: 'editor', description: 'Edits the draft.', prompt: 'Tighten the draft.' },
    ],
    stations: [
      { id: 'inbox', name: 'Inbox', kind: 'queue' },
      { id: 'write', name: 'Write', kind: 'agent', agent: 'writer', wip: 2 },
      { id: 'edit', name: 'Edit', kind: 'agent', agent: 'editor', wip: 1 },
      { id: 'done', name: 'Done', kind: 'done' },
    ],
  },
  {
    name: 'incident-triage',
    version: 1,
    title: 'Incident triage',
    summary: 'Sorts alerts and investigates the urgent ones.',
    description: 'A triager sorts each alert; an investigator digs into the urgent ones before a person decides.',
    purpose: 'Triages alerts and investigates incidents.',
    connectors: [],
    changed: '2026-10-01',
    workTypes: ['incident', 'toil'],
    agents: [
      { name: 'triager', description: 'Sorts alerts.', prompt: 'Sort the alert.' },
      { name: 'investigator', description: 'Finds the cause.', prompt: 'Find the cause.' },
    ],
    stations: [
      { id: 'inbox', name: 'Inbox', kind: 'queue' },
      { id: 'triage', name: 'Triage', kind: 'agent', agent: 'triager', wip: 4 },
      { id: 'investigate', name: 'Investigate', kind: 'agent', agent: 'investigator', wip: 2 },
      { id: 'approve', name: 'Approve', kind: 'human' },
      { id: 'done', name: 'Done', kind: 'done' },
    ],
  },
];

export const seedTemplates = (): MockTemplate[] => templateSpecs.map(template);

// The skills.sh catalog, as `goal catalog skills sync` left it.

const skill = (name: string, description: string, body: string) => `---
name: ${name}
description: ${description}
---

# ${name}

${body}
`;

export function seedSkills(): MockSkill[] {
  return [
    {
      owner: 'acme',
      repo: 'agent-skills',
      skill: 'terraform-style-guide',
      commit: '3f1c2a9e8b7d6c5f4e3d2c1b0a9f8e7d6c5b4a39',
      description: 'Write Terraform the way the style guide asks: files, names, variables and modules.',
      installs: 4210,
      files: {
        'SKILL.md': skill(
          'terraform-style-guide',
          'Write Terraform the way the style guide asks.',
          'Name resources in snake_case. One module per folder.',
        ),
        'references/naming.md': '# Naming\n\nResources: snake_case, no type in the name.\n',
      },
    },
    {
      owner: 'acme',
      repo: 'agent-skills',
      skill: 'aws-cost-explorer',
      commit: '9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b',
      description: 'Read AWS Cost Explorer reports and find the largest avoidable costs.',
      installs: 1830,
      files: {
        'SKILL.md': skill(
          'aws-cost-explorer',
          'Read AWS Cost Explorer reports.',
          'Group by service, then by usage type.',
        ),
      },
    },
    {
      owner: 'example-labs',
      repo: 'skills',
      skill: 'paper-notes',
      commit: '0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c',
      description: 'Summarize a paper in one page with its claims and evidence.',
      installs: 312,
      files: { 'SKILL.md': skill('paper-notes', 'Summarize a paper in one page.', 'Claims first, then evidence.') },
    },
  ];
}
