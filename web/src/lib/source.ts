import { LocalServeSource, type DataSource } from "./datasource";

/** The always-available local data source (co-located serve). */
export function localSource(): DataSource {
  return new LocalServeSource();
}
