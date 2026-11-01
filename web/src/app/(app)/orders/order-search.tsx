"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { Alert, Field } from "@/components/ui";
import { VirtualList } from "@/components/virtual-list";
import { api, ApiError } from "@/lib/api";
import type { Order } from "@/lib/types";
import { OrderRow } from "./orders-table";

const TYPING_PAUSE_MS = 300;

/** Shows matches for what's typed, or children when the box is empty. */
export function OrderSearch({ children }: { children: ReactNode }) {
  const [text, setText] = useState("");
  const [term, setTerm] = useState("");

  useEffect(() => {
    const timer = setTimeout(() => setTerm(text.trim()), TYPING_PAUSE_MS);
    return () => clearTimeout(timer);
  }, [text]);

  const results = useQuery({
    queryKey: ["order-search", term],
    enabled: term !== "",
    placeholderData: keepPreviousData,
    retry: false,
    queryFn: async () => (await api<{ orders: Order[] }>(`orders/search?q=${encodeURIComponent(term)}&limit=100`)).data,
  });
  const matches = results.data?.orders ?? [];
  const unavailable = results.error instanceof ApiError && results.error.status === 503;

  return (
    <>
      <div className="mb-4">
        <Field label="Buscar item" type="search" maxLength={200} value={text} onChange={(e) => setText(e.target.value)} />
      </div>
      {term === "" && children}
      {term !== "" && results.isPending && <p className="text-sm text-muted">Buscando…</p>}
      {term !== "" && results.isError && (
        <Alert>{unavailable ? "A busca ainda não está configurada." : "Não foi possível buscar agora."}</Alert>
      )}
      {term !== "" && results.isSuccess && matches.length === 0 && (
        <p className="text-sm text-muted">Nenhum item encontrado.</p>
      )}
      {term !== "" && !results.isError && matches.length > 0 && (
        <VirtualList items={matches} getKey={(order) => order.id} renderRow={OrderRow} />
      )}
    </>
  );
}
