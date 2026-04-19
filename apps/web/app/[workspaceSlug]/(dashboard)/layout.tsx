"use client";

import { DashboardLayout } from "@aurion/views/layout";
import { AurionIcon } from "@aurion/ui/components/common/aurion-icon";
import { SearchCommand, SearchTrigger } from "@aurion/views/search";
import { ChatFab, ChatWindow } from "@aurion/views/chat";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<AurionIcon className="size-6" />}
      searchSlot={<SearchTrigger />}
      extra={<><SearchCommand /><ChatWindow /><ChatFab /></>}
    >
      {children}
    </DashboardLayout>
  );
}
