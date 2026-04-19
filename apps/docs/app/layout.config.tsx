import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";

export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="font-semibold text-base">Aurion Docs</span>
    ),
  },
  links: [
    {
      text: "GitHub",
      url: "https://github.com/aurion-ai/aurion",
    },
    {
      text: "Cloud",
      url: "https://aurion.studio",
    },
  ],
};
