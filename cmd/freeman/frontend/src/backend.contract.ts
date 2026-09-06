// Compile-time proof that the two $backend implementations agree.
//
// vite.config.ts picks one of them per build — backend.wails.ts for the
// desktop app, backend.http.ts for the container server — and App.svelte
// is written against whichever it gets. tsconfig.json can only resolve
// $backend to one of them (the Wails variant), so type-checking App.svelte
// proves nothing about the other: adding an export to one and forgetting
// the other used to fail at runtime, in whichever build you didn't test,
// and only when that code path ran.
//
// The two assertions below are the enforcement. Nothing imports this
// module, so it is never bundled and adds no runtime code; it exists to
// make `npm run check` fail. Both directions are asserted, because the
// contract is that the modules have the *same* shape, not that one is a
// superset — an export on the HTTP side that Wails lacks is drift too.
import type * as HttpBackend from './backend.http'
import type * as WailsBackend from './backend.wails'

/**
 * Fails to compile unless Impl is assignable to Contract. The unusual
 * shape is deliberate: putting the requirement in a generic constraint
 * makes TypeScript report the missing or mismatched export by name.
 */
type AssertImplements<Contract, Impl extends Contract> = Impl

// The Wails module is the natural side to call the contract: its exports
// are a straight re-export of bindings generated from the Go methods, so
// it can't drift from the backend on its own.
type Backend = typeof WailsBackend

export type HttpImplementsBackend = AssertImplements<Backend, typeof HttpBackend>
export type BackendImplementsHttp = AssertImplements<typeof HttpBackend, Backend>
