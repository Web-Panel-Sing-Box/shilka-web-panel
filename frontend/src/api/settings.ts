import { apiGet, apiPost, apiPut } from "./client";

export type SettingsDTO = Record<string, string>;

export function getSettings(): Promise<SettingsDTO> {
  return apiGet<SettingsDTO>("/settings");
}

export function saveSettings(settings: SettingsDTO): Promise<{ ok: string }> {
  return apiPut<{ ok: string }>("/settings", settings);
}

export type NotificationLastTest = {
  channel: string;
  ok: boolean;
  message: string;
  at: string;
};

export type NotificationSettingsDTO = {
  smtp: {
    enabled: boolean;
    host: string;
    port: number;
    mode: "starttls" | "tls";
    from: string;
    recipients: string[];
    username: string;
    passwordConfigured: boolean;
    timeoutSec: number;
  };
  telegram: {
    enabled: boolean;
    apiBase: string;
    botTokenConfigured: boolean;
    chatId: string;
    timeoutSec: number;
  };
  quotaWarningPercent: number;
  expiryWarningHours: number;
  lastTest: NotificationLastTest;
};

export type NotificationSettingsUpdate = {
  smtp: Omit<NotificationSettingsDTO["smtp"], "passwordConfigured"> & {
    password: string;
    clearPassword: boolean;
  };
  telegram: Omit<NotificationSettingsDTO["telegram"], "botTokenConfigured"> & {
    botToken: string;
    clearBotToken: boolean;
  };
  quotaWarningPercent: number;
  expiryWarningHours: number;
};

export function getNotificationSettings(): Promise<NotificationSettingsDTO> {
  return apiGet<NotificationSettingsDTO>("/settings/notifications");
}

export function saveNotificationSettings(settings: NotificationSettingsUpdate): Promise<NotificationSettingsDTO> {
  return apiPut<NotificationSettingsDTO>("/settings/notifications", settings);
}

export function testNotification(channel: "smtp" | "telegram"): Promise<NotificationLastTest> {
  return apiPost<NotificationLastTest>("/settings/notifications/test", { channel });
}
