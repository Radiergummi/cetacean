import { TopologyWidget } from "./TopologyWidget";
import { StrictMode } from "react";
import "@/index.css";
import { createRoot } from "react-dom/client";

const container = document.getElementById("root");

if (!container) {
  throw new Error("widget root element is missing");
}

createRoot(container).render(
  <StrictMode>
    <TopologyWidget />
  </StrictMode>,
);
