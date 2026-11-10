import { execFileSync } from "node:child_process";

const targets =
  process.env.BUILD_ALL === "1"
    ? [
        ["linux", "amd64"],
        ["linux", "arm64"],
        ["darwin", "arm64"],
        ["darwin", "amd64"],
        ["windows", "amd64"],
      ]
    : [
        [
          process.platform === "win32" ? "windows" : process.platform,
          process.arch === "x64" ? "amd64" : process.arch,
        ],
      ];

for (const [os, arch] of targets) {
  const suffix = os === "windows" ? ".exe" : "";
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-ldflags=-s -w",
      "-o",
      `dist/gpx_cloudflare_${os}_${arch}${suffix}`,
      "./pkg",
    ],
    {
      env: { ...process.env, CGO_ENABLED: "0", GOOS: os, GOARCH: arch },
      stdio: "inherit",
    },
  );
}
