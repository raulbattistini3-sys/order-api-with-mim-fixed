"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { OrderSummary as Summary } from "@/lib/types";

const number = new Intl.NumberFormat("pt-BR");

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="text-xl font-semibold">{number.format(value)}</dd>
    </div>
  );
}

export function OrderSummary() {
  const summary = useQuery({
    queryKey: ["order-summary"],
    queryFn: async () => (await api<Summary>("orders/summary")).data,
  });
  const data = summary.data;
  if (!data) return null;

  return (
    <div className="mb-4 space-y-3 border-b border-border pb-4">
      <dl className="grid grid-cols-3 gap-4">
        <Stat label="Pedidos" value={data.orders} />
        <Stat label="Unidades" value={data.units} />
        <Stat label="Itens diferentes" value={data.items} />
      </dl>
      {data.top_items.length > 0 && (
        <p className="truncate text-sm text-muted">
          Mais pedidos: {data.top_items.map((top) => `${top.item} (${number.format(top.units)})`).join(" · ")}
        </p>
      )}
    </div>
  );
}
