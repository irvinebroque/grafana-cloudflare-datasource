import { cp, mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";

const manifest = JSON.parse(await readFile("dist/plugin.json", "utf8"));
const name = `${manifest.id}-${manifest.info.version}`;

await mkdir("artifacts", { recursive: true });

await rm(".local/package", { recursive: true, force: true });

await mkdir(".local/package", { recursive: true });

await cp("dist", `.local/package/${manifest.id}`, { recursive: true });

for (const target of [
  "linux_amd64",
  "linux_arm64",
  "darwin_amd64",
  "darwin_arm64",
  "windows_amd64.exe",
]) {
  await stat(`dist/gpx_cloudflare_${target}`);
}

await rm(`artifacts/${name}.zip`, { force: true });

execFileSync("zip", ["-qr", `../../artifacts/${name}.zip`, manifest.id], { cwd: ".local/package" });

const bytes = await readFile(`artifacts/${name}.zip`);

await writeFile(
  `artifacts/${name}.zip.sha256`,
  createHash("sha256").update(bytes).digest("hex") + `  ${name}.zip\n`,
);

console.log(`Packaged artifacts/${name}.zip (${bytes.length} bytes)`);
