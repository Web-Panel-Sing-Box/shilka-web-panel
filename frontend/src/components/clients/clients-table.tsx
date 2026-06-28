import { memo, useCallback, useMemo, useState } from "react";

import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { StatusDot } from "@/components/ui/status-dot";
import { formatBytes, formatDate, truncateMiddle } from "@/lib/format";
import { useClients, useInbounds } from "@/lib/store";
import { useClientFilter } from "@/hooks/useClientFilter";
import type { Client } from "@/lib/store";
import { cn } from "@/lib/utils";
import { useI18n } from "@/lib/i18n";

import type { FilterState } from "./client-filter-bar";

type Props = {
  filter: FilterState;
  onSelect: (client: Client) => void;
};

// Name and Data Usage are clustered on the left; Inbound and Status are
// clustered on the right. Expiry sits in the middle column with text-center
// so its content visually floats away from both clusters.
const GRID = "grid-cols-[1.4fr_220px_minmax(160px,1.6fr)_200px_140px] gap-x-3";

export function ClientsTable({ filter, onSelect }: Props) {
  const clients = useClients();
  const inbounds = useInbounds();
  const { t } = useI18n();
  const inboundMap = useMemo(() => new Map(inbounds.map((i) => [i.id, i])), [inbounds]);

  const rows = useClientFilter(clients, filter);

  return (
    <Card padded={false} className="flex max-h-[calc(100dvh-170px)] flex-col overflow-hidden">
      <div className="flex-1 overflow-auto">
        <div className="min-w-[960px]">
          <div className={cn("sticky top-0 z-10 grid items-center border-b border-subtle bg-surface px-5 py-3 text-xs font-medium text-ink-tertiary", GRID)}>
            <span>{t("clients.name")}</span>
            <span>{t("clients.dataUsage")}</span>
            <span className="text-center">{t("clients.expiry")}</span>
            <span className="text-right">{t("clients.inbound")}</span>
            <span className="text-right">{t("common.status")}</span>
          </div>
          <div className="divide-y divide-subtle">
            {rows.length === 0 ? (
              <div className="px-5 py-10 text-center text-sm text-ink-tertiary">{t("clients.noMatch")}</div>
            ) : null}
            {rows.map((c) => {
              const ids = c.inboundIds?.length ? c.inboundIds : [c.inboundId];
              const inboundLabel = ids.map((id) => inboundMap.get(id)?.remark ?? id).join(", ") || "-";
              const total = c.usedDown + c.usedUp;
              const pct = c.totalQuota > 0 ? (total / c.totalQuota) * 100 : 0;
              return (
                <Row
                  key={c.id}
                  client={c}
                  inboundLabel={inboundLabel}
                  pct={pct}
                  total={total}
                  onSelect={onSelect}
                />
              );
            })}
          </div>
        </div>
      </div>
    </Card>
  );
}

const Row = memo(function Row({
  client,
  inboundLabel,
  pct,
  total,
  onSelect
}: {
  client: Client;
  inboundLabel: string;
  pct: number;
  total: number;
  onSelect: (client: Client) => void;
}) {
  const [hover, setHover] = useState(false);
  const { t } = useI18n();
  const handleClick = useCallback(() => onSelect(client), [onSelect, client]);
  const handleEnter = useCallback(() => setHover(true), []);
  const handleLeave = useCallback(() => setHover(false), []);
  return (
    <button
      type="button"
      onClick={handleClick}
      onMouseEnter={handleEnter}
      onMouseLeave={handleLeave}
      className={cn("grid w-full items-center px-5 py-3 text-left transition-colors duration-200", GRID, hover && "bg-elevated")}
    >
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <StatusDot state={client.online ? "online" : "neutral"} size={6} />
          <div className="truncate text-sm text-ink-primary">{client.name}</div>
        </div>
        <div className="truncate font-mono text-[11px] text-ink-tertiary">
          {client.nodeId ? `node:${client.nodeId} · ` : `${t("common.local")} · `}
          {truncateMiddle(client.uuid, 8, 6)}
        </div>
      </div>
      <div className="min-w-0">
        <div className="mb-1 flex justify-between font-mono text-[11px] text-ink-tertiary">
          <span>{formatBytes(total)}</span>
          <span>{formatBytes(client.totalQuota)}</span>
        </div>
        <Progress value={pct} />
      </div>
      <span className="text-center text-xs text-ink-secondary">{formatDate(client.expiry)}</span>
      <span className="truncate text-right text-xs text-ink-secondary">{inboundLabel}</span>
      <div className="flex justify-end">
        <StatusBadge status={client.status} />
      </div>
    </button>
  );
});

function StatusBadge({ status }: { status: Client["status"] }) {
  const { t } = useI18n();
  const map: Record<Client["status"], { label: string; dot: "online" | "stopped" | "neutral"; cls: string }> = {
    active: { label: t("common.active"), dot: "online", cls: "text-success" },
    disabled: { label: t("common.disabled"), dot: "neutral", cls: "text-ink-tertiary" },
    expired: { label: t("common.expired"), dot: "stopped", cls: "text-danger" }
  };
  const v = map[status];
  return (
    <span className="inline-flex w-fit max-w-full items-center gap-2 whitespace-nowrap rounded-full border border-subtle bg-canvas px-2.5 py-1 text-[11px]">
      <StatusDot state={v.dot} />
      <span className={v.cls}>{v.label}</span>
    </span>
  );
}
