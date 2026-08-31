import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const widgetsDir = path.join(frontendDir, "src/widgets");
const outDir = path.join(frontendDir, "dist-widgets");

/**
 * Every directory under src/widgets holding an index.html is a widget. Reading
 * the directory rather than listing names here means adding a widget is one
 * directory and no build edit, and the Go side discovers them the same way — by
 * reading the build output — so the two cannot fall out of step.
 */
const widgets = fs
  .readdirSync(widgetsDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map(({ name }) => name)
  .filter((name) => fs.existsSync(path.join(widgetsDir, name, "index.html")));

if (widgets.length === 0) {
  console.error(`no widgets found in ${widgetsDir}`);
  process.exit(1);
}

// Cleared once, here rather than per build, so a widget that has been deleted
// does not survive in the output as a resource the server would still serve.
fs.rmSync(outDir, { recursive: true, force: true });

for (const widget of widgets) {
  process.stdout.write(`\nbuilding widget: ${widget}\n`);

  const result = spawnSync("npx", ["vite", "build", "--config", "vite.config.widgets.ts"], {
    cwd: frontendDir,
    env: { ...process.env, CETACEAN_WIDGET: widget },
    stdio: "inherit",
  });

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}
