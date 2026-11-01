"use client";

import Link from "next/link";
import { useActionState } from "react";
import { forgotPassword, type FormState } from "@/app/actions/auth";
import { Alert, Button, Card, Field } from "@/components/ui";

export default function ForgotPasswordPage() {
  const [state, action, pending] = useActionState(forgotPassword, {} as FormState);

  return (
    <Card>
      <h1 className="mb-1 text-xl font-semibold">Esqueci minha senha</h1>
      <p className="mb-5 text-sm text-muted">Enviaremos um link para redefinir sua senha.</p>
      {state.success ? (
        <Alert tone="success">Se este e-mail tiver uma conta, o link chegará em instantes.</Alert>
      ) : (
        <form action={action} className="space-y-4">
          <Field label="E-mail" name="email" type="email" autoComplete="email" required defaultValue={String(state.values?.email ?? "")} />
          {state.error && <Alert>{state.error}</Alert>}
          <Button type="submit" disabled={pending} className="w-full">
            {pending ? "Enviando…" : "Enviar link"}
          </Button>
        </form>
      )}
      <p className="mt-5 text-center text-sm">
        <Link href="/login" className="text-muted hover:text-ink">
          Voltar para o login
        </Link>
      </p>
    </Card>
  );
}
