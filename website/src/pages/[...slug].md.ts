import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { docsDir, getDocPaths } from "../lib/docs";

export async function getStaticPaths() {
  const docs = await getDocPaths();
  return docs.map((doc) => ({
    params: { slug: doc.id },
    props: { filePath: doc.filePath },
  }));
}

export async function GET({ props }: { props: { filePath?: string } }) {
  const path = props.filePath ?? join(docsDir, "not-found");
  try {
    const content = await readFile(path, "utf-8");
    return new Response(content, {
      headers: { "Content-Type": "text/markdown; charset=utf-8" },
    });
  } catch {
    return new Response("Not found", { status: 404 });
  }
}
