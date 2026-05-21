import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    {
      type: 'category',
      label: 'Reference',
      collapsed: false,
      items: ['commands', 'output-contract'],
    },
    {
      type: 'category',
      label: 'Design notes',
      collapsed: false,
      items: ['discovery-enrichment', 'roadmap'],
    },
  ],
};

export default sidebars;
