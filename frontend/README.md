# HyperCDR Community frontend

The Community control-plane UI is a React, TypeScript, Tailwind, and Vite
application. It also exports the public `@hypercdr/community-frontend` extension
surface consumed by HyperCDR Enterprise.

- `src/app/` defines application and extension composition.
- `src/components/` contains shared controls, including the common table system.
- `src/features/` contains product feature pages.
- `src/public.ts` is the supported Enterprise-facing package entry point.

Use `../scripts/build-frontend.sh` or `make build-frontend`. Dependencies,
build output, test reports, and browser evidence are generated under the sibling
`hypercdr-runtime` directory.
