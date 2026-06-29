import { afterEach, expect, test, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import * as api from "@/api";
import { ClientDetailModal } from "@/components/clients/client-detail-modal";
import { renderWithProviders } from "@/test/test-utils";
import type { ClientDTO } from "@/api/clients";

const CLIENT: ClientDTO = {
  id: "9",
  name: "carol",
  uuid: "81514c35-8f9a-4785-9afc-013bb4f0f13e",
  inboundId: "7",
  inboundIds: ["7"],
  usedDown: 0,
  usedUp: 0,
  totalQuota: 0,
  expiry: "",
  status: "active",
  subscription: "/sub/tok",
  startAfterFirstUse: false,
  online: false,
};

afterEach(() => {
  vi.restoreAllMocks();
});

test("loads and renders backend QR PNG for the share link", async () => {
  const user = userEvent.setup();
  vi.spyOn(api, "listNodes").mockResolvedValue([]);
  const getLinks = vi.spyOn(api, "getClientLinks").mockResolvedValue({
    link: "vless://client-link",
    shareLink: "vless://client-link",
    subscription: "/sub/tok",
    qrPng: "data:image/png;base64,iVBORw0KGgo=",
    links: [],
  });

  renderWithProviders(<ClientDetailModal client={CLIENT} onClose={() => {}} />, {
    seed: { clients: [CLIENT] },
  });

  await user.click(screen.getByRole("button", { name: "Get QR" }));

  await waitFor(() => expect(getLinks).toHaveBeenCalledWith("9"));
  expect(await screen.findByRole("img", { name: "Scannable client share QR code" }))
    .toHaveAttribute("src", "data:image/png;base64,iVBORw0KGgo=");
  expect(screen.getByText("vless://client-link")).toBeInTheDocument();
  expect(document.querySelector("svg[aria-label*='QR']")).not.toBeInTheDocument();
});
