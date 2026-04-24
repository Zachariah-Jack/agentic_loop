const test = require("node:test");
const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");

const { createTerminalManager } = require("../electron/terminal-manager");

function fakeSpawnFactory() {
  const children = [];
  const calls = [];
  let nextPID = 4200;

  return {
    children,
    calls,
    spawnImpl: (command, args, options) => {
      calls.push({ command, args, options });
      const child = new EventEmitter();
      child.pid = nextPID++;
      child.stdout = new EventEmitter();
      child.stderr = new EventEmitter();
      child.stdin = {
        writes: [],
        write(value) {
          this.writes.push(value);
        },
      };
      child.kill = () => {
        child.emit("close", 0);
      };

      children.push(child);
      process.nextTick(() => child.emit("spawn"));
      return child;
    },
  };
}

async function flushTicks() {
  await new Promise((resolve) => process.nextTick(resolve));
}

test("terminal manager creates multiple sessions and tracks the active tab", async () => {
  const { children, spawnImpl } = fakeSpawnFactory();
  const manager = createTerminalManager({ spawnImpl, maxBufferChars: 2048 });

  manager.start({ cwd: "D:/Projects/agentic_loop" });
  await flushTicks();
  manager.start({ cwd: "D:/Projects/agentic_loop/console" });
  await flushTicks();

  children[0].stdout.emit("data", "root shell\n");
  children[1].stdout.emit("data", "console shell\n");

  const snapshot = manager.snapshot();
  assert.equal(snapshot.count, 2);
  assert.equal(snapshot.active_session_id, "terminal_2");
  assert.equal(snapshot.active_session.label, "PowerShell 2");
  assert.match(snapshot.active_session.buffered_output, /console shell/);
  assert.equal(snapshot.sessions[0].label, "PowerShell 1");
});

test("terminal manager hides spawned shell windows on Windows-friendly launches", async () => {
  const { calls, spawnImpl } = fakeSpawnFactory();
  const manager = createTerminalManager({ spawnImpl, maxBufferChars: 2048 });

  manager.start({ cwd: "D:/Projects/agentic_loop" });
  await flushTicks();

  assert.equal(calls.length, 1);
  assert.equal(calls[0].options.windowsHide, true);
});

test("terminal manager sends input to the active or explicitly selected session", async () => {
  const { children, spawnImpl } = fakeSpawnFactory();
  const manager = createTerminalManager({ spawnImpl, maxBufferChars: 1024 });

  manager.start({ cwd: "D:/Projects/agentic_loop" });
  manager.start({ cwd: "D:/Projects/agentic_loop/console" });
  await flushTicks();

  manager.activate("terminal_1");
  manager.send("dir\n");
  manager.send("pwd\n", "terminal_2");

  assert.deepEqual(children[0].stdin.writes, ["dir\n"]);
  assert.deepEqual(children[1].stdin.writes, ["pwd\n"]);
});

test("terminal manager can clear, stop, and close individual sessions", async () => {
  const { children, spawnImpl } = fakeSpawnFactory();
  const manager = createTerminalManager({ spawnImpl, maxBufferChars: 1024 });

  manager.start({ cwd: "D:/Projects/agentic_loop" });
  manager.start({ cwd: "D:/Projects/agentic_loop/console" });
  await flushTicks();

  children[0].stdout.emit("data", "alpha\n");
  children[1].stdout.emit("data", "beta\n");
  manager.clear("terminal_1");
  assert.equal(manager.snapshot().sessions[0].buffered_output, "");

  manager.stop("terminal_2");
  await flushTicks();
  const stopped = manager.snapshot().sessions.find((session) => session.session_id === "terminal_2");
  assert.equal(stopped.status, "stopped");
  assert.equal(stopped.exit_code, 0);

  manager.close("terminal_1");
  const snapshot = manager.snapshot();
  assert.equal(snapshot.count, 1);
  assert.equal(snapshot.active_session_id, "terminal_2");
});
