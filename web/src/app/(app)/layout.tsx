import Link from "next/link";
import { redirect } from "next/navigation";
import { logout } from "@/app/actions/auth";
import { Button } from "@/components/ui";
import { getMe } from "@/lib/session";

const LINKS = [
  ["/dashboard", "Início"],
  ["/orders", "Pedidos"],
  ["/todos", "Tarefas"],
];

// The real session check for every page in this group.
export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const me = await getMe();
  if (!me) redirect("/login");

  return (
    <>
      <header className="border-b border-border bg-surface">
        <div className="mx-auto flex max-w-4xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <Link href="/dashboard" className="font-mono text-sm font-semibold text-accent">
            orderguard
          </Link>
          <nav className="flex gap-4 text-sm">
            {LINKS.map(([href, label]) => (
              <Link key={href} href={href} className="text-muted hover:text-ink">
                {label}
              </Link>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <span className="hidden text-sm text-muted sm:inline">{me.user.name}</span>
            <form action={logout}>
              <Button type="submit" variant="ghost" className="!px-3 !py-1.5">
                Sair
              </Button>
            </form>
          </div>
        </div>
      </header>
      <main className="mx-auto w-full max-w-4xl flex-1 px-4 py-8">{children}</main>
    </>
  );
}
