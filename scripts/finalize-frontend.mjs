import { transformAsync } from "@babel/core";
import { cp, copyFile, mkdir, readFile, writeFile } from "node:fs/promises";

const source = await readFile("dist/module.js", "utf8");

const result = await transformAsync(source, {
  filename: "module.js",
  plugins: ["@babel/plugin-transform-modules-systemjs"],
  sourceMaps: true,
  inputSourceMap: JSON.parse(await readFile("dist/module.js.map", "utf8")),
});

if (!result?.code || !result.map) throw new Error("SystemJS compilation produced no output");

await writeFile("dist/module.js", result.code + "\n//# sourceMappingURL=module.js.map\n");

await writeFile("dist/module.js.map", JSON.stringify(result.map));

await mkdir("dist/img", { recursive: true });

await copyFile("src/plugin.json", "dist/plugin.json");

await copyFile("src/img/logo.svg", "dist/img/logo.svg");

await copyFile("README.md", "dist/README.md");

await copyFile("LICENSE", "dist/LICENSE");

await cp("src/dashboards", "dist/dashboards", { recursive: true });

await cp("docs", "dist/docs", { recursive: true });
