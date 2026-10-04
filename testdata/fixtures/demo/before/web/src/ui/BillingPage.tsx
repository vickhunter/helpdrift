import { Button } from "./Button";
import { t } from "../i18n";

export function BillingPage({ onExport, onArchive }: Props) {
  return (
    <section>
      <h1>{t("billing.title")}</h1>
      <Button onClick={onExport}>Export CSV</Button>
      <Button variant="secondary" onClick={onArchive}>
        Archive invoices
      </Button>
    </section>
  );
}
