import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

let repoRoot: string | null = null;

async function getRepoRoot(): Promise<string> {
  if (repoRoot) return repoRoot;
  const { stdout } = await execFileAsync("git", ["rev-parse", "--show-toplevel"], {
    encoding: "utf-8",
    timeout: 5000,
  });
  repoRoot = stdout.trim();
  return repoRoot;
}

/**
 * Returns the author date of the last commit that touched `relativePath`
 * (relative to the repo root), or null if git isn't available or the file
 * has no commit history (e.g. untracked).
 */
export async function getLastModified(relativePath: string): Promise<Date | null> {
  try {
    const root = await getRepoRoot();
    const { stdout } = await execFileAsync(
      "git",
      ["log", "-1", "--format=%aI", "--", relativePath],
      { cwd: root, encoding: "utf-8", timeout: 5000 },
    );

    const trimmed = stdout.trim();
    return trimmed ? new Date(trimmed) : null;
  } catch {
    return null;
  }
}
