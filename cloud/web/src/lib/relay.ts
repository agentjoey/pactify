// Mission Control's relay connection now lives in the shared, framework-agnostic
// @pactify-apps/relay-client package (reused by the React dashboard's RelaySource).
// This module re-exports it under the historical names so this app is unchanged.
export {
  RelayClient as MissionControlRelay,
  type Project,
  type PactEvent,
  type PactEventBroadcast,
  type PactEventHeader,
} from '@pactify-apps/relay-client'
