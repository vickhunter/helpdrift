import { Button } from "./Button";

export function BillingPage({ exportCsv, archive }: Props) {
  return (
    <section>
      <h1>Billing</h1>
      <Button onClick={exportCsv}>Export CSV</Button>
      <Button variant="secondary" onClick={archive}>
        Archive invoices
      </Button>
    </section>
  );
}
