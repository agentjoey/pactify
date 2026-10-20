import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Panel } from "./Panel";
import "./panel.css";
import { initializeTheme } from "./theme";
import { initializeSession } from "./auth";
initializeTheme();
void initializeSession().then((authenticated) => createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Panel sessionEnded={!authenticated} />
  </StrictMode>,
));
