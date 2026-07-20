import { auth } from "@/auth";
import { Dashboard } from "@/components/Dashboard";

// Server component: session is already verified by middleware.ts before we
// even get here, but we re-check to get the user's email for display and to
// satisfy TypeScript — never trust the client for this.
export default async function Home() {
  const session = await auth();
  if (!session?.user) return null; // middleware already redirected

  return <Dashboard userEmail={session.user.email ?? ""} />;
}
