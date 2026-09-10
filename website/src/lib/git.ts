import { execFile, execFileSync } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

let repoRoot: string | null = null;

async function getRepoRoot(): Promise<string> {
  if (repoRoot) {
    return repoRoot;
  }

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

function gitSync(args: string[], cwd?: string): string {
  return execFileSync("git", args, { cwd, encoding: "utf-8", timeout: 5000 }).trim();
}

/**
 * The same date as `getLastModified`, read synchronously. The sitemap's
 * `serialize` hook runs inside `astro.config.ts`, which has no way to await.
 *
 * Unlike its async twin this does not swallow a failure to run git, so `null`
 * means one thing: no commit in this clone touches the path. The two contracts
 * differ because the callers do — a doc page renders fine with no date beside
 * its heading, whereas the sitemap treats a missing date as a build error and
 * needs to be able to say which of the two happened.
 */
export function getLastModifiedSync(relativePath: string): Date | null {
  const root = gitSync(["rev-parse", "--show-toplevel"]);
  const stdout = gitSync(["log", "-1", "--format=%aI", "--", relativePath], root);

  return stdout ? new Date(stdout) : null;
}

/**
 * Whether this is a shallow clone.
 *
 * Worth asking separately because a shallow clone does not fail the date lookup
 * — it corrupts it silently. The clone holds a single commit with no parent, so
 * git reports every path in the tree as added by it and `git log -1 -- <path>`
 * hands back the tip commit's date for *every* file rather than nothing. The
 * sitemap would then publish one date, the day of the deploy, as the `lastmod`
 * of all sixteen pages, and no missing-date check would ever notice.
 */
export function isShallowSync(): boolean {
  return gitSync(["rev-parse", "--is-shallow-repository"]) === "true";
}
