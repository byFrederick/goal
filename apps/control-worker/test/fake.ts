// A stand-in for ctx.container in the Workers runtime under Vitest, where no container runs. It
// records what the Worker asks of it and plays a few commands, the way a box with GNU timeout
// would: output, exit codes, a long command that ends when killed, and the base file a box's
// entrypoint writes. Real containers are sandboxtest's (internal/adapters/sandboxes/cloudflare).

/** What a command's stand-in gets: its argv (without timeout's wrapper), env and cwd. */
export interface Call {
  argv: string[];
  env: Record<string, string>;
  cwd: string | undefined;
}

/** Every stand-in container, by its Durable Object's id. */
export const fakes = new Map<string, FakeContainer>();

export function fakeFor(id: DurableObjectId): FakeContainer {
  const key = id.toString();
  let fake = fakes.get(key);
  if (!fake) {
    fake = new FakeContainer();
    fakes.set(key, fake);
  }
  return fake;
}

/** The base file a fake box's entrypoint writes: the image's working directory and env. */
export const imageBase = '/workspace\nHOME=/home/node\0PATH=/usr/bin:/bin\0IMAGE_ONLY=image\0';

export class FakeContainer {
  running = false;
  images: Record<string, string> = { 'goal-agent': 'registry.example.com/goal-agent@sha256:0' };
  started: ContainerStartupOptions[] = [];
  calls: Call[] = [];
  intercepts: string[] = [];
  idleMs: number | bigint | undefined;
  destroyed = 0;
  /** port answers getTcpPort's fetches. */
  port: (port: number, request: Request) => Response | Promise<Response> = () => new Response('ok');
  private readonly live = new Set<() => void>();

  start(options?: ContainerStartupOptions): void {
    if (this.running) throw new Error('already running');
    this.started.push(options ?? { enableInternet: false });
    this.running = true;
  }

  async destroy(): Promise<void> {
    this.running = false;
    this.destroyed++;
    for (const end of this.live) end();
  }

  async setInactivityTimeout(ms: number | bigint): Promise<void> {
    this.idleMs = ms;
  }

  async interceptOutboundHttp(addr: string): Promise<void> {
    this.intercepts.push(addr);
  }

  getTcpPort(port: number): Fetcher {
    return {
      fetch: async (input: RequestInfo, init?: RequestInit) => {
        const request = new Request(input, init);
        // As Cloudflare does: a container's port is reached over HTTP only.
        if (new URL(request.url).protocol !== 'http:') {
          throw new Error('Connecting to a container using HTTPS is not currently supported; use HTTP instead.');
        }
        return this.port(port, request);
      },
    } as Fetcher;
  }

  async exec(cmd: string[], options?: ContainerExecOptions): Promise<ExecProcess> {
    if (!this.running) throw new Error('exec() cannot be called on a container that is not running');
    let argv = cmd;
    if (cmd[0] === 'timeout') {
      if (cmd[1] !== '--kill-after=10' || !/^\d+$/.test(cmd[2] ?? '')) throw new Error(`unexpected wrapper ${cmd}`);
      argv = cmd.slice(3);
    }
    this.calls.push({ argv, env: { ...options?.env }, cwd: options?.cwd });
    return this.play(argv, cmd[0] === 'timeout' ? Number(cmd[2]) : 0, options?.env ?? {});
  }

  /** play answers a command: cat of the base file, echo, fail, env, long, sleep. */
  private play(argv: string[], timeout: number, env: Record<string, string>): ExecProcess {
    const out = new TransformStream<Uint8Array, Uint8Array>();
    const err = new TransformStream<Uint8Array, Uint8Array>();
    const o = out.writable.getWriter();
    const e = err.writable.getWriter();
    const enc = new TextEncoder();
    let exit: (code: number) => void = () => {};
    const exitCode = new Promise<number>((resolve) => {
      exit = resolve;
    });
    let ended = false;
    const finish = async (code: number) => {
      if (ended) return;
      ended = true;
      this.live.delete(gone);
      await Promise.all([o.close(), e.close()]).catch(() => {});
      exit(code);
    };
    const gone = () => {
      void o.abort(new Error('container destroyed')).catch(() => {});
      void e.abort(new Error('container destroyed')).catch(() => {});
    };
    this.live.add(gone);

    const [name, ...args] = argv;
    void (async () => {
      switch (name) {
        case 'cat': // read with output() below
          return finish(0);
        case 'echo':
          await o.write(enc.encode(`${args.join(' ')}\n`));
          return finish(0);
        case 'fail':
          await o.write(enc.encode('out\n'));
          await e.write(enc.encode('oops\n'));
          return finish(Number(args[0] ?? 1));
        case 'env':
          await o.write(enc.encode(`${JSON.stringify(env)}\n`));
          return finish(0);
        case 'long':
          await o.write(enc.encode('first\n'));
          if (timeout > 0) setTimeout(() => void finish(124), 20);
          return;
        default:
          await e.write(enc.encode(`${name}: not found\n`));
          return finish(127);
      }
    })();

    const process: ExecProcess = {
      stdin: null,
      stdout: out.readable,
      stderr: err.readable,
      pid: 1,
      isPty: false,
      exitCode,
      output: () => Promise.reject(new Error('the Worker streams')),
      kill: (signal?: number) => {
        if (ended) throw new Error('internal error: the process exited');
        void finish(128 + (signal ?? 15));
      },
      resize: () => {},
    };
    // cat's output is read with output() by the Worker.
    if (name === 'cat') {
      process.output = async () => ({
        stdout: enc.encode(imageBase).buffer as ArrayBuffer,
        stderr: new ArrayBuffer(0),
        exitCode: 0,
      });
    }
    return process;
  }
}
