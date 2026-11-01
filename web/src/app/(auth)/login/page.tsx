import { LoginForm } from "./login-form";

export const metadata = { title: "Entrar · orderguard" };

export default async function LoginPage({ searchParams }: PageProps<"/login">) {
  const { next, reset } = await searchParams;
  return <LoginForm next={typeof next === "string" ? next : "/dashboard"} wasReset={reset === "1"} />;
}
