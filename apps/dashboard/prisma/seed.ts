// Bootstraps the first admin user. There is no public signup route on
// purpose (RBAC v1 = "any logged-in user is admin", so open signup would
// mean open admin access) — run this once per environment, then invite
// further users the same way or wire up SSO.
//
// Usage: ADMIN_EMAIL=you@company.com ADMIN_PASSWORD=... npm run db:seed
import { hash } from "@node-rs/argon2";
import { PrismaClient } from "@prisma/client";

const prisma = new PrismaClient();

async function main() {
  const email = process.env.ADMIN_EMAIL;
  const password = process.env.ADMIN_PASSWORD;
  if (!email || !password) {
    throw new Error("ADMIN_EMAIL and ADMIN_PASSWORD env vars are required");
  }
  if (password.length < 12) {
    throw new Error("ADMIN_PASSWORD must be at least 12 characters");
  }

  const passwordHash = await hash(password);
  const user = await prisma.user.upsert({
    where: { email },
    update: { passwordHash },
    create: { email, passwordHash },
  });
  console.log(`Seeded admin user ${user.email} (${user.id})`);
}

main()
  .catch((err) => {
    console.error(err);
    process.exit(1);
  })
  .finally(() => prisma.$disconnect());
