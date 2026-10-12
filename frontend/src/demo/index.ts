import { startDemo } from "./worker";

// GitHub Pages answers a deep link with the site's 404 page, which sends it
// here with the route to restore before the router reads the location.
const route = new URLSearchParams(window.location.search).get("route");

if (route?.startsWith("/demo/")) {
  window.history.replaceState(null, "", route);
}

const badge = document.createElement("div");
badge.textContent = "Demo · simulated cluster";
badge.className =
  "pointer-events-none fixed right-3 bottom-3 z-50 rounded-md bg-amber-400 px-2 py-1 text-xs font-semibold text-black shadow";
document.body.append(badge);

startDemo().then(() => import("../main"));
