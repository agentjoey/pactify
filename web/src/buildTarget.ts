// Replaced with true only by vite.panel.config.ts, allowing dead-code removal
// of write actions while tests and the dashboard keep their normal behavior.
export const panelBuild = import.meta.env.PACTIFY_PANEL === "true";
