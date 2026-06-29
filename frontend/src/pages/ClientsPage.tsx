import { Suspense, lazy, useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Ban, Check, Plus, RotateCcw, Trash2, X } from "lucide-react";

import { ApiError } from "@/api/client";
import type { BulkClientResult } from "@/api/clients";
import { Button } from "@/components/ui/button";
import { Modal, ModalBody, ModalFooter, ModalHeader } from "@/components/ui/modal";
import { useToast } from "@/components/ui/toast";
import {
  ClientFilterBar,
  type FilterState,
} from "@/components/clients/client-filter-bar";
import { ClientsTable } from "@/components/clients/clients-table";
import { useDisclosure } from "@/hooks/useDisclosure";
import type { Client } from "@/lib/store";
import { useI18n } from "@/lib/i18n";
import { useClients, useStoreActions } from "@/lib/store";

const AddClientModal = lazy(() =>
  import("@/components/clients/add-client-modal").then((m) => ({
    default: m.AddClientModal,
  })),
);
const ClientDetailModal = lazy(() =>
  import("@/components/clients/client-detail-modal").then((m) => ({
    default: m.ClientDetailModal,
  })),
);

const prefetchAddClient = () => {
  void import("@/components/clients/add-client-modal");
};
const prefetchDetailModal = () => {
  void import("@/components/clients/client-detail-modal");
};

type BulkAction = "enable" | "disable" | "reset" | "delete";

export function ClientsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [filter, setFilter] = useState<FilterState>({
    query: "",
    inboundId: "all",
    nodeId: "all",
    status: "all",
  });
  const [selected, setSelected] = useState<Client | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());
  const [bulkFailures, setBulkFailures] = useState<BulkClientResult[]>([]);
  const [runningAction, setRunningAction] = useState<BulkAction | null>(null);
  const [confirmAction, setConfirmAction] = useState<Extract<BulkAction, "reset" | "delete"> | null>(null);
  const addModal = useDisclosure(false);
  const clients = useClients();
  const { bulkDeleteClients, bulkResetClientTraffic, bulkSetClientStatus } = useStoreActions();
  const { push } = useToast();
  const { t } = useI18n();
  const clientMap = useMemo(() => new Map(clients.map((client) => [client.id, client])), [clients]);

  useEffect(() => {
    const inboundId = searchParams.get("inbound") ?? "all";
    const nodeId = searchParams.get("node") ?? "all";
    setFilter((prev) =>
      prev.inboundId === inboundId && prev.nodeId === nodeId
        ? prev
        : { ...prev, inboundId, nodeId },
    );
    setSelectedIds(new Set());
    setBulkFailures([]);
  }, [searchParams]);

  const clearSelection = useCallback(() => {
    setSelectedIds(new Set());
    setBulkFailures([]);
  }, []);

  const updateFilter = useCallback(
    (next: FilterState) => {
      setFilter(next);
      clearSelection();
      const params = new URLSearchParams(searchParams);
      if (next.inboundId === "all") params.delete("inbound");
      else params.set("inbound", next.inboundId);
      if (next.nodeId === "all") params.delete("node");
      else params.set("node", next.nodeId);
      setSearchParams(params, { replace: true });
    },
    [clearSelection, searchParams, setSearchParams],
  );

  const closeDetail = useCallback(() => setSelected(null), []);

  const runBulkAction = useCallback(async (action: BulkAction) => {
    const ids = Array.from(selectedIds);
    if (ids.length === 0) return;
    setRunningAction(action);
    setBulkFailures([]);
    try {
      const response = action === "delete"
        ? await bulkDeleteClients(ids)
        : action === "reset"
          ? await bulkResetClientTraffic(ids)
          : await bulkSetClientStatus(ids, action === "enable" ? "active" : "disabled");
      const failures = response.results.filter((result) => !result.ok);
      const successCount = response.results.length - failures.length;
      setSelectedIds(new Set(failures.map((result) => result.id)));
      setBulkFailures(failures);
      push(
        t("clients.bulkSummary", { success: successCount, failed: failures.length }),
        failures.length > 0 ? "error" : "success",
      );
    } catch (error) {
      const body = error instanceof ApiError ? error.body : null;
      const message = body && typeof body === "object" && body !== null && "error" in body
        ? String((body as { error: unknown }).error)
        : t("clients.bulkRequestFailed");
      push(message, "error");
    } finally {
      setRunningAction(null);
      setConfirmAction(null);
    }
  }, [bulkDeleteClients, bulkResetClientTraffic, bulkSetClientStatus, push, selectedIds, t]);

  const confirmTitle = confirmAction === "delete"
    ? t("clients.bulkDeleteTitle")
    : t("clients.bulkResetTitle");

  return (
    <div
      className="mx-auto flex max-w-[1320px] flex-col gap-6"
      onMouseEnter={prefetchDetailModal}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-2xl font-semibold text-ink-primary">
          {t("clients.title")}
        </h2>
        <Button
          variant="white"
          onClick={addModal.open}
          onMouseEnter={prefetchAddClient}
          onFocus={prefetchAddClient}
        >
          <Plus size={16} />
          {t("clients.add")}
        </Button>
      </div>

      <ClientFilterBar value={filter} onChange={updateFilter} />
      {selectedIds.size > 0 ? (
        <div className="rounded-xl border border-subtle bg-surface px-4 py-3">
          <div className="flex flex-wrap items-center gap-2">
            <span className="mr-auto text-sm font-medium text-ink-primary">
              {t("clients.selectedCount", { count: selectedIds.size })}
            </span>
            <Button
              size="sm"
              onClick={() => void runBulkAction("enable")}
              loading={runningAction === "enable"}
              disabled={runningAction !== null}
            >
              <Check size={14} />
              {t("clients.bulkEnable")}
            </Button>
            <Button
              size="sm"
              onClick={() => void runBulkAction("disable")}
              loading={runningAction === "disable"}
              disabled={runningAction !== null}
            >
              <Ban size={14} />
              {t("clients.bulkDisable")}
            </Button>
            <Button size="sm" onClick={() => setConfirmAction("reset")} disabled={runningAction !== null}>
              <RotateCcw size={14} />
              {t("clients.bulkReset")}
            </Button>
            <Button variant="danger" size="sm" onClick={() => setConfirmAction("delete")} disabled={runningAction !== null}>
              <Trash2 size={14} />
              {t("clients.bulkDelete")}
            </Button>
            <Button variant="ghost" size="sm" onClick={clearSelection} disabled={runningAction !== null}>
              <X size={14} />
              {t("clients.clearSelection")}
            </Button>
          </div>
          {bulkFailures.length > 0 ? (
            <div role="alert" className="mt-3 border-t border-subtle pt-3">
              <div className="text-xs font-medium text-danger">{t("clients.bulkFailures")}</div>
              <ul className="mt-1 space-y-1 text-xs text-ink-secondary">
                {bulkFailures.map((failure) => (
                  <li key={failure.id}>
                    <span className="text-ink-primary">{clientMap.get(failure.id)?.name ?? failure.id}</span>
                    {": "}{failure.error}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
      ) : null}
      <ClientsTable
        filter={filter}
        onSelect={setSelected}
        selectedIds={selectedIds}
        onSelectionChange={(ids) => {
          setSelectedIds(ids);
          setBulkFailures([]);
        }}
        selectionDisabled={runningAction !== null}
      />

      {selected ? (
        <Suspense fallback={null}>
          <ClientDetailModal client={selected} onClose={closeDetail} />
        </Suspense>
      ) : null}
      {addModal.isOpen ? (
        <Suspense fallback={null}>
          <AddClientModal
            open={addModal.isOpen}
            onClose={addModal.close}
            defaultInboundId={
              filter.inboundId !== "all" ? filter.inboundId : undefined
            }
            defaultNodeId={filter.nodeId !== "all" ? filter.nodeId : undefined}
          />
        </Suspense>
      ) : null}
      <Modal open={confirmAction !== null} onClose={() => setConfirmAction(null)} width="max-w-[420px]">
        <ModalHeader title={confirmTitle} onClose={() => setConfirmAction(null)} />
        <ModalBody>
          <p className="text-sm text-ink-secondary">
            {confirmAction === "delete"
              ? t("clients.bulkDeleteBody", { count: selectedIds.size })
              : t("clients.bulkResetBody", { count: selectedIds.size })}
          </p>
        </ModalBody>
        <ModalFooter>
          <Button variant="secondary" onClick={() => setConfirmAction(null)} disabled={runningAction !== null}>
            {t("common.cancel")}
          </Button>
          <Button
            variant={confirmAction === "delete" ? "danger" : "primary"}
            onClick={() => confirmAction && void runBulkAction(confirmAction)}
            loading={runningAction === confirmAction}
          >
            {confirmAction === "delete" ? t("clients.bulkDeleteConfirm") : t("clients.bulkResetConfirm")}
          </Button>
        </ModalFooter>
      </Modal>
    </div>
  );
}
