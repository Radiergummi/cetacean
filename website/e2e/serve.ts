import { createServer } from "node:http";
import sirv from "sirv";

// `astro preview` detaches when it decides it is not in a terminal, and
// Playwright reads a server that exits as one that failed. `dev` rereads
// dist/ per request, so a rebuild reaches a server that is already up.
const port = Number(process.argv[2]);

createServer(
  sirv(new URL("../dist", import.meta.url).pathname, { dev: true, extensions: ["html"] }),
).listen(port);
