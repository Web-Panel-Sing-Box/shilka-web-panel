import { beforeEach, expect, test, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import * as api from "@/api";
import { InboundFormModal } from "@/components/inbounds/inbound-form-modal";
import { renderWithProviders } from "@/test/test-utils";

vi.mock("@/api", () => ({
  createInbound: vi.fn().mockResolvedValue({}),
  listNodes: vi.fn().mockResolvedValue([]),
}));

beforeEach(() => {
  vi.mocked(api.createInbound).mockClear();
  vi.mocked(api.listNodes).mockResolvedValue([]);
});

test("sends allowInsecure for new TLS inbounds", async () => {
  const user = userEvent.setup();
  const onClose = vi.fn();
  renderWithProviders(<InboundFormModal open onClose={onClose} />, { seed: { inbounds: [] } });

  const toggle = screen.getByRole("switch", { name: /allow insecure/i });
  expect(toggle).toHaveAttribute("aria-checked", "true");

  await user.type(screen.getByPlaceholderText("e.g. vadim-vless#0001"), "hy2-self-signed");
  await user.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(api.createInbound).toHaveBeenCalledTimes(1));
  expect(api.createInbound).toHaveBeenCalledWith(
    expect.objectContaining({
      protocol: "naive",
      tls: "tls",
      allowInsecure: true,
    }),
  );
});

test("sends nodeId when creating an inbound on a remote node", async () => {
  vi.mocked(api.listNodes).mockResolvedValue([
    {
      id: "1",
      name: "edge-node",
      remark: "",
      scheme: "https",
      address: "edge.example.com",
      port: 443,
      basePath: "",
      enabled: true,
      allowPrivateAddress: false,
      skipTlsVerify: false,
      status: "online",
      latencyMs: 0,
      panelVersion: "",
      coreVersion: "",
      cpuPct: 0,
      ramPct: 0,
      uptimeSeconds: 0,
      hasApiToken: true,
      createdAt: "",
      updatedAt: "",
    },
  ]);
  const user = userEvent.setup();
  renderWithProviders(<InboundFormModal open onClose={vi.fn()} />, { seed: { inbounds: [] } });

  await user.click(await screen.findByRole("button", { name: "Local" }));
  await user.click(await screen.findByRole("option", { name: "edge-node" }));
  await user.type(screen.getByPlaceholderText("e.g. vadim-vless#0001"), "remote-hy2");
  await user.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(api.createInbound).toHaveBeenCalledTimes(1));
  expect(api.createInbound).toHaveBeenCalledWith(
    expect.objectContaining({
      nodeId: "1",
      remark: "remote-hy2",
    }),
  );
});

test("sends controlled HTTPUpgrade path and host", async () => {
  const user = userEvent.setup();
  renderWithProviders(<InboundFormModal open onClose={vi.fn()} />, { seed: { inbounds: [] } });

  await user.click(screen.getByRole("button", { name: "Naive Proxy" }));
  await user.click(screen.getByRole("option", { name: "VLESS" }));
  await user.click(screen.getByRole("button", { name: "TCP (RAW)" }));
  await user.click(screen.getByRole("option", { name: "HTTPUpgrade" }));

  await user.type(screen.getByPlaceholderText("e.g. vadim-vless#0001"), "vless-upgrade");
  await user.type(screen.getByPlaceholderText("/up"), "/upgrade");
  const hostFields = screen.getAllByPlaceholderText("panel.example");
  await user.type(hostFields[hostFields.length - 1], "edge.example.com");
  await user.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(api.createInbound).toHaveBeenCalledTimes(1));
  expect(api.createInbound).toHaveBeenCalledWith(
    expect.objectContaining({
      protocol: "vless",
      transmission: "httpupgrade",
      httpUpgradePath: "/upgrade",
      httpUpgradeHost: "edge.example.com",
    }),
  );
});
