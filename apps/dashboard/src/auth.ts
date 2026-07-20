import NextAuth from "next-auth";
import Credentials from "next-auth/providers/credentials";
import type { OIDCConfig } from "next-auth/providers";
import { verify } from "@node-rs/argon2";
import { prisma } from "@/lib/prisma";

// Generic OIDC provider: point it at any standards-compliant IdP (Okta,
// Azure AD, Google Workspace, ...) via issuer discovery — no vendor lock-in
// while the company hasn't picked one. Only registered when the env vars
// are set, so credentials-only deployments don't need an IdP configured.
function oidcProvider(): OIDCConfig<Record<string, unknown>> | null {
  if (!process.env.OIDC_ISSUER || !process.env.OIDC_CLIENT_ID || !process.env.OIDC_CLIENT_SECRET) {
    return null;
  }
  return {
    id: "oidc",
    name: "Company SSO",
    type: "oidc",
    issuer: process.env.OIDC_ISSUER,
    clientId: process.env.OIDC_CLIENT_ID,
    clientSecret: process.env.OIDC_CLIENT_SECRET,
  };
}

export const { handlers, auth, signIn, signOut } = NextAuth({
  // JWT sessions: RBAC v1 is "any authenticated user is admin", so we don't
  // need a server-side session table for now. Revisit if immediate
  // server-side logout/revocation becomes a requirement.
  session: { strategy: "jwt" },
  pages: { signIn: "/login" },
  providers: [
    Credentials({
      credentials: {
        email: { label: "Email", type: "email" },
        password: { label: "Password", type: "password" },
      },
      async authorize(credentials) {
        const email = credentials?.email as string | undefined;
        const password = credentials?.password as string | undefined;
        if (!email || !password) return null;

        const user = await prisma.user.findUnique({ where: { email } });
        if (!user?.passwordHash) return null;

        const valid = await verify(user.passwordHash, password);
        if (!valid) return null;

        return { id: user.id, email: user.email };
      },
    }),
    ...(oidcProvider() ? [oidcProvider()!] : []),
  ],
  callbacks: {
    // Link an OIDC login to an existing (or newly created) User row by
    // email, so the same person always resolves to one account regardless
    // of which method they used to sign in.
    async signIn({ user, account }) {
      if (account?.provider !== "oidc" || !user.email) return true;

      const existing = await prisma.user.upsert({
        where: { email: user.email },
        update: {},
        create: { email: user.email },
      });
      await prisma.account.upsert({
        where: {
          provider_providerAccountId: {
            provider: account.provider,
            providerAccountId: account.providerAccountId,
          },
        },
        update: {},
        create: {
          userId: existing.id,
          provider: account.provider,
          providerAccountId: account.providerAccountId,
        },
      });
      user.id = existing.id;
      return true;
    },
    async jwt({ token, user }) {
      if (user?.id) token.userId = user.id;
      return token;
    },
    async session({ session, token }) {
      if (session.user) session.user.id = token.userId as string;
      return session;
    },
  },
});
