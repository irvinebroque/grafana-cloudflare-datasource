import { execFileSync, spawn } from "node:child_process";
import { access, mkdir, readFile, realpath, symlink, writeFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { homedir } from "node:os";

const root = process.cwd();
const home = resolve(process.env.GRAFANA_HOME ?? ".local/grafana");
const runtime = resolve(".local/runtime");
const accountId = process.env.CLOUDFLARE_ACCOUNT_ID;
let token = process.env.CLOUDFLARE_API_TOKEN;

if (!token && process.argv.includes("--cf-auth")) {
  execFileSync("cf", ["auth", "whoami"], { stdio: "ignore" });

  const config =
    process.env.CLOUDFLARE_AUTH_CONFIG ??
    join(
      homedir(),
      process.platform === "darwin"
        ? "Library/Preferences/cloudflare/config/default.json"
        : ".config/cloudflare/config/default.json",
    );

  token = JSON.parse(await readFile(config, "utf8")).oauth_token;
}

if (!token || !accountId)
  throw new Error(
    "Set CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID. For local validation, --cf-auth can use the existing cf OAuth credential.",
  );

await access(join(home, "bin", process.platform === "win32" ? "grafana.exe" : "grafana"));

await access("dist/plugin.json");

for (const path of [
  "data",
  "logs",
  "plugins",
  "provisioning/datasources",
  "provisioning/dashboards",
])
  await mkdir(join(runtime, path), { recursive: true });

const pluginPath = join(runtime, "plugins/cloudflare-cloudflare-datasource");

try {
  await access(pluginPath);
} catch {
  await symlink(resolve("dist"), pluginPath, "dir");
}

if ((await realpath(pluginPath)) !== resolve("dist"))
  throw new Error("Local plugin path points to a different checkout");

const provisioning = {
  apiVersion: 1,
  datasources: [
    {
      name: "Cloudflare",
      type: "cloudflare-cloudflare-datasource",
      uid: "cloudflare-live",
      access: "proxy",
      isDefault: true,
      jsonData: {
        accountId,
        zoneId: process.env.CLOUDFLARE_ZONE_ID ?? "",
        timeoutSeconds: 60,
        maxRows: 10000,
      },
      secureJsonData: { apiToken: token },
    },
  ],
};

await writeFile(
  join(runtime, "provisioning/datasources/cloudflare.yaml"),
  JSON.stringify(provisioning),
  { mode: 0o600 },
);

const ini = `[server]\nhttp_addr = 127.0.0.1\nhttp_port = 3100\nroot_url = http://localhost:3100/\n[paths]\ndata = ${join(runtime, "data")}\nlogs = ${join(runtime, "logs")}\nplugins = ${join(runtime, "plugins")}\nprovisioning = ${join(runtime, "provisioning")}\n[plugins]\nallow_loading_unsigned_plugins = cloudflare-cloudflare-datasource\npreinstall_disabled = true\n[analytics]\nreporting_enabled = false\ncheck_for_updates = false\n[auth.anonymous]\nenabled = true\norg_role = Admin\n[security]\nadmin_user = admin\nadmin_password = cloudflare-local\n`;

await writeFile(join(runtime, "grafana.ini"), ini);

const child = spawn(
  join(home, "bin", process.platform === "win32" ? "grafana.exe" : "grafana"),
  ["server", "--homepath", home, "--config", join(runtime, "grafana.ini")],
  { cwd: root, stdio: "inherit" },
);

for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => child.kill(signal));

child.on("exit", (code) => {
  process.exitCode = code ?? 1;
});
