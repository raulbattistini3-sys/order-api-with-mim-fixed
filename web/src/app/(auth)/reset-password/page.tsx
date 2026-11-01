import Link from "next/link";
import { Alert, Card } from "@/components/ui";
import { ResetForm } from "./reset-form";

export const metadata = { title: "Redefinir senha · orderguard" };

export default async function ResetPasswordPage({ searchParams }: PageProps<"/reset-password">) {
  const { token } = await searchParams;
  if (typeof token !== "string" || !token) {
    return (
      <Card>
        <Alert>Link inválido.</Alert>
        <Link href="/forgot-password" className="mt-3 block text-sm font-medium text-accent">
          Pedir um novo link
        </Link>
      </Card>
    );
  }
  return <ResetForm token={token} />;
}
