import { expect, test } from "vitest";
import { screen, waitFor } from "@testing-library/react";

import { ClientsPage } from "@/pages/ClientsPage";
import { renderWithProviders } from "@/test/test-utils";
import type { InboundDTO, ClientDTO } from "@/lib/store";

const INBOUNDS: InboundDTO[] = [
  { id: "4", remark: "frankfurt-ws-01", protocol: "vless", port: 27440, transmission: "ws", tls: "tls", enabled: true, clientCount: 2, createdAt: "2026-04-18T12:24:00Z" },
];

const CLIENTS: ClientDTO[] = [
  { id: "1", name: "alex_kim", uuid: "a", inboundId: "1", inboundIds: ["1"], usedDown: 0, usedUp: 0, totalQuota: 0, expiry: "", status: "active", subscription: "", startAfterFirstUse: false, online: false },
  { id: "2", name: "miyu", uuid: "b", inboundId: "4", inboundIds: ["4"], usedDown: 0, usedUp: 0, totalQuota: 0, expiry: "", status: "active", subscription: "", startAfterFirstUse: false, online: false },
];

test("initializes the inbound filter from the URL", async () => {
  renderWithProviders(<ClientsPage />, {
    seed: { inbounds: INBOUNDS, clients: CLIENTS },
    route: "/clients?inbound=4",
  });

  expect(
    await screen.findByRole("button", { name: "frankfurt-ws-01" }),
  ).toBeInTheDocument();
  expect(screen.getByText("miyu")).toBeInTheDocument();
  await waitFor(() =>
    expect(screen.queryByText("alex_kim")).not.toBeInTheDocument(),
  );
});
