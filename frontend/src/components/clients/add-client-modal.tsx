import { useEffect, useMemo, useRef, useState } from "react";

import { ApiError } from "@/api/client";
import { listNodes, type NodeDTO } from "@/api";
import { Button } from "@/components/ui/button";
import { DateInput, Input, Label, NumberInput } from "@/components/ui/input";
import { Modal, ModalBody, ModalFooter, ModalHeader } from "@/components/ui/modal";
import { Select } from "@/components/ui/select";
import { MultiSelect } from "@/components/ui/multi-select";
import { Toggle } from "@/components/ui/toggle";
import { useToast } from "@/components/ui/toast";
import { useInbounds, useStoreActions } from "@/lib/store";
import { useI18n } from "@/lib/i18n";

const GB = 1024 ** 3;

function defaultExpiryIso() {
  const d = new Date();
  d.setMonth(d.getMonth() + 1);
  return d.toISOString();
}

type Props = {
  open: boolean;
  onClose: () => void;
  /** Optional inbound pre-selection — e.g. when navigated from the filter bar. */
  defaultInboundId?: string;
  defaultNodeId?: string;
};

export function AddClientModal({ open, onClose, defaultInboundId, defaultNodeId }: Props) {
  const inbounds = useInbounds();
  // Keep the latest inbounds in a ref so the init effect can read them without
  // re-running every time polling delivers a new array (SIN-35).
  const inboundsRef = useRef(inbounds);
  inboundsRef.current = inbounds;
  const { addClient } = useStoreActions();
  const { push } = useToast();
  const { t } = useI18n();

  const [name, setName] = useState("");
  const [nodes, setNodes] = useState<NodeDTO[]>([]);
  const [nodeId, setNodeId] = useState("local");
  const [inboundIds, setInboundIds] = useState<string[]>([]);
  const [totalFlowGb, setTotalFlowGb] = useState("100");
  const [expiry, setExpiry] = useState("");
  // Quota and expiry are opt-in: off by default → unlimited traffic / no expiry.
  const [enableQuota, setEnableQuota] = useState(false);
  const [enableExpiry, setEnableExpiry] = useState(false);
  const [startAfterFirstUse, setStartAfterFirstUse] = useState(false);
  const [saving, setSaving] = useState(false);
  const [nameError, setNameError] = useState<string | undefined>(undefined);
  const nodeOptions = useMemo(
    () => [
      { value: "local", label: t("common.local") },
      ...nodes
        .filter((node) => node.enabled && node.hasApiToken)
        .map((node) => ({ value: node.id, label: node.name })),
    ],
    [nodes, t],
  );
  const inboundOptions = useMemo(
    () =>
      inbounds
        .filter((inbound) => (nodeId === "local" ? !inbound.nodeId : inbound.nodeId === nodeId))
        .map((inbound) => ({ value: inbound.id, label: inbound.remark })),
    [inbounds, nodeId],
  );

  useEffect(() => {
    if (!open) return;
    void listNodes()
      .then(setNodes)
      .catch(() => setNodes([]));
    setName("");
    setNameError(undefined);
    const currentInbounds = inboundsRef.current;
    const defaultInbound = defaultInboundId
      ? currentInbounds.find((inbound) => inbound.id === defaultInboundId)
      : undefined;
    const nextNodeId = defaultInbound?.nodeId ?? (defaultNodeId && defaultNodeId !== "all" ? defaultNodeId : "local");
    const nextInbound =
      defaultInbound && (nextNodeId === "local" ? !defaultInbound.nodeId : defaultInbound.nodeId === nextNodeId)
        ? defaultInbound.id
        : (currentInbounds.find((inbound) => (nextNodeId === "local" ? !inbound.nodeId : inbound.nodeId === nextNodeId))?.id ?? "");
    setNodeId(nextNodeId);
    setInboundIds(nextInbound ? [nextInbound] : []);
    setTotalFlowGb("100");
    setExpiry(defaultExpiryIso().slice(0, 10));
    setEnableQuota(false);
    setEnableExpiry(false);
    setStartAfterFirstUse(false);
  }, [open, defaultInboundId, defaultNodeId]);

  // Prune selections that don't belong to the current node's inbounds (e.g.
  // after switching node); default to the first option when nothing remains.
  useEffect(() => {
    if (!open) return;
    const valid = inboundIds.filter((id) => inboundOptions.some((o) => o.value === id));
    if (valid.length !== inboundIds.length) {
      setInboundIds(valid.length > 0 ? valid : inboundOptions[0] ? [inboundOptions[0].value] : []);
      return;
    }
    if (valid.length === 0 && inboundOptions[0]) {
      setInboundIds([inboundOptions[0].value]);
    }
  }, [open, inboundIds, inboundOptions]);

  async function handleSave() {
    if (!name.trim()) {
      setNameError(t("clients.nameRequired"));
      return;
    }
    if (inboundIds.length === 0) {
      push(t("clients.inboundRequired"), "error");
      return;
    }
    setSaving(true);
    try {
      await addClient({
        ...(nodeId !== "local" ? { nodeId } : {}),
        name: name.trim(),
        inboundIds,
        totalQuota: enableQuota ? (Number(totalFlowGb) || 0) * GB : 0,
        expiry: enableExpiry && expiry ? new Date(expiry).toISOString() : "",
        startAfterFirstUse,
      });
      push(t("clients.created"), "success");
      onClose();
    } catch (err) {
      const body = err instanceof ApiError ? err.body : null;
      const message =
        body && typeof body === "object" && body !== null && "error" in body
          ? String((body as { error: unknown }).error)
          : t("clients.createFailed");
      push(message, "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal open={open} onClose={onClose} width="max-w-[560px]">
      <ModalHeader title={t("clients.addTitle")} onClose={onClose} />
      <ModalBody className="space-y-3">
        <div>
          <Label>{t("clients.userName")}</Label>
          <Input
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              if (nameError) setNameError(undefined);
            }}
            placeholder="e.g. vadim_denisych#0001"
            error={nameError}
          />
        </div>
        {nodeOptions.length > 1 ? (
          <div>
            <Label>{t("common.node")}</Label>
            <Select
              value={nodeId}
              options={nodeOptions}
              onChange={setNodeId}
            />
          </div>
        ) : null}
        <div>
          <Label>{t("clients.inbound")}</Label>
          <MultiSelect
            values={inboundIds}
            options={inboundOptions}
            onChange={setInboundIds}
          />
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-2 rounded-lg border border-subtle bg-canvas/40 px-3 py-2.5">
            <Toggle checked={enableQuota} onChange={setEnableQuota} label={t("clients.enableQuota")} />
            {enableQuota ? (
              <div>
                <Label>{t("inbounds.totalFlow")}</Label>
                <NumberInput value={totalFlowGb} onChange={setTotalFlowGb} min={0} mono placeholder="0" />
              </div>
            ) : null}
          </div>
          <div className="space-y-2 rounded-lg border border-subtle bg-canvas/40 px-3 py-2.5">
            <Toggle checked={enableExpiry} onChange={setEnableExpiry} label={t("clients.enableExpiry")} />
            {enableExpiry ? (
              <div>
                <Label>{t("inbounds.expiryDate")}</Label>
                <DateInput value={expiry} onChange={setExpiry} />
              </div>
            ) : null}
          </div>
        </div>
        <div className="flex min-h-[72px] items-center justify-center rounded-lg rounded-lg border border-subtle bg-canvas/40 px-3 py-2">
          <Toggle
            size="lg"
            checked={startAfterFirstUse}
            onChange={setStartAfterFirstUse}
            label={t("inbounds.startAfterFirstUse")}
          />
        </div>
      </ModalBody>
      <ModalFooter>
        <Button variant="danger" onClick={onClose}>
          {t("common.cancel")}
        </Button>
        <Button variant="primary" onClick={handleSave} loading={saving}>
          {t("common.save")}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
