import { Button } from "./Button";

export function BillingPage({ exportCsv, archive }: Props) {
  return (
    <section>
      <h1>Billing</h1>
      <Button onClick={exportCsv}>Download report</Button>
      <Button variant="secondary" onClick={archive}>
        Archive all invoices
      </Button>
    </section>
  );
}
