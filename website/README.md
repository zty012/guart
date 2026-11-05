# GUART website

Astro static site with a Vue interactive island and Tailwind CSS (Vite plugin).
The demos run entirely in the browser; they never request serial-port access.

## Development

```sh
pnpm install
pnpm dev
```

## Check and build

```sh
pnpm check
pnpm build
pnpm preview
```

## Cloudflare Workers Static Assets

Run commands from `website/`:

```sh
pnpm build
pnpm exec wrangler deploy
```

Or use `pnpm deploy`. Authenticate Wrangler with your own Cloudflare account
before deploying. `wrangler.toml` serves `dist/` directly without a Worker
script, SSR, or the Astro Cloudflare adapter. Unknown paths use Astro's generated
`404.html`, not an SPA fallback. No account ID, token, or domain is checked in.
For Cloudflare's Git integration, set the root to `website`, the build command
to `pnpm build`, and the deploy command to `pnpm exec wrangler deploy`.

Download links use the project's GitHub latest-release page rather than a
hard-coded version or an assumed platform artifact.

## Application screenshots

The images in `public/images` are real GUART UI renders produced by the existing
MyGo Tester fixture, using simulated MCU receive data (not hardware capture).
They are stored as WebP; the fixture still renders PNG, so regenerate and
re-encode from the repository root in PowerShell (needs ImageMagick's `magick`,
or libwebp's `cwebp`):

```powershell
$env:CGO_ENABLED = '0'
$env:GUART_SCREENSHOTS = "$PWD/artifacts/website"
go test -run '^TestRenderMCUScreenshots$' .
magick artifacts/website/guart-mcu-light.png -quality 88 website/public/images/guart-light.webp
magick artifacts/website/guart-mcu-dark.png -quality 88 website/public/images/guart-dark.webp
```
