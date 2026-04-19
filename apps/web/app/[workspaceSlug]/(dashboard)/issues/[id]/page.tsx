"use client";

import { use } from "react";
import { IssueDetail } from "@aurion/views/issues/components";

export default function IssueDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <IssueDetail issueId={id} />;
}
