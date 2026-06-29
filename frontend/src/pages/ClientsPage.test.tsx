import { afterEach, expect, test, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import * as api from "@/api";
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

afterEach(() => {
  vi.restoreAllMocks();
});

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

test("select all is limited to filtered clients and checkbox does not open details", async () => {
  const user = userEvent.setup();
  renderWithProviders(<ClientsPage />, {
    seed: { inbounds: INBOUNDS, clients: CLIENTS },
    route: "/clients?inbound=4",
  });

  const selectAll = await screen.findByRole("checkbox", { name: "Select all filtered clients" });
  await user.click(selectAll);

  expect(screen.getByText("Selected: 1")).toBeInTheDocument();
  expect(screen.getByRole("checkbox", { name: "Select miyu" })).toBeChecked();
  expect(screen.queryByText("User name")).not.toBeInTheDocument();
});

test("bulk delete confirms, keeps failures selected, and shows per-client error", async () => {
  const user = userEvent.setup();
  const bulkDelete = vi.spyOn(api, "bulkDeleteClients").mockResolvedValue({
    results: [
      { id: "1", ok: true, error: "" },
      { id: "2", ok: false, error: "not found" },
    ],
  });
  renderWithProviders(<ClientsPage />, {
    seed: { inbounds: INBOUNDS, clients: CLIENTS },
  });

  const selectAll = await screen.findByRole("checkbox", { name: "Select all filtered clients" });
  await user.click(screen.getByRole("checkbox", { name: "Select alex_kim" }));
  expect(selectAll).toBePartiallyChecked();
  await user.click(selectAll);
  await user.click(screen.getByRole("button", { name: "Delete" }));
  expect(screen.getByText("Delete selected clients?")).toBeInTheDocument();
  expect(bulkDelete).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: "Delete clients" }));
  await waitFor(() => expect(bulkDelete).toHaveBeenCalledWith(["1", "2"]));
  expect(await screen.findByText("Failed clients")).toBeInTheDocument();
  expect(screen.getByRole("alert")).toHaveTextContent("miyu: not found");
  expect(screen.getByRole("checkbox", { name: "Select alex_kim" })).not.toBeChecked();
  expect(screen.getByRole("checkbox", { name: "Select miyu" })).toBeChecked();
});

test("bulk traffic reset requires confirmation", async () => {
  const user = userEvent.setup();
  const bulkReset = vi.spyOn(api, "bulkResetClientTraffic").mockResolvedValue({
    results: [{ id: "1", ok: true, error: "" }],
  });
  renderWithProviders(<ClientsPage />, {
    seed: { inbounds: INBOUNDS, clients: CLIENTS },
  });

  await user.click(await screen.findByRole("checkbox", { name: "Select alex_kim" }));
  await user.click(screen.getByRole("button", { name: "Reset traffic" }));
  expect(screen.getByText("Reset selected traffic?")).toBeInTheDocument();
  expect(bulkReset).not.toHaveBeenCalled();

  const resetButtons = screen.getAllByRole("button", { name: "Reset traffic" });
  await user.click(resetButtons[resetButtons.length - 1]);
  await waitFor(() => expect(bulkReset).toHaveBeenCalledWith(["1"]));
});
