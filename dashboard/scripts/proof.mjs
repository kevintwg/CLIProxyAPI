import { randomBytes } from "node:crypto";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  rmSync,
  openSync,
  closeSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { isAbsolute, join } from "node:path";
import { spawn } from "node:child_process";

const binary = process.env.DASHBOARD_BINARY;
if (!binary || !isAbsolute(binary)) {
  throw new Error(
    "Set DASHBOARD_BINARY to an absolute path to the compiled server.",
  );
}
const port = Number(process.env.DASHBOARD_PROOF_PORT ?? 8318);
if (!Number.isInteger(port) || port < 1024 || port > 65535)
  throw new Error("Invalid proof port");
const directory = mkdtempSync(join(tmpdir(), "relay-proof-"));
const key = randomBytes(24).toString("hex");
mkdirSync(join(directory, "auth"), { mode: 0o700 });
const blockedProxy = "http://127.0.0.1:9";
if (!process.argv.includes("--empty")) {
  for (const provider of ["codex", "claude"]) {
    const fixture = {
      type: provider,
      email: `${randomBytes(8).toString("hex")}@example.invalid`,
      access_token: randomBytes(32).toString("hex"),
      expired: "2099-01-01T00:00:00Z",
      disabled: false,
      proxy_url: blockedProxy,
    };
    writeFileSync(
      join(directory, "auth", `example-${provider}.json`),
      JSON.stringify(fixture, null, 2),
      { mode: 0o600 },
    );
  }
}
const config = {
  "config-version": 8,
  server: { host: "127.0.0.1", port },
  management: {
    "secret-key": key,
    "allow-remote": false,
    "disable-auto-update-panel": true,
  },
  access: { "api-keys": [randomBytes(24).toString("hex")] },
  oauth: { "auth-dir": join(directory, "auth") },
  routing: { strategy: "round-robin" },
  requests: { "proxy-url": blockedProxy },
};
writeFileSync(join(directory, "config.yaml"), JSON.stringify(config, null, 2), {
  mode: 0o600,
});
writeFileSync(
  join(directory, "access.json"),
  JSON.stringify({ key, port, directory }),
  { mode: 0o600 },
);
const args = ["--config", join(directory, "config.yaml"), "--local-model"];
const log = openSync(join(directory, "server.log"), "a", 0o600);
const env = {
  PATH: process.env.PATH,
  HOME: directory,
  HTTP_PROXY: blockedProxy,
  HTTPS_PROXY: blockedProxy,
  ALL_PROXY: blockedProxy,
  NO_PROXY: "127.0.0.1,localhost,::1",
};
const child = spawn(binary, args, {
  cwd: directory,
  env,
  stdio: ["ignore", log, log],
});
console.log(`Isolated proof: http://127.0.0.1:${port}/dashboard/`);
console.log(`Generated access details: ${join(directory, "access.json")}`);
console.log(
  "Only generated file-backed accounts are loaded. Tokens expire in 2099 and have no refresh tokens. Outbound HTTP proxies point at closed loopback port 9. Do not start a real provider sign-in during proof capture.",
);
let stopping = false;
function stop() {
  stopping = true;
  child.kill("SIGTERM");
}
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
child.on("error", (error) => {
  console.error(error.message);
  closeSync(log);
  rmSync(directory, { recursive: true, force: true });
  process.exitCode = 1;
});
child.on("exit", (code) => {
  closeSync(log);
  if (stopping || code === 0)
    rmSync(directory, { recursive: true, force: true });
  else
    console.error(
      `Proof server failed. Diagnostic files retained in ${directory}`,
    );
  process.exitCode = stopping ? 0 : (code ?? 1);
});
