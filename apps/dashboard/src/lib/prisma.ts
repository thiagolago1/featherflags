import { PrismaClient } from "@prisma/client";

// Avoid exhausting Postgres connections from Next.js hot-reload creating a
// new client per edit in dev.
const globalForPrisma = globalThis as unknown as { prisma?: PrismaClient };

export const prisma = globalForPrisma.prisma ?? new PrismaClient();

if (process.env.NODE_ENV !== "production") {
  globalForPrisma.prisma = prisma;
}
