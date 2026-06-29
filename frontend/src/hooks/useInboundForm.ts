import { useCallback, useEffect, useRef, useState } from "react";

import { useToast } from "@/components/ui/toast";
import { listNodes, type NodeDTO } from "@/api";
import { ApiError } from "@/api/client";
import { useI18n } from "@/lib/i18n";
import {
  useStoreActions,
  DEFAULT_NETWORK,
  DEFAULT_QUIC_CC,
  DEFAULT_TRANSMISSION,
} from "@/lib/store";
import { networkFromApi, networkToApi } from "@/api/types";
import type {
  Inbound,
  InboundCreateRequest,
  Network,
  ObfsType,
  Protocol,
  QuicCc,
  TlsMode,
  Transmission,
} from "@/lib/store";
import { randomHex, randomPort } from "@/lib/random";

export type InboundFormMode = "create" | "edit" | "clone";

// Where the cert for a tls-mode inbound comes from. "selfSigned" sends no cert
// material so the backend falls back to a panel-managed self-signed cert (SIN-52).
export type TlsSource = "selfSigned" | "acme" | "custom";

function tlsSourceFromSettings(s: Inbound["settings"]): TlsSource {
  if (s?.acmeDomain) return "acme";
  if (s?.certPath || s?.keyPath) return "custom";
  return "selfSigned";
}

type Params = {
  open: boolean;
  mode: InboundFormMode;
  inbound: Inbound | null | undefined;
  onClose: () => void;
};

// naive + hysteria2 always run over TLS; only vless can be plain / reality.
function tlsForProtocol(protocol: Protocol, current: TlsMode): TlsMode {
  if (protocol === "naive" || protocol === "hysteria2") return "tls";
  return current;
}

export function useInboundForm({ open, mode, inbound, onClose }: Params) {
  const { addInbound, updateInbound, removeInbound } = useStoreActions();
  const { push } = useToast();
  const { t } = useI18n();
  const remarkRef = useRef<HTMLInputElement>(null);

  const [confirmDelete, setConfirmDelete] = useState(false);
  const [nodes, setNodes] = useState<NodeDTO[]>([]);
  const [nodeId, setNodeId] = useState("local");
  const [remark, setRemark] = useState("");
  const [protocol, setProtocolState] = useState<Protocol>("naive");
  const [port, setPort] = useState<number | string>(() => randomPort());
  const [trafficReset, setTrafficReset] = useState("never");

  // vless transport
  const [transmission, setTransmission] = useState<Transmission>(DEFAULT_TRANSMISSION);
  const [wsPath, setWsPath] = useState("");
  const [grpcServiceName, setGrpcServiceName] = useState("");
  const [httpUpgradePath, setHttpUpgradePath] = useState("");
  const [httpUpgradeHost, setHttpUpgradeHost] = useState("");

  const [sniffing, setSniffing] = useState(true);
  const [snifHttp, setSnifHttp] = useState(true);
  const [snifTls, setSnifTls] = useState(true);
  const [snifQuic, setSnifQuic] = useState(false);
  const [snifFakedns, setSnifFakedns] = useState(false);
  const [metadataOnly, setMetadataOnly] = useState(false);
  const [routeOnly, setRouteOnly] = useState(true);
  const [ipsExcluded, setIpsExcluded] = useState("");
  const [domainsExcluded, setDomainsExcluded] = useState("");

  const [tls, setTls] = useState<TlsMode>("none");
  const [dest, setDest] = useState("www.cloudflare.com:443");
  const [sni, setSni] = useState("www.cloudflare.com");
  const [allowInsecure, setAllowInsecure] = useState(true);
  // TLS certificate source (tls mode).
  const [tlsSource, setTlsSource] = useState<TlsSource>("selfSigned");
  const [acmeDomain, setAcmeDomain] = useState("");
  const [acmeEmail, setAcmeEmail] = useState("");
  const [certPath, setCertPath] = useState("");
  const [keyPath, setKeyPath] = useState("");
  const [shortIds, setShortIds] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const [publicKey, setPublicKey] = useState("");

  // naive transport-level
  const [network, setNetwork] = useState<Network>(DEFAULT_NETWORK);
  const [quicCc, setQuicCc] = useState<QuicCc>(DEFAULT_QUIC_CC);

  // hysteria2
  const [obfsType, setObfsType] = useState<ObfsType>("none");
  const [obfsPassword, setObfsPassword] = useState("");
  const [upMbps, setUpMbps] = useState<string>("100");
  const [downMbps, setDownMbps] = useState<string>("100");

  const [diceSpin, setDiceSpin] = useState(0);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    void listNodes()
      .then(setNodes)
      .catch(() => setNodes([]));
    const nextProtocol = inbound?.protocol ?? "naive";
    const s = inbound?.settings;
    setNodeId(mode === "edit" && inbound?.nodeId ? inbound.nodeId : "local");
    setRemark(mode === "clone" && inbound ? `${inbound.remark}-copy` : (inbound?.remark ?? ""));
    setProtocolState(nextProtocol);
    setPort(mode === "clone" ? randomPort() : (inbound?.port ?? randomPort()));
    setTrafficReset("never");
    setTransmission(inbound?.transmission ?? DEFAULT_TRANSMISSION);
    setWsPath(s?.wsPath ?? "");
    setGrpcServiceName(s?.grpcServiceName ?? "");
    setHttpUpgradePath(s?.httpUpgradePath ?? "");
    setHttpUpgradeHost(s?.httpUpgradeHost ?? "");
    setTls(tlsForProtocol(nextProtocol, inbound?.tls ?? "none"));
    setSni(inbound?.sni ?? "www.cloudflare.com");
    setDest(inbound?.dest ?? "www.cloudflare.com:443");
    setAllowInsecure(s?.allowInsecure ?? true);
    setTlsSource(tlsSourceFromSettings(s));
    setAcmeDomain(s?.acmeDomain ?? "");
    setAcmeEmail(s?.acmeEmail ?? "");
    setCertPath(s?.certPath ?? "");
    setKeyPath(s?.keyPath ?? "");
    setNetwork(networkFromApi(s?.naiveNetwork));
    setQuicCc((s?.naiveQuicCongestionCtrl as QuicCc) ?? DEFAULT_QUIC_CC);
    setObfsType(s?.hy2ObfsPassword ? "salamander" : "none");
    setObfsPassword(s?.hy2ObfsPassword ?? randomHex(12));
    setUpMbps(s?.hy2UpMbps != null ? String(s.hy2UpMbps) : "100");
    setDownMbps(s?.hy2DownMbps != null ? String(s.hy2DownMbps) : "100");
    setPrivateKey("");
    setPublicKey(s?.publicKey ?? "");
    setShortIds(s?.shortId ?? "");
    setConfirmDelete(false);
    if (mode === "clone") {
      window.setTimeout(() => {
        remarkRef.current?.focus();
        remarkRef.current?.select();
      }, 50);
    }
  }, [open, mode, inbound]);

  // Switching protocol normalizes invalid combinations (naive/hy2 require TLS).
  const setProtocol = useCallback((next: Protocol) => {
    setProtocolState(next);
    setTls((current) => tlsForProtocol(next, current));
  }, []);

  const randomizePort = useCallback(() => {
    setDiceSpin((v) => v + 360);
    setPort(randomPort());
  }, []);

  const regenerateObfsPassword = useCallback(() => {
    setObfsPassword(randomHex(12));
  }, []);

  const generateKeypair = useCallback(() => {
    setPrivateKey(randomHex(64));
    setPublicKey(randomHex(64));
  }, []);

  // Cert material for tls mode. selfSigned sends nothing → backend self-signed.
  const tlsCertFields = useCallback((): Partial<InboundCreateRequest> => {
    if (tlsSource === "acme") {
      return { acmeDomain: acmeDomain.trim() || undefined, acmeEmail: acmeEmail.trim() || undefined };
    }
    if (tlsSource === "custom") {
      return { certPath: certPath.trim() || undefined, keyPath: keyPath.trim() || undefined };
    }
    return {};
  }, [tlsSource, acmeDomain, acmeEmail, certPath, keyPath]);

  const buildPayload = useCallback((): InboundCreateRequest => {
    const safePort = Number(port) || randomPort();
    const selectedNodeId = mode === "edit" && inbound?.nodeId ? inbound.nodeId : nodeId;
    const base = {
      ...(selectedNodeId !== "local" ? { nodeId: selectedNodeId } : {}),
      remark: remark.trim(),
      protocol,
      port: safePort,
    };
    if (protocol === "vless") {
      return {
        ...base,
        transmission,
        tls,
        sni: tls === "none" ? undefined : sni,
        dest: tls === "reality" ? dest : undefined,
        allowInsecure: tls === "tls" ? allowInsecure : undefined,
        wsPath: transmission === "ws" ? wsPath.trim() || undefined : undefined,
        grpcServiceName: transmission === "grpc" ? grpcServiceName.trim() || undefined : undefined,
        httpUpgradePath: transmission === "httpupgrade" ? httpUpgradePath.trim() || undefined : undefined,
        httpUpgradeHost: transmission === "httpupgrade" ? httpUpgradeHost.trim() || undefined : undefined,
        ...(tls === "tls" ? tlsCertFields() : {}),
      };
    }
    if (protocol === "naive") {
      return {
        ...base,
        tls: "tls",
        sni,
        allowInsecure,
        ...tlsCertFields(),
        naiveNetwork: networkToApi(network),
        naiveQuicCongestionCtrl: quicCc,
      };
    }
    // hysteria2
    return {
      ...base,
      tls: "tls",
      sni,
      allowInsecure,
      ...tlsCertFields(),
      hy2UpMbps: Number(upMbps) || undefined,
      hy2DownMbps: Number(downMbps) || undefined,
      hy2ObfsPassword: obfsType === "salamander" ? obfsPassword : undefined,
    };
  }, [
    remark,
    mode,
    inbound,
    nodeId,
    protocol,
    port,
    transmission,
    wsPath,
    grpcServiceName,
    httpUpgradePath,
    httpUpgradeHost,
    tls,
    sni,
    dest,
    allowInsecure,
    tlsCertFields,
    network,
    quicCc,
    obfsType,
    obfsPassword,
    upMbps,
    downMbps,
  ]);

  const handleSave = useCallback(async () => {
    if (!remark.trim()) {
      push(t("inbounds.remarkRequired"), "error");
      return;
    }
    setSaving(true);
    const payload = buildPayload();
    try {
      if (mode === "edit" && inbound) {
        await updateInbound(inbound.id, payload as never);
        push(t("inbounds.updated"), "success");
      } else {
        await addInbound(payload as never);
        push(mode === "clone" ? t("inbounds.cloned") : t("inbounds.created"), "success");
      }
      onClose();
    } catch (err) {
      const body = err instanceof ApiError ? err.body : null;
      const message =
        body && typeof body === "object" && body !== null && "error" in body
          ? String((body as { error: unknown }).error)
          : t("inbounds.saveFailed");
      push(message, "error");
    } finally {
      setSaving(false);
    }
  }, [remark, buildPayload, mode, inbound, push, t, addInbound, updateInbound, onClose]);

  const handleDelete = useCallback(async () => {
    if (!inbound) return;
    try {
      await removeInbound(inbound.id);
      push(t("inbounds.deleted", { remark: inbound.remark }), "success");
      setConfirmDelete(false);
      onClose();
    } catch (err) {
      const body = err instanceof ApiError ? err.body : null;
      const message =
        body && typeof body === "object" && body !== null && "error" in body
          ? String((body as { error: unknown }).error)
          : t("inbounds.deleteFailed");
      push(message, "error");
    }
  }, [inbound, removeInbound, push, t, onClose]);

  const openConfirmDelete = useCallback(() => setConfirmDelete(true), []);
  const closeConfirmDelete = useCallback(() => setConfirmDelete(false), []);

  return {
    remarkRef,
    confirmDelete,
    openConfirmDelete,
    closeConfirmDelete,
    nodes,
    nodeId, setNodeId,
    remark, setRemark,
    protocol, setProtocol,
    port, setPort,
    trafficReset, setTrafficReset,
    transmission, setTransmission,
    wsPath, setWsPath,
    grpcServiceName, setGrpcServiceName,
    httpUpgradePath, setHttpUpgradePath,
    httpUpgradeHost, setHttpUpgradeHost,
    sniffing, setSniffing,
    snifHttp, setSnifHttp,
    snifTls, setSnifTls,
    snifQuic, setSnifQuic,
    snifFakedns, setSnifFakedns,
    metadataOnly, setMetadataOnly,
    routeOnly, setRouteOnly,
    ipsExcluded, setIpsExcluded,
    domainsExcluded, setDomainsExcluded,
    tls, setTls,
    dest, setDest,
    sni, setSni,
    allowInsecure, setAllowInsecure,
    tlsSource, setTlsSource,
    acmeDomain, setAcmeDomain,
    acmeEmail, setAcmeEmail,
    certPath, setCertPath,
    keyPath, setKeyPath,
    shortIds, setShortIds,
    privateKey, publicKey, generateKeypair,
    network, setNetwork,
    quicCc, setQuicCc,
    obfsType, setObfsType,
    obfsPassword, setObfsPassword, regenerateObfsPassword,
    upMbps, setUpMbps,
    downMbps, setDownMbps,
    diceSpin, randomizePort,
    saving, handleSave, handleDelete,
  };
}
