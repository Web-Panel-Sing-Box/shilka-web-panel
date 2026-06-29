
import { useMemo } from "react";
import { m } from "framer-motion";
import { Copy, Dices, RefreshCw, ShieldCheck, Trash2 } from "lucide-react";

import { Accordion } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Input, Label, NumberInput, Textarea } from "@/components/ui/input";
import { Modal, ModalBody, ModalFooter, ModalHeader } from "@/components/ui/modal";
import { Segmented } from "@/components/ui/tabs";
import { Select } from "@/components/ui/select";
import { Toggle } from "@/components/ui/toggle";
import { useInboundForm, type InboundFormMode, type TlsSource } from "@/hooks/useInboundForm";
import {
  NETWORK_OPTIONS,
  OBFS_OPTIONS,
  PROTOCOL_OPTIONS,
  QUIC_CC_OPTIONS,
  TRAFFIC_RESET_OPTIONS,
  TRANSMISSION_OPTIONS,
  type Inbound,
  type TlsMode
} from "@/lib/store";
import { useI18n } from "@/lib/i18n";

export type { InboundFormMode } from "@/hooks/useInboundForm";

type InboundFormModalProps = {
  open: boolean;
  mode?: InboundFormMode;
  inbound?: Inbound | null;
  onClose: () => void;
  onClone?: (inbound: Inbound) => void;
};

export function InboundFormModal({ open, mode = "create", inbound, onClose, onClone }: InboundFormModalProps) {
  const { t } = useI18n();
  const f = useInboundForm({ open, mode, inbound, onClose });

  const isVless = f.protocol === "vless";
  const isNaive = f.protocol === "naive";
  const isHy2 = f.protocol === "hysteria2";
  const isEdit = mode === "edit";
  const nodeOptions = useMemo(
    () => [
      { value: "local", label: t("common.local") },
      ...f.nodes
        .filter((node) => node.enabled && node.hasApiToken)
        .map((node) => ({ value: node.id, label: node.name })),
    ],
    [f.nodes, t],
  );
  const currentNodeLabel = useMemo(() => {
    if (!inbound?.nodeId) return t("common.local");
    return f.nodes.find((node) => node.id === inbound.nodeId)?.name ?? `node:${inbound.nodeId}`;
  }, [f.nodes, inbound?.nodeId, t]);
  // Hide the node picker on create when only the local server exists. Always
  // show it when editing (read-only label) or when remote nodes are available.
  const showNode = isEdit || nodeOptions.length > 1;

  // Transport-specific extra fields — VLESS only (naive/hysteria2 have no v2ray transport).
  const transportFields = useMemo(() => {
    if (f.protocol !== "vless") return null;
    switch (f.transmission) {
      case "ws":
        return (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label>{t("inbounds.transportPath")}</Label>
              <Input value={f.wsPath} onChange={(e) => f.setWsPath(e.target.value)} placeholder="/ws" mono />
            </div>
            <div>
              <Label>{t("inbounds.transportHost")}</Label>
              <Input value={f.sni} readOnly placeholder="panel.example" mono />
            </div>
          </div>
        );
      case "grpc":
        return (
          <div>
            <Label>{t("inbounds.grpcServiceName")}</Label>
            <Input value={f.grpcServiceName} onChange={(e) => f.setGrpcServiceName(e.target.value)} placeholder="grpc-svc" mono />
          </div>
        );
      case "httpupgrade":
        return (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label>{t("inbounds.transportPath")}</Label>
              <Input value={f.httpUpgradePath} onChange={(e) => f.setHttpUpgradePath(e.target.value)} placeholder="/up" mono />
            </div>
            <div>
              <Label>{t("inbounds.transportHost")}</Label>
              <Input value={f.httpUpgradeHost} onChange={(e) => f.setHttpUpgradeHost(e.target.value)} placeholder="panel.example" mono />
            </div>
          </div>
        );
      default:
        return null; // tcp / raw
    }
  }, [
    f.protocol,
    f.transmission,
    f.wsPath,
    f.grpcServiceName,
    f.httpUpgradePath,
    f.httpUpgradeHost,
    f.sni,
    t,
  ]);

  // Second column of the "traffic reset" row: transport (vless) / network (naive) / QUIC (hysteria2).
  const connectionControl = isVless ? (
    <div>
      <Label>{t("inbounds.transport")}</Label>
      <Select value={f.transmission} options={TRANSMISSION_OPTIONS} onChange={f.setTransmission} />
    </div>
  ) : isNaive ? (
    <div>
      <Label>{t("inbounds.network")}</Label>
      <Select value={f.network} options={NETWORK_OPTIONS} onChange={f.setNetwork} />
    </div>
  ) : (
    <div>
      <Label>{t("inbounds.transport")}</Label>
      <Input value="QUIC" mono readOnly />
    </div>
  );

  const title =
    mode === "edit"
      ? t("inbounds.modalEdit")
      : mode === "clone"
        ? t("inbounds.modalClone")
        : t("inbounds.modalCreate");

  return (
    <>
    <Modal open={open} onClose={onClose} width="max-w-[760px]">
      <ModalHeader title={title} onClose={onClose} />
      <ModalBody className="space-y-3">
        <Accordion title={t("inbounds.basics")}>
          <div className="space-y-3">
            <div>
              <Label>{t("common.remark")}</Label>
              <Input
                ref={f.remarkRef}
                value={f.remark}
                onChange={(e) => f.setRemark(e.target.value)}
                placeholder="e.g. vadim-vless#0001"
                className={mode === "clone" ? "selection:bg-brand/40" : undefined}
              />
            </div>
            <div className={`grid grid-cols-1 gap-3 ${showNode ? "sm:grid-cols-3" : "sm:grid-cols-2"}`}>
              {showNode ? (
                <div>
                  <Label>{t("common.node")}</Label>
                  {isEdit ? (
                    <Input value={currentNodeLabel} readOnly />
                  ) : (
                    <Select value={f.nodeId} options={nodeOptions} onChange={f.setNodeId} />
                  )}
                </div>
              ) : null}
              <div>
                <Label>{t("common.protocol")}</Label>
                <Select value={f.protocol} options={PROTOCOL_OPTIONS} onChange={f.setProtocol} />
              </div>
              <div>
                <Label>{t("common.port")}</Label>
                <Input
                  type="number"
                  value={f.port}
                  onChange={(e) => f.setPort(e.target.value)}
                  mono
                  trailing={
                    <m.button
                      type="button"
                      animate={{ rotate: f.diceSpin }}
                      transition={{ duration: 0.4, ease: "easeOut" }}
                      onClick={f.randomizePort}
                      className="grid size-7 place-items-center rounded-md text-ink-secondary transition-colors duration-150 hover:bg-hover hover:text-ink-primary"
                      title="Randomize"
                    >
                      <Dices size={14} />
                    </m.button>
                  }
                />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <Label>{t("inbounds.trafficReset")}</Label>
                <Select value={f.trafficReset} options={TRAFFIC_RESET_OPTIONS} onChange={f.setTrafficReset} />
              </div>
              {connectionControl}
            </div>
            {transportFields ? <div className="rounded-lg border border-subtle bg-canvas/60 p-3">{transportFields}</div> : null}
          </div>
        </Accordion>

        <Accordion title={t("inbounds.transportSecurity")}>
          <div className="space-y-5">
            <div className="">
              <Toggle checked={f.sniffing} onChange={f.setSniffing} label={t("inbounds.enableSniffing")} description={t("inbounds.sniffingDesc")} />
              {f.sniffing ? (
                <div className="mt-4 space-y-3">
                  <div className="flex flex-wrap gap-3 text-xs text-ink-secondary">
                    <Checkbox label="HTTP" checked={f.snifHttp} onChange={f.setSnifHttp} />
                    <Checkbox label="TLS" checked={f.snifTls} onChange={f.setSnifTls} />
                    <Checkbox label="QUIC" checked={f.snifQuic} onChange={f.setSnifQuic} />
                    <Checkbox label="FAKEDNS" checked={f.snifFakedns} onChange={f.setSnifFakedns} />
                  </div>
                  <div className="flex flex-wrap gap-6">
                    <Toggle checked={f.metadataOnly} onChange={f.setMetadataOnly} label={t("inbounds.metadataOnly")} />
                    <Toggle checked={f.routeOnly} onChange={f.setRouteOnly} label={t("inbounds.routeOnly")} />
                  </div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div>
                      <Label>{t("inbounds.ipsExcluded")}</Label>
                      <Textarea rows={2} value={f.ipsExcluded} onChange={(e) => f.setIpsExcluded(e.target.value)} mono placeholder="10.0.0.0/8" />
                    </div>
                    <div>
                      <Label>{t("inbounds.domainsExcluded")}</Label>
                      <Textarea rows={2} value={f.domainsExcluded} onChange={(e) => f.setDomainsExcluded(e.target.value)} mono placeholder="local.lan" />
                    </div>
                  </div>
                </div>
              ) : null}
            </div>

            <div>
              <Label>TLS</Label>
              {isVless ? (
                <Segmented<TlsMode>
                  value={f.tls}
                  onChange={f.setTls}
                  options={[
                    { value: "none", label: "None" },
                    { value: "tls", label: "TLS" },
                    { value: "reality", label: "Reality" }
                  ]}
                />
              ) : (
                <div className="flex items-center gap-2 rounded-lg border border-subtle bg-canvas/60 px-3 py-2 text-xs text-ink-secondary">
                  <ShieldCheck size={14} className="text-success" />
                  {t("inbounds.tlsRequired")}
                </div>
              )}
            </div>

            {f.tls === "tls" ? (
              <div className="space-y-3">
                <div>
                  <Label>{t("inbounds.tlsCertSource")}</Label>
                  <Segmented<TlsSource>
                    value={f.tlsSource}
                    onChange={f.setTlsSource}
                    options={[
                      { value: "selfSigned", label: t("inbounds.certSourceSelfSigned") },
                      { value: "acme", label: t("inbounds.certSourceAcme") },
                      { value: "custom", label: t("inbounds.certSourceCustom") },
                    ]}
                  />
                </div>
                {f.tlsSource === "selfSigned" ? (
                  <div className="flex items-start gap-2 rounded-lg border border-subtle bg-canvas/60 px-3 py-2 text-xs text-ink-secondary">
                    <ShieldCheck size={14} className="mt-0.5 shrink-0 text-success" />
                    <span>{t("inbounds.certSourceSelfSignedNote")}</span>
                  </div>
                ) : null}
                {f.tlsSource === "acme" ? (
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div>
                      <Label>{t("inbounds.acmeDomain")}</Label>
                      <Input value={f.acmeDomain} onChange={(e) => f.setAcmeDomain(e.target.value)} mono placeholder="vpn.example.com" />
                    </div>
                    <div>
                      <Label>{t("inbounds.acmeEmail")}</Label>
                      <Input value={f.acmeEmail} onChange={(e) => f.setAcmeEmail(e.target.value)} mono placeholder="admin@example.com" />
                    </div>
                  </div>
                ) : null}
                {f.tlsSource === "custom" ? (
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div>
                      <Label>{t("inbounds.certPath")}</Label>
                      <Input value={f.certPath} onChange={(e) => f.setCertPath(e.target.value)} mono placeholder="/var/lib/shilka/tls/cert.pem" />
                    </div>
                    <div>
                      <Label>{t("inbounds.keyPath")}</Label>
                      <Input value={f.keyPath} onChange={(e) => f.setKeyPath(e.target.value)} mono placeholder="/var/lib/shilka/tls/key.pem" />
                    </div>
                  </div>
                ) : null}
                <Toggle
                  checked={f.allowInsecure}
                  onChange={f.setAllowInsecure}
                  label={t("inbounds.allowInsecure")}
                  description={t("inbounds.allowInsecureDesc")}
                />
              </div>
            ) : null}

            {isVless && f.tls === "reality" ? (
              <div className="grid grid-cols-1 gap-3">
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <Label>{t("inbounds.destination")}</Label>
                    <Input value={f.dest} onChange={(e) => f.setDest(e.target.value)} mono />
                  </div>
                  <div>
                    <Label>SNI</Label>
                    <Input value={f.sni} onChange={(e) => f.setSni(e.target.value)} mono />
                  </div>
                </div>
                <div>
                  <Label>{t("inbounds.shortIds")}</Label>
                  <Textarea rows={2} value={f.shortIds} onChange={(e) => f.setShortIds(e.target.value)} mono placeholder="One short id per line" />
                </div>
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-ink-secondary">{t("inbounds.keypair")}</span>
                    <Button size="sm" onClick={f.generateKeypair}>
                      <RefreshCw size={14} />
                      {t("inbounds.generateKeypair")}
                    </Button>
                  </div>
                  <div>
                    <Label>{t("inbounds.privateKey")}</Label>
                    <Input value={f.privateKey} mono readOnly placeholder="-" />
                  </div>
                  <div>
                    <Label>{t("inbounds.publicKey")}</Label>
                    <Input value={f.publicKey} mono readOnly placeholder="-" />
                  </div>
                </div>
              </div>
            ) : f.tls === "tls" ? (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <Label>SNI</Label>
                  <Input value={f.sni} onChange={(e) => f.setSni(e.target.value)} mono />
                </div>
              </div>
            ) : null}

            {isNaive ? (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <Label>{t("inbounds.quicCc")}</Label>
                  <Select value={f.quicCc} options={QUIC_CC_OPTIONS} onChange={f.setQuicCc} />
                </div>
              </div>
            ) : null}

            {isHy2 ? (
              <div className="space-y-3">
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <Label>{t("inbounds.upMbps")}</Label>
                    <NumberInput value={f.upMbps} onChange={f.setUpMbps} min={0} mono placeholder="0" />
                  </div>
                  <div>
                    <Label>{t("inbounds.downMbps")}</Label>
                    <NumberInput value={f.downMbps} onChange={f.setDownMbps} min={0} mono placeholder="0" />
                  </div>
                </div>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <Label>{t("inbounds.obfs")}</Label>
                    <Select value={f.obfsType} options={OBFS_OPTIONS} onChange={f.setObfsType} />
                  </div>
                  {f.obfsType === "salamander" ? (
                    <div>
                      <Label>{t("inbounds.obfsPassword")}</Label>
                      <Input
                        value={f.obfsPassword}
                        onChange={(e) => f.setObfsPassword(e.target.value)}
                        mono
                        trailing={
                          <button
                            type="button"
                            onClick={f.regenerateObfsPassword}
                            className="grid size-7 place-items-center rounded-md text-ink-secondary transition-colors duration-150 hover:bg-hover hover:text-ink-primary"
                            title="Regenerate"
                          >
                            <RefreshCw size={14} />
                          </button>
                        }
                      />
                    </div>
                  ) : null}
                </div>
              </div>
            ) : null}
          </div>
        </Accordion>

      </ModalBody>
      <ModalFooter>
        {mode === "edit" && inbound ? (
          <>
            {!inbound.nodeId ? (
              <IconActionButton title={t("common.clone")} onClick={() => onClone?.(inbound)}>
                <Copy size={16} />
              </IconActionButton>
            ) : null}
            <IconActionButton title={t("common.delete")} danger onClick={f.openConfirmDelete}>
              <Trash2 size={16} />
            </IconActionButton>
            <div className="flex-1" />
          </>
        ) : null}
        <Button variant="danger" onClick={onClose}>
          {t("common.cancel")}
        </Button>
        <Button variant="primary" onClick={f.handleSave} loading={f.saving}>
          {t("common.save")}
        </Button>
      </ModalFooter>
    </Modal>
    <Modal open={f.confirmDelete} onClose={f.closeConfirmDelete} width="max-w-[420px]">
      <ModalHeader title={t("inbounds.deleteQuestion")} subtitle={inbound ? t("inbounds.deleteBody", { remark: inbound.remark }) : undefined} onClose={f.closeConfirmDelete} />
      <ModalFooter>
        <Button variant="secondary" onClick={f.closeConfirmDelete}>
          {t("common.cancel")}
        </Button>
        <Button variant="danger" onClick={f.handleDelete}>
          {t("common.delete")}
        </Button>
      </ModalFooter>
    </Modal>
    </>
  );
}

type CheckboxProps = {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
};

function IconActionButton({
  title,
  onClick,
  danger,
  children
}: {
  title: string;
  onClick: () => void;
  danger?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      onClick={onClick}
      className={`grid size-9 place-items-center rounded-lg border border-subtle bg-canvas text-ink-secondary transition-colors duration-200 ${danger ? "hover:border-danger/40 hover:text-danger" : "hover:border-white/20 hover:text-ink-primary"}`}
    >
      {children}
    </button>
  );
}

function Checkbox(props: CheckboxProps) {
  const { label, checked, onChange } = props;

  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className="inline-flex items-center gap-2"
    >
      <span
        className={`grid size-4 place-items-center rounded border ${checked ? "border-brand bg-brand text-white" : "border-white/15 bg-canvas"}`}
      >
        {checked ? (
          <svg viewBox="0 0 12 12" width="10" height="10" fill="none">
            <path d="m2 6 3 3 5-6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        ) : null}
      </span>
      <span className="text-xs text-ink-secondary">{label}</span>
    </button>
  );
}
