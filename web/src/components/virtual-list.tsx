"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useRef, type ReactNode } from "react";

// How close to the last loaded row, in rows, the next page starts loading.
const LOAD_AHEAD = 30;

type Props<T> = {
  items: T[];
  getKey: (item: T) => string;
  renderRow: (item: T) => ReactNode;
  rowHeight?: number;
  height?: number;
  hasMore?: boolean;
  loadingMore?: boolean;
  onEndReached?: () => void;
};

/** Renders only the rows in view, so the list length doesn't cost the browser anything. */
export function VirtualList<T>({
  items,
  getKey,
  renderRow,
  rowHeight = 44,
  height = 480,
  hasMore = false,
  loadingMore = false,
  onEndReached,
}: Props<T>) {
  const scroller = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => rowHeight,
    overscan: 10,
  });
  const rows = virtualizer.getVirtualItems();
  const lastVisible = rows.at(-1)?.index ?? -1;

  useEffect(() => {
    if (hasMore && !loadingMore && lastVisible >= items.length - LOAD_AHEAD) onEndReached?.();
  }, [hasMore, loadingMore, lastVisible, items.length, onEndReached]);

  return (
    <div ref={scroller} className="overflow-y-auto" style={{ maxHeight: height }}>
      <ul className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {rows.map((row) => (
          <li
            key={getKey(items[row.index])}
            className="absolute inset-x-0 top-0 border-b border-border"
            style={{ height: row.size, transform: `translateY(${row.start}px)` }}
          >
            {renderRow(items[row.index])}
          </li>
        ))}
      </ul>
    </div>
  );
}
