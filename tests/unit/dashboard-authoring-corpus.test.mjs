import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { copyFileSync, cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

const root = resolve(import.meta.dirname, "../..");

test("generate-dashboard-ir corpus is indexed and valid", () => {
  execFileSync("npm", ["--prefix", "dashboard/site", "run", "validate:corpus"], {
    cwd: root,
    encoding: "utf8",
    stdio: "pipe",
  });
});

test("dashboard lint validates every dashboard.json", () => {
  const campaignJson = JSON.parse(readFileSync(resolve(root, "dashboard/site/package.json"), "utf8"));
  assert.match(campaignJson.scripts.lint, /npm run validate:dashboards/);
  execFileSync("npm", ["--prefix", "dashboard/site", "run", "validate:dashboards"], {
    cwd: root,
    encoding: "utf8",
    stdio: "pipe",
  });
});

test("dashboard lint tolerates directories that vanish during discovery", () => {
  const fixture = mkdtempSync(join(tmpdir(), "cao-dashboard-scan-"));
  const vanish = `
    import fs from "node:fs/promises";
    import { syncBuiltinESMExports } from "node:module";
    const readdir = fs.readdir;
    fs.readdir = (directory, options) => String(directory).endsWith("vanishing")
      ? Promise.reject(Object.assign(new Error("vanished"), { code: "ENOENT" }))
      : readdir(directory, options);
    syncBuiltinESMExports();`;
  try {
    for (const directory of ["vanishing", "stable", "site/cwd"]) mkdirSync(join(fixture, directory), { recursive: true });
    copyFileSync(resolve(root, "dashboard/site/dashboard.json"), join(fixture, "stable/dashboard.json"));
    cpSync(resolve(root, "dashboard/site/dashboard-fragments"), join(fixture, "stable/dashboard-fragments"), { recursive: true });
    const output = execFileSync(
      process.execPath,
      ["--import", `data:text/javascript,${encodeURIComponent(vanish)}`, resolve(root, "dashboard/site/scripts/validate-dashboards.mjs")],
      { cwd: join(fixture, "site/cwd"), encoding: "utf8", stdio: "pipe" },
    );
    assert.match(output, /Validated 1 dashboard\.json file\(s\)\./);
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
