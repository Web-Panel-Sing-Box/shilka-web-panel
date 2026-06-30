import { useEffect, useState } from "react";

import { getMe, getSettings, saveSettings } from "@/api";
import { ChangePasswordModal } from "@/components/auth/change-password-modal";
import { TwoFactorDisableModal } from "@/components/auth/two-factor-disable-modal";
import { TwoFactorSetupModal } from "@/components/auth/two-factor-setup-modal";
import { ScheduledTaskList } from "@/components/settings/scheduled-task-list";
import { NotificationSettings } from "@/components/settings/notification-settings";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardTitle } from "@/components/ui/card";
import { Input, Label } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Toggle } from "@/components/ui/toggle";
import { useToast } from "@/components/ui/toast";
import { useDisclosure } from "@/hooks/useDisclosure";
import { useAuth } from "@/lib/auth";
import { useI18n, type Language } from "@/lib/i18n";

const LANGUAGE_OPTIONS: { value: Language; label: string }[] = [
  { value: "en", label: "English" },
  { value: "ru", label: "Русский" },
];

const LOG_LEVELS = [
  { value: "debug", label: "Debug" },
  { value: "info", label: "Info" },
  { value: "warn", label: "Warn" },
  { value: "error", label: "Error" },
];

export function SettingsPage() {
  const { push } = useToast();
  const { language, setLanguage, t } = useI18n();
  const { twoFactorEnabled, setTwoFactorEnabled } = useAuth();
  const twoFactorSetup = useDisclosure();
  const twoFactorDisable = useDisclosure();
  const changePassword = useDisclosure();
  const [username, setUsername] = useState("admin");
  const [panelName, setPanelName] = useState("Shilka");
  const [binaryPath, setBinaryPath] = useState("/usr/local/bin/sing-box");
  const [logLevel, setLogLevel] = useState("info");
  const [publicHost, setPublicHost] = useState("");
  const [ttl, setTtl] = useState("72h");

  useEffect(() => {
    getSettings()
      .then((s) => {
        if (s.panel_name) setPanelName(s.panel_name);
        if (s.binary_path) setBinaryPath(s.binary_path);
        if (s.log_level) setLogLevel(s.log_level);
        if (s.sub_public_url) setPublicHost(s.sub_public_url);
        if (s.token_ttl) setTtl(s.token_ttl);
      })
      .catch(() => {
        push(t("settings.loadError"), "error");
      });
    getMe()
      .then((me) => {
        if (me.username) setUsername(me.username);
      })
      .catch(() => {});
  }, []);

  const save = async () => {
    try {
      await saveSettings({
        panel_name: panelName,
        binary_path: binaryPath,
        log_level: logLevel,
        sub_public_url: publicHost,
        token_ttl: ttl,
      });
      push(t("settings.saved"), "success");
    } catch {
      push(t("settings.saveError"), "error");
    }
  };

  const handleTwoFactorToggle = (next: boolean) => {
    if (next) {
      twoFactorSetup.open();
    } else {
      twoFactorDisable.open();
    }
  };

  return (
    <div className="mx-auto flex max-w-[920px] flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-2xl font-semibold text-ink-primary">
          {t("settings.title")}
        </h2>
        <Button variant="white" onClick={save}>
          {t("common.save")}
        </Button>
      </div>

      <Section title={t("settings.general")}>
        <Row label={t("settings.panelName")} hint={t("settings.panelNameHint")}>
          <Input
            value={panelName}
            onChange={(e) => setPanelName(e.target.value)}
          />
        </Row>
        <Row label={t("settings.language")} hint={t("settings.languageHint")}>
          <Select<Language>
            value={language}
            options={LANGUAGE_OPTIONS}
            onChange={setLanguage}
          />
        </Row>
      </Section>

      <Section title={t("settings.security")}>
        <Row label={t("settings.adminUsername")} hint={t("settings.readOnly")}>
          <Input value={username} readOnly mono />
        </Row>
        <Row label={t("settings.twoFactor")} hint={t("settings.twoFactorHint")}>
          <Toggle checked={twoFactorEnabled} onChange={handleTwoFactorToggle} />
        </Row>
        <Row
          label={t("settings.changePassword")}
          hint={t("settings.changePasswordHint")}
        >
          <Button variant="secondary" onClick={changePassword.open}>
            {t("settings.changePassword")}
          </Button>
        </Row>
      </Section>

      <Section title={t("settings.singBox")}>
        <Row
          label={t("settings.binaryPath")}
          hint={t("settings.binaryPathHint")}
        >
          <Input
            value={binaryPath}
            onChange={(e) => setBinaryPath(e.target.value)}
            mono
          />
        </Row>
        <Row label={t("settings.logLevel")} hint={t("settings.logLevelHint")}>
          <Select
            value={logLevel}
            options={LOG_LEVELS}
            onChange={setLogLevel}
          />
        </Row>
        <Row label={t("settings.clashPort")} hint={t("settings.boundLocal")}>
          <Input value="9090" readOnly mono />
        </Row>
        <Row label={t("settings.v2rayPort")} hint={t("settings.boundLocal")}>
          <Input value="9091" readOnly mono />
        </Row>
      </Section>

      <Section title={t("settings.subscriptions")}>
        <Row
          label={t("settings.publicBaseUrl")}
          hint={t("settings.publicBaseUrlHint")}
        >
          <Input
            value={publicHost}
            onChange={(e) => setPublicHost(e.target.value)}
            mono
          />
        </Row>
        <Row label={t("settings.tokenTtl")} hint={t("settings.tokenTtlHint")}>
          <Input value={ttl} onChange={(e) => setTtl(e.target.value)} mono />
        </Row>
      </Section>

      <Section title={t("settings.notifications.title")}>
        <NotificationSettings />
      </Section>

      <Section title={t("settings.tasks.title")}>
        <ScheduledTaskList />
      </Section>

      <TwoFactorSetupModal
        open={twoFactorSetup.isOpen}
        onClose={twoFactorSetup.close}
        onConfirmed={() => setTwoFactorEnabled(true)}
      />

      <TwoFactorDisableModal
        open={twoFactorDisable.isOpen}
        onClose={twoFactorDisable.close}
        onDisabled={() => setTwoFactorEnabled(false)}
      />

      <ChangePasswordModal open={changePassword.isOpen} onClose={changePassword.close} />
    </div>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <div className="space-y-4">{children}</div>
    </Card>
  );
}

function Row({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-1 items-start gap-3 sm:grid-cols-[1fr_320px]">
      <div className="space-y-0.5">
        <Label>{label}</Label>
        {hint ? <p className="text-xs text-ink-tertiary">{hint}</p> : null}
      </div>
      <div>{children}</div>
    </div>
  );
}
