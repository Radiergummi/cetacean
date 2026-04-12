import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { docsDir, getDocPaths } from "../lib/docs";

export async function getStaticPaths() {
  const docs = await getDocPaths();
  return docs.map((doc) => ({
    params: { slug: doc.id },
    props: { id: doc.id },
  }));
}

export async function GET({ props }: { props: { id: string } }) {
  for (const ext of ["mdx", "md"]) {
    try {
      const content = await readFile(join(docsDir, `${props.id}.${ext}`), "utf-8");
      return new Response(content, {
        headers: { "Content-Type": "text/markdown; charset=utf-8" },
      });
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "ENOENT") throw err;
    }
  }
  return new Response("Not found", { status: 404 });
}
