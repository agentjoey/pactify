import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { ElementsGallery } from "./components/ElementsGallery";
import "./index.css";

// `?gallery` renders the living element library instead of the app (Phase 0
// design-system review). Everything else is the normal dashboard.
const gallery = new URLSearchParams(window.location.search).has("gallery");

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>{gallery ? <ElementsGallery /> : <App />}</React.StrictMode>,
);
