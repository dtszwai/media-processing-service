# Media Processing Service - Web UI

Web interface for the Media Processing Service built with Svelte 5, TypeScript, and Tailwind CSS.

## Features

- Image upload with drag & drop
- Direct upload for files < 5MB
- Presigned S3 URL upload for large files (up to 5GB) with progress tracking
- Image resize with width slider (100-1024px)
- Side-by-side original vs processed comparison
- Media history with status badges
- Delete with confirmation

## Development

```bash
# Install dependencies
pnpm install

# Start dev server (port 3000)
pnpm dev

# Type check
pnpm check

# Build for production
pnpm build
```

Requires the API server running at `http://localhost:9000`.

## Testing

```bash
# Unit tests (Vitest)
pnpm test              # Run once
pnpm test:watch        # Watch mode

# E2E tests (Playwright)
pnpm test:e2e          # Run headless
pnpm test:e2e:ui       # Interactive UI mode
```

**Test structure:**
- `src/lib/*.test.ts` - Unit tests for utilities and stores
- `e2e/*.spec.ts` - E2E tests for user flows

## Project Structure

```
src/
├── components/       # Svelte components
│   ├── Header.svelte
│   ├── MediaList.svelte
│   ├── ResultSection.svelte
│   └── UploadZone.svelte
├── lib/              # Shared utilities
│   ├── api.ts        # API client
│   ├── stores.ts     # Svelte stores
│   ├── types.ts      # TypeScript types
│   └── utils.ts      # Formatting helpers
├── App.svelte        # Root component
├── app.css           # Global styles
└── main.ts           # Entry point
```

## Code Style & Patterns

### Architecture

- **Feature-based components** - Each component handles one feature (upload, results, list)
- **Centralized state** - Svelte stores in `lib/stores.ts` for shared state
- **Separated API layer** - All fetch calls in `lib/api.ts`, components don't call fetch directly

### Svelte 5

- Use `$state()` for local reactive state
- Use `$derived()` for computed values
- Use `$effect()` for side effects
- Use `$props()` for component props with TypeScript interfaces

### TypeScript

- Define types in `lib/types.ts`
- Use explicit return types on exported functions
- Prefer `interface` over `type` for object shapes

### CSS

- Tailwind for utility classes in components
- Custom CSS in `app.css` for reusable patterns (`.btn-primary`, `.status-badge`, etc.)
- CSS variables in `:root` for theme colors

### Code Guidelines

- Keep functions small and focused
- Extract shared logic into helper functions (e.g., `createMediaEntry()`)
- Handle errors at the call site with try/catch
- Use early returns to reduce nesting

## Tech Stack

- [Svelte 5](https://svelte.dev/) - UI framework with runes
- [TypeScript](https://www.typescriptlang.org/) - Type safety
- [Tailwind CSS v4](https://tailwindcss.com/) - Styling
- [Vite](https://vite.dev/) - Build tool
- [Vitest](https://vitest.dev/) - Unit testing
- [Playwright](https://playwright.dev/) - E2E testing
