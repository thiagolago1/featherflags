"use client";

import { useState, type FormEvent } from "react";
import { signIn } from "next-auth/react";

export default function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const res = await signIn("credentials", { email, password, redirect: false });
    setBusy(false);
    if (res?.error) {
      setError("Invalid email or password.");
      return;
    }
    window.location.href = "/";
  }

  return (
    <div className="gate">
      <form onSubmit={submit}>
        <div className="wordmark">
          <span className="feather">🪶</span> featherflags
        </div>
        <p>Sign in to open the dashboard.</p>
        <input
          type="email"
          placeholder="Email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoFocus
          required
        />
        <input
          type="password"
          placeholder="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        {error && <div className="error">{error}</div>}
        <button className="btn btn-primary" type="submit" disabled={busy}>
          Sign in
        </button>
        {process.env.NEXT_PUBLIC_SSO_ENABLED === "true" && (
          <button
            type="button"
            className="btn"
            style={{ marginTop: 8 }}
            onClick={() => void signIn("oidc", { callbackUrl: "/" })}
          >
            Sign in with SSO
          </button>
        )}
      </form>
    </div>
  );
}
