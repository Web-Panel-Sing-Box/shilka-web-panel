import { apiGet, apiPost, apiPut, apiDelete } from "./client";
import type { Protocol, Transmission, TlsMode } from "./types";

// ------- DTOs -------

export type InboundSettings = {
  publicKey?: string;
  shortId?: string;
  flow?: string;
  wsPath?: string;
  grpcServiceName?: string;
  httpUpgradePath?: string;
  httpUpgradeHost?: string;
  multiplexEnabled?: boolean;
  // Hysteria2
  hy2UpMbps?: number;
  hy2DownMbps?: number;
  hy2IgnoreClientBandwidth?: boolean;
  hy2ObfsPassword?: string;
  hy2ObfsMinPacketSize?: number;
  hy2ObfsMaxPacketSize?: number;
  hy2Masquerade?: string;
  hy2Network?: string;
  hy2BrutalDebug?: boolean;
  hy2BbrProfile?: string;
  // Naive
  naiveNetwork?: string;
  naiveQuicCongestionCtrl?: string;
  allowInsecure?: boolean;
  // TLS certificate source (tls mode). Empty acme/cert ⇒ panel self-signed.
  acmeDomain?: string;
  acmeEmail?: string;
  certPath?: string;
  keyPath?: string;
};

export type InboundDTO = {
  id: string;
  nodeId?: string;
  remoteId?: string;
  remark: string;
  protocol: Protocol;
  port: number;
  transmission: Transmission;
  tls: TlsMode;
  sni?: string;
  dest?: string;
  enabled: boolean;
  clientCount: number;
  createdAt: string;
  updatedAt?: string;
  settings?: InboundSettings;
};

export type InboundCreateRequest = {
  nodeId?: string;
  remark: string;
  protocol: Protocol;
  port: number;
  transmission?: Transmission;
  tls?: TlsMode;
  sni?: string;
  dest?: string;
  acmeDomain?: string;
  acmeEmail?: string;
  certPath?: string;
  keyPath?: string;
  multiplexEnabled?: boolean;
  wsPath?: string;
  grpcServiceName?: string;
  httpUpgradePath?: string;
  httpUpgradeHost?: string;
  // Hysteria2
  hy2UpMbps?: number;
  hy2DownMbps?: number;
  hy2IgnoreClientBandwidth?: boolean;
  hy2ObfsPassword?: string;
  hy2ObfsMinPacketSize?: number;
  hy2ObfsMaxPacketSize?: number;
  hy2Masquerade?: string;
  hy2Network?: string;
  hy2BrutalDebug?: boolean;
  hy2BbrProfile?: string;
  // Naive
  naiveNetwork?: string;
  naiveQuicCongestionCtrl?: string;
  allowInsecure?: boolean;
};

export type InboundUpdateRequest = InboundCreateRequest;

export type MessageResponse = {
  message: string;
};

// ------- API functions -------

export function listInbounds(): Promise<InboundDTO[]> {
  return apiGet<InboundDTO[]>("/inbounds");
}

export function getInbound(id: string): Promise<InboundDTO> {
  return apiGet<InboundDTO>(`/inbounds/${id}`);
}

export function createInbound(body: InboundCreateRequest): Promise<InboundDTO> {
  return apiPost<InboundDTO>("/inbounds", body);
}

export function updateInbound(id: string, body: InboundUpdateRequest): Promise<InboundDTO> {
  return apiPut<InboundDTO>(`/inbounds/${id}`, body);
}

export function deleteInbound(id: string): Promise<MessageResponse> {
  return apiDelete<MessageResponse>(`/inbounds/${id}`);
}

export function toggleInbound(id: string): Promise<InboundDTO> {
  return apiPost<InboundDTO>(`/inbounds/${id}/toggle`);
}

export function cloneInbound(id: string): Promise<InboundDTO> {
  return apiPost<InboundDTO>(`/inbounds/${id}/clone`);
}
