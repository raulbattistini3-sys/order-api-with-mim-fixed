"use client";

import Link from "next/link";
import { useActionState } from "react";
import { login, type FormState } from "@/app/actions/auth";
import { Alert, Button, Card, Field } from "@/components/ui";

export function LoginForm({ next, wasReset }: { next: string; wasReset: boolean }) {
  const [state, action, pending] = useActionState(login, {} as FormState);

  return (
    <Card>
      <h1 className="mb-1 text-xl font-semibold">Entrar</h1>
      <p className="mb-5 text-sm text-muted">Acesse sua conta para fazer pedidos.</p>
      <form action={action} className="space-y-4">
        <input type="hidden" name="next" value={next} />
        <Field label="E-mail" name="email" type="email" autoComplete="email" required defaultValue={String(state.values?.email ?? "")} />
        <Field label="Senha" name="password" type="password" autoComplete="current-password" required />
        {wasReset && !state.error && <Alert tone="success">Senha redefinida. Entre com a nova senha.</Alert>}
        {state.error && <Alert>{state.error}</Alert>}
        <Button type="submit" disabled={pending} className="w-full">
          {pending ? "Entrando…" : "Entrar"}
        </Button>
      </form>
      <div className="mt-5 flex justify-between text-sm">
        <Link href="/forgot-password" className="text-muted hover:text-ink">
          Esqueci minha senha
        </Link>
        <Link href="/register" className="font-medium text-accent">
          Criar conta
        </Link>
      </div>
    </Card>
  );
}
