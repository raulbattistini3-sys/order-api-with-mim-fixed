"use client";

import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { useCallback, useRef, useState, type FormEvent } from "react";
import { Alert, Button, Card, Field } from "@/components/ui";
import { ImportCard } from "./import-card";
import { OrderSummary } from "./order-summary";
import { OrdersTable } from "./orders-table";
import { api, ApiError, type ApiResult } from "@/lib/api";
import type { Order, OrderPage } from "@/lib/types";

type Draft = { item: string; quantity: number };

export default function OrdersPage() {
  const client = useQueryClient();
  const [item, setItem] = useState("");
  const [quantity, setQuantity] = useState(1);
  const [notice, setNotice] = useState("");
  // One key per order the user means to place. A retry reuses it, and changing
  // the form makes it a different order.
  const attempt = useRef<{ body: string; key: string } | null>(null);

  // New orders land at the top, so drop to the first page before refetching.
  // Otherwise every progress tick of an import would reload all the loaded pages.
  const refreshOrders = useCallback(() => {
    client.setQueryData<InfiniteData<OrderPage, string>>(["orders"], (data) =>
      data && { pages: data.pages.slice(0, 1), pageParams: data.pageParams.slice(0, 1) },
    );
    client.invalidateQueries({ queryKey: ["orders"] });
    client.invalidateQueries({ queryKey: ["order-summary"] });
  }, [client]);

  function keyFor(draft: Draft) {
    const body = JSON.stringify(draft);
    if (attempt.current?.body !== body) attempt.current = { body, key: crypto.randomUUID() };
    return attempt.current.key;
  }

  const place = useMutation({
    mutationFn: (draft: Draft) =>
      api<Order>("orders", {
        method: "POST",
        headers: { "Idempotency-Key": keyFor(draft) },
        body: JSON.stringify(draft),
      }),
    // 409 means this key is still in flight, so asking again is safe.
    retry: (count, error) => error instanceof ApiError && error.status === 409 && count < 5,
    retryDelay: (count) => 250 * (count + 1),
    onSuccess: (result: ApiResult<Order>) => {
      setNotice(result.replayed ? "Este pedido já tinha sido registrado." : "Pedido criado.");
      attempt.current = null;
      setItem("");
      setQuantity(1);
      refreshOrders();
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    place.mutate({ item, quantity });
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Pedidos</h1>
      <Card title="Novo pedido">
        <form onSubmit={submit} className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <Field label="Item" required maxLength={200} value={item} onChange={(e) => setItem(e.target.value)} />
          <Field
            label="Quantidade"
            type="number"
            required
            min={1}
            max={10000}
            value={quantity}
            onChange={(e) => setQuantity(e.target.valueAsNumber)}
          />
          <div className="sm:col-span-2">
            <Button type="submit" disabled={place.isPending}>
              {place.isPending ? "Enviando…" : "Fazer pedido"}
            </Button>
          </div>
          {notice && !place.isError && (
            <div className="sm:col-span-2">
              <Alert tone="success">{notice}</Alert>
            </div>
          )}
          {place.isError && (
            <div className="sm:col-span-2">
              <Alert>{(place.error as Error).message}</Alert>
            </div>
          )}
        </form>
      </Card>

      <ImportCard onImported={refreshOrders} />

      <Card title="Seus pedidos">
        <OrderSummary />
        <OrdersTable />
      </Card>
    </div>
  );
}
