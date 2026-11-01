"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { Alert } from "@/components/ui";
import { VirtualList } from "@/components/virtual-list";
import { api } from "@/lib/api";
import type { Order, OrderPage } from "@/lib/types";

const PAGE_SIZE = 100;

function OrderRow(order: Order) {
  return (
    <div className="flex h-full items-center justify-between gap-4 text-sm">
      <span className="truncate">{order.item}</span>
      <span className="shrink-0 text-muted">
        {order.quantity} un. · {new Date(order.created.replace(" ", "T")).toLocaleString("pt-BR")}
      </span>
    </div>
  );
}

export function OrdersTable() {
  const orders = useInfiniteQuery({
    queryKey: ["orders"],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : "";
      return (await api<OrderPage>(`orders?limit=${PAGE_SIZE}${cursor}`)).data;
    },
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const items = useMemo(() => orders.data?.pages.flatMap((page) => page.orders) ?? [], [orders.data]);

  if (orders.isPending) return <p className="text-sm text-muted">Carregando…</p>;
  if (orders.isError) return <Alert>Não foi possível carregar os pedidos.</Alert>;
  if (items.length === 0) return <p className="text-sm text-muted">Nenhum pedido ainda.</p>;

  return (
    <VirtualList
      items={items}
      getKey={(order) => order.id}
      renderRow={OrderRow}
      hasMore={orders.hasNextPage}
      loadingMore={orders.isFetchingNextPage}
      onEndReached={orders.fetchNextPage}
    />
  );
}
