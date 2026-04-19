import "./global.css";
import { RootProvider } from "fumadocs-ui/provider";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import type { ReactNode } from "react";
import type { Metadata } from "next";
import { baseOptions } from "@/app/layout.config";
import { source } from "@/lib/source";

export const metadata: Metadata = {
  title: {
    template: "%s | Aurion Docs",
    default: "Aurion Docs",
  },
  description:
    "Documentation for Aurion — the open-source managed agents platform.",
};

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body>
        <RootProvider>
          <DocsLayout tree={source.pageTree} {...baseOptions}>
            {children}
          </DocsLayout>
        </RootProvider>
      </body>
    </html>
  );
}
