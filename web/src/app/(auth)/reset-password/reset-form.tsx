"use client";

import { useActionState } from "react";
import { resetPassword, type FormState } from "@/app/actions/auth";
import { Alert, Button, Card, Field } from "@/components/ui";

export function ResetForm({ token }: { token: string }) {
  const [state, action, pending] = useActionState(resetPassword, {} as FormState);

  return (
    <Card>
      <h1 className="mb-5 text-xl font-semibold">Redefinir senha</h1>
      <form action={action} className="space-y-4">
        <input type="hidden" name="token" value={token} />
        <Field label="Nova senha" name="password" type="password" autoComplete="new-password" required minLength={10} maxLength={72} hint="Pelo menos 10 caracteres." />
        <Field label="Confirmar nova senha" name="confirm" type="password" autoComplete="new-password" required />
        {state.error && <Alert>{state.error}</Alert>}
        <Button type="submit" disabled={pending} className="w-full">
          {pending ? "Salvando…" : "Salvar nova senha"}
        </Button>
      </form>
    </Card>
  );
}
