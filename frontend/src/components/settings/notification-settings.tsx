import { useEffect, useState } from "react";

import {
  ApiError,
  getNotificationSettings,
  saveNotificationSettings,
  testNotification,
  type NotificationLastTest,
  type NotificationSettingsDTO,
  type NotificationSettingsUpdate,
} from "@/api";
import { Button } from "@/components/ui/button";
import { Input, Label, NumberInput } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Toggle } from "@/components/ui/toggle";
import { useToast } from "@/components/ui/toast";
import { useI18n } from "@/lib/i18n";

type FormState = NotificationSettingsUpdate & {
  smtpPasswordConfigured: boolean;
  telegramTokenConfigured: boolean;
  lastTest: NotificationLastTest;
};

function toForm(settings: NotificationSettingsDTO): FormState {
  return {
    smtp: { ...settings.smtp, password: "", clearPassword: false },
    telegram: { ...settings.telegram, botToken: "", clearBotToken: false },
    quotaWarningPercent: settings.quotaWarningPercent,
    expiryWarningHours: settings.expiryWarningHours,
    smtpPasswordConfigured: settings.smtp.passwordConfigured,
    telegramTokenConfigured: settings.telegram.botTokenConfigured,
    lastTest: settings.lastTest,
  };
}

export function NotificationSettings() {
  const { push } = useToast();
  const { t } = useI18n();
  const smtpModes: { value: "starttls" | "tls"; label: string }[] = [
    { value: "starttls", label: t("settings.notifications.starttls") },
    { value: "tls", label: t("settings.notifications.implicitTls") },
  ];
  const [form, setForm] = useState<FormState | null>(null);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<"smtp" | "telegram" | null>(null);

  useEffect(() => {
    getNotificationSettings()
      .then((settings) => setForm(toForm(settings)))
      .catch(() => push(t("settings.notifications.loadError"), "error"));
  }, []);

  if (!form) {
    return <p className="text-sm text-ink-tertiary">{t("common.loading")}</p>;
  }

  const updateSMTP = (patch: Partial<FormState["smtp"]>) =>
    setForm((current) => current ? { ...current, smtp: { ...current.smtp, ...patch } } : current);
  const updateTelegram = (patch: Partial<FormState["telegram"]>) =>
    setForm((current) => current ? { ...current, telegram: { ...current.telegram, ...patch } } : current);

  const persist = async (toast = true) => {
    setSaving(true);
    try {
      const saved = await saveNotificationSettings({
        smtp: form.smtp,
        telegram: form.telegram,
        quotaWarningPercent: form.quotaWarningPercent,
        expiryWarningHours: form.expiryWarningHours,
      });
      setForm(toForm(saved));
      if (toast) push(t("settings.notifications.saved"), "success");
      return true;
    } catch (error) {
      push(apiMessage(error, t("settings.notifications.saveError")), "error");
      return false;
    } finally {
      setSaving(false);
    }
  };

  const sendTest = async (channel: "smtp" | "telegram") => {
    if (!(await persist(false))) return;
    setTesting(channel);
    try {
      const result = await testNotification(channel);
      setForm((current) => current ? { ...current, lastTest: result } : current);
      push(t("settings.notifications.testSent"), "success");
    } catch (error) {
      const result = testResult(error);
      if (result) setForm((current) => current ? { ...current, lastTest: result } : current);
      push(apiMessage(error, t("settings.notifications.testError")), "error");
    } finally {
      setTesting(null);
    }
  };

  return (
    <div className="space-y-7">
      <div className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <Label>{t("settings.notifications.smtp")}</Label>
            <p className="text-xs text-ink-tertiary">{t("settings.notifications.smtpHint")}</p>
          </div>
          <Toggle checked={form.smtp.enabled} onChange={(enabled) => updateSMTP({ enabled })} />
        </div>
        <Field label={t("settings.notifications.host")}>
          <Input value={form.smtp.host} onChange={(event) => updateSMTP({ host: event.target.value })} mono />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t("settings.notifications.port")}>
            <NumberInput value={String(form.smtp.port)} min={1} max={65535} onChange={(value) => updateSMTP({ port: Number(value || 0) })} mono />
          </Field>
          <Field label={t("settings.notifications.security")}>
            <Select<"starttls" | "tls"> value={form.smtp.mode} options={smtpModes} onChange={(mode) => updateSMTP({ mode })} />
          </Field>
        </div>
        <Field label={t("settings.notifications.timeout")}>
          <NumberInput value={String(form.smtp.timeoutSec)} min={1} max={60} onChange={(value) => updateSMTP({ timeoutSec: Number(value || 0) })} trailing="s" mono />
        </Field>
        <Field label={t("settings.notifications.sender")}>
          <Input value={form.smtp.from} onChange={(event) => updateSMTP({ from: event.target.value })} />
        </Field>
        <Field label={t("settings.notifications.recipients")} hint={t("settings.notifications.recipientsHint")}>
          <Input value={form.smtp.recipients.join(", ")} onChange={(event) => updateSMTP({ recipients: event.target.value.split(",").map((value) => value.trim()).filter(Boolean) })} />
        </Field>
        <Field label={t("settings.notifications.username")}>
          <Input value={form.smtp.username} onChange={(event) => updateSMTP({ username: event.target.value })} />
        </Field>
        <SecretField
          label={t("settings.notifications.password")}
          configured={form.smtpPasswordConfigured && !form.smtp.clearPassword}
          value={form.smtp.password}
          onChange={(password) => updateSMTP({ password, clearPassword: false })}
          onClear={() => {
            updateSMTP({ password: "", clearPassword: true });
            setForm((current) => current ? { ...current, smtpPasswordConfigured: false } : current);
          }}
          configuredLabel={t("settings.notifications.secretConfigured")}
          clearLabel={t("common.clear")}
        />
        <Button variant="secondary" loading={testing === "smtp"} onClick={async () => { await sendTest("smtp"); }}>
          {t("settings.notifications.testSmtp")}
        </Button>
      </div>

      <div className="space-y-4 border-t border-white/10 pt-6">
        <div className="flex items-center justify-between gap-3">
          <div>
            <Label>{t("settings.notifications.telegram")}</Label>
            <p className="text-xs text-ink-tertiary">{t("settings.notifications.telegramHint")}</p>
          </div>
          <Toggle checked={form.telegram.enabled} onChange={(enabled) => updateTelegram({ enabled })} />
        </div>
        <Field label={t("settings.notifications.apiBase")}>
          <Input value={form.telegram.apiBase} onChange={(event) => updateTelegram({ apiBase: event.target.value })} mono />
        </Field>
        <SecretField
          label={t("settings.notifications.botToken")}
          configured={form.telegramTokenConfigured && !form.telegram.clearBotToken}
          value={form.telegram.botToken}
          onChange={(botToken) => updateTelegram({ botToken, clearBotToken: false })}
          onClear={() => {
            updateTelegram({ botToken: "", clearBotToken: true });
            setForm((current) => current ? { ...current, telegramTokenConfigured: false } : current);
          }}
          configuredLabel={t("settings.notifications.secretConfigured")}
          clearLabel={t("common.clear")}
        />
        <Field label={t("settings.notifications.chatId")}>
          <Input value={form.telegram.chatId} onChange={(event) => updateTelegram({ chatId: event.target.value })} mono />
        </Field>
        <Field label={t("settings.notifications.timeout")}>
          <NumberInput value={String(form.telegram.timeoutSec)} min={1} max={60} onChange={(value) => updateTelegram({ timeoutSec: Number(value || 0) })} trailing="s" mono />
        </Field>
        <Button variant="secondary" loading={testing === "telegram"} onClick={async () => { await sendTest("telegram"); }}>
          {t("settings.notifications.testTelegram")}
        </Button>
      </div>

      <div className="grid gap-4 border-t border-white/10 pt-6 sm:grid-cols-2">
        <Field label={t("settings.notifications.quotaWarning")} hint={t("settings.notifications.zeroDisables")}>
          <NumberInput value={String(form.quotaWarningPercent)} min={0} max={100} onChange={(value) => setForm({ ...form, quotaWarningPercent: Number(value || 0) })} trailing="%" mono />
        </Field>
        <Field label={t("settings.notifications.expiryWarning")} hint={t("settings.notifications.zeroDisables")}>
          <NumberInput value={String(form.expiryWarningHours)} min={0} max={8760} onChange={(value) => setForm({ ...form, expiryWarningHours: Number(value || 0) })} trailing={t("settings.notifications.hours")} mono />
        </Field>
      </div>

      {form.lastTest.at ? (
        <p className={form.lastTest.ok ? "text-xs text-success" : "text-xs text-danger"}>
          {t("settings.notifications.lastTest")}: {form.lastTest.channel} - {form.lastTest.message}
        </p>
      ) : null}

      <Button variant="white" loading={saving} onClick={async () => { await persist(); }}>
        {t("settings.notifications.save")}
      </Button>
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return <div><Label hint={hint}>{label}</Label>{children}</div>;
}

function SecretField({ label, configured, value, onChange, onClear, configuredLabel, clearLabel }: {
  label: string;
  configured: boolean;
  value: string;
  onChange: (value: string) => void;
  onClear: () => void;
  configuredLabel: string;
  clearLabel: string;
}) {
  return (
    <Field label={label} hint={configured ? configuredLabel : undefined}>
      <div className="flex gap-2">
        <div className="min-w-0 flex-1">
          <Input type="password" value={value} placeholder={configured ? "••••••••" : ""} onChange={(event) => onChange(event.target.value)} />
        </div>
        {configured ? <Button variant="secondary" onClick={onClear}>{clearLabel}</Button> : null}
      </div>
    </Field>
  );
}

function apiMessage(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError) || !error.body || typeof error.body !== "object") return fallback;
  const body = error.body as { error?: unknown; message?: unknown };
  if (typeof body.error === "string") return body.error;
  if (typeof body.message === "string") return body.message;
  return fallback;
}

function testResult(error: unknown): NotificationLastTest | null {
  if (!(error instanceof ApiError) || !error.body || typeof error.body !== "object") return null;
  const body = error.body as Partial<NotificationLastTest>;
  return typeof body.channel === "string" && typeof body.ok === "boolean" && typeof body.message === "string" && typeof body.at === "string"
    ? body as NotificationLastTest
    : null;
}
