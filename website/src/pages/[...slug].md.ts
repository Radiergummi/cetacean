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
  let content: string;
  try {
    content = await readFile(join(docsDir, `${props.id}.mdx`), "utf-8");
  } catch {
    content = await readFile(join(docsDir, `${props.id}.md`), "utf-8");
  }
  return new Response(content, {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
}
