import { afterEach, expect, test, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { SettingsPage } from "@/pages/SettingsPage";
import { renderWithProviders } from "@/test/test-utils";

const { notificationSettings, saveNotificationSettings, testNotification } = vi.hoisted(() => {
  const notificationSettings = {
    smtp: { enabled: false, host: "", port: 587, mode: "starttls" as const, from: "", recipients: [], username: "", passwordConfigured: true, timeoutSec: 10 },
    telegram: { enabled: false, apiBase: "https://api.telegram.org", botTokenConfigured: true, chatId: "", timeoutSec: 10 },
    quotaWarningPercent: 80,
    expiryWarningHours: 24,
    lastTest: { channel: "", ok: false, message: "", at: "" },
  };
  return {
    notificationSettings,
    saveNotificationSettings: vi.fn().mockResolvedValue(notificationSettings),
    testNotification: vi.fn().mockResolvedValue({ channel: "telegram", ok: true, message: "sent", at: "2026-06-30T00:00:00Z" }),
  };
});

vi.mock("@/api/settings", () => ({
  getSettings: vi.fn().mockResolvedValue({}),
  saveSettings: vi.fn().mockResolvedValue({ ok: "saved" }),
  getNotificationSettings: vi.fn().mockResolvedValue(notificationSettings),
  saveNotificationSettings,
  testNotification,
}));

afterEach(() => {
  window.localStorage.clear();
  vi.clearAllMocks();
});

test("awaits notification save before test and renders persisted result", async () => {
  const user = userEvent.setup();
  renderWithProviders(<SettingsPage />);

  await user.click(await screen.findByRole("button", { name: "Test Telegram" }));

  expect(saveNotificationSettings).toHaveBeenCalledTimes(1);
  expect(testNotification).toHaveBeenCalledWith("telegram");
  expect(await screen.findByText(/Last test: telegram - sent/)).toBeInTheDocument();
});

test("uses a single page-level save button and switches to Russian", async () => {
  const user = userEvent.setup();
  renderWithProviders(<SettingsPage />);

  expect(screen.getAllByRole("button", { name: "Save" })).toHaveLength(1);

  await user.click(screen.getByRole("button", { name: "English" }));
  await user.click(screen.getByRole("option", { name: "Русский" }));

  expect(
    screen.getByRole("heading", { name: "Настройки" }),
  ).toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: "Сохранить" })).toHaveLength(1);
});
