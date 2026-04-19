"use client";

import { useRouter, useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@aurion/core/auth";
import { paths } from "@aurion/core/paths";
import { workspaceListOptions } from "@aurion/core/workspace/queries";
import { InvitePage } from "@aurion/views/invite";

export default function InviteAcceptPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  // Wait for auth to resolve — auto-token will kick in if configured.

  if (isLoading || !user) return null;

  const onBack =
    wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  return <InvitePage invitationId={params.id} onBack={onBack} />;
}
