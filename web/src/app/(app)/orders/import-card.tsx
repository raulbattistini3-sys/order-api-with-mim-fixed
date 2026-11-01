"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Alert, Button, Card } from "@/components/ui";
import { api } from "@/lib/api";

type Summary = {
  total: number;
  created: number;
  duplicates: number;
  failed: number;
  skipped: { line: number; message: string }[] | null;
};

// Large files answer 202 at once with a job, which is then polled.
type Job = Summary & { id: string; status: "queued" | "running" | "done" | "failed"; processed: number };

const SAMPLE = "item,quantity\nTeclado mecânico,1\nMouse sem fio,2\nMonitor 27 polegadas,1\n";

const finished = (job?: Job) => job?.status === "done" || job?.status === "failed";

async function importFile(file: File): Promise<Summary | Job> {
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
  return data;
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

  const queued = upload.data && "id" in upload.data ? upload.data : undefined;
  const job = useQuery({
    queryKey: ["import-job", queued?.id],
    enabled: !!queued,
    queryFn: async () => (await api<Job>(`orders/import/${queued?.id}`)).data,
    refetchInterval: (query) => (finished(query.state.data) ? false : 1000),
  });

  // The job saves its progress every 1000 rows, so the table refreshes then too.
  const processed = job.data?.processed;
  const status = job.data?.status;
  useEffect(() => {
    if (processed !== undefined) onImported();
  }, [processed, status, onImported]);

  function downloadSample() {
    const url = URL.createObjectURL(new Blob([SAMPLE], { type: "text/csv" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "modelo-pedidos.csv";
    a.click();
    URL.revokeObjectURL(url);
  }

  const current = queued ? (job.data ?? queued) : undefined;
  const s = current ? (finished(current) ? current : undefined) : (upload.data as Summary | undefined);

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
      {current && !finished(current) && (
        <p className="mt-4 text-sm text-muted">
          Planilha na fila. {current.processed} de {current.total} linha(s) processada(s). Você pode continuar usando o app.
        </p>
      )}
      {current?.status === "failed" && (
        <div className="mt-3">
          <Alert>Não foi possível processar a planilha.</Alert>
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
