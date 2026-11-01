"use client";

import { useMutation } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { Alert, Button, Card } from "@/components/ui";

type Summary = {
  total: number;
  created: number;
  duplicates: number;
  failed: number;
  skipped: { line: number; message: string }[] | null;
};

const SAMPLE = "item,quantity\nTeclado mecânico,1\nMouse sem fio,2\nMonitor 27 polegadas,1\n";

async function importFile(file: File): Promise<Summary> {
  const res = await fetch("/api/me/orders/import", {
    method: "POST",
    headers: { "Content-Type": file.type || "application/octet-stream" },
    body: file,
  });
  if (res.status === 401) {
    // Full reload so the next user can't see this user's cached queries.
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.assign("/login?next=/orders");
    throw new Error("Sessão expirada.");
  }
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error(data?.error ?? "Não foi possível importar a planilha.");
  return data as Summary;
}

export function ImportCard({ onImported }: { onImported: () => void }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);

  const upload = useMutation({
    mutationFn: importFile,
    onSuccess: () => {
      setFile(null);
      if (inputRef.current) inputRef.current.value = "";
      onImported();
    },
  });

  function downloadSample() {
    const url = URL.createObjectURL(new Blob([SAMPLE], { type: "text/csv" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "modelo-pedidos.csv";
    a.click();
    URL.revokeObjectURL(url);
  }

  const s = upload.data;

  return (
    <Card title="Importar planilha">
      <p className="mb-3 text-sm text-muted">
        Envie um <code>.csv</code> ou <code>.xlsx</code> com as colunas <strong>item</strong> e{" "}
        <strong>quantity</strong>. Reenviar a mesma planilha não duplica pedidos.
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <input
          ref={inputRef}
          type="file"
          accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
          onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          className="text-sm text-muted file:mr-3 file:rounded-lg file:border file:border-border file:bg-background file:px-3 file:py-1.5 file:text-sm file:text-ink"
        />
        <Button type="button" disabled={!file || upload.isPending} onClick={() => file && upload.mutate(file)}>
          {upload.isPending ? "Importando…" : "Importar"}
        </Button>
        <Button type="button" variant="ghost" onClick={downloadSample}>
          Baixar modelo
        </Button>
      </div>

      {upload.isError && (
        <div className="mt-3">
          <Alert>{(upload.error as Error).message}</Alert>
        </div>
      )}
      {s && (
        <div className="mt-4 space-y-2 text-sm">
          <p className="text-ink">
            <strong>{s.created}</strong> criado(s) · <strong>{s.duplicates}</strong> já existia(m) ·{" "}
            <strong>{s.failed}</strong> com erro · {s.total} linha(s) válida(s)
          </p>
          {!!s.skipped?.length && (
            <details>
              <summary className="cursor-pointer text-muted">{s.skipped.length} linha(s) ignorada(s)</summary>
              <ul className="mt-2 space-y-1 text-muted">
                {s.skipped.slice(0, 20).map((row) => (
                  <li key={row.line}>
                    Linha {row.line}: {row.message}
                  </li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}
    </Card>
  );
}
