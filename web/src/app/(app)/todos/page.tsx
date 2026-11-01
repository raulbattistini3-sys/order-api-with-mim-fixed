"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { Alert, Button, Card, Field } from "@/components/ui";
import { api } from "@/lib/api";
import type { Todo } from "@/lib/types";

const KEY = ["todos"];

export default function TodosPage() {
  const client = useQueryClient();
  const [title, setTitle] = useState("");

  const todos = useQuery({
    queryKey: KEY,
    queryFn: async () => (await api<Todo[]>("todos")).data,
  });

  const refresh = () => client.invalidateQueries({ queryKey: KEY });

  const add = useMutation({
    mutationFn: (value: string) => api<Todo>("todos", { method: "POST", body: JSON.stringify({ title: value }) }),
    onSuccess: () => {
      setTitle("");
      refresh();
    },
  });

  // Optimistic: the box ticks at once and rolls back if the request fails.
  const toggle = useMutation({
    mutationFn: (todo: Todo) =>
      api<Todo>(`todos/${todo.id}`, { method: "PATCH", body: JSON.stringify({ done: !todo.done }) }),
    onMutate: async (todo) => {
      await client.cancelQueries({ queryKey: KEY });
      const previous = client.getQueryData<Todo[]>(KEY);
      client.setQueryData<Todo[]>(KEY, (list) =>
        list?.map((t) => (t.id === todo.id ? { ...t, done: !t.done } : t)),
      );
      return { previous };
    },
    onError: (_error, _todo, context) => client.setQueryData(KEY, context?.previous),
    onSettled: refresh,
  });

  const remove = useMutation({
    mutationFn: (id: string) => api(`todos/${id}`, { method: "DELETE" }),
    onSuccess: refresh,
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    add.mutate(title);
  }

  const error = add.error ?? toggle.error ?? remove.error;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Tarefas</h1>
      <Card>
        <form onSubmit={submit} className="flex items-end gap-3">
          <div className="flex-1">
            <Field label="Nova tarefa" required maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />
          </div>
          <Button type="submit" disabled={add.isPending}>
            Adicionar
          </Button>
        </form>
        {error && (
          <div className="mt-3">
            <Alert>{error.message}</Alert>
          </div>
        )}
      </Card>

      <Card>
        {todos.isPending && <p className="text-sm text-muted">Carregando…</p>}
        {todos.isError && <Alert>Não foi possível carregar as tarefas.</Alert>}
        {todos.data?.length === 0 && <p className="text-sm text-muted">Nenhuma tarefa ainda.</p>}
        {!!todos.data?.length && (
          <ul className="divide-y divide-border">
            {todos.data.map((todo) => (
              <li key={todo.id} className="flex items-center gap-3 py-2.5 text-sm">
                <input
                  type="checkbox"
                  aria-label={`Concluir ${todo.title}`}
                  className="size-4 accent-[var(--accent)]"
                  checked={todo.done}
                  onChange={() => toggle.mutate(todo)}
                />
                <span className={`flex-1 ${todo.done ? "text-muted line-through" : ""}`}>{todo.title}</span>
                <Button variant="ghost" className="!px-2.5 !py-1" onClick={() => remove.mutate(todo.id)}>
                  Excluir
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
