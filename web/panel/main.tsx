import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Panel } from "./Panel";
import "./panel.css";
import { initializeTheme } from "./theme";
import { initializeSession } from "./auth";
initializeSession();
initializeTheme();
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Panel />
  </StrictMode>,
);
