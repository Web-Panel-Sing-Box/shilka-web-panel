import { useMemo } from "react";

import type { Client, ClientStatus } from "@/lib/store";

export type ClientFilterState = {
  query: string;
  inboundId: string;
  nodeId: string;
  status: ClientStatus | "all";
};

export function useClientFilter(
  clients: Client[],
  filter: ClientFilterState,
): Client[] {
  return useMemo(() => {
    const q = filter.query.trim().toLowerCase();
    return clients.filter((c) => {
      if (filter.inboundId !== "all") {
        const ids = c.inboundIds?.length ? c.inboundIds : [c.inboundId];
        if (!ids.includes(filter.inboundId)) return false;
      }
      if (filter.nodeId === "local" && c.nodeId) return false;
      if (filter.nodeId !== "all" && filter.nodeId !== "local" && c.nodeId !== filter.nodeId)
        return false;
      if (filter.status !== "all" && c.status !== filter.status) return false;
      if (!q) return true;
      return (
        c.name.toLowerCase().includes(q) || c.uuid.toLowerCase().includes(q)
      );
    });
  }, [clients, filter]);
}
