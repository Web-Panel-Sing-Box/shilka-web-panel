import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import * as api from "@/api";
import { getToken } from "@/api/client";
import type { InboundDTO } from "@/api/inbounds";
import type { BulkClientResponse, ClientDTO } from "@/api/clients";
import type { ClientStatus } from "@/api/types";
import type { MetricsDTO, TrafficPoint } from "@/api/dashboard";
import type { LogEntryDTO } from "@/api/logs";
import { getPanelLogs } from "@/api/logs";

// Re-export types for backward compatibility
export type { InboundDTO as Inbound } from "@/api/inbounds";
export type { InboundDTO } from "@/api/inbounds";
export type { ClientDTO as Client } from "@/api/clients";
export type { ClientDTO } from "@/api/clients";
export type { ClientStatus } from "@/api/types";
export type { MetricsDTO as Metrics } from "@/api/dashboard";
export type { TrafficPoint } from "@/api/dashboard";
export type { LogEntryDTO as LogEntry } from "@/api/logs";
export type { LogLevel, LogSource, Protocol, Transmission, TlsMode, Network, Flow, ObfsType, QuicCc } from "@/api/types";
export type { InboundSettings, InboundCreateRequest } from "@/api/inbounds";
export {
  PROTOCOL_OPTIONS,
  TRANSMISSION_OPTIONS,
  TRAFFIC_RESET_OPTIONS,
  NETWORK_OPTIONS,
  FLOW_OPTIONS,
  QUIC_CC_OPTIONS,
  OBFS_OPTIONS,
  DEFAULT_TRANSMISSION,
  DEFAULT_NETWORK,
  DEFAULT_QUIC_CC,
} from "@/api/types";

type StoreState = {
  metrics: MetricsDTO;
  history: TrafficPoint[];
  inbounds: InboundDTO[];
  clients: ClientDTO[];
  logs: LogEntryDTO[];
  paused: boolean;
};

type StoreActions = {
  toggleInbound: (id: string) => Promise<void>;
  addInbound: (input: Omit<InboundDTO, "id" | "createdAt" | "clientCount" | "enabled">) => Promise<void>;
  updateInbound: (id: string, patch: Partial<Omit<InboundDTO, "id" | "createdAt" | "clientCount">>) => Promise<void>;
  removeInbound: (id: string) => Promise<void>;
  cloneInbound: (id: string) => Promise<void>;
  addClient: (input: { nodeId?: string; name: string; inboundIds: string[]; totalQuota: number; expiry: string; startAfterFirstUse?: boolean }) => Promise<void>;
  updateClient: (id: string, patch: Partial<ClientDTO>) => Promise<void>;
  removeClient: (id: string) => Promise<void>;
  resetClientTraffic: (id: string) => Promise<void>;
  setClientStatus: (id: string, status: ClientStatus) => Promise<void>;
  bulkDeleteClients: (ids: string[]) => Promise<BulkClientResponse>;
  bulkResetClientTraffic: (ids: string[]) => Promise<BulkClientResponse>;
  bulkSetClientStatus: (ids: string[], status: Extract<ClientStatus, "active" | "disabled">) => Promise<BulkClientResponse>;
  setPaused: (v: boolean) => void;
  appendLog: (level: string, message: string) => void;
  setCoreRunning: (v: boolean) => void;
  startCore: () => Promise<void>;
  stopCore: () => Promise<void>;
};

const emptyMetrics: MetricsDTO = {
  cpu: 0, ram: 0, swap: 0, ramUsedBytes: 0, ramTotalBytes: 0,
  swapUsedBytes: 0, swapTotalBytes: 0, uptimeSeconds: 0,
  uploadBps: 0, downloadBps: 0, todayBytes: 0, monthBytes: 0,
  totalSent: 0, totalReceived: 0, diskSegments: [],
  inboundsActive: 0, totalUsers: 0, onlineNow: 0,
  coreRunning: false, coreVersion: "",
};

const StoreContext = createContext<(StoreState & StoreActions) | null>(null);

type SeedData = {
  inbounds?: InboundDTO[];
  clients?: ClientDTO[];
  metrics?: Partial<MetricsDTO>;
};

export function StoreProvider({ children, seed }: { children: React.ReactNode; seed?: SeedData }) {
  const [metrics, setMetrics] = useState<MetricsDTO>({ ...emptyMetrics, ...seed?.metrics });
  const [history, setHistory] = useState<TrafficPoint[]>([]);
  const [inbounds, setInbounds] = useState<InboundDTO[]>(seed?.inbounds ?? []);
  const [clients, setClients] = useState<ClientDTO[]>(seed?.clients ?? []);
  const [logs, setLogs] = useState<LogEntryDTO[]>([]);
  const [paused, setPaused] = useState(false);

  const loadAll = useCallback(async () => {
    if (!getToken()) return;
    try {
      const [ibList, clList, m] = await Promise.all([
        api.listInbounds(),
        api.listClients(),
        api.getMetrics(),
      ]);
      setInbounds(ibList);
      setClients(clList);
      setMetrics(m);
      setHistory(await api.getTrafficHistory());
      setLogs(await getPanelLogs({ limit: 500 }));
    } catch {
      // backend not yet available; stay silent
    }
  }, []);

  useEffect(() => {
    loadAll();
    const id = setInterval(loadAll, 3000);
    return () => clearInterval(id);
  }, [loadAll]);

  const value = useMemo<StoreState & StoreActions>(() => ({
    metrics, history, inbounds, clients, logs, paused,
    toggleInbound: async (id) => { await api.toggleInbound(id); await loadAll(); },
    addInbound: async (input) => { await api.createInbound(input as any); await loadAll(); },
    updateInbound: async (id, patch) => { await api.updateInbound(id, patch as any); await loadAll(); },
    removeInbound: async (id) => { await api.deleteInbound(id); await loadAll(); },
    cloneInbound: async (id) => { await api.cloneInbound(id); await loadAll(); },
    addClient: async (input) => { await api.createClient(input); await loadAll(); },
    updateClient: async (id, patch) => { await api.updateClient(id, patch); await loadAll(); },
    removeClient: async (id) => { await api.deleteClient(id); await loadAll(); },
    resetClientTraffic: async (id) => { await api.resetClientTraffic(id); await loadAll(); },
    setClientStatus: async (id, status) => { await api.setClientStatus(id, { status }); await loadAll(); },
    bulkDeleteClients: async (ids) => { const result = await api.bulkDeleteClients(ids); await loadAll(); return result; },
    bulkResetClientTraffic: async (ids) => { const result = await api.bulkResetClientTraffic(ids); await loadAll(); return result; },
    bulkSetClientStatus: async (ids, status) => { const result = await api.bulkSetClientStatus(ids, status); await loadAll(); return result; },
    setPaused,
    appendLog: () => {},
    setCoreRunning: (v) => setMetrics(prev => ({ ...prev, coreRunning: v })),
    startCore: async () => { await api.startCore(); await loadAll(); },
    stopCore: async () => { await api.stopCore(); await loadAll(); },
  }), [metrics, history, inbounds, clients, logs, paused, loadAll]);

  return <StoreContext.Provider value={value}>{children}</StoreContext.Provider>;
}

export function useMetrics() {
  const ctx = useContext(StoreContext);
  if (!ctx) return { metrics: emptyMetrics, history: [] as TrafficPoint[] };
  return { metrics: ctx.metrics, history: ctx.history };
}

export function useInbounds() {
  const ctx = useContext(StoreContext);
  if (!ctx) return [] as InboundDTO[];
  return ctx.inbounds;
}

export function useClients() {
  const ctx = useContext(StoreContext);
  if (!ctx) return [] as ClientDTO[];
  return ctx.clients;
}

export function useLogs() {
  const ctx = useContext(StoreContext);
  if (!ctx) return [] as LogEntryDTO[];
  return ctx.logs;
}

export function useRuntime() {
  const ctx = useContext(StoreContext);
  return { paused: ctx?.paused ?? false };
}

export function useStoreActions() {
  const ctx = useContext(StoreContext);
  if (!ctx) {
    const noop = async () => {};
    const noopBulk = async (): Promise<BulkClientResponse> => ({ results: [] });
    return {
      toggleInbound: noop, addInbound: noop, updateInbound: noop,
      removeInbound: noop, cloneInbound: noop, addClient: noop,
      updateClient: noop, removeClient: noop, resetClientTraffic: noop,
      setClientStatus: noop, setPaused: () => {}, appendLog: () => {},
      bulkDeleteClients: noopBulk, bulkResetClientTraffic: noopBulk,
      bulkSetClientStatus: noopBulk,
      setCoreRunning: () => {}, startCore: noop, stopCore: noop,
    } as StoreActions;
  }
  return ctx;
}
