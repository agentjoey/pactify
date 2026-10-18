import { pathToFileURL } from "node:url";
import { startMock } from "./mock-server.mjs";
export const startPanelMock = (port) => startMock(port, true);
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const { server, baseURL } = await startPanelMock(
    Number(process.env.PANEL_E2E_PORT || 4176),
  );
  console.log(baseURL);
  for (const signal of ["SIGTERM", "SIGINT"])
    process.on(signal, () => {
      server.closeAllConnections();
      server.close();
    });
}
