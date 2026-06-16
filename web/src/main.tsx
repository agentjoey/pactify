import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { ElementsGallery } from "./components/ElementsGallery";
import { ShellPreview } from "./components/shell/ShellPreview";
import "./index.css";

// `?gallery` renders the living element library (Phase 0 design-system review).
// `?shell` renders the macOS-app shell mock (sidebar + unified toolbar) for
// layout review before it's wired into the real App. Everything else is the
// normal dashboard.
const params = new URLSearchParams(window.location.search);
const root = params.has("gallery") ? <ElementsGallery /> : params.has("shell") ? <ShellPreview /> : <App />;

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>{root}</React.StrictMode>,
);
