# Media Processing Service - Web UI

Web interface for the Media Processing Service built with Svelte 5, TypeScript, and Tailwind CSS.

## Features

- Image upload with drag & drop
- Direct upload for files up to 50MB
- Presigned S3 URL upload for large files (up to 1GB) with progress tracking
- Image resize with width slider (100-1024px)
- Side-by-side original vs processed comparison
- Media history with status badges
- Analytics dashboard with view counts and format usage
- Delete with confirmation

## Routes

| Path         | Page          | Description                                           |
| ------------ | ------------- | ----------------------------------------------------- |
| `/`          | MediaPage     | Upload images, view processing results, media history |
| `/analytics` | AnalyticsPage | View counts, top media, format usage statistics       |

## Development

```bash
# Install dependencies
pnpm install

# Start dev server (port 3001)
pnpm dev

# Type check
pnpm check

# Build for production
pnpm build
```

### Environment Variables

Configure the API backend URL via environment variable:

```bash
# .env.local (optional, defaults to localhost:9000)
VITE_API_URL=http://localhost:9000
```

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

- `src/**/*.test.ts` - Unit tests for services, utils, and stores
- `e2e/*.spec.ts` - E2E tests for user flows

## Project Structure

The codebase follows a **Domain-Driven Design (DDD)** feature-based architecture:

```
src/
├── features/                 # Feature modules (bounded contexts)
│   ├── media/                # Media upload & management feature
│   │   ├── components/       # UploadZone, ResultSection, MediaList
│   │   ├── pages/            # MediaPage
│   │   ├── services/         # media.service.ts (API calls)
│   │   ├── queries/          # media.queries.ts (TanStack Query)
│   │   ├── stores/           # currentMediaId, isProcessing
│   │   └── index.ts          # Barrel export
│   │
│   └── analytics/            # Analytics dashboard feature
│       ├── components/       # Dashboard, Charts, Tables, Modal
│       ├── pages/            # AnalyticsPage
│       ├── services/         # analytics.service.ts
│       ├── queries/          # analytics.queries.ts
│       └── index.ts          # Barrel export
│
├── shared/                   # Cross-cutting concerns
│   ├── components/           # Header (shared UI)
│   ├── config/               # Environment variables (env.ts)
│   ├── http/                 # HTTP client utilities
│   ├── queries/              # Query client, keys, health queries
│   ├── types/                # TypeScript types, Zod schemas, errors
│   ├── utils/                # Formatting, image utilities
│   └── index.ts              # Barrel export
│
├── App.svelte                # Root component with routing
├── app.css                   # Global styles
└── main.ts                   # Entry point
```

### Feature Module Structure

Each feature module is self-contained with its own:

| Directory     | Purpose                                |
| ------------- | -------------------------------------- |
| `components/` | UI components specific to the feature  |
| `pages/`      | Page-level components (route targets)  |
| `services/`   | API calls (fetch logic)                |
| `queries/`    | TanStack Query hooks for data fetching |
| `stores/`     | Svelte stores for feature state        |
| `index.ts`    | Barrel export for clean imports        |

## Code Style & Patterns

### Architecture

- **DDD feature modules** - Each feature (media, analytics) is a self-contained bounded context
- **Layered architecture** - Components → Queries → Services → API
- **Shared infrastructure** - Cross-cutting concerns in `shared/` (types, utils, http, queries)
- **Custom routing** - Simple path-based routing in App.svelte (no external router)
- **Centralized state** - Svelte stores for shared state within features
- **Separated API layer** - All fetch calls in services, components use queries
- **Zod validation** - API responses validated with Zod schemas
- **Barrel exports** - Clean imports via index.ts files

### Svelte 5

- Use `$state()` for local reactive state
- Use `$derived()` for computed values
- Use `$effect()` for side effects
- Use `$props()` for component props with TypeScript interfaces

### TypeScript

- Define types in `shared/types/` directory
- Use Zod schemas for API response types (type-safe validation)
- Use explicit return types on exported functions
- Prefer `interface` over `type` for object shapes
- Import from barrel exports: `import { type Media } from "../shared"`

### CSS

- Tailwind for utility classes in components
- Custom CSS in `app.css` for reusable patterns (`.btn-primary`, `.status-badge`, etc.)
- CSS variables in `:root` for theme colors

### Code Guidelines

- Keep functions small and focused
- Extract shared logic into helper functions
- Handle errors at the call site with try/catch
- Use early returns to reduce nesting

## Tech Stack

- [Svelte 5](https://svelte.dev/) - UI framework with runes
- [TanStack Query](https://tanstack.com/query) - Data fetching and caching
- [TypeScript](https://www.typescriptlang.org/) - Type safety
- [Zod](https://zod.dev/) - Runtime validation
- [Tailwind CSS v4](https://tailwindcss.com/) - Styling
- [Vite](https://vite.dev/) - Build tool
- [Vitest](https://vitest.dev/) - Unit testing
- [Playwright](https://playwright.dev/) - E2E testing
