// Smart Invest Solutions - Prisma Configuration
// Uses MONGODB_URI from .env (same as Go backend)
import "dotenv/config";
import { defineConfig } from "prisma/config";

export default defineConfig({
  schema: "prisma/schema.prisma",
  datasource: {
    url: process.env["DATABASE_URL"],
  },
});
