
import { useEffect, useMemo, useState } from "react";
import { Copy, QrCode, RefreshCw } from "lucide-react";

import { ApiError } from "@/api/client";
import { getClientLinks, listNodes, type ClientLinksDTO, type NodeDTO } from "@/api";
import { Button } from "@/components/ui/button";
import { DateInput, Input, Label, NumberInput } from "@/components/ui/input";
import { Modal, ModalBody, ModalFooter, ModalHeader } from "@/components/ui/modal";
import { Select } from "@/components/ui/select";
import { MultiSelect } from "@/components/ui/multi-select";
import { Toggle } from "@/components/ui/toggle";
import { useToast } from "@/components/ui/toast";
import type { Client, ClientStatus } from "@/lib/store";
import { useInbounds, useStoreActions } from "@/lib/store";
import { useI18n } from "@/lib/i18n";

import { StatusDot } from "@/components/ui/status-dot";
import { QrModal } from "./qr-modal";

const GB = 1024 ** 3;

type Props = {
  client: Client | null;
  onClose: () => void;
};

export function ClientDetailModal({ client, onClose }: Props) {
  const inbounds = useInbounds();
  const { updateClient } = useStoreActions();
  const { push } = useToast();
  const { t } = useI18n();

  const [draft, setDraft] = useState<Client | null>(client);
  const [nodes, setNodes] = useState<NodeDTO[]>([]);
  const [qrOpen, setQrOpen] = useState(false);
  const [qrLinks, setQrLinks] = useState<ClientLinksDTO | null>(null);
  const [qrLoading, setQrLoading] = useState(false);
  const [copied, setCopied] = useState(false);
  const [totalFlowGb, setTotalFlowGb] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void listNodes()
      .then(setNodes)
      .catch(() => setNodes([]));
    setDraft(client);
    setCopied(false);
    setQrOpen(false);
    setQrLinks(null);
    if (client) {
      const gb = Math.round(client.totalQuota / GB);
      setTotalFlowGb(gb > 0 ? String(gb) : "");
    }
  }, [client]);

  const currentNodeId = draft?.nodeId ?? "local";
  const currentNodeLabel = useMemo(() => {
    if (!draft?.nodeId) return t("common.local");
    return nodes.find((node) => node.id === draft.nodeId)?.name ?? `node:${draft.nodeId}`;
  }, [draft?.nodeId, nodes, t]);
  const inboundOptions = useMemo(
    () =>
      inbounds
        .filter((inbound) => (currentNodeId === "local" ? !inbound.nodeId : inbound.nodeId === currentNodeId))
        .map((inbound) => ({ value: inbound.id, label: inbound.remark })),
    [currentNodeId, inbounds],
  );

  if (!draft || !client) {
    return <Modal open={false} onClose={onClose}>{null}</Modal>;
  }

  function update<K extends keyof Client>(key: K, value: Client[K]) {
    setDraft((prev) => (prev ? { ...prev, [key]: value } : prev));
  }

  async function copyLink() {
    if (!draft) return;
    try {
      await navigator.clipboard.writeText(draft.subscription);
    } catch {
      // ignore
    }
    setCopied(true);
    push(t("clients.linkCopied"), "success");
    window.setTimeout(() => setCopied(false), 1500);
  }

  async function save() {
    if (!draft) return;
    const parsedFlow = totalFlowGb === "" ? 0 : Number(totalFlowGb);
    setSaving(true);
    try {
      await updateClient(draft.id, {
        nodeId: draft.nodeId,
        name: draft.name,
        inboundIds: draft.inboundIds,
        totalQuota: parsedFlow * GB,
        expiry: draft.expiry,
        status: draft.status,
        startAfterFirstUse: draft.startAfterFirstUse,
      });
      push(t("clients.updated"), "success");
      onClose();
    } catch (err) {
      const body = err instanceof ApiError ? err.body : null;
      const message =
        body && typeof body === "object" && body !== null && "error" in body
          ? String((body as { error: unknown }).error)
          : t("clients.updateFailed");
      push(message, "error");
    } finally {
      setSaving(false);
    }
  }

  async function openQr() {
    if (!draft || qrLoading) return;
    setQrOpen(true);
    setQrLoading(true);
    try {
      const links = await getClientLinks(draft.id);
      if (!links.qrPng || !links.shareLink) {
        throw new Error("missing qr payload");
      }
      setQrLinks(links);
    } catch (err) {
      const body = err instanceof ApiError ? err.body : null;
      const message =
        body && typeof body === "object" && body !== null && "error" in body
          ? String((body as { error: unknown }).error)
          : t("clients.qrLoadFailed");
      setQrOpen(false);
      push(message, "error");
    } finally {
      setQrLoading(false);
    }
  }

  return (
    <>
      <Modal open={!!client} onClose={onClose} width="max-w-[640px]">
        <ModalHeader title={draft.name} onClose={onClose} />
        <ModalBody className="space-y-3">
          <div className="-mt-2 flex items-center gap-1.5 text-xs text-ink-tertiary">
            <StatusDot state={draft.online ? "online" : "neutral"} size={6} />
            <span>{draft.online ? t("clients.online") : t("clients.offline")}</span>
          </div>
          <div>
            <Label>{t("common.node")}</Label>
            <Input value={currentNodeLabel} readOnly />
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label>{t("clients.userName")}</Label>
              <Input value={draft.name} onChange={(e) => update("name", e.target.value)} />
            </div>
            <div>
              <Label>UUID</Label>
              <Input
                value={draft.uuid}
                mono
                readOnly
                trailing={
                  <button
                    type="button"
                    onClick={() => update("uuid", crypto.randomUUID())}
                    className="grid size-7 place-items-center rounded-md text-ink-secondary transition-colors duration-150 hover:bg-hover hover:text-ink-primary"
                    title="Regenerate"
                  >
                    <RefreshCw size={14} />
                  </button>
                }
              />
            </div>
          </div>
          <div>
            <Label>{t("clients.inbound")}</Label>
            <MultiSelect
              values={draft.inboundIds ?? []}
              options={inboundOptions}
              onChange={(v) => update("inboundIds", v)}
            />
          </div>
          <div>
            <Label>{t("inbounds.subscription")}</Label>
            <Input value={draft.subscription} mono onChange={(e) => update("subscription", e.target.value)} />
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label>{t("inbounds.totalFlow")}</Label>
              <NumberInput value={totalFlowGb} onChange={setTotalFlowGb} min={0} mono placeholder="0" />
            </div>
            <div>
              <Label>{t("inbounds.expiryDate")}</Label>
              <DateInput
                value={draft.expiry.slice(0, 10)}
                onChange={(v) => update("expiry", v ? new Date(v).toISOString() : draft.expiry)}
              />
            </div>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label>{t("common.status")}</Label>
              <Select<ClientStatus>
                value={draft.status}
                options={[
                  { value: "active", label: t("common.active") },
                  { value: "disabled", label: t("common.disabled") },
                  { value: "expired", label: t("common.expired") }
                ]}
                onChange={(v) => update("status", v)}
              />
            </div>
            <div className="flex min-h-[66px] items-center justify-center rounded-lg border border-subtle bg-canvas/40 px-3">
              <Toggle
                checked={draft.startAfterFirstUse}
                onChange={(v) => update("startAfterFirstUse", v)}
                label={t("inbounds.startAfterFirstUse")}
              />
            </div>
          </div>
        </ModalBody>
        <ModalFooter>
          <Button variant="secondary" onClick={() => void openQr()} loading={qrLoading}>
            <QrCode size={14} />
            {t("clients.getQr")}
          </Button>
          <Button variant="secondary" onClick={copyLink}>
            <Copy size={14} />
            {copied ? t("clients.copied") : t("clients.copyLink")}
          </Button>
          <div className="flex-1" />
          <Button variant="danger" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button variant="primary" onClick={save} loading={saving}>
            {t("common.save")}
          </Button>
        </ModalFooter>
      </Modal>
      <QrModal
        open={qrOpen}
        onClose={() => setQrOpen(false)}
        qrPng={qrLinks?.qrPng ?? ""}
        payload={qrLinks?.shareLink ?? ""}
        loading={qrLoading}
      />
    </>
  );
}
