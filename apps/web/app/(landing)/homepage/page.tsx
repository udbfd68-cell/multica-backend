import type { Metadata } from "next";
import { AurionLanding } from "@/features/landing/components/aurion-landing";

export const metadata: Metadata = {
  title: "Homepage",
  description:
    "Aurion — open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  openGraph: {
    title: "Aurion — Project Management for Human + Agent Teams",
    description:
      "Manage your human + agent workforce in one place.",
    url: "/homepage",
  },
  alternates: {
    canonical: "/homepage",
  },
};

export default function HomepagePage() {
  return <AurionLanding />;
}
