---
name: shadcn-ui
description: This skill should be used when adding, customizing, or theming a UI component under web/ (e.g. "add a dialog", "add a shadcn button", "set up a form", "change the color palette", "add dark mode"), or when asked how shadcn/ui works in this project. Documents the official shadcn/ui CLI and conventions only — no third-party registries or paid services.
---

# shadcn/ui

shadcn/ui is not a component library — the CLI copies component *source code* directly into the project (`web/components/ui/`), built on Radix UI primitives and Tailwind CSS. There's no npm package to update; installed components are owned code, editable like any other file in the repo.

## This project's setup

`web/components.json` is already initialized:

```json
{
  "style": "radix-nova",
  "tailwind": { "css": "app/globals.css", "baseColor": "neutral", "cssVariables": true },
  "iconLibrary": "lucide",
  "aliases": { "components": "@/components", "utils": "@/lib/utils", "ui": "@/components/ui", "lib": "@/lib", "hooks": "@/hooks" },
  "registries": {}
}
```

No custom registries are configured — every component comes from the official public shadcn/ui registry, free, no API key, no account. `npx shadcn@latest init` has already been run; don't re-run it unless deliberately resetting the config.

## Adding a component

```bash
cd web
npx shadcn@latest add button dialog form    # space-separated, install several at once
```

This writes source files into `components/ui/` per the aliases above, and installs any Radix UI packages the component depends on. Per the [`web-conventions`](../web-conventions/SKILL.md) skill, new primitives always come in this way — never hand-write a `components/ui/*` file from scratch.

## Other CLI commands

```bash
npx shadcn@latest add <component> --overwrite   # re-pull a component, overwriting local edits
npx shadcn@latest diff <component>               # show upstream changes vs. the installed (possibly edited) version
npx shadcn@latest view <component>               # preview a component's source before installing
```

`diff` is the update mechanism — since component code is copied in, not versioned as a dependency, there's no `npm update` equivalent. Use it to check whether an installed component has drifted from upstream before deciding whether to re-pull it.

## Theming

Color tokens are CSS variables defined in `app/globals.css` under `:root` (light mode) and `.dark` (dark mode overrides), in OKLCH color space — this project has `cssVariables: true` and `baseColor: "neutral"`. Tokens are semantic background/foreground pairs (`background`/`foreground`, `card`/`card-foreground`, `primary`/`primary-foreground`, plus `secondary`, `muted`, `accent`, `destructive`, `border`, `input`, `ring`). Change the palette by editing the variable values in `globals.css`, not by overriding component classes — components already consume these tokens by default.

## Finding a component

Full component list and live previews: [ui.shadcn.com/docs/components](https://ui.shadcn.com/docs/components). Common ones: `accordion`, `alert`, `alert-dialog`, `avatar`, `badge`, `button`, `calendar`, `card`, `checkbox`, `combobox`, `command`, `data-table`, `dialog`, `dropdown-menu`, `form`, `input`, `label`, `popover`, `select`, `separator`, `sheet`, `skeleton`, `sonner` (toasts), `table`, `tabs`, `textarea`, `tooltip`.
