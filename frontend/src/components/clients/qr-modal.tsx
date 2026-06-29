
import { Modal, ModalBody, ModalHeader } from "@/components/ui/modal";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/lib/i18n";

type QrModalProps = {
  open: boolean;
  onClose: () => void;
  qrPng: string;
  payload: string;
  loading?: boolean;
};

export function QrModal({ open, onClose, qrPng, payload, loading = false }: QrModalProps) {
  const { t } = useI18n();

  return (
    <Modal open={open} onClose={onClose} width="max-w-[380px]">
      <ModalHeader title={t("clients.shareQr")} onClose={onClose} />
      <ModalBody className="flex flex-col items-center gap-4 pb-6">
        {loading || !qrPng ? (
          <div
            className="grid size-[252px] place-items-center rounded-2xl bg-white/5 text-xs text-ink-tertiary"
            aria-label={t("clients.qrLoading")}
          >
            {t("clients.qrLoading")}
          </div>
        ) : (
          <div className="rounded-2xl bg-white p-4">
            <img src={qrPng} alt={t("clients.shareQrAlt")} width={220} height={220} />
          </div>
        )}
        {payload ? (
          <p className="break-all text-center font-mono text-[11px] text-ink-tertiary">{payload}</p>
        ) : null}
        <Button variant="secondary" onClick={onClose} className="w-full">
          {t("common.close")}
        </Button>
      </ModalBody>
    </Modal>
  );
}
