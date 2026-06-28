import { apiGet, apiPost, apiPut, apiDelete } from "./client";
import type { ClientStatus } from "./types";

// ------- DTOs -------

export type ClientDTO = {
  id: string;
  nodeId?: string;
  remoteId?: string;
  name: string;
  uuid: string;
  inboundId: string;
  inboundIds: string[];
  usedDown: number;
  usedUp: number;
  totalQuota: number;
  expiry: string;
  status: ClientStatus;
  subscription: string;
  subToken?: string;
  enabled?: boolean;
  startAfterFirstUse: boolean;
  online: boolean;
};

export type ClientCreateRequest = {
  nodeId?: string;
  name: string;
  inboundIds: string[];
  totalQuota?: number;
  expiry?: string;
  startAfterFirstUse?: boolean;
};

export type ClientUpdateRequest = {
  nodeId?: string;
  name?: string;
  inboundIds?: string[];
  totalQuota?: number;
  expiry?: string;
  status?: ClientStatus;
  startAfterFirstUse?: boolean;
};

export type ClientSetStatusRequest = {
  status: ClientStatus;
};

export type ClientLink = {
  label: string;
  url: string;
  protocol: string;
};

export type ClientLinksDTO = {
  link: string;
  shareLink: string;
  subscription: string;
  links: ClientLink[];
};

type ClientLinksResponse = {
  link?: string;
  shareLink?: string;
  subscription: string;
  links?: ClientLink[];
};

export type MessageResponse = {
  message: string;
};

// ------- API functions -------

export function listClients(inboundId?: string): Promise<ClientDTO[]> {
  const query = inboundId ? `?inboundId=${encodeURIComponent(inboundId)}` : "";
  return apiGet<ClientDTO[]>(`/clients${query}`);
}

export function getClient(id: string): Promise<ClientDTO> {
  return apiGet<ClientDTO>(`/clients/${id}`);
}

export function createClient(body: ClientCreateRequest): Promise<ClientDTO> {
  return apiPost<ClientDTO>("/clients", body);
}

export function updateClient(id: string, body: ClientUpdateRequest): Promise<ClientDTO> {
  return apiPut<ClientDTO>(`/clients/${id}`, body);
}

export function deleteClient(id: string): Promise<MessageResponse> {
  return apiDelete<MessageResponse>(`/clients/${id}`);
}

export function resetClientTraffic(id: string): Promise<ClientDTO> {
  return apiPost<ClientDTO>(`/clients/${id}/reset-traffic`);
}

export function setClientStatus(id: string, body: ClientSetStatusRequest): Promise<ClientDTO> {
  return apiPost<ClientDTO>(`/clients/${id}/status`, body);
}

export async function getClientLinks(id: string): Promise<ClientLinksDTO> {
  const links = await apiGet<ClientLinksResponse>(`/clients/${id}/links`);
  const shareLink = links.shareLink ?? links.link ?? "";
  return {
    link: links.link ?? shareLink,
    shareLink,
    subscription: links.subscription,
    links: links.links ?? [],
  };
}
