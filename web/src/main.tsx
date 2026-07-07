import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { ElementsGallery } from "./components/ElementsGallery";
import "./index.css";

// `?gallery` renders the living element library (Phase 0 design-system review).
// Everything else is the normal dashboard.
const params = new URLSearchParams(window.location.search);
const root = params.has("gallery") ? <ElementsGallery /> : <App />;

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>{root}</React.StrictMode>,
);
