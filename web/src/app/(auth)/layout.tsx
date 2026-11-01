export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <main className="flex flex-1 items-center justify-center px-4 py-10">
      <div className="w-full max-w-md">
        <p className="mb-6 text-center font-mono text-sm font-semibold tracking-tight text-accent">orderguard</p>
        {children}
      </div>
    </main>
  );
}
