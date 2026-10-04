import { Button } from "./Button";
import { t } from "../i18n";

export function BillingPage({ onExport, onArchive }: Props) {
  return (
    <section>
      <h1>{t("billing.title")}</h1>
      <Button onClick={onExport}>Download report</Button>
      <Button variant="secondary" onClick={onArchive}>
        Archive all invoices
      </Button>
    </section>
  );
}
